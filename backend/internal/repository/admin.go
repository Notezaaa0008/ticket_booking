package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

// AdminRepository holds the SQL behind the admin API (catalog management, back-office lists,
// refunds, check-in and stats). Methods taking a *gorm.DB run on the plain connection or a transaction.
type AdminRepository struct {
	db *gorm.DB
}

func NewAdminRepository(db *gorm.DB) *AdminRepository { return &AdminRepository{db: db} }

func (r *AdminRepository) DB() *gorm.DB { return r.db }

func (r *AdminRepository) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

func one[T any](res *gorm.DB, v *T) (*T, error) {
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return v, nil
}

// ---------- events / showtimes ----------

type AdminEventRow struct {
	ID          string    `gorm:"column:id"`
	Title       string    `gorm:"column:title"`
	Description string    `gorm:"column:description"`
	Venue       string    `gorm:"column:venue"`
	PosterURL   string    `gorm:"column:poster_url"`
	Status      string    `gorm:"column:status"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

const adminEventCols = `id, title, description, venue, poster_url, status, created_at`

func (r *AdminRepository) ListEvents(ctx context.Context) ([]AdminEventRow, error) {
	var rows []AdminEventRow
	err := r.db.WithContext(ctx).Raw(`SELECT ` + adminEventCols + ` FROM events ORDER BY created_at DESC, id LIMIT 500`).Scan(&rows).Error
	return rows, err
}

func (r *AdminRepository) EventByID(ctx context.Context, db *gorm.DB, id string) (*AdminEventRow, error) {
	var e AdminEventRow
	return one(db.WithContext(ctx).Raw(`SELECT `+adminEventCols+` FROM events WHERE id = ?`, id).Scan(&e), &e)
}

func (r *AdminRepository) InsertEvent(ctx context.Context, e AdminEventRow) (*AdminEventRow, error) {
	var out AdminEventRow
	err := r.db.WithContext(ctx).Raw(
		`INSERT INTO events (title, description, venue, poster_url, status) VALUES (?, ?, ?, ?, ?)
		 RETURNING `+adminEventCols, e.Title, e.Description, e.Venue, e.PosterURL, e.Status).Scan(&out).Error
	return &out, err
}

func (r *AdminRepository) UpdateEvent(ctx context.Context, db *gorm.DB, e AdminEventRow) (*AdminEventRow, error) {
	var out AdminEventRow
	return one(db.WithContext(ctx).Raw(
		`UPDATE events SET title = ?, description = ?, venue = ?, poster_url = ?, status = ? WHERE id = ?
		 RETURNING `+adminEventCols, e.Title, e.Description, e.Venue, e.PosterURL, e.Status, e.ID).Scan(&out), &out)
}

func (r *AdminRepository) DeleteEvent(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Exec(`DELETE FROM events WHERE id = ?`, id).Error
}

func (r *AdminRepository) LockEvent(ctx context.Context, tx *gorm.DB, id string) (*AdminEventRow, error) {
	var e AdminEventRow
	return one(tx.WithContext(ctx).Raw(`SELECT `+adminEventCols+` FROM events WHERE id = ? FOR UPDATE`, id).Scan(&e), &e)
}

func (r *AdminRepository) CountShowtimes(ctx context.Context, db *gorm.DB, eventID string) (int64, error) {
	var n int64
	err := db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM showtimes WHERE event_id = ?`, eventID).Scan(&n).Error
	return n, err
}

type AdminShowtimeRow struct {
	ID           string    `gorm:"column:id"`
	EventID      string    `gorm:"column:event_id"`
	StartsAt     time.Time `gorm:"column:starts_at"`
	Status       string    `gorm:"column:status"`
	SeatCount    int64     `gorm:"column:seat_count"`
	BookingCount int64     `gorm:"column:booking_count"`
}

const adminShowtimeSelect = `SELECT st.id, st.event_id, st.starts_at, st.status,
	(SELECT COUNT(*) FROM seats s WHERE s.showtime_id = st.id) AS seat_count,
	(SELECT COUNT(*) FROM bookings b WHERE b.showtime_id = st.id) AS booking_count
	FROM showtimes st`

func (r *AdminRepository) ShowtimesForEvents(ctx context.Context, eventIDs []string) ([]AdminShowtimeRow, error) {
	var rows []AdminShowtimeRow
	if len(eventIDs) == 0 {
		return rows, nil
	}
	err := r.db.WithContext(ctx).Raw(adminShowtimeSelect+` WHERE st.event_id IN ? ORDER BY st.starts_at, st.id`, eventIDs).Scan(&rows).Error
	return rows, err
}

func (r *AdminRepository) ShowtimeByID(ctx context.Context, db *gorm.DB, id string) (*AdminShowtimeRow, error) {
	var s AdminShowtimeRow
	return one(db.WithContext(ctx).Raw(adminShowtimeSelect+` WHERE st.id = ?`, id).Scan(&s), &s)
}

func (r *AdminRepository) LockShowtime(ctx context.Context, tx *gorm.DB, id string) (bool, error) {
	var got string
	res := tx.WithContext(ctx).Raw(`SELECT id FROM showtimes WHERE id = ? FOR UPDATE`, id).Scan(&got)
	return res.RowsAffected > 0, res.Error
}

func (r *AdminRepository) InsertShowtime(ctx context.Context, tx *gorm.DB, eventID string, startsAt time.Time) (string, error) {
	var id string
	err := tx.WithContext(ctx).Raw(
		`INSERT INTO showtimes (event_id, starts_at, status) VALUES (?, ?, 'on_sale') RETURNING id`, eventID, startsAt).Scan(&id).Error
	return id, err
}

type SeatSpec struct {
	RowLabel    string
	SeatNumber  int
	PriceSatang int64
}

// InsertSeats bulk-inserts all seats of a showtime in a single statement.
func (r *AdminRepository) InsertSeats(ctx context.Context, tx *gorm.DB, showtimeID string, seats []SeatSpec) error {
	if len(seats) == 0 {
		return nil
	}
	var sb strings.Builder
	args := make([]any, 0, len(seats)*4)
	sb.WriteString(`INSERT INTO seats (showtime_id, row_label, seat_number, price_satang) VALUES `)
	for i, s := range seats {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?, ?)")
		args = append(args, showtimeID, s.RowLabel, s.SeatNumber, s.PriceSatang)
	}
	return tx.WithContext(ctx).Exec(sb.String(), args...).Error
}

func (r *AdminRepository) UpdateShowtime(ctx context.Context, tx *gorm.DB, id, status string, startsAt time.Time) error {
	return tx.WithContext(ctx).Exec(`UPDATE showtimes SET status = ?, starts_at = ? WHERE id = ?`, status, startsAt, id).Error
}

func (r *AdminRepository) SetShowtimeStatus(ctx context.Context, tx *gorm.DB, id, status string) error {
	return tx.WithContext(ctx).Exec(`UPDATE showtimes SET status = ? WHERE id = ?`, status, id).Error
}

// LockEventShowtimesForCancel locks showtimes that are still on sale, or not yet started.
// Already-cancelled rows are skipped. Past closed showtimes are left as-is.
func (r *AdminRepository) LockEventShowtimesForCancel(ctx context.Context, tx *gorm.DB, eventID string) ([]string, error) {
	var rows []struct {
		ID string `gorm:"column:id"`
	}
	err := tx.WithContext(ctx).Raw(`SELECT id FROM showtimes
		 WHERE event_id = ?
		   AND status <> 'cancelled'
		   AND (status = 'on_sale' OR starts_at > now())
		 ORDER BY id
		 FOR UPDATE`, eventID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	return ids, nil
}

// CascadePayment is a SUCCEEDED payment locked for a showtime cancellation.
type CascadePayment struct {
	ID           string `gorm:"column:id"`
	BookingID    string `gorm:"column:booking_id"`
	ProviderRef  string `gorm:"column:provider_ref"`
	AmountSatang int64  `gorm:"column:amount_satang"`
}

// LockSucceededPayments locks SUCCEEDED payments of PAID bookings on a showtime, payment id order,
// before bookings are locked (same payment-then-booking order as refund processing).
func (r *AdminRepository) LockSucceededPayments(ctx context.Context, tx *gorm.DB, showtimeID string) ([]CascadePayment, error) {
	var rows []CascadePayment
	err := tx.WithContext(ctx).Raw(`SELECT p.id, p.booking_id, p.provider_ref, p.amount_satang
		FROM payments p
		JOIN bookings b ON b.id = p.booking_id
		WHERE b.showtime_id = ? AND b.status = 'PAID' AND p.status = 'SUCCEEDED'
		ORDER BY p.id
		FOR UPDATE OF p`, showtimeID).Scan(&rows).Error
	return rows, err
}

// CascadeBooking is a PENDING or PAID booking locked for cancellation.
type CascadeBooking struct {
	ID         string `gorm:"column:id"`
	UserID     string `gorm:"column:user_id"`
	ShowtimeID string `gorm:"column:showtime_id"`
	Status     string `gorm:"column:status"`
}

// HasCheckedInTicket locks the showtime or event tickets and reports whether any is already used.
// status USED or a non-null checked_in_at both count. The lock blocks a check-in until this transaction ends.
func (r *AdminRepository) HasCheckedInTicket(ctx context.Context, tx *gorm.DB, eventID, showtimeID string) (bool, error) {
	var rows []struct {
		Status      string     `gorm:"column:status"`
		CheckedInAt *time.Time `gorm:"column:checked_in_at"`
	}
	q := `SELECT t.status, t.checked_in_at
		FROM tickets t
		JOIN booking_items bi ON bi.id = t.booking_item_id
		JOIN bookings b ON b.id = bi.booking_id
		JOIN showtimes st ON st.id = b.showtime_id
		WHERE `
	var arg string
	if showtimeID != "" {
		q += `b.showtime_id = ?`
		arg = showtimeID
	} else {
		q += `st.event_id = ?`
		arg = eventID
	}
	err := tx.WithContext(ctx).Raw(q+` ORDER BY t.id FOR UPDATE OF t`, arg).Scan(&rows).Error
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.Status == "USED" || row.CheckedInAt != nil {
			return true, nil
		}
	}
	return false, nil
}

func (r *AdminRepository) LockOpenBookings(ctx context.Context, tx *gorm.DB, showtimeID string) ([]CascadeBooking, error) {
	var rows []CascadeBooking
	err := tx.WithContext(ctx).Raw(`SELECT id, user_id, showtime_id, status FROM bookings
		WHERE showtime_id = ? AND status IN ('PENDING','PAID')
		ORDER BY id
		FOR UPDATE`, showtimeID).Scan(&rows).Error
	return rows, err
}

// SetRowPrice changes the seat price for future bookings; existing booking_items keep their own price.
func (r *AdminRepository) SetRowPrice(ctx context.Context, tx *gorm.DB, showtimeID, rowLabel string, price int64) (int64, error) {
	res := tx.WithContext(ctx).Exec(`UPDATE seats SET price_satang = ? WHERE showtime_id = ? AND row_label = ?`, price, showtimeID, rowLabel)
	return res.RowsAffected, res.Error
}

// ---------- bookings ----------

type AdminBookingRow struct {
	ID                  string    `gorm:"column:id"`
	UserEmail           string    `gorm:"column:user_email"`
	ShowtimeID          string    `gorm:"column:showtime_id"`
	StartsAt            time.Time `gorm:"column:starts_at"`
	EventTitle          string    `gorm:"column:event_title"`
	Seats               string    `gorm:"column:seats"`
	Status              string    `gorm:"column:status"`
	TotalSatang         int64     `gorm:"column:total_satang"`
	LatestPaymentStatus string    `gorm:"column:latest_payment_status"`
	CreatedAt           time.Time `gorm:"column:created_at"`
	ExpiresAt           time.Time `gorm:"column:expires_at"`
}

const adminBookingSelect = `SELECT b.id, u.email AS user_email, b.showtime_id, st.starts_at, e.title AS event_title,
	COALESCE((SELECT string_agg(s.row_label || s.seat_number::text, ',' ORDER BY s.row_label, s.seat_number)
	            FROM booking_items bi JOIN seats s ON s.id = bi.seat_id WHERE bi.booking_id = b.id), '') AS seats,
	b.status, b.total_satang,
	COALESCE((SELECT p.status FROM payments p WHERE p.booking_id = b.id
	           ORDER BY p.created_at DESC, p.id DESC LIMIT 1), '') AS latest_payment_status,
	b.created_at, b.expires_at
	FROM bookings b
	JOIN users u ON u.id = b.user_id
	JOIN showtimes st ON st.id = b.showtime_id
	JOIN events e ON e.id = st.event_id`

// AdminBookingFilters narrows the admin booking list (empty fields are ignored).
type AdminBookingFilters struct {
	Status     string
	UserID     string
	UserEmail  string
	ShowtimeID string
}

func adminBookingWhere(f AdminBookingFilters) (clause string, args []any) {
	clause = ` WHERE 1=1`
	if f.Status != "" {
		clause += ` AND b.status = ?`
		args = append(args, f.Status)
	}
	if f.UserID != "" {
		clause += ` AND b.user_id = ?::uuid`
		args = append(args, f.UserID)
	}
	if f.UserEmail != "" {
		clause += ` AND LOWER(u.email) = LOWER(?)`
		args = append(args, f.UserEmail)
	}
	if f.ShowtimeID != "" {
		clause += ` AND b.showtime_id = ?::uuid`
		args = append(args, f.ShowtimeID)
	}
	return clause, args
}

func (r *AdminRepository) ListBookings(ctx context.Context, f AdminBookingFilters, limit, offset int) ([]AdminBookingRow, int64, error) {
	var rows []AdminBookingRow
	where, args := adminBookingWhere(f)
	db := r.db.WithContext(ctx)
	var total int64
	if err := db.Raw(`SELECT COUNT(*) FROM bookings b JOIN users u ON u.id = b.user_id`+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	listArgs := append(append([]any{}, args...), limit, offset)
	err := db.Raw(adminBookingSelect+where+` ORDER BY b.created_at DESC, b.id LIMIT ? OFFSET ?`, listArgs...).Scan(&rows).Error
	return rows, total, err
}

func (r *AdminRepository) BookingByID(ctx context.Context, id string) (*AdminBookingRow, error) {
	var b AdminBookingRow
	return one(r.db.WithContext(ctx).Raw(adminBookingSelect+` WHERE b.id = ?`, id).Scan(&b), &b)
}

func (r *AdminRepository) PaymentsForBooking(ctx context.Context, bookingID string) ([]PaymentRow, error) {
	var rows []PaymentRow
	err := r.db.WithContext(ctx).Raw(`SELECT `+paymentCols+` FROM payments p WHERE p.booking_id = ? ORDER BY p.created_at, p.id`, bookingID).Scan(&rows).Error
	return rows, err
}

type TimelineRow struct {
	Source     string    `gorm:"column:source"`
	EventType  string    `gorm:"column:event_type"`
	Outcome    string    `gorm:"column:outcome"`
	ReasonCode string    `gorm:"column:reason_code"`
	ActorType  string    `gorm:"column:actor_type"`
	FromStatus string    `gorm:"column:from_status"`
	ToStatus   string    `gorm:"column:to_status"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (r *AdminRepository) Timeline(ctx context.Context, bookingID string) ([]TimelineRow, error) {
	var rows []TimelineRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT 'booking' AS source, event_type, outcome, reason_code, actor_type, from_status, to_status, created_at
		   FROM booking_events WHERE booking_id = @id
		 UNION ALL
		 SELECT 'payment', event_type, outcome, reason_code, actor_type, from_status, to_status, created_at
		   FROM payment_events WHERE booking_id = @id
		 ORDER BY created_at`, map[string]any{"id": bookingID}).Scan(&rows).Error
	return rows, err
}

// ---------- payments ----------

type AdminPaymentRow struct {
	PaymentRow
	UserEmail     string `gorm:"column:user_email"`
	BookingStatus string `gorm:"column:booking_status"`
	EventTitle    string `gorm:"column:event_title"`
}

func (r *AdminRepository) ListPayments(ctx context.Context, status string, limit, offset int) ([]AdminPaymentRow, int64, error) {
	var rows []AdminPaymentRow
	var total int64
	db := r.db.WithContext(ctx)
	if err := db.Raw(`SELECT COUNT(*) FROM payments p WHERE (? = '' OR p.status = ?)`, status, status).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Raw(`SELECT `+paymentCols+`, u.email AS user_email, b.status AS booking_status, e.title AS event_title
		FROM payments p
		JOIN bookings b ON b.id = p.booking_id
		JOIN users u ON u.id = b.user_id
		JOIN showtimes st ON st.id = b.showtime_id
		JOIN events e ON e.id = st.event_id
		WHERE (? = '' OR p.status = ?) ORDER BY p.created_at DESC, p.id LIMIT ? OFFSET ?`,
		status, status, limit, offset).Scan(&rows).Error
	return rows, total, err
}

// SetPaymentStatus changes only the status (refund completion keeps provider_ref, paid_at, failure fields).
func (r *AdminRepository) SetPaymentStatus(ctx context.Context, tx *gorm.DB, id, status string) error {
	return tx.WithContext(ctx).Exec(`UPDATE payments SET status = ? WHERE id = ?`, status, id).Error
}

// ---------- refunds ----------

type RefundRow struct {
	ID           string     `gorm:"column:id"`
	PaymentID    string     `gorm:"column:payment_id"`
	BookingID    string     `gorm:"column:booking_id"`
	AmountSatang int64      `gorm:"column:amount_satang"`
	Status       string     `gorm:"column:status"`
	ReasonCode   string     `gorm:"column:reason_code"`
	Note         string     `gorm:"column:note"`
	RequestedBy  string     `gorm:"column:requested_by"`
	ProcessedBy  *string    `gorm:"column:processed_by"`
	ProviderRef  string     `gorm:"column:provider_ref"`
	FailureCode  string     `gorm:"column:failure_code"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	CompletedAt  *time.Time `gorm:"column:completed_at"`
}

const refundCols = `id, payment_id, booking_id, amount_satang, status, reason_code, note, requested_by, processed_by,
	provider_ref, failure_code, created_at, completed_at`

func (r *AdminRepository) RefundByID(ctx context.Context, db *gorm.DB, id string) (*RefundRow, error) {
	var x RefundRow
	return one(db.WithContext(ctx).Raw(`SELECT `+refundCols+` FROM refunds WHERE id = ?`, id).Scan(&x), &x)
}

func (r *AdminRepository) LockRefund(ctx context.Context, tx *gorm.DB, id string) (*RefundRow, error) {
	var x RefundRow
	return one(tx.WithContext(ctx).Raw(`SELECT `+refundCols+` FROM refunds WHERE id = ? FOR UPDATE`, id).Scan(&x), &x)
}

func (r *AdminRepository) ListRefunds(ctx context.Context, status string, limit, offset int) ([]RefundRow, int64, error) {
	var rows []RefundRow
	var total int64
	db := r.db.WithContext(ctx)
	if err := db.Raw(`SELECT COUNT(*) FROM refunds WHERE (? = '' OR status = ?)`, status, status).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Raw(`SELECT `+refundCols+` FROM refunds WHERE (? = '' OR status = ?) ORDER BY created_at DESC, id LIMIT ? OFFSET ?`,
		status, status, limit, offset).Scan(&rows).Error
	return rows, total, err
}

func (r *AdminRepository) RefundsForBooking(ctx context.Context, bookingID string) ([]RefundRow, error) {
	var rows []RefundRow
	err := r.db.WithContext(ctx).Raw(`SELECT `+refundCols+` FROM refunds WHERE booking_id = ? ORDER BY created_at, id`, bookingID).Scan(&rows).Error
	return rows, err
}

// RefundIDByIdempotencyKey finds the refund created by an earlier request with the same key. The key is
// kept in the raw_payload of the REFUND_REQUESTED event (refunds has no idempotency column).
func (r *AdminRepository) RefundIDByIdempotencyKey(ctx context.Context, db *gorm.DB, key string) (string, error) {
	var id string
	err := db.WithContext(ctx).Raw(
		`SELECT raw_payload->>'refund_id' FROM payment_events
		  WHERE event_type = 'REFUND_REQUESTED' AND outcome = 'SUCCESS' AND raw_payload->>'idempotency_key' = ?
		  ORDER BY created_at LIMIT 1`, key).Scan(&id).Error
	return id, err
}

func (r *AdminRepository) HasActiveRefund(ctx context.Context, db *gorm.DB, paymentID string) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM refunds WHERE payment_id = ? AND status IN ('REQUESTED','COMPLETED')`, paymentID).Scan(&n).Error
	return n > 0, err
}

func (r *AdminRepository) CompletedRefundTotal(ctx context.Context, db *gorm.DB, paymentID string) (int64, error) {
	var n int64
	err := db.WithContext(ctx).Raw(
		`SELECT COALESCE(SUM(amount_satang), 0) FROM refunds WHERE payment_id = ? AND status = 'COMPLETED'`, paymentID).Scan(&n).Error
	return n, err
}

func (r *AdminRepository) InsertRefund(ctx context.Context, tx *gorm.DB, x RefundRow) (*RefundRow, error) {
	var out RefundRow
	err := tx.WithContext(ctx).Raw(
		`INSERT INTO refunds (payment_id, booking_id, amount_satang, status, reason_code, note, requested_by)
		 VALUES (?, ?, ?, 'REQUESTED', ?, ?, ?) RETURNING `+refundCols,
		x.PaymentID, x.BookingID, x.AmountSatang, x.ReasonCode, x.Note, x.RequestedBy).Scan(&out).Error
	return &out, err
}

func (r *AdminRepository) CompleteRefund(ctx context.Context, tx *gorm.DB, id, providerRef, processedBy string) (*RefundRow, error) {
	var out RefundRow
	err := tx.WithContext(ctx).Raw(
		`UPDATE refunds SET status = 'COMPLETED', provider_ref = ?, processed_by = ?, completed_at = now(), failure_code = ''
		  WHERE id = ? RETURNING `+refundCols, providerRef, processedBy, id).Scan(&out).Error
	return &out, err
}

func (r *AdminRepository) FailRefund(ctx context.Context, tx *gorm.DB, id, failureCode, processedBy string) (*RefundRow, error) {
	var out RefundRow
	err := tx.WithContext(ctx).Raw(
		`UPDATE refunds SET status = 'FAILED', failure_code = ?, processed_by = ?, provider_ref = '' WHERE id = ? RETURNING `+refundCols,
		failureCode, processedBy, id).Scan(&out).Error
	return &out, err
}

// ClaimRefundProcessing marks a REQUESTED refund as in-flight using provider_ref (no external I/O while rows are locked).
func (r *AdminRepository) ClaimRefundProcessing(ctx context.Context, tx *gorm.DB, id, marker string) (bool, error) {
	res := tx.WithContext(ctx).Exec(
		`UPDATE refunds SET provider_ref = ? WHERE id = ? AND status = 'REQUESTED' AND provider_ref = ''`, marker, id)
	return res.RowsAffected == 1, res.Error
}

// ClearRefundProcessingClaim releases an in-flight claim so process can be retried after a failure.
func (r *AdminRepository) ClearRefundProcessingClaim(ctx context.Context, db *gorm.DB, id, marker string) error {
	return db.WithContext(ctx).Exec(
		`UPDATE refunds SET provider_ref = '' WHERE id = ? AND status = 'REQUESTED' AND provider_ref = ?`, id, marker).Error
}

func (r *AdminRepository) HasUsedTicket(ctx context.Context, db *gorm.DB, bookingID string) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id
		  WHERE bi.booking_id = ? AND t.status = 'USED'`, bookingID).Scan(&n).Error
	return n > 0, err
}

func (r *AdminRepository) VoidTickets(ctx context.Context, tx *gorm.DB, bookingID string) (int64, error) {
	res := tx.WithContext(ctx).Exec(
		`UPDATE tickets t SET status = 'VOID' FROM booking_items bi
		  WHERE bi.id = t.booking_item_id AND bi.booking_id = ? AND t.status <> 'VOID'`, bookingID)
	return res.RowsAffected, res.Error
}

func (r *AdminRepository) ShowtimeStarted(ctx context.Context, db *gorm.DB, showtimeID string) (bool, error) {
	var started bool
	err := db.WithContext(ctx).Raw(`SELECT starts_at <= now() FROM showtimes WHERE id = ?`, showtimeID).Scan(&started).Error
	return started, err
}

// ---------- check-in ----------

// CheckIn marks a VALID ticket of a PAID booking as USED. The conditional UPDATE is atomic, so among
// concurrent check-ins of the same code exactly one gets true.
func (r *AdminRepository) CheckIn(ctx context.Context, db *gorm.DB, code string) (bool, error) {
	res := db.WithContext(ctx).Exec(
		`UPDATE tickets t SET status = 'USED', checked_in_at = now()
		   FROM booking_items bi, bookings b
		  WHERE t.code = ? AND t.status = 'VALID'
		    AND bi.id = t.booking_item_id AND b.id = bi.booking_id AND b.status = 'PAID'`, code)
	return res.RowsAffected == 1, res.Error
}

type TicketState struct {
	TicketStatus  string `gorm:"column:ticket_status"`
	BookingStatus string `gorm:"column:booking_status"`
}

func (r *AdminRepository) TicketState(ctx context.Context, code string) (*TicketState, error) {
	var s TicketState
	return one(r.db.WithContext(ctx).Raw(
		`SELECT t.status AS ticket_status, b.status AS booking_status
		   FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id JOIN bookings b ON b.id = bi.booking_id
		  WHERE t.code = ?`, code).Scan(&s), &s)
}

type TicketAuditRow struct {
	BookingID  string `gorm:"column:booking_id"`
	UserID     string `gorm:"column:user_id"`
	ShowtimeID string `gorm:"column:showtime_id"`
	Status     string `gorm:"column:status"`
}

func (r *AdminRepository) TicketAudit(ctx context.Context, db *gorm.DB, code string) (*TicketAuditRow, error) {
	var row TicketAuditRow
	return one(db.WithContext(ctx).Raw(
		`SELECT b.id AS booking_id, b.user_id, b.showtime_id, t.status
		   FROM tickets t
		   JOIN booking_items bi ON bi.id = t.booking_item_id
		   JOIN bookings b ON b.id = bi.booking_id
		  WHERE t.code = ?`, code).Scan(&row), &row)
}

func (r *AdminRepository) TicketByCode(ctx context.Context, db *gorm.DB, code string) (*TicketRow, error) {
	var t TicketRow
	return one(db.WithContext(ctx).Raw(ticketSelect+` WHERE t.code = ?`, code).Scan(&t), &t)
}

// ---------- stats ----------

type Stats struct {
	RevenueSatang    int64            `json:"revenue_satang"`
	TicketsSold      int64            `json:"tickets_sold"`
	BookingsByStatus map[string]int64 `json:"bookings_by_status"`
	PaymentsByStatus map[string]int64 `json:"payments_by_status"`
	NeedsRefundCount int64            `json:"needs_refund_count"`
	BookingsToday    int64            `json:"bookings_today"`
}

type statusCount struct {
	Status string `gorm:"column:status"`
	N      int64  `gorm:"column:n"`
}

func (r *AdminRepository) Stats(ctx context.Context) (*Stats, error) {
	db := r.db.WithContext(ctx)
	s := &Stats{BookingsByStatus: map[string]int64{}, PaymentsByStatus: map[string]int64{}}
	var head struct {
		Revenue       int64 `gorm:"column:revenue"`
		TicketsSold   int64 `gorm:"column:tickets_sold"`
		NeedsRefund   int64 `gorm:"column:needs_refund"`
		BookingsToday int64 `gorm:"column:bookings_today"`
	}
	err := db.Raw(`SELECT
		(SELECT COALESCE(SUM(amount_satang), 0) FROM payments WHERE status IN ('SUCCEEDED','REFUNDED'))
		 - (SELECT COALESCE(SUM(amount_satang), 0) FROM refunds WHERE status = 'COMPLETED') AS revenue,
		(SELECT COUNT(*) FROM tickets WHERE status IN ('VALID','USED')) AS tickets_sold,
		(SELECT COUNT(*) FROM payments WHERE status = 'NEEDS_REFUND') AS needs_refund,
		(SELECT COUNT(*) FROM bookings WHERE created_at >= date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS bookings_today`).
		Scan(&head).Error
	if err != nil {
		return nil, err
	}
	s.RevenueSatang, s.TicketsSold, s.NeedsRefundCount, s.BookingsToday = head.Revenue, head.TicketsSold, head.NeedsRefund, head.BookingsToday
	var bc, pc []statusCount
	if err := db.Raw(`SELECT status, COUNT(*) AS n FROM bookings GROUP BY status`).Scan(&bc).Error; err != nil {
		return nil, err
	}
	if err := db.Raw(`SELECT status, COUNT(*) AS n FROM payments GROUP BY status`).Scan(&pc).Error; err != nil {
		return nil, err
	}
	for _, c := range bc {
		s.BookingsByStatus[c.Status] = c.N
	}
	for _, c := range pc {
		s.PaymentsByStatus[c.Status] = c.N
	}
	return s, nil
}
