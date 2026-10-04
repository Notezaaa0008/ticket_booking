package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"ticketbooking/internal/audit"
	"ticketbooking/internal/domain"
	"ticketbooking/internal/repository"
)

const (
	adminOpTimeout = 10 * time.Second
	AdminPageSize  = 20
	MaxShowRows    = 20
	MaxSeatsPerRow = 30
)

// AdminService backs the /admin API. Refund methods live in refund.go.
type AdminService struct {
	repo     *repository.AdminRepository
	payments *repository.PaymentRepository
	bookings *repository.BookingRepository
	rdb      *redis.Client // may be nil; only used to invalidate the public events list cache
}

func NewAdminService(repo *repository.AdminRepository, payments *repository.PaymentRepository,
	bookings *repository.BookingRepository, rdb *redis.Client) *AdminService {
	return &AdminService{repo: repo, payments: payments, bookings: bookings, rdb: rdb}
}

func validationErr(msg string) *domain.AppError {
	return domain.NewError(domain.ReasonValidationFailed, msg)
}

func notFound(what string) *domain.AppError {
	return domain.NewError(domain.ReasonNotFound, what+" not found")
}

func parseID(id, what string) (string, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return "", notFound(what)
	}
	return u.String(), nil
}

func pageOffset(page int) int {
	if page < 1 {
		page = 1
	}
	return (page - 1) * AdminPageSize
}

func validStatus(status string, allowed ...string) bool {
	if status == "" {
		return true
	}
	for _, a := range allowed {
		if a == status {
			return true
		}
	}
	return false
}

// invalidateEventsCache drops every cached public events list. A Redis error only delays freshness
// until the cache TTL, so it is logged and not returned.
func (s *AdminService) invalidateEventsCache(ctx context.Context) {
	if s.rdb == nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisOpTimeout)
	defer cancel()
	iter := s.rdb.Scan(rctx, 0, eventsListCachePrefix+"*", 200).Iterator()
	for iter.Next(rctx) {
		if err := s.rdb.Del(rctx, iter.Val()).Err(); err != nil {
			log.Printf("request_id=%v events cache invalidation warning: %v", ctx.Value(RequestIDKey), err)
			return
		}
	}
	if err := iter.Err(); err != nil {
		log.Printf("request_id=%v events cache invalidation warning: %v", ctx.Value(RequestIDKey), err)
	}
}

// ---------- events ----------

type AdminShowtimeView struct {
	ID           string    `json:"id"`
	EventID      string    `json:"event_id"`
	StartsAt     time.Time `json:"starts_at"`
	Status       string    `json:"status"`
	SeatCount    int64     `json:"seat_count"`
	BookingCount int64     `json:"booking_count"`
}

type AdminEventView struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Venue       string              `json:"venue"`
	PosterURL   string              `json:"poster_url"`
	Status      string              `json:"status"`
	CreatedAt   time.Time           `json:"created_at"`
	Showtimes   []AdminShowtimeView `json:"showtimes"`
}

func showtimeView(r repository.AdminShowtimeRow) AdminShowtimeView {
	return AdminShowtimeView{ID: r.ID, EventID: r.EventID, StartsAt: r.StartsAt.UTC(), Status: r.Status,
		SeatCount: r.SeatCount, BookingCount: r.BookingCount}
}

func eventView(e *repository.AdminEventRow) *AdminEventView {
	return &AdminEventView{ID: e.ID, Title: e.Title, Description: e.Description, Venue: e.Venue, PosterURL: e.PosterURL,
		Status: e.Status, CreatedAt: e.CreatedAt.UTC(), Showtimes: []AdminShowtimeView{}}
}

func (s *AdminService) ListEvents(ctx context.Context) ([]*AdminEventView, error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	rows, err := s.repo.ListEvents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*AdminEventView, 0, len(rows))
	byID := map[string]*AdminEventView{}
	ids := make([]string, 0, len(rows))
	for i := range rows {
		v := eventView(&rows[i])
		out = append(out, v)
		byID[v.ID] = v
		ids = append(ids, v.ID)
	}
	sts, err := s.repo.ShowtimesForEvents(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, st := range sts {
		if v := byID[st.EventID]; v != nil {
			v.Showtimes = append(v.Showtimes, showtimeView(st))
		}
	}
	return out, nil
}

type EventInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Venue       *string `json:"venue"`
	PosterURL   *string `json:"poster_url"`
	Status      *string `json:"status"`
}

func applyEventInput(e *repository.AdminEventRow, in EventInput) error {
	if in.Title != nil {
		e.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		e.Description = strings.TrimSpace(*in.Description)
	}
	if in.Venue != nil {
		e.Venue = strings.TrimSpace(*in.Venue)
	}
	if in.PosterURL != nil {
		e.PosterURL = strings.TrimSpace(*in.PosterURL)
	}
	if in.Status != nil {
		e.Status = *in.Status
	}
	switch {
	case e.Title == "" || len(e.Title) > 200:
		return validationErr("title is required (max 200 characters)")
	case e.Venue == "" || len(e.Venue) > 200:
		return validationErr("venue is required (max 200 characters)")
	case len(e.Description) > 5000:
		return validationErr("description is too long")
	case len(e.PosterURL) > 1000:
		return validationErr("poster_url is too long")
	case e.Status != "draft" && e.Status != "published" && e.Status != "archived":
		return validationErr("status must be draft, published or archived")
	}
	return nil
}

func (s *AdminService) CreateEvent(ctx context.Context, in EventInput) (*AdminEventView, error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	e := repository.AdminEventRow{Status: "published"}
	if err := applyEventInput(&e, in); err != nil {
		return nil, err
	}
	row, err := s.repo.InsertEvent(ctx, e)
	if err != nil {
		return nil, err
	}
	s.invalidateEventsCache(ctx)
	return eventView(row), nil
}

func (s *AdminService) UpdateEvent(ctx context.Context, id string, in EventInput) (*AdminEventView, error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	id, err := parseID(id, "event")
	if err != nil {
		return nil, err
	}
	var out *repository.AdminEventRow
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		e, err := s.repo.LockEvent(ctx, tx, id)
		if err != nil {
			return err
		}
		if e == nil {
			return notFound("event")
		}
		if err := applyEventInput(e, in); err != nil {
			return err
		}
		out, err = s.repo.UpdateEvent(ctx, tx, *e)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.invalidateEventsCache(ctx)
	return eventView(out), nil
}

// DeleteEvent hard-deletes an event without showtimes; otherwise it archives it. Returns "deleted" or "archived".
func (s *AdminService) DeleteEvent(ctx context.Context, id string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	id, err := parseID(id, "event")
	if err != nil {
		return "", err
	}
	result := ""
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		e, err := s.repo.LockEvent(ctx, tx, id)
		if err != nil {
			return err
		}
		if e == nil {
			return notFound("event")
		}
		n, err := s.repo.CountShowtimes(ctx, tx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			result = "deleted"
			return s.repo.DeleteEvent(ctx, tx, id)
		}
		result = "archived"
		e.Status = "archived"
		_, err = s.repo.UpdateEvent(ctx, tx, *e)
		return err
	})
	if err != nil {
		return "", err
	}
	s.invalidateEventsCache(ctx)
	return result, nil
}

// ---------- showtimes ----------

type ShowtimeInput struct {
	StartsAt         time.Time        `json:"starts_at"`
	Rows             int              `json:"rows"`
	SeatsPerRow      int              `json:"seats_per_row"`
	PriceSatang      int64            `json:"price_satang"`
	PriceSatangByRow map[string]int64 `json:"price_satang_by_row"`
}

func rowLabel(i int) string { return string(rune('A' + i)) }

func (s *AdminService) CreateShowtime(ctx context.Context, eventID string, in ShowtimeInput) (*AdminShowtimeView, error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	eventID, err := parseID(eventID, "event")
	if err != nil {
		return nil, err
	}
	switch {
	case in.StartsAt.IsZero() || !in.StartsAt.After(time.Now()):
		return nil, validationErr("starts_at must be a future RFC 3339 time")
	case in.Rows < 1 || in.Rows > MaxShowRows:
		return nil, validationErr(fmt.Sprintf("rows must be between 1 and %d", MaxShowRows))
	case in.SeatsPerRow < 1 || in.SeatsPerRow > MaxSeatsPerRow:
		return nil, validationErr(fmt.Sprintf("seats_per_row must be between 1 and %d", MaxSeatsPerRow))
	case in.PriceSatang < 0:
		return nil, validationErr("price_satang must not be negative")
	}
	labels := map[string]bool{}
	for i := 0; i < in.Rows; i++ {
		labels[rowLabel(i)] = true
	}
	for row, p := range in.PriceSatangByRow {
		if !labels[row] {
			return nil, validationErr("price_satang_by_row has an unknown row: " + row)
		}
		if p < 0 {
			return nil, validationErr("price_satang_by_row must not be negative")
		}
	}
	seats := make([]repository.SeatSpec, 0, in.Rows*in.SeatsPerRow)
	for i := 0; i < in.Rows; i++ {
		label := rowLabel(i)
		price := in.PriceSatang
		if p, ok := in.PriceSatangByRow[label]; ok {
			price = p
		}
		for n := 1; n <= in.SeatsPerRow; n++ {
			seats = append(seats, repository.SeatSpec{RowLabel: label, SeatNumber: n, PriceSatang: price})
		}
	}

	var st *repository.AdminShowtimeRow
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		e, err := s.repo.LockEvent(ctx, tx, eventID)
		if err != nil {
			return err
		}
		if e == nil {
			return notFound("event")
		}
		if e.Status == "archived" {
			return validationErr("cannot add showtimes to an archived event")
		}
		id, err := s.repo.InsertShowtime(ctx, tx, eventID, in.StartsAt.UTC())
		if err != nil {
			return err
		}
		if err := s.repo.InsertSeats(ctx, tx, id, seats); err != nil {
			return err
		}
		st, err = s.repo.ShowtimeByID(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.invalidateEventsCache(ctx)
	v := showtimeView(*st)
	return &v, nil
}

type ShowtimeUpdate struct {
	Status           *string          `json:"status"`
	StartsAt         *time.Time       `json:"starts_at"`
	PriceSatangByRow map[string]int64 `json:"price_satang_by_row"`
}

// UpdateShowtime changes status, starts_at and/or row prices. Showtimes are never deleted (D5);
// new prices apply to future bookings only because booking_items store their own price.
func (s *AdminService) UpdateShowtime(ctx context.Context, id string, in ShowtimeUpdate) (*AdminShowtimeView, error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	id, err := parseID(id, "showtime")
	if err != nil {
		return nil, err
	}
	if in.Status != nil && *in.Status != "on_sale" && *in.Status != "closed" && *in.Status != "cancelled" {
		return nil, validationErr("status must be on_sale, closed or cancelled")
	}
	if in.StartsAt != nil && in.StartsAt.IsZero() {
		return nil, validationErr("starts_at is invalid")
	}
	var st *repository.AdminShowtimeRow
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		ok, err := s.repo.LockShowtime(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return notFound("showtime")
		}
		cur, err := s.repo.ShowtimeByID(ctx, tx, id)
		if err != nil {
			return err
		}
		status, startsAt := cur.Status, cur.StartsAt
		if in.Status != nil {
			status = *in.Status
		}
		if in.StartsAt != nil {
			startsAt = in.StartsAt.UTC()
		}
		if err := s.repo.UpdateShowtime(ctx, tx, id, status, startsAt); err != nil {
			return err
		}
		for row, p := range in.PriceSatangByRow {
			if p < 0 {
				return validationErr("price_satang_by_row must not be negative")
			}
			n, err := s.repo.SetRowPrice(ctx, tx, id, row, p)
			if err != nil {
				return err
			}
			if n == 0 {
				return validationErr("price_satang_by_row has an unknown row: " + row)
			}
		}
		st, err = s.repo.ShowtimeByID(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.invalidateEventsCache(ctx)
	v := showtimeView(*st)
	return &v, nil
}

// ---------- bookings / payments ----------

type AdminBookingView struct {
	ID                  string    `json:"id"`
	UserEmail           string    `json:"user_email"`
	ShowtimeID          string    `json:"showtime_id"`
	StartsAt            time.Time `json:"starts_at"`
	EventTitle          string    `json:"event_title"`
	Seats               []string  `json:"seats"`
	Status              string    `json:"status"`
	TotalSatang         int64     `json:"total_satang"`
	LatestPaymentStatus string    `json:"latest_payment_status"`
	CreatedAt           time.Time `json:"created_at"`
	ExpiresAt           time.Time `json:"expires_at"`
}

func adminBookingView(r *repository.AdminBookingRow) AdminBookingView {
	seats := []string{}
	if r.Seats != "" {
		seats = strings.Split(r.Seats, ",")
	}
	return AdminBookingView{ID: r.ID, UserEmail: r.UserEmail, ShowtimeID: r.ShowtimeID, StartsAt: r.StartsAt.UTC(),
		EventTitle: r.EventTitle, Seats: seats, Status: r.Status, TotalSatang: r.TotalSatang,
		LatestPaymentStatus: r.LatestPaymentStatus, CreatedAt: r.CreatedAt.UTC(), ExpiresAt: r.ExpiresAt.UTC()}
}

type Page[T any] struct {
	Items []T   `json:"items"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}

var bookingStatuses = []string{"PENDING", "PAID", "EXPIRED", "CANCELLED", "REFUNDED"}
var paymentStatuses = []string{"PENDING", "SUCCEEDED", "FAILED", "NEEDS_REFUND", "REFUNDED"}
var refundStatuses = []string{"REQUESTED", "COMPLETED", "FAILED", "REJECTED"}

type AdminBookingListQuery struct {
	Status     string
	UserID     string
	Email      string
	ShowtimeID string
	Page       int
}

func (s *AdminService) ListBookings(ctx context.Context, q AdminBookingListQuery) (*Page[AdminBookingView], error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	q.Status = strings.TrimSpace(q.Status)
	q.UserID = strings.TrimSpace(q.UserID)
	q.Email = strings.TrimSpace(q.Email)
	q.ShowtimeID = strings.TrimSpace(q.ShowtimeID)
	if !validStatus(q.Status, bookingStatuses...) {
		return nil, validationErr("unknown booking status")
	}
	if q.UserID != "" {
		u, err := uuid.Parse(q.UserID)
		if err != nil {
			return nil, validationErr("user_id must be a valid UUID")
		}
		q.UserID = u.String()
	}
	if q.ShowtimeID != "" {
		u, err := uuid.Parse(q.ShowtimeID)
		if err != nil {
			return nil, validationErr("showtime_id must be a valid UUID")
		}
		q.ShowtimeID = u.String()
	}
	if len(q.Email) > 254 {
		return nil, validationErr("email filter is too long")
	}
	rows, total, err := s.repo.ListBookings(ctx, repository.AdminBookingFilters{
		Status: q.Status, UserID: q.UserID, UserEmail: q.Email, ShowtimeID: q.ShowtimeID,
	}, AdminPageSize, pageOffset(q.Page))
	if err != nil {
		return nil, err
	}
	out := &Page[AdminBookingView]{Items: make([]AdminBookingView, 0, len(rows)), Page: max(q.Page, 1), Limit: AdminPageSize, Total: total}
	for i := range rows {
		out.Items = append(out.Items, adminBookingView(&rows[i]))
	}
	return out, nil
}

type AdminPaymentView struct {
	PaymentView
	ProviderRef   string `json:"provider_ref"`
	UserEmail     string `json:"user_email,omitempty"`
	BookingStatus string `json:"booking_status,omitempty"`
	EventTitle    string `json:"event_title,omitempty"`
}

func adminPaymentView(p *repository.PaymentRow) AdminPaymentView {
	return AdminPaymentView{PaymentView: *paymentView(p), ProviderRef: p.ProviderRef}
}

func (s *AdminService) ListPayments(ctx context.Context, status string, page int) (*Page[AdminPaymentView], error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	if !validStatus(status, paymentStatuses...) {
		return nil, validationErr("unknown payment status")
	}
	rows, total, err := s.repo.ListPayments(ctx, status, AdminPageSize, pageOffset(page))
	if err != nil {
		return nil, err
	}
	out := &Page[AdminPaymentView]{Items: make([]AdminPaymentView, 0, len(rows)), Page: max(page, 1), Limit: AdminPageSize, Total: total}
	for i := range rows {
		v := adminPaymentView(&rows[i].PaymentRow)
		v.UserEmail, v.BookingStatus, v.EventTitle = rows[i].UserEmail, rows[i].BookingStatus, rows[i].EventTitle
		out.Items = append(out.Items, v)
	}
	return out, nil
}

type TimelineEntry struct {
	Source     string    `json:"source"`
	EventType  string    `json:"event_type"`
	Outcome    string    `json:"outcome"`
	ReasonCode string    `json:"reason_code"`
	ActorType  string    `json:"actor_type"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	CreatedAt  time.Time `json:"created_at"`
}

type BookingTimeline struct {
	Booking  AdminBookingView   `json:"booking"`
	Payments []AdminPaymentView `json:"payments"`
	Refunds  []RefundView       `json:"refunds"`
	Events   []TimelineEntry    `json:"events"`
}

func (s *AdminService) Timeline(ctx context.Context, bookingID string) (*BookingTimeline, error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	bookingID, err := parseID(bookingID, "booking")
	if err != nil {
		return nil, err
	}
	b, err := s.repo.BookingByID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, notFound("booking")
	}
	pays, err := s.repo.PaymentsForBooking(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	refs, err := s.repo.RefundsForBooking(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	evs, err := s.repo.Timeline(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	out := &BookingTimeline{Booking: adminBookingView(b), Payments: make([]AdminPaymentView, 0, len(pays)),
		Refunds: make([]RefundView, 0, len(refs)), Events: make([]TimelineEntry, 0, len(evs))}
	for i := range pays {
		out.Payments = append(out.Payments, adminPaymentView(&pays[i]))
	}
	for i := range refs {
		out.Refunds = append(out.Refunds, refundView(&refs[i]))
	}
	for _, e := range evs {
		out.Events = append(out.Events, TimelineEntry{Source: e.Source, EventType: e.EventType, Outcome: e.Outcome,
			ReasonCode: e.ReasonCode, ActorType: e.ActorType, FromStatus: e.FromStatus, ToStatus: e.ToStatus,
			CreatedAt: e.CreatedAt.UTC()})
	}
	return out, nil
}

// ---------- check-in ----------

type CheckInResult struct {
	Result string     `json:"result"` // "CHECKED_IN"
	Ticket TicketView `json:"ticket"`
}

// CheckIn uses a ticket (D6): VALID ticket of a PAID booking only; the first use wins.
// The status change and its audit event commit in one transaction.
func (s *AdminService) CheckIn(ctx context.Context, adminID, code string) (*CheckInResult, error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	code = strings.TrimSpace(code)

	var result *CheckInResult
	var reject error
	err := s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		if code == "" || len(code) > 64 {
			reject = notFound("ticket")
			return s.recordCheckIn(ctx, tx, adminID, nil, code, "", "", domain.EvTicketCheckInRejected, domain.OutcomeFailure, domain.ReasonNotFound)
		}
		ok, err := s.repo.CheckIn(ctx, tx, code)
		if err != nil {
			return err
		}
		row, err := s.repo.TicketAudit(ctx, tx, code)
		if err != nil {
			return err
		}
		if ok {
			if row == nil {
				return notFound("ticket")
			}
			if err := s.recordCheckIn(ctx, tx, adminID, row, code, string(domain.TicketValid), string(domain.TicketUsed),
				domain.EvTicketCheckedIn, domain.OutcomeSuccess, ""); err != nil {
				return err
			}
			t, err := s.repo.TicketByCode(ctx, tx, code)
			if err != nil {
				return err
			}
			if t == nil {
				return notFound("ticket")
			}
			result = &CheckInResult{Result: "CHECKED_IN", Ticket: ticketViews([]repository.TicketRow{*t})[0]}
			return nil
		}
		if row == nil {
			reject = notFound("ticket")
			return s.recordCheckIn(ctx, tx, adminID, nil, code, "", "", domain.EvTicketCheckInRejected, domain.OutcomeFailure, domain.ReasonNotFound)
		}
		reason := domain.ReasonTicketVoid
		msg := "ticket is void or its booking is not paid"
		if row.Status == string(domain.TicketUsed) {
			reason = domain.ReasonTicketAlreadyUsed
			msg = "ticket was already used"
		}
		reject = domain.NewError(reason, msg)
		return s.recordCheckIn(ctx, tx, adminID, row, code, row.Status, row.Status, domain.EvTicketCheckInRejected, domain.OutcomeFailure, reason)
	})
	if err != nil {
		return nil, err
	}
	if reject != nil {
		return nil, reject
	}
	return result, nil
}

func (s *AdminService) recordCheckIn(ctx context.Context, tx *gorm.DB, adminID string, row *repository.TicketAuditRow, code, from, to string, ev domain.BookingEventType, outcome domain.Outcome, reason domain.ReasonCode) error {
	var bookingID, userID, showtimeID *string
	if row != nil {
		bookingID, userID, showtimeID = &row.BookingID, &row.UserID, &row.ShowtimeID
	}
	return audit.RecordBookingEvent(ctx, tx, audit.BookingEvent{
		BookingID: bookingID, UserID: userID, ShowtimeID: showtimeID,
		EventType: ev, Outcome: outcome, ReasonCode: reason, FromStatus: from, ToStatus: to,
		ActorType: domain.ActorAdmin, ActorID: &adminID,
		Metadata: map[string]any{"ticket_code": code},
	})
}

// ---------- stats ----------

func (s *AdminService) Stats(ctx context.Context) (*repository.Stats, error) {
	ctx, cancel := context.WithTimeout(ctx, adminOpTimeout)
	defer cancel()
	return s.repo.Stats(ctx)
}

// recordPaymentFailure writes a payment event with the plain connection after a rolled-back
// transaction; if that fails it logs to stderr and the caller keeps its original result.
func (s *AdminService) recordPaymentFailure(ctx context.Context, e audit.PaymentEvent) {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditOpTimeout)
	defer cancel()
	if err := audit.RecordPaymentEvent(actx, s.repo.DB(), e); err != nil {
		fmt.Fprintf(os.Stderr, "request_id=%v audit write failed (event=%s reason=%s): %v\n",
			ctx.Value(RequestIDKey), e.EventType, e.ReasonCode, err)
	}
}
