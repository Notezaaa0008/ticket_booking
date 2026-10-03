package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// PaymentRepository holds SQL for payments and tickets. Like BookingRepository, methods taking a
// *gorm.DB run on the plain connection or an open transaction.
type PaymentRepository struct {
	db *gorm.DB
}

func NewPaymentRepository(db *gorm.DB) *PaymentRepository { return &PaymentRepository{db: db} }

func (r *PaymentRepository) DB() *gorm.DB { return r.db }

func (r *PaymentRepository) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

type PaymentRow struct {
	ID             string     `gorm:"column:id"`
	BookingID      string     `gorm:"column:booking_id"`
	AmountSatang   int64      `gorm:"column:amount_satang"`
	Status         string     `gorm:"column:status"`
	ProviderRef    string     `gorm:"column:provider_ref"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	FailureCode    string     `gorm:"column:failure_code"`
	FailureMessage string     `gorm:"column:failure_message"`
	PaidAt         *time.Time `gorm:"column:paid_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
}

const paymentCols = `p.id, p.booking_id, p.amount_satang, p.status, p.provider_ref, p.idempotency_key,
	p.failure_code, p.failure_message, p.paid_at, p.created_at`

func scanOnePayment(res *gorm.DB, p *PaymentRow) (*PaymentRow, error) {
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return p, nil
}

// LockPayment locks a payment row (waits for other holders); nil, nil when it does not exist.
func (r *PaymentRepository) LockPayment(ctx context.Context, tx *gorm.DB, id string) (*PaymentRow, error) {
	var p PaymentRow
	return scanOnePayment(tx.WithContext(ctx).Raw(`SELECT `+paymentCols+` FROM payments p WHERE p.id = ? FOR UPDATE`, id).Scan(&p), &p)
}

func (r *PaymentRepository) ByIdempotencyKey(ctx context.Context, db *gorm.DB, key string) (*PaymentRow, error) {
	var p PaymentRow
	return scanOnePayment(db.WithContext(ctx).Raw(`SELECT `+paymentCols+` FROM payments p WHERE p.idempotency_key = ?`, key).Scan(&p), &p)
}

func (r *PaymentRepository) ByID(ctx context.Context, id string) (*PaymentRow, error) {
	var p PaymentRow
	return scanOnePayment(r.db.WithContext(ctx).Raw(`SELECT `+paymentCols+` FROM payments p WHERE p.id = ?`, id).Scan(&p), &p)
}

// GetForUser returns nil, nil when the payment does not exist or its booking belongs to someone else.
func (r *PaymentRepository) GetForUser(ctx context.Context, id, userID string) (*PaymentRow, error) {
	var p PaymentRow
	return scanOnePayment(r.db.WithContext(ctx).Raw(
		`SELECT `+paymentCols+` FROM payments p JOIN bookings b ON b.id = p.booking_id
		  WHERE p.id = ? AND b.user_id = ?`, id, userID).Scan(&p), &p)
}

func (r *PaymentRepository) LatestForBooking(ctx context.Context, bookingID string) (*PaymentRow, error) {
	var p PaymentRow
	return scanOnePayment(r.db.WithContext(ctx).Raw(
		`SELECT `+paymentCols+` FROM payments p WHERE p.booking_id = ?
		  ORDER BY p.created_at DESC, p.id DESC LIMIT 1`, bookingID).Scan(&p), &p)
}

func (r *PaymentRepository) HasPending(ctx context.Context, db *gorm.DB, bookingID string) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM payments WHERE booking_id = ? AND status = 'PENDING'`, bookingID).Scan(&n).Error
	return n > 0, err
}

// ExpiredByDB reports whether the booking's hold has run out according to the database clock.
func (r *PaymentRepository) ExpiredByDB(ctx context.Context, db *gorm.DB, bookingID string) (bool, error) {
	var expired bool
	err := db.WithContext(ctx).Raw(`SELECT expires_at <= now() FROM bookings WHERE id = ?`, bookingID).Scan(&expired).Error
	return expired, err
}

type ItemStats struct {
	Total       int64 `gorm:"column:total"`
	Count       int   `gorm:"column:n"`
	ActiveCount int   `gorm:"column:active_n"`
}

// ItemStats sums the DB prices of a booking's items and counts how many are still active.
func (r *PaymentRepository) ItemStats(ctx context.Context, db *gorm.DB, bookingID string) (ItemStats, error) {
	var s ItemStats
	err := db.WithContext(ctx).Raw(
		`SELECT COALESCE(SUM(price_satang), 0) AS total, COUNT(*) AS n, COUNT(*) FILTER (WHERE active) AS active_n
		   FROM booking_items WHERE booking_id = ?`, bookingID).Scan(&s).Error
	return s, err
}

func (r *PaymentRepository) InsertPayment(ctx context.Context, tx *gorm.DB, bookingID string, amount int64, key string) (*PaymentRow, error) {
	var p PaymentRow
	err := tx.WithContext(ctx).Raw(
		`INSERT INTO payments AS p (booking_id, amount_satang, status, idempotency_key)
		 VALUES (?, ?, 'PENDING', ?) RETURNING `+paymentCols, bookingID, amount, key).Scan(&p).Error
	return &p, err
}

// SetOutcome moves a payment to a terminal status; paid_at is set by the DB clock when paid is true.
func (r *PaymentRepository) SetOutcome(ctx context.Context, tx *gorm.DB, id, status, providerRef, failureCode, failureMessage string, paid bool) error {
	return tx.WithContext(ctx).Exec(
		`UPDATE payments SET status = ?, provider_ref = ?, failure_code = ?, failure_message = ?,
		        paid_at = CASE WHEN ? THEN now() ELSE paid_at END
		  WHERE id = ?`, status, providerRef, failureCode, failureMessage, paid, id).Error
}

// ActiveItemIDs returns the booking_items ids of a booking that still hold their seat.
func (r *PaymentRepository) ActiveItemIDs(ctx context.Context, tx *gorm.DB, bookingID string) ([]string, error) {
	var ids []string
	err := tx.WithContext(ctx).Raw(
		`SELECT id FROM booking_items WHERE booking_id = ? AND active ORDER BY id`, bookingID).Scan(&ids).Error
	return ids, err
}

func (r *PaymentRepository) InsertTicket(ctx context.Context, tx *gorm.DB, itemID, code string) error {
	return tx.WithContext(ctx).Exec(`INSERT INTO tickets (booking_item_id, code) VALUES (?, ?)`, itemID, code).Error
}

type TicketRow struct {
	Code       string    `gorm:"column:code"`
	Status     string    `gorm:"column:status"`
	IssuedAt   time.Time `gorm:"column:issued_at"`
	BookingID  string    `gorm:"column:booking_id"`
	EventTitle string    `gorm:"column:event_title"`
	Venue      string    `gorm:"column:venue"`
	StartsAt   time.Time `gorm:"column:starts_at"`
	RowLabel   string    `gorm:"column:row_label"`
	SeatNumber int       `gorm:"column:seat_number"`
	Zone       string    `gorm:"column:zone"`
}

const ticketSelect = `SELECT t.code, t.status, t.issued_at, b.id AS booking_id, e.title AS event_title, e.venue,
	st.starts_at, s.row_label, s.seat_number, s.zone
	FROM tickets t
	JOIN booking_items bi ON bi.id = t.booking_item_id
	JOIN bookings b ON b.id = bi.booking_id
	JOIN seats s ON s.id = bi.seat_id
	JOIN showtimes st ON st.id = b.showtime_id
	JOIN events e ON e.id = st.event_id`

func (r *PaymentRepository) TicketsForUser(ctx context.Context, userID string) ([]TicketRow, error) {
	var rows []TicketRow
	err := r.db.WithContext(ctx).Raw(ticketSelect+` WHERE b.user_id = ?
		ORDER BY st.starts_at, s.row_label, s.seat_number LIMIT 200`, userID).Scan(&rows).Error
	return rows, err
}

func (r *PaymentRepository) TicketsForBooking(ctx context.Context, bookingID string) ([]TicketRow, error) {
	var rows []TicketRow
	err := r.db.WithContext(ctx).Raw(ticketSelect+` WHERE b.id = ? ORDER BY s.row_label, s.seat_number`, bookingID).Scan(&rows).Error
	return rows, err
}

// TicketByCodeForUser returns nil, nil when the code is unknown or the ticket belongs to someone else.
func (r *PaymentRepository) TicketByCodeForUser(ctx context.Context, code, userID string) (*TicketRow, error) {
	var t TicketRow
	res := r.db.WithContext(ctx).Raw(ticketSelect+` WHERE t.code = ? AND b.user_id = ?`, code, userID).Scan(&t)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &t, nil
}
