package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"ticketbooking/internal/audit"
	"ticketbooking/internal/domain"
	"ticketbooking/internal/repository"
)

const (
	activeRefundIndex   = "uq_refunds_one_active_per_payment"
	maxRefundNoteLen    = 1000
	refundProcessingRef = "processing"
)

var (
	refundReasonPattern = regexp.MustCompile(`^[A-Z_]{1,64}$`)
	errRefundInProgress = errors.New("refund processing in progress")
)

// RefundProvider is the mock refund provider. It returns the provider reference on success.
// Tests replace it to simulate provider success or failure.
var RefundProvider = func(ctx context.Context, refundID string, amountSatang int64) (string, error) {
	return "mock_refund_" + refundID, nil
}

type RefundView struct {
	ID           string     `json:"id"`
	PaymentID    string     `json:"payment_id"`
	BookingID    string     `json:"booking_id"`
	AmountSatang int64      `json:"amount_satang"`
	Status       string     `json:"status"`
	ReasonCode   string     `json:"reason_code"`
	Note         string     `json:"note"`
	ProviderRef  string     `json:"provider_ref,omitempty"`
	FailureCode  string     `json:"failure_code,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}

func refundView(r *repository.RefundRow) RefundView {
	v := RefundView{ID: r.ID, PaymentID: r.PaymentID, BookingID: r.BookingID, AmountSatang: r.AmountSatang,
		Status: r.Status, ReasonCode: r.ReasonCode, Note: r.Note, ProviderRef: r.ProviderRef,
		FailureCode: r.FailureCode, CreatedAt: r.CreatedAt.UTC()}
	if r.CompletedAt != nil {
		t := r.CompletedAt.UTC()
		v.CompletedAt = &t
	}
	return v
}

type RefundRequest struct {
	ReasonCode string `json:"reason_code"`
	Note       string `json:"note"`
}

type refundProcessSnapshot struct {
	refundID     string
	amountSatang int64
}

// RequestRefund opens a full refund for a payment (D1-D3). created is false when the
// Idempotency-Key was already used and the original refund is returned.
func (s *AdminService) RequestRefund(ctx context.Context, adminID, paymentID, key string, in RefundRequest) (view *RefundView, created bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()

	var knownPayment *repository.PaymentRow
	defer func() {
		if err == nil {
			return
		}
		reason := reasonOf(err)
		switch reason {
		case domain.ReasonIdempotencyRequired:
			reason = domain.ReasonValidationFailed
		case domain.ReasonNotFound:
			reason = domain.ReasonUnknownPayment
		}
		e := audit.PaymentEvent{EventType: domain.EvRefundRejected, Outcome: domain.OutcomeFailure, ReasonCode: reason,
			ActorType: domain.ActorAdmin, ActorID: &adminID}
		if knownPayment != nil {
			e.PaymentID, e.BookingID = &knownPayment.ID, &knownPayment.BookingID
			e.FromStatus, e.ToStatus = knownPayment.Status, knownPayment.Status
			amount := knownPayment.AmountSatang
			e.AmountSatang = &amount
		}
		s.recordPaymentFailure(ctx, e)
	}()

	key = strings.TrimSpace(key)
	in.ReasonCode = strings.TrimSpace(in.ReasonCode)
	in.Note = strings.TrimSpace(in.Note)
	if key == "" {
		return nil, false, domain.NewError(domain.ReasonIdempotencyRequired, "Idempotency-Key header is required")
	}
	if len(key) > maxIdempotencyKeyLen {
		return nil, false, validationErr("Idempotency-Key is too long")
	}
	paymentID, err = parseID(paymentID, "payment")
	if err != nil {
		return nil, false, err
	}

	var refund *repository.RefundRow
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		p, err := s.payments.LockPayment(ctx, tx, paymentID)
		if err != nil {
			return err
		}
		if p == nil {
			return notFound("payment")
		}
		knownPayment = p

		prevID, err := s.repo.RefundIDByIdempotencyKey(ctx, tx, key)
		if err != nil {
			return err
		}
		if prevID != "" {
			prev, err := s.repo.RefundByID(ctx, tx, prevID)
			if err != nil {
				return err
			}
			if prev == nil || prev.PaymentID != p.ID {
				e := validationErr("Idempotency-Key was already used for another refund")
				e.Status = http.StatusConflict
				return e
			}
			refund = prev
			return nil
		}

		if !refundReasonPattern.MatchString(in.ReasonCode) {
			return validationErr("reason_code must be 1-64 characters of A-Z and _")
		}
		if len(in.Note) > maxRefundNoteLen {
			return validationErr("note is too long")
		}
		if p.Status != string(domain.PaymentSucceeded) && p.Status != string(domain.PaymentNeedsRefund) {
			return domain.NewError(domain.ReasonRefundNotEligible, "only SUCCEEDED or NEEDS_REFUND payments can be refunded")
		}
		if p.AmountSatang <= 0 {
			return domain.NewError(domain.ReasonRefundNotEligible, "the payment has no amount to refund")
		}
		refunded, err := s.repo.CompletedRefundTotal(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if refunded+p.AmountSatang > p.AmountSatang {
			return domain.NewError(domain.ReasonAmountExceeds, "the refund would exceed the payment amount")
		}
		active, err := s.repo.HasActiveRefund(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if active {
			return domain.NewError(domain.ReasonRefundAlreadyExists, "this payment already has an active refund")
		}
		used, err := s.repo.HasUsedTicket(ctx, tx, p.BookingID)
		if err != nil {
			return err
		}
		if used {
			return domain.NewError(domain.ReasonTicketAlreadyUsed, "a ticket of this booking was already used")
		}

		refund, err = s.repo.InsertRefund(ctx, tx, repository.RefundRow{
			PaymentID: p.ID, BookingID: p.BookingID, AmountSatang: p.AmountSatang,
			ReasonCode: in.ReasonCode, Note: in.Note, RequestedBy: adminID,
		})
		if err != nil {
			if uniqueViolation(err) == activeRefundIndex {
				return domain.NewError(domain.ReasonRefundAlreadyExists, "this payment already has an active refund")
			}
			return err
		}
		created = true
		payload, err := json.Marshal(map[string]string{
			"refund_id": refund.ID, "idempotency_key": key, "refund_reason": in.ReasonCode,
		})
		if err != nil {
			return err
		}
		amount := refund.AmountSatang
		return audit.RecordPaymentEvent(ctx, tx, audit.PaymentEvent{
			PaymentID: &p.ID, BookingID: &p.BookingID, EventType: domain.EvRefundRequested, Outcome: domain.OutcomeSuccess,
			FromStatus: p.Status, ToStatus: p.Status, AmountSatang: &amount,
			ActorType: domain.ActorAdmin, ActorID: &adminID, RawPayload: payload,
		})
	})
	if err != nil {
		return nil, false, err
	}
	v := refundView(refund)
	return &v, created, nil
}

// ProcessRefund calls the refund provider outside any DB transaction, then records the outcome
// in a short follow-up transaction with row locks. Concurrent calls are idempotent.
func (s *AdminService) ProcessRefund(ctx context.Context, adminID, refundID string) (view *RefundView, err error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	refundID, err = parseID(refundID, "refund")
	if err != nil {
		return nil, err
	}

	var snap *refundProcessSnapshot
	var paymentID, bookingID *string
	deadline := time.Now().Add(adminOpTimeout)

	for {
		var terminal *repository.RefundRow
		var inProgress bool
		err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
			r, err := s.repo.LockRefund(ctx, tx, refundID)
			if err != nil {
				return err
			}
			if r == nil {
				return notFound("refund")
			}
			if r.Status != string(domain.RefundRequested) {
				terminal = r
				return nil
			}
			if r.ProviderRef != "" && r.ProviderRef != refundProcessingRef {
				return errors.New("refund has unexpected provider_ref while REQUESTED")
			}
			if r.ProviderRef == refundProcessingRef {
				inProgress = true
				return errRefundInProgress
			}
			paymentID, bookingID = &r.PaymentID, &r.BookingID
			p, err := s.payments.LockPayment(ctx, tx, r.PaymentID)
			if err != nil {
				return err
			}
			b, err := s.bookings.Lock(ctx, tx, r.BookingID)
			if err != nil {
				return err
			}
			if p == nil || b == nil {
				return errors.New("refund references a missing payment or booking")
			}
			claimed, err := s.repo.ClaimRefundProcessing(ctx, tx, r.ID, refundProcessingRef)
			if err != nil {
				return err
			}
			if !claimed {
				inProgress = true
				return errRefundInProgress
			}
			snap = &refundProcessSnapshot{refundID: r.ID, amountSatang: r.AmountSatang}
			return nil
		})
		if err == errRefundInProgress {
			if terminal != nil {
				v := refundView(terminal)
				return &v, nil
			}
			if inProgress && time.Now().After(deadline) {
				return nil, domain.NewError(domain.ReasonInternalError, "refund processing timed out waiting for completion")
			}
			time.Sleep(15 * time.Millisecond)
			continue
		}
		if err != nil {
			if paymentID != nil {
				s.recordPaymentFailure(ctx, audit.PaymentEvent{
					PaymentID: paymentID, BookingID: bookingID, EventType: domain.EvRefundFailed, Outcome: domain.OutcomeFailure,
					ReasonCode: domain.ReasonInternalError, ActorType: domain.ActorAdmin, ActorID: &adminID,
				})
			}
			return nil, err
		}
		if terminal != nil {
			v := refundView(terminal)
			return &v, nil
		}
		if snap != nil {
			break
		}
	}

	defer func() {
		if err != nil && snap != nil {
			_ = s.repo.ClearRefundProcessingClaim(ctx, s.repo.DB(), snap.refundID, refundProcessingRef)
		}
	}()

	providerRef, perr := RefundProvider(ctx, snap.refundID, snap.amountSatang)

	var out *repository.RefundRow
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		r, err := s.repo.LockRefund(ctx, tx, refundID)
		if err != nil {
			return err
		}
		if r == nil {
			return notFound("refund")
		}
		out = r
		if r.Status != string(domain.RefundRequested) {
			return nil
		}
		p, err := s.payments.LockPayment(ctx, tx, r.PaymentID)
		if err != nil {
			return err
		}
		b, err := s.bookings.Lock(ctx, tx, r.BookingID)
		if err != nil {
			return err
		}
		if p == nil || b == nil {
			return errors.New("refund references a missing payment or booking")
		}
		paymentID, bookingID = &p.ID, &b.ID
		amount := r.AmountSatang

		if perr != nil {
			out, err = s.repo.FailRefund(ctx, tx, r.ID, string(domain.ReasonProviderFailed), adminID)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(map[string]string{"refund_id": r.ID})
			if err != nil {
				return err
			}
			return audit.RecordPaymentEvent(ctx, tx, audit.PaymentEvent{
				PaymentID: &p.ID, BookingID: &b.ID, EventType: domain.EvRefundFailed, Outcome: domain.OutcomeFailure,
				ReasonCode: domain.ReasonProviderFailed, FromStatus: p.Status, ToStatus: p.Status, AmountSatang: &amount,
				ActorType: domain.ActorAdmin, ActorID: &adminID, RawPayload: payload,
			})
		}

		if out, err = s.repo.CompleteRefund(ctx, tx, r.ID, providerRef, adminID); err != nil {
			return err
		}
		if err := s.repo.SetPaymentStatus(ctx, tx, p.ID, string(domain.PaymentRefunded)); err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]string{"refund_id": r.ID})
		if err != nil {
			return err
		}
		if err := audit.RecordPaymentEvent(ctx, tx, audit.PaymentEvent{
			PaymentID: &p.ID, BookingID: &b.ID, EventType: domain.EvRefundCompleted, Outcome: domain.OutcomeSuccess,
			FromStatus: p.Status, ToStatus: string(domain.PaymentRefunded), AmountSatang: &amount, ProviderRef: providerRef,
			ActorType: domain.ActorAdmin, ActorID: &adminID, RawPayload: payload,
		}); err != nil {
			return err
		}

		bookingTo := b.Status
		if b.Status == string(domain.BookingPaid) {
			bookingTo = string(domain.BookingRefunded)
			if err := s.bookings.SetStatus(ctx, tx, b.ID, bookingTo); err != nil {
				return err
			}
		}
		voided, err := s.repo.VoidTickets(ctx, tx, b.ID)
		if err != nil {
			return err
		}
		started, err := s.repo.ShowtimeStarted(ctx, tx, b.ShowtimeID)
		if err != nil {
			return err
		}
		if !started {
			if err := s.bookings.DeactivateItems(ctx, tx, b.ID); err != nil {
				return err
			}
		}
		return audit.RecordBookingEvent(ctx, tx, audit.BookingEvent{
			BookingID: &b.ID, UserID: &b.UserID, ShowtimeID: &b.ShowtimeID,
			EventType: domain.EvBookingRefunded, Outcome: domain.OutcomeSuccess,
			FromStatus: b.Status, ToStatus: bookingTo, ActorType: domain.ActorAdmin, ActorID: &adminID,
			Metadata: map[string]any{"refund_id": r.ID, "payment_id": p.ID, "amount_satang": amount,
				"tickets_voided": voided, "seats_released": !started},
		})
	})
	if err != nil {
		if paymentID != nil {
			s.recordPaymentFailure(ctx, audit.PaymentEvent{
				PaymentID: paymentID, BookingID: bookingID, EventType: domain.EvRefundFailed, Outcome: domain.OutcomeFailure,
				ReasonCode: domain.ReasonInternalError, ActorType: domain.ActorAdmin, ActorID: &adminID,
			})
		}
		return nil, err
	}
	v := refundView(out)
	return &v, nil
}

func (s *AdminService) ListRefunds(ctx context.Context, status string, page int) (*Page[RefundView], error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	if !validStatus(status, refundStatuses...) {
		return nil, validationErr("unknown refund status")
	}
	rows, total, err := s.repo.ListRefunds(ctx, status, AdminPageSize, pageOffset(page))
	if err != nil {
		return nil, err
	}
	out := &Page[RefundView]{Items: make([]RefundView, 0, len(rows)), Page: max(page, 1), Limit: AdminPageSize, Total: total}
	for i := range rows {
		out.Items = append(out.Items, refundView(&rows[i]))
	}
	return out, nil
}
