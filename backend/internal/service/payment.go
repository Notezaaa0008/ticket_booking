package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"ticketbooking/internal/audit"
	"ticketbooking/internal/domain"
	"ticketbooking/internal/repository"
)

const (
	paymentOpTimeout       = 10 * time.Second
	maxIdempotencyKeyLen   = 200
	pendingPaymentIndex    = "uq_payments_one_pending_per_booking"
	idempotencyKeyIndex    = "payments_idempotency_key_key"
	webhookStatusSucceeded = "succeeded"
	webhookStatusFailed    = "failed"
	maxRawTextInAudit      = 2048
)

var failureMessages = map[domain.ReasonCode]string{
	domain.ReasonGatewayDeclined: "The payment was declined by the gateway.",
	domain.ReasonAmountMismatch:  "The paid amount did not match the booking total. The payment will be refunded.",
	domain.ReasonBookingExpired:  "The booking expired before the payment arrived. The payment will be refunded.",
	domain.ReasonSeatLost:        "The seats were no longer reserved when the payment arrived. The payment will be refunded.",
}

type PaymentService struct {
	repo     *repository.PaymentRepository
	bookings *repository.BookingRepository
	holds    *BookingService // releases Redis hold keys after a successful payment
	secret   []byte
}

func NewPaymentService(repo *repository.PaymentRepository, bookings *repository.BookingRepository, holds *BookingService, webhookSecret string) *PaymentService {
	return &PaymentService{repo: repo, bookings: bookings, holds: holds, secret: []byte(webhookSecret)}
}

type PaymentView struct {
	ID             string     `json:"id"`
	BookingID      string     `json:"booking_id"`
	Status         string     `json:"status"`
	AmountSatang   int64      `json:"amount_satang"`
	FailureCode    string     `json:"failure_code,omitempty"`
	FailureMessage string     `json:"failure_message,omitempty"`
	PaidAt         *time.Time `json:"paid_at"`
	CreatedAt      time.Time  `json:"created_at"`
	PayURL         string     `json:"pay_url"`
}

type TicketView struct {
	Code       string    `json:"code"`
	Status     string    `json:"status"`
	BookingID  string    `json:"booking_id"`
	EventTitle string    `json:"event_title"`
	Venue      string    `json:"venue"`
	StartsAt   time.Time `json:"starts_at"`
	SeatLabel  string    `json:"seat_label"`
	Zone       string    `json:"zone"`
	IssuedAt   time.Time `json:"issued_at"`
}

func paymentView(p *repository.PaymentRow) *PaymentView {
	v := &PaymentView{
		ID: p.ID, BookingID: p.BookingID, Status: p.Status, AmountSatang: p.AmountSatang,
		FailureCode: p.FailureCode, FailureMessage: p.FailureMessage, CreatedAt: p.CreatedAt.UTC(),
		PayURL: "/pay/" + p.ID,
	}
	if p.PaidAt != nil {
		t := p.PaidAt.UTC()
		v.PaidAt = &t
	}
	return v
}

func ticketViews(rows []repository.TicketRow) []TicketView {
	out := make([]TicketView, 0, len(rows))
	for _, r := range rows {
		out = append(out, TicketView{
			Code: r.Code, Status: r.Status, BookingID: r.BookingID, EventTitle: r.EventTitle, Venue: r.Venue,
			StartsAt: r.StartsAt.UTC(), SeatLabel: r.RowLabel + strconv.Itoa(r.SeatNumber), Zone: r.Zone,
			IssuedAt: r.IssuedAt.UTC(),
		})
	}
	return out
}

// ---------- create ----------

// Create starts a payment for the caller's PENDING booking. created is false when the
// Idempotency-Key was already used for this booking and the existing payment is returned.
func (s *PaymentService) Create(ctx context.Context, userID, bookingID, key string) (view *PaymentView, created bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, paymentOpTimeout)
	defer cancel()

	var knownBooking *string // set once the booking is known to exist and belong to the caller
	key = strings.TrimSpace(key)
	defer func() {
		if err != nil {
			s.recordCreateFailure(ctx, userID, knownBooking, err)
		}
	}()
	if key == "" {
		return nil, false, domain.NewError(domain.ReasonIdempotencyRequired, "Idempotency-Key header is required")
	}
	if len(key) > maxIdempotencyKeyLen {
		return nil, false, domain.NewError(domain.ReasonValidationFailed, "Idempotency-Key is too long")
	}
	u, perr := uuid.Parse(bookingID)
	if perr != nil {
		return nil, false, domain.NewError(domain.ReasonBookingNotFound, "booking not found")
	}
	bookingID = u.String()

	var p *repository.PaymentRow
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		b, err := s.bookings.Lock(ctx, tx, bookingID)
		if err != nil {
			return err
		}
		if b == nil || b.UserID != userID {
			return domain.NewError(domain.ReasonBookingNotFound, "booking not found")
		}
		knownBooking = &bookingID

		existing, err := s.repo.ByIdempotencyKey(ctx, tx, key)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.BookingID != bookingID {
				return keyReusedError()
			}
			p = existing
			return nil
		}

		if b.Status == string(domain.BookingExpired) {
			return domain.NewError(domain.ReasonBookingExpired, "the booking has expired")
		}
		if b.Status != string(domain.BookingPending) {
			return domain.NewError(domain.ReasonBookingNotPayable, "only a pending booking can be paid")
		}
		expired, err := s.repo.ExpiredByDB(ctx, tx, bookingID)
		if err != nil {
			return err
		}
		if expired {
			return domain.NewError(domain.ReasonBookingExpired, "the booking has expired")
		}
		stats, err := s.repo.ItemStats(ctx, tx, bookingID)
		if err != nil {
			return err
		}
		if stats.Count == 0 || stats.ActiveCount != stats.Count {
			return domain.NewError(domain.ReasonBookingNotPayable, "the booking no longer holds its seats")
		}
		pending, err := s.repo.HasPending(ctx, tx, bookingID)
		if err != nil {
			return err
		}
		if pending {
			return pendingPaymentError()
		}

		p, err = s.repo.InsertPayment(ctx, tx, bookingID, stats.Total, key)
		if err != nil {
			switch uniqueViolation(err) {
			case pendingPaymentIndex:
				return pendingPaymentError()
			case idempotencyKeyIndex:
				return keyReusedError()
			}
			return err
		}
		created = true
		amount := p.AmountSatang
		return audit.RecordPaymentEvent(ctx, tx, audit.PaymentEvent{
			PaymentID: &p.ID, BookingID: &bookingID,
			EventType: domain.EvPaymentCreated, Outcome: domain.OutcomeSuccess,
			ToStatus: string(domain.PaymentPending), AmountSatang: &amount,
			ActorType: domain.ActorUser, ActorID: &userID,
		})
	})
	if err != nil {
		return nil, false, err
	}
	return paymentView(p), created, nil
}

func keyReusedError() *domain.AppError {
	e := domain.NewError(domain.ReasonValidationFailed, "Idempotency-Key was already used for another booking")
	e.Status = http.StatusConflict
	return e
}

func pendingPaymentError() *domain.AppError {
	return domain.NewError(domain.ReasonBookingNotPayable, "a payment for this booking is already in progress")
}

func uniqueViolation(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}

func (s *PaymentService) recordCreateFailure(ctx context.Context, userID string, bookingID *string, err error) {
	reason := reasonOf(err)
	if reason == domain.ReasonIdempotencyRequired {
		reason = domain.ReasonValidationFailed // API-only code; the audit log uses the generic reason
	}
	s.recordPaymentFailure(ctx, audit.PaymentEvent{
		BookingID: bookingID, EventType: domain.EvPaymentCreateFailed, Outcome: domain.OutcomeFailure,
		ReasonCode: reason, ActorType: domain.ActorUser, ActorID: &userID,
	})
}

// recordPaymentFailure writes an event with the plain connection (outside any rolled-back
// transaction). If that fails it logs to stderr; the caller keeps its original result.
func (s *PaymentService) recordPaymentFailure(ctx context.Context, e audit.PaymentEvent) {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditOpTimeout)
	defer cancel()
	if err := audit.RecordPaymentEvent(actx, s.repo.DB(), e); err != nil {
		fmt.Fprintf(os.Stderr, "request_id=%v audit write failed (event=%s reason=%s): %v\n",
			ctx.Value(RequestIDKey), e.EventType, e.ReasonCode, err)
	}
}

// ---------- webhook ----------

type WebhookPayload struct {
	EventID      string `json:"event_id"`
	PaymentID    string `json:"payment_id"`
	Status       string `json:"status"`
	AmountSatang *int64 `json:"amount_satang"`
	ProviderRef  string `json:"provider_ref"`
	FailureCode  string `json:"failure_code"`
}

// WebhookResult is the outcome of a processed (or deliberately ignored) webhook.
type WebhookResult struct {
	Result        string `json:"result"` // "processed" | "ignored"
	PaymentStatus string `json:"payment_status"`
}

// Sign returns the hex HMAC-SHA256 of body: the value expected in the X-Signature header.
func Sign(secret, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func (s *PaymentService) validSignature(raw []byte, sigHex string) bool {
	got, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil || len(got) == 0 {
		return false
	}
	m := hmac.New(sha256.New, s.secret)
	m.Write(raw)
	return hmac.Equal(got, m.Sum(nil))
}

// auditPayload keeps a valid JSON body as is; anything else is stored as truncated text.
func auditPayload(raw []byte) json.RawMessage {
	if json.Valid(raw) {
		return raw
	}
	txt := string(raw)
	if len(txt) > maxRawTextInAudit {
		txt = txt[:maxRawTextInAudit]
	}
	j, err := json.Marshal(map[string]string{"raw_text": txt})
	if err != nil {
		return nil // audit stores {} instead
	}
	return j
}

// HandleWebhook verifies and applies one gateway notification. It returns an *domain.AppError for
// rejected requests (401/400/404) and a plain error for internal failures (500).
func (s *PaymentService) HandleWebhook(ctx context.Context, raw []byte, sigHex string) (*WebhookResult, error) {
	ctx, cancel := context.WithTimeout(ctx, paymentOpTimeout)
	defer cancel()
	valid, invalid := true, false

	if !s.validSignature(raw, sigHex) {
		s.recordPaymentFailure(ctx, audit.PaymentEvent{
			EventType: domain.EvWebhookRejected, Outcome: domain.OutcomeFailure, ReasonCode: domain.ReasonInvalidSignature,
			SignatureValid: &invalid, ActorType: domain.ActorGateway, RawPayload: auditPayload(raw),
		})
		return nil, domain.NewError(domain.ReasonInvalidSignature, "invalid signature")
	}

	var in WebhookPayload
	if err := json.Unmarshal(raw, &in); err != nil || in.PaymentID == "" ||
		(in.Status != webhookStatusSucceeded && in.Status != webhookStatusFailed) ||
		(in.Status == webhookStatusSucceeded && in.AmountSatang == nil) {
		s.recordPaymentFailure(ctx, audit.PaymentEvent{
			EventType: domain.EvWebhookRejected, Outcome: domain.OutcomeFailure, ReasonCode: domain.ReasonMalformedPayload,
			SignatureValid: &valid, ActorType: domain.ActorGateway, RawPayload: auditPayload(raw),
		})
		return nil, domain.NewError(domain.ReasonMalformedPayload, "malformed payload")
	}

	var known *repository.PaymentRow
	if u, err := uuid.Parse(in.PaymentID); err == nil {
		if known, err = s.repo.ByID(ctx, u.String()); err != nil {
			return nil, err
		}
	}
	base := audit.PaymentEvent{SignatureValid: &valid, ActorType: domain.ActorGateway, RawPayload: raw, AmountSatang: in.AmountSatang, ProviderRef: in.ProviderRef}
	if known != nil {
		base.PaymentID, base.BookingID = &known.ID, &known.BookingID
	}

	received := base
	received.EventType, received.Outcome = domain.EvWebhookReceived, domain.OutcomeSuccess
	if err := audit.RecordPaymentEvent(ctx, s.repo.DB(), received); err != nil {
		return nil, err
	}

	if known == nil {
		rej := base
		rej.EventType, rej.Outcome, rej.ReasonCode = domain.EvWebhookRejected, domain.OutcomeFailure, domain.ReasonUnknownPayment
		s.recordPaymentFailure(ctx, rej)
		return nil, domain.NewError(domain.ReasonUnknownPayment, "unknown payment")
	}

	var res WebhookResult
	var releaseBooking *repository.BookingRow
	err := s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		p, err := s.repo.LockPayment(ctx, tx, known.ID)
		if err != nil {
			return err
		}
		if p == nil {
			return errors.New("payment disappeared")
		}
		b, err := s.bookings.Lock(ctx, tx, p.BookingID)
		if err != nil {
			return err
		}
		if b == nil {
			return errors.New("booking of payment disappeared")
		}
		res, releaseBooking, err = s.applyWebhook(ctx, tx, p, b, in, base)
		return err
	})
	if err != nil {
		rej := base
		rej.EventType, rej.Outcome, rej.ReasonCode = domain.EvWebhookRejected, domain.OutcomeFailure, domain.ReasonInternalError
		s.recordPaymentFailure(ctx, rej)
		return nil, err
	}
	if releaseBooking != nil {
		seatIDs, qerr := s.bookings.SeatIDsOfBooking(ctx, s.bookings.DB(), releaseBooking.ID)
		if qerr != nil {
			fmt.Fprintf(os.Stderr, "request_id=%v warning: hold release lookup failed (keys expire by TTL): %v\n", ctx.Value(RequestIDKey), qerr)
		} else {
			s.holds.releaseHolds(seatIDs, releaseBooking.ID)
		}
	}
	return &res, nil
}

// applyWebhook runs inside the transaction with the payment and its booking locked. It returns the
// booking whose Redis holds should be released after commit (only on success).
func (s *PaymentService) applyWebhook(ctx context.Context, tx *gorm.DB, p *repository.PaymentRow, b *repository.BookingRow,
	in WebhookPayload, base audit.PaymentEvent) (WebhookResult, *repository.BookingRow, error) {
	ev := base
	ev.FromStatus = p.Status

	switch domain.PaymentStatus(p.Status) {
	case domain.PaymentSucceeded, domain.PaymentFailed, domain.PaymentNeedsRefund, domain.PaymentRefunded:
		ev.EventType, ev.Outcome, ev.ReasonCode, ev.ToStatus = domain.EvPaymentDuplicateIgnore, domain.OutcomeIgnored, domain.ReasonDuplicateEvent, p.Status
		return WebhookResult{Result: "ignored", PaymentStatus: p.Status}, nil, audit.RecordPaymentEvent(ctx, tx, ev)
	}

	if in.Status == webhookStatusFailed {
		code := domain.ReasonGatewayDeclined
		if err := s.repo.SetOutcome(ctx, tx, p.ID, string(domain.PaymentFailed), in.ProviderRef, string(code), failureMessages[code], false); err != nil {
			return WebhookResult{}, nil, err
		}
		ev.EventType, ev.Outcome, ev.ReasonCode, ev.ToStatus = domain.EvPaymentFailed, domain.OutcomeFailure, code, string(domain.PaymentFailed)
		return WebhookResult{Result: "processed", PaymentStatus: string(domain.PaymentFailed)}, nil, audit.RecordPaymentEvent(ctx, tx, ev)
	}

	needsRefund := func(evType domain.PaymentEventType, code domain.ReasonCode) (WebhookResult, *repository.BookingRow, error) {
		if err := s.repo.SetOutcome(ctx, tx, p.ID, string(domain.PaymentNeedsRefund), in.ProviderRef, string(code), failureMessages[code], true); err != nil {
			return WebhookResult{}, nil, err
		}
		ev.EventType, ev.Outcome, ev.ReasonCode, ev.ToStatus = evType, domain.OutcomeFailure, code, string(domain.PaymentNeedsRefund)
		return WebhookResult{Result: "processed", PaymentStatus: string(domain.PaymentNeedsRefund)}, nil, audit.RecordPaymentEvent(ctx, tx, ev)
	}

	if *in.AmountSatang != p.AmountSatang {
		return needsRefund(domain.EvPaymentAmountMismatch, domain.ReasonAmountMismatch)
	}
	if b.Status == string(domain.BookingExpired) {
		return needsRefund(domain.EvPaymentAfterExpiry, domain.ReasonBookingExpired)
	}
	stats, err := s.repo.ItemStats(ctx, tx, b.ID)
	if err != nil {
		return WebhookResult{}, nil, err
	}
	if b.Status != string(domain.BookingPending) || stats.Count == 0 || stats.ActiveCount != stats.Count {
		return needsRefund(domain.EvPaymentAfterExpiry, domain.ReasonSeatLost)
	}

	// D1: PENDING with every item active means the seats are still ours, even just past expires_at.
	if err := s.repo.SetOutcome(ctx, tx, p.ID, string(domain.PaymentSucceeded), in.ProviderRef, "", "", true); err != nil {
		return WebhookResult{}, nil, err
	}
	ev.EventType, ev.Outcome, ev.ToStatus = domain.EvPaymentSucceeded, domain.OutcomeSuccess, string(domain.PaymentSucceeded)
	if err := audit.RecordPaymentEvent(ctx, tx, ev); err != nil {
		return WebhookResult{}, nil, err
	}
	if err := s.bookings.SetStatus(ctx, tx, b.ID, string(domain.BookingPaid)); err != nil {
		return WebhookResult{}, nil, err
	}
	if err := audit.RecordBookingEvent(ctx, tx, audit.BookingEvent{
		BookingID: &b.ID, UserID: &b.UserID, ShowtimeID: &b.ShowtimeID,
		EventType: domain.EvBookingPaid, Outcome: domain.OutcomeSuccess,
		FromStatus: string(domain.BookingPending), ToStatus: string(domain.BookingPaid), ActorType: domain.ActorGateway,
		Metadata: map[string]any{"payment_id": p.ID, "provider_ref": in.ProviderRef, "amount_satang": p.AmountSatang},
	}); err != nil {
		return WebhookResult{}, nil, err
	}
	itemIDs, err := s.repo.ActiveItemIDs(ctx, tx, b.ID)
	if err != nil {
		return WebhookResult{}, nil, err
	}
	for _, itemID := range itemIDs {
		code, err := newTicketCode()
		if err != nil {
			return WebhookResult{}, nil, err
		}
		if err := s.repo.InsertTicket(ctx, tx, itemID, code); err != nil {
			return WebhookResult{}, nil, err
		}
	}
	if err := audit.RecordBookingEvent(ctx, tx, audit.BookingEvent{
		BookingID: &b.ID, UserID: &b.UserID, ShowtimeID: &b.ShowtimeID,
		EventType: domain.EvTicketsIssued, Outcome: domain.OutcomeSuccess,
		FromStatus: string(domain.BookingPaid), ToStatus: string(domain.BookingPaid), ActorType: domain.ActorSystem,
		Metadata: map[string]any{"payment_id": p.ID, "ticket_count": len(itemIDs), "booking_item_ids": itemIDs},
	}); err != nil {
		return WebhookResult{}, nil, err
	}
	return WebhookResult{Result: "processed", PaymentStatus: string(domain.PaymentSucceeded)}, b, nil
}

// newTicketCode is 16 random bytes, base64url without padding (22 characters).
func newTicketCode() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ---------- read ----------

func (s *PaymentService) Get(ctx context.Context, userID, id string) (*PaymentView, error) {
	ctx, cancel := context.WithTimeout(ctx, paymentOpTimeout)
	defer cancel()
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, domain.NewError(domain.ReasonNotFound, "payment not found")
	}
	p, err := s.repo.GetForUser(ctx, u.String(), userID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, domain.NewError(domain.ReasonNotFound, "payment not found")
	}
	return paymentView(p), nil
}

func (s *PaymentService) ListTickets(ctx context.Context, userID string) ([]TicketView, error) {
	ctx, cancel := context.WithTimeout(ctx, paymentOpTimeout)
	defer cancel()
	rows, err := s.repo.TicketsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return ticketViews(rows), nil
}

func (s *PaymentService) GetTicket(ctx context.Context, userID, code string) (*TicketView, error) {
	ctx, cancel := context.WithTimeout(ctx, paymentOpTimeout)
	defer cancel()
	if code == "" || len(code) > 64 {
		return nil, domain.NewError(domain.ReasonNotFound, "ticket not found")
	}
	t, err := s.repo.TicketByCodeForUser(ctx, code, userID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, domain.NewError(domain.ReasonNotFound, "ticket not found")
	}
	v := ticketViews([]repository.TicketRow{*t})[0]
	return &v, nil
}
