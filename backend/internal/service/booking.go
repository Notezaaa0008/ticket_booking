package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"ticketbooking/internal/audit"
	"ticketbooking/internal/domain"
	"ticketbooking/internal/repository"
)

const (
	MaxSeatsPerBooking = 6
	holdKeyPrefix      = "hold:"
	bookingOpTimeout   = 10 * time.Second
	redisOpTimeout     = time.Second
	auditOpTimeout     = 5 * time.Second
	activeSeatIndex    = "uq_booking_items_active_seat"
	holdModeRedis      = "redis"
	holdModeFallback   = "db_fallback"
)

type ctxKey string

// RequestIDKey carries the HTTP request id into services for log lines.
const RequestIDKey ctxKey = "request_id"

// releaseScript deletes a hold only if this booking still owns it.
var releaseScript = redis.NewScript(
	`if redis.call("get",KEYS[1])==ARGV[1] then return redis.call("del",KEYS[1]) else return 0 end`)

// SeatConflictError is a 409 SEAT_UNAVAILABLE that also tells the client which seats were taken.
type SeatConflictError struct {
	*domain.AppError
	SeatIDs []string
}

func (e *SeatConflictError) Unwrap() error { return e.AppError }

func newSeatConflict(ids []string) *SeatConflictError {
	return &SeatConflictError{
		AppError: domain.NewError(domain.ReasonSeatUnavailable, "one or more seats are no longer available"),
		SeatIDs:  ids,
	}
}

type BookingService struct {
	repo *repository.BookingRepository
	rdb  *redis.Client // may be nil: DB-only mode
	ttl  time.Duration
}

func NewBookingService(repo *repository.BookingRepository, rdb *redis.Client, holdTTL time.Duration) *BookingService {
	return &BookingService{repo: repo, rdb: rdb, ttl: holdTTL}
}

type CreateInput struct {
	ShowtimeID string
	SeatIDs    []string
}

type BookingItemView struct {
	SeatID      string `json:"seat_id"`
	RowLabel    string `json:"row_label"`
	SeatNumber  int    `json:"seat_number"`
	Zone        string `json:"zone"`
	PriceSatang int64  `json:"price_satang"`
}

type BookingView struct {
	ID               string            `json:"id"`
	ShowtimeID       string            `json:"showtime_id"`
	EventTitle       string            `json:"event_title"`
	Venue            string            `json:"venue"`
	StartsAt         time.Time         `json:"starts_at"`
	Status           string            `json:"status"`
	StatusReason     string            `json:"status_reason,omitempty"`
	TotalSatang      int64             `json:"total_satang"`
	ExpiresAt        time.Time         `json:"expires_at"`
	SecondsRemaining int64             `json:"seconds_remaining"`
	CreatedAt        time.Time         `json:"created_at"`
	Items            []BookingItemView `json:"items"`
	Payment          *PaymentView      `json:"payment,omitempty"` // latest payment (GET /bookings/:id only)
	Tickets          []TicketView      `json:"tickets,omitempty"` // only when PAID
}

// createTrace collects what the failure audit event needs.
type createTrace struct {
	requested   []string
	showtimeID  *string
	conflicting []string
}

// ---------- create ----------

func (s *BookingService) Create(ctx context.Context, userID string, in CreateInput) (*BookingView, error) {
	ctx, cancel := context.WithTimeout(ctx, bookingOpTimeout)
	defer cancel()
	tr := &createTrace{requested: capStrings(in.SeatIDs, 20)}
	bookingID, err := s.create(ctx, userID, in, tr)
	if err != nil {
		s.recordCreateFailure(ctx, userID, tr, err)
		return nil, err
	}
	return s.Get(ctx, userID, bookingID)
}

// RejectInvalid audits and returns a validation failure for a request that never reached Create
// (for example malformed JSON).
func (s *BookingService) RejectInvalid(ctx context.Context, userID, msg string) error {
	err := domain.NewError(domain.ReasonValidationFailed, msg)
	s.recordCreateFailure(ctx, userID, &createTrace{requested: []string{}}, err)
	return err
}

func (s *BookingService) create(ctx context.Context, userID string, in CreateInput, tr *createTrace) (string, error) {
	if len(in.SeatIDs) == 0 {
		return "", domain.NewError(domain.ReasonValidationFailed, "seat_ids is required")
	}
	if len(in.SeatIDs) > MaxSeatsPerBooking {
		return "", domain.NewError(domain.ReasonTooManySeats, fmt.Sprintf("at most %d seats per booking", MaxSeatsPerBooking))
	}
	showtimeUUID, err := uuid.Parse(in.ShowtimeID)
	if err != nil {
		return "", domain.NewError(domain.ReasonValidationFailed, "invalid showtime_id")
	}
	showtimeID := showtimeUUID.String()
	seatIDs, err := normalizeSeatIDs(in.SeatIDs)
	if err != nil {
		return "", err
	}

	st, err := s.repo.GetShowtime(ctx, s.repo.DB(), showtimeID)
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", domain.NewError(domain.ReasonValidationFailed, "showtime not found")
	}
	tr.showtimeID = &showtimeID // only set once it is known to exist (foreign key)
	if st.Status != "on_sale" || !st.StartsAt.After(time.Now()) {
		return "", domain.NewError(domain.ReasonShowtimeNotOnSale, "showtime is not on sale")
	}
	if _, err := s.loadSeats(ctx, s.repo.DB(), showtimeID, seatIDs); err != nil {
		return "", err
	}
	if err := s.checkNoLivePending(ctx, s.repo.DB(), userID, showtimeID); err != nil {
		return "", err
	}

	bookingID := uuid.NewString()
	acquired, conflicts, holdMode := s.acquireHolds(ctx, bookingID, seatIDs)
	if len(conflicts) > 0 {
		s.releaseHolds(acquired, bookingID)
		tr.conflicting = conflicts
		return "", newSeatConflict(conflicts)
	}

	if err := s.insertBooking(ctx, userID, showtimeID, bookingID, seatIDs, holdMode); err != nil {
		s.releaseHolds(acquired, bookingID)
		if isActiveSeatViolation(err) {
			taken, qerr := s.repo.ActiveSeatIDs(ctx, s.repo.DB(), seatIDs)
			if qerr != nil || len(taken) == 0 {
				taken = seatIDs
			}
			tr.conflicting = taken
			return "", newSeatConflict(taken)
		}
		return "", err
	}
	return bookingID, nil
}

// insertBooking is the ONE transaction: lazy expiry, booking, items and the audit event.
func (s *BookingService) insertBooking(ctx context.Context, userID, showtimeID, bookingID string, seatIDs []string, holdMode string) error {
	return s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.repo.LockUserShowtime(ctx, tx, userID, showtimeID); err != nil {
			return err
		}
		if err := s.checkNoLivePending(ctx, tx, userID, showtimeID); err != nil {
			return err
		}
		stale, err := s.repo.LockStaleBlocking(ctx, tx, seatIDs)
		if err != nil {
			return err
		}
		for i := range stale {
			if err := s.expireLocked(ctx, tx, &stale[i], "lazy"); err != nil {
				return err
			}
		}
		seats, err := s.loadSeats(ctx, tx, showtimeID, seatIDs)
		if err != nil {
			return err
		}
		var total int64
		for _, p := range seats {
			total += p.PriceSatang
		}
		row, err := s.repo.InsertBooking(ctx, tx, bookingID, userID, showtimeID, total, int(s.ttl/time.Second))
		if err != nil {
			return err
		}
		for _, p := range seats {
			if err := s.repo.InsertItem(ctx, tx, bookingID, p.ID, p.PriceSatang); err != nil {
				return err
			}
		}
		return audit.RecordBookingEvent(ctx, tx, audit.BookingEvent{
			BookingID: &bookingID, UserID: &userID, ShowtimeID: &showtimeID,
			EventType: domain.EvBookingCreated, Outcome: domain.OutcomeSuccess,
			ToStatus: string(domain.BookingPending), ActorType: domain.ActorUser, ActorID: &userID,
			Metadata: map[string]any{
				"seat_ids":     seatIDs,
				"total_satang": total,
				"expires_at":   row.ExpiresAt.UTC().Format(time.RFC3339Nano),
				"hold_mode":    holdMode,
			},
		})
	})
}

// loadSeats returns the seats with DB prices, or SEAT_NOT_IN_SHOWTIME if any id is unknown or foreign.
func (s *BookingService) loadSeats(ctx context.Context, db *gorm.DB, showtimeID string, seatIDs []string) ([]repository.SeatPrice, error) {
	rows, err := s.repo.SeatsByIDs(ctx, db, seatIDs)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(seatIDs) {
		return nil, domain.NewError(domain.ReasonSeatNotInShowtime, "one or more seats do not exist")
	}
	for _, r := range rows {
		if r.ShowtimeID != showtimeID {
			return nil, domain.NewError(domain.ReasonSeatNotInShowtime, "one or more seats do not belong to this showtime")
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}

func (s *BookingService) checkNoLivePending(ctx context.Context, db *gorm.DB, userID, showtimeID string) error {
	has, err := s.repo.HasLivePending(ctx, db, userID, showtimeID)
	if err != nil {
		return err
	}
	if has {
		e := domain.NewError(domain.ReasonValidationFailed, "you already have a pending booking for this showtime")
		e.Status = http.StatusConflict
		return e
	}
	return nil
}

func normalizeSeatIDs(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		u, err := uuid.Parse(r)
		if err != nil {
			return nil, domain.NewError(domain.ReasonValidationFailed, "invalid seat id")
		}
		id := u.String()
		if _, dup := seen[id]; dup {
			return nil, domain.NewError(domain.ReasonValidationFailed, "duplicate seat ids")
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func isActiveSeatViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == activeSeatIndex
}

// ---------- Redis holds ----------

func holdKey(seatID string) string { return holdKeyPrefix + seatID }

// acquireHolds runs SET hold:{seat} {booking} NX EX ttl per seat. It returns the keys it owns, the
// seats already held by someone else, and the hold mode. A Redis error (not "key exists") switches to
// DB-only mode: the unique index in PostgreSQL still prevents double booking.
func (s *BookingService) acquireHolds(ctx context.Context, bookingID string, seatIDs []string) (acquired, conflicts []string, mode string) {
	if s.rdb == nil {
		return nil, nil, holdModeFallback
	}
	mode = holdModeRedis
	for _, id := range seatIDs {
		rctx, cancel := context.WithTimeout(ctx, redisOpTimeout)
		ok, err := s.rdb.SetNX(rctx, holdKey(id), bookingID, s.ttl).Result()
		cancel()
		if err != nil {
			log.Printf("request_id=%v warning: redis hold failed, continuing DB-only: %v", ctx.Value(RequestIDKey), err)
			mode = holdModeFallback
			break
		}
		if ok {
			acquired = append(acquired, id)
		} else {
			conflicts = append(conflicts, id)
		}
	}
	return acquired, conflicts, mode
}

// releaseHolds deletes only the keys still owned by bookingID (compare-and-delete).
// Errors are logged: an unreleased key simply expires with its TTL.
func (s *BookingService) releaseHolds(seatIDs []string, bookingID string) {
	if s.rdb == nil || len(seatIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, id := range seatIDs {
		if err := releaseScript.Run(ctx, s.rdb, []string{holdKey(id)}, bookingID).Err(); err != nil {
			log.Printf("warning: release hold %s failed (will expire by TTL): %v", id, err)
		}
	}
}

// ---------- audit helpers ----------

func (s *BookingService) recordCreateFailure(ctx context.Context, userID string, tr *createTrace, err error) {
	reason := reasonOf(err)
	meta := map[string]any{"requested_seat_ids": tr.requested}
	if len(tr.conflicting) > 0 {
		meta["conflicting_seat_ids"] = tr.conflicting
	}
	s.recordFailure(ctx, audit.BookingEvent{
		UserID: &userID, ShowtimeID: tr.showtimeID,
		EventType: domain.EvBookingCreateFailed, Outcome: domain.OutcomeFailure, ReasonCode: reason,
		ActorType: domain.ActorUser, ActorID: &userID, Metadata: meta,
	})
}

// recordFailure writes a failure event with the plain connection (after rollback). If that fails it
// logs to stderr with the request id; the caller still returns the original error.
func (s *BookingService) recordFailure(ctx context.Context, e audit.BookingEvent) {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditOpTimeout)
	defer cancel()
	if err := audit.RecordBookingEvent(actx, s.repo.DB(), e); err != nil {
		fmt.Fprintf(os.Stderr, "request_id=%v audit write failed (event=%s reason=%s): %v\n",
			ctx.Value(RequestIDKey), e.EventType, e.ReasonCode, err)
	}
}

func reasonOf(err error) domain.ReasonCode {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return domain.ReasonInternalError
}

func capStrings(in []string, n int) []string {
	out := make([]string, 0, min(len(in), n))
	for i, v := range in {
		if i >= n {
			break
		}
		if len(v) > 64 {
			v = v[:64]
		}
		out = append(out, v)
	}
	return out
}

// ---------- read ----------

func (s *BookingService) Get(ctx context.Context, userID, id string) (*BookingView, error) {
	ctx, cancel := context.WithTimeout(ctx, bookingOpTimeout)
	defer cancel()
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, domain.NewError(domain.ReasonBookingNotFound, "booking not found")
	}
	d, err := s.repo.GetForUser(ctx, u.String(), userID)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, domain.NewError(domain.ReasonBookingNotFound, "booking not found")
	}
	views, err := s.buildViews(ctx, []repository.BookingDetailRow{*d})
	if err != nil {
		return nil, err
	}
	v := &views[0]
	payments := repository.NewPaymentRepository(s.repo.DB())
	p, err := payments.LatestForBooking(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	if p != nil {
		v.Payment = paymentView(p)
	}
	if v.Status == string(domain.BookingPaid) {
		rows, err := payments.TicketsForBooking(ctx, v.ID)
		if err != nil {
			return nil, err
		}
		v.Tickets = ticketViews(rows)
	}
	return v, nil
}

func (s *BookingService) List(ctx context.Context, userID string) ([]BookingView, error) {
	ctx, cancel := context.WithTimeout(ctx, bookingOpTimeout)
	defer cancel()
	rows, err := s.repo.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.buildViews(ctx, rows)
}

func (s *BookingService) buildViews(ctx context.Context, rows []repository.BookingDetailRow) ([]BookingView, error) {
	views := make([]BookingView, 0, len(rows))
	if len(rows) == 0 {
		return views, nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	items, err := s.repo.ItemsForBookings(ctx, ids)
	if err != nil {
		return nil, err
	}
	byBooking := make(map[string][]BookingItemView, len(rows))
	for _, it := range items {
		byBooking[it.BookingID] = append(byBooking[it.BookingID], BookingItemView{
			SeatID: it.SeatID, RowLabel: it.RowLabel, SeatNumber: it.SeatNumber, Zone: it.Zone, PriceSatang: it.PriceSatang,
		})
	}
	reasons, err := s.repo.LatestReasons(ctx, ids)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, r := range rows {
		v := BookingView{
			ID: r.ID, ShowtimeID: r.ShowtimeID, EventTitle: r.EventTitle, Venue: r.Venue, StartsAt: r.StartsAt,
			Status: r.Status, TotalSatang: r.TotalSatang, ExpiresAt: r.ExpiresAt.UTC(), CreatedAt: r.CreatedAt.UTC(),
			Items: byBooking[r.ID],
		}
		if v.Items == nil {
			v.Items = []BookingItemView{}
		}
		if r.Status == string(domain.BookingPending) {
			if rem := r.ExpiresAt.Sub(now); rem > 0 {
				v.SecondsRemaining = int64((rem + time.Second - 1) / time.Second)
			}
		}
		if r.Status == string(domain.BookingExpired) || r.Status == string(domain.BookingCancelled) {
			v.StatusReason = reasons[r.ID]
		}
		views = append(views, v)
	}
	return views, nil
}

// ---------- cancel ----------

// Cancel cancels the caller's PENDING booking. Other users' or unknown bookings give 404.
func (s *BookingService) Cancel(ctx context.Context, userID, id string) (*BookingView, error) {
	ctx, cancel := context.WithTimeout(ctx, bookingOpTimeout)
	defer cancel()
	u, perr := uuid.Parse(id)
	if perr != nil {
		err := domain.NewError(domain.ReasonBookingNotFound, "booking not found")
		s.recordCancelRejected(ctx, userID, nil, "", id, err)
		return nil, err
	}
	bookingID := u.String()

	var current string
	var showtimeID *string
	var seatIDs []string
	err := s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		b, err := s.repo.Lock(ctx, tx, bookingID)
		if err != nil {
			return err
		}
		if b == nil || b.UserID != userID {
			return domain.NewError(domain.ReasonBookingNotFound, "booking not found")
		}
		current = b.Status
		showtimeID = &b.ShowtimeID
		if b.Status != string(domain.BookingPending) {
			return domain.NewError(domain.ReasonBookingNotPending, "only a pending booking can be cancelled")
		}
		if err := s.repo.SetStatus(ctx, tx, bookingID, string(domain.BookingCancelled)); err != nil {
			return err
		}
		if err := s.repo.DeactivateItems(ctx, tx, bookingID); err != nil {
			return err
		}
		if seatIDs, err = s.repo.SeatIDsOfBooking(ctx, tx, bookingID); err != nil {
			return err
		}
		return audit.RecordBookingEvent(ctx, tx, audit.BookingEvent{
			BookingID: &bookingID, UserID: &userID, ShowtimeID: showtimeID,
			EventType: domain.EvBookingCancelled, Outcome: domain.OutcomeSuccess, ReasonCode: domain.ReasonUserCancelled,
			FromStatus: string(domain.BookingPending), ToStatus: string(domain.BookingCancelled),
			ActorType: domain.ActorUser, ActorID: &userID,
		})
	})
	if err != nil {
		var bid *string
		if reasonOf(err) != domain.ReasonBookingNotFound {
			bid = &bookingID
		}
		s.recordCancelRejected(ctx, userID, bid, current, bookingID, err)
		return nil, err
	}
	s.releaseHolds(seatIDs, bookingID)
	return s.Get(ctx, userID, bookingID)
}

func (s *BookingService) recordCancelRejected(ctx context.Context, userID string, bookingID *string, fromStatus, requestedID string, err error) {
	var showtime *string
	if bookingID != nil {
		if d, qerr := s.repo.GetForUser(ctx, *bookingID, userID); qerr == nil && d != nil {
			showtime = &d.ShowtimeID
		}
	}
	s.recordFailure(ctx, audit.BookingEvent{
		BookingID: bookingID, UserID: &userID, ShowtimeID: showtime,
		EventType: domain.EvBookingCancelRejected, Outcome: domain.OutcomeFailure, ReasonCode: reasonOf(err),
		FromStatus: fromStatus, ActorType: domain.ActorUser, ActorID: &userID,
		Metadata: map[string]any{"requested_booking_id": truncate(requestedID, 64)},
	})
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---------- expiry ----------

// expireLocked moves a locked PENDING booking to EXPIRED, frees its seats and records the event.
func (s *BookingService) expireLocked(ctx context.Context, tx *gorm.DB, b *repository.BookingRow, trigger string) error {
	if err := s.repo.SetStatus(ctx, tx, b.ID, string(domain.BookingExpired)); err != nil {
		return err
	}
	if err := s.repo.DeactivateItems(ctx, tx, b.ID); err != nil {
		return err
	}
	return audit.RecordBookingEvent(ctx, tx, audit.BookingEvent{
		BookingID: &b.ID, UserID: &b.UserID, ShowtimeID: &b.ShowtimeID,
		EventType: domain.EvBookingExpired, Outcome: domain.OutcomeSuccess, ReasonCode: domain.ReasonHoldExpired,
		FromStatus: string(domain.BookingPending), ToStatus: string(domain.BookingExpired),
		ActorType: domain.ActorSystem, Metadata: map[string]any{"trigger": trigger},
	})
}

// ExpireDue expires up to one batch of due PENDING bookings, one transaction each.
// One failing booking is logged and does not stop the others. Returns how many were expired.
func (s *BookingService) ExpireDue(ctx context.Context) (int, error) {
	ids, err := s.repo.DueBookingIDs(ctx, 100)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		var seatIDs []string
		expired := false
		err := s.repo.Transaction(ctx, func(tx *gorm.DB) error {
			b, err := s.repo.LockDue(ctx, tx, id)
			if err != nil {
				return err
			}
			if b == nil {
				return nil // already handled or locked by another worker
			}
			if err := s.expireLocked(ctx, tx, b, "job"); err != nil {
				return err
			}
			if seatIDs, err = s.repo.SeatIDsOfBooking(ctx, tx, id); err != nil {
				return err
			}
			expired = true
			return nil
		})
		if err != nil {
			log.Printf("expiry job: booking %s failed: %v", id, err)
			continue
		}
		if expired {
			n++
			s.releaseHolds(seatIDs, id)
		}
	}
	return n, nil
}

// RunExpiryJob calls ExpireDue every interval until ctx is cancelled.
func (s *BookingService) RunExpiryJob(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			jctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			if _, err := s.ExpireDue(jctx); err != nil {
				log.Printf("expiry job error: %v", err)
			}
			cancel()
		}
	}
}
