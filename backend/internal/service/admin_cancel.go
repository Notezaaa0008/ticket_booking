package service

import (
	"context"

	"gorm.io/gorm"

	"ticketbooking/internal/audit"
	"ticketbooking/internal/domain"
	"ticketbooking/internal/repository"
)

// holdRelease is a Redis seat hold to drop after the cancellation transaction commits.
type holdRelease struct {
	bookingID string
	seatIDs   []string
}

func cancelFailureMessage(reason domain.ReasonCode) string {
	switch reason {
	case domain.ReasonEventCancelled:
		return "The event was cancelled. This payment needs a refund."
	default:
		return "The showtime was cancelled. This payment needs a refund."
	}
}

// cancelEventShowtimes marks active or future showtimes cancelled and cascades their bookings.
// The event row itself is updated by the caller. Past closed showtimes are not touched.
func cannotCancelCheckedIn(event bool) *domain.AppError {
	if event {
		return domain.NewError(domain.ReasonCannotCancelUsedEvent, "Cannot cancel event because one or more tickets have already been checked in")
	}
	return domain.NewError(domain.ReasonCannotCancelUsedShowtime, "Cannot cancel showtime because one or more tickets have already been checked in")
}

func (s *AdminService) rejectCheckedIn(ctx context.Context, tx *gorm.DB, eventID, showtimeID string, event bool) error {
	used, err := s.repo.HasCheckedInTicket(ctx, tx, eventID, showtimeID)
	if err != nil {
		return err
	}
	if used {
		return cannotCancelCheckedIn(event)
	}
	return nil
}

func (s *AdminService) cancelEventShowtimes(ctx context.Context, tx *gorm.DB, eventID, adminID string, releases *[]holdRelease) error {
	if err := s.rejectCheckedIn(ctx, tx, eventID, "", true); err != nil {
		return err
	}
	ids, err := s.repo.LockEventShowtimesForCancel(ctx, tx, eventID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.repo.SetShowtimeStatus(ctx, tx, id, "cancelled"); err != nil {
			return err
		}
		if err := s.cascadeShowtimeBookings(ctx, tx, id, domain.ReasonEventCancelled, adminID, releases); err != nil {
			return err
		}
	}
	return nil
}

// cascadeShowtimeBookings cancels PENDING and PAID bookings on a showtime inside the caller's transaction.
// PENDING seats are deactivated here. PAID seats stay active until a refund completes, unless the show has started.
// Checked-in tickets are rejected before this runs. A USED ticket that still appears here is left unchanged.
func (s *AdminService) cascadeShowtimeBookings(ctx context.Context, tx *gorm.DB, showtimeID string, reason domain.ReasonCode, adminID string, releases *[]holdRelease) error {
	payments, err := s.repo.LockSucceededPayments(ctx, tx, showtimeID)
	if err != nil {
		return err
	}
	byBooking := map[string][]repository.CascadePayment{}
	for _, p := range payments {
		byBooking[p.BookingID] = append(byBooking[p.BookingID], p)
	}
	bookings, err := s.repo.LockOpenBookings(ctx, tx, showtimeID)
	if err != nil {
		return err
	}
	actor := &adminID
	for _, b := range bookings {
		if b.Status == string(domain.BookingPending) {
			if err := s.cancelPendingBooking(ctx, tx, b, reason, actor, releases); err != nil {
				return err
			}
			continue
		}
		used, err := s.repo.HasUsedTicket(ctx, tx, b.ID)
		if err != nil {
			return err
		}
		if used {
			continue
		}
		if err := s.cancelPaidBooking(ctx, tx, b, byBooking[b.ID], reason, actor); err != nil {
			return err
		}
	}
	return nil
}

func (s *AdminService) cancelPendingBooking(ctx context.Context, tx *gorm.DB, b repository.CascadeBooking, reason domain.ReasonCode, actor *string, releases *[]holdRelease) error {
	if err := s.bookings.SetStatus(ctx, tx, b.ID, string(domain.BookingCancelled)); err != nil {
		return err
	}
	if err := s.bookings.DeactivateItems(ctx, tx, b.ID); err != nil {
		return err
	}
	seatIDs, err := s.bookings.SeatIDsOfBooking(ctx, tx, b.ID)
	if err != nil {
		return err
	}
	*releases = append(*releases, holdRelease{bookingID: b.ID, seatIDs: seatIDs})
	return audit.RecordBookingEvent(ctx, tx, audit.BookingEvent{
		BookingID: &b.ID, UserID: &b.UserID, ShowtimeID: &b.ShowtimeID,
		EventType: domain.EvBookingCancelled, Outcome: domain.OutcomeSuccess, ReasonCode: reason,
		FromStatus: string(domain.BookingPending), ToStatus: string(domain.BookingCancelled),
		ActorType: domain.ActorAdmin, ActorID: actor,
		Metadata: map[string]any{"seats_released": true},
	})
}

func (s *AdminService) cancelPaidBooking(ctx context.Context, tx *gorm.DB, b repository.CascadeBooking, payments []repository.CascadePayment, reason domain.ReasonCode, actor *string) error {
	if err := s.bookings.SetStatus(ctx, tx, b.ID, string(domain.BookingCancelled)); err != nil {
		return err
	}
	voided, err := s.repo.VoidTickets(ctx, tx, b.ID)
	if err != nil {
		return err
	}
	for _, p := range payments {
		if err := s.payments.SetOutcome(ctx, tx, p.ID, string(domain.PaymentNeedsRefund), p.ProviderRef, string(reason), cancelFailureMessage(reason), false); err != nil {
			return err
		}
		amount := p.AmountSatang
		if err := audit.RecordPaymentEvent(ctx, tx, audit.PaymentEvent{
			PaymentID: &p.ID, BookingID: &b.ID, EventType: domain.EvPaymentNeedsRefund, Outcome: domain.OutcomeSuccess,
			ReasonCode: reason, FromStatus: string(domain.PaymentSucceeded), ToStatus: string(domain.PaymentNeedsRefund),
			AmountSatang: &amount, ProviderRef: p.ProviderRef, ActorType: domain.ActorAdmin, ActorID: actor,
		}); err != nil {
			return err
		}
	}
	return audit.RecordBookingEvent(ctx, tx, audit.BookingEvent{
		BookingID: &b.ID, UserID: &b.UserID, ShowtimeID: &b.ShowtimeID,
		EventType: domain.EvBookingCancelled, Outcome: domain.OutcomeSuccess, ReasonCode: reason,
		FromStatus: string(domain.BookingPaid), ToStatus: string(domain.BookingCancelled),
		ActorType: domain.ActorAdmin, ActorID: actor,
		Metadata: map[string]any{"tickets_voided": voided, "seats_released": false},
	})
}

func (s *AdminService) releaseAfterCommit(releases []holdRelease) {
	for _, rel := range releases {
		releaseOwnedHolds(s.rdb, rel.bookingID, rel.seatIDs)
	}
}
