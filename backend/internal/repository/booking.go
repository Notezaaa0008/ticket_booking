package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// BookingRepository holds SQL for bookings. Methods that take a *gorm.DB work on either the
// plain connection or an open transaction, so the service decides the transaction boundary.
type BookingRepository struct {
	db *gorm.DB
}

func NewBookingRepository(db *gorm.DB) *BookingRepository { return &BookingRepository{db: db} }

// DB is the plain (non-transactional) connection.
func (r *BookingRepository) DB() *gorm.DB { return r.db }

func (r *BookingRepository) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

type ShowtimeInfo struct {
	ID       string    `gorm:"column:id"`
	Status   string    `gorm:"column:status"`
	StartsAt time.Time `gorm:"column:starts_at"`
}

// GetShowtime returns nil, nil when the showtime does not exist.
func (r *BookingRepository) GetShowtime(ctx context.Context, db *gorm.DB, id string) (*ShowtimeInfo, error) {
	var s ShowtimeInfo
	tx := db.WithContext(ctx).Raw(`SELECT id, status, starts_at FROM showtimes WHERE id = ?`, id).Scan(&s)
	if tx.Error != nil {
		return nil, tx.Error
	}
	if tx.RowsAffected == 0 {
		return nil, nil
	}
	return &s, nil
}

type SeatPrice struct {
	ID          string `gorm:"column:id"`
	ShowtimeID  string `gorm:"column:showtime_id"`
	PriceSatang int64  `gorm:"column:price_satang"`
}

// SeatsByIDs reads seats and their prices; prices only ever come from here.
func (r *BookingRepository) SeatsByIDs(ctx context.Context, db *gorm.DB, ids []string) ([]SeatPrice, error) {
	var rows []SeatPrice
	err := db.WithContext(ctx).Raw(`SELECT id, showtime_id, price_satang FROM seats WHERE id IN ?`, ids).Scan(&rows).Error
	return rows, err
}

// HasLivePending reports whether the user has a PENDING, not yet expired booking for the showtime.
func (r *BookingRepository) HasLivePending(ctx context.Context, db *gorm.DB, userID, showtimeID string) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM bookings
		  WHERE user_id = ? AND showtime_id = ? AND status = 'PENDING' AND expires_at > now()`,
		userID, showtimeID).Scan(&n).Error
	return n > 0, err
}

// LockUserShowtime serialises concurrent creates by one user for one showtime until the
// transaction ends, so the "one pending booking per showtime" rule cannot be raced.
func (r *BookingRepository) LockUserShowtime(ctx context.Context, tx *gorm.DB, userID, showtimeID string) error {
	return tx.WithContext(ctx).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, userID+":"+showtimeID).Error
}

type BookingRow struct {
	ID          string    `gorm:"column:id"`
	UserID      string    `gorm:"column:user_id"`
	ShowtimeID  string    `gorm:"column:showtime_id"`
	Status      string    `gorm:"column:status"`
	TotalSatang int64     `gorm:"column:total_satang"`
	ExpiresAt   time.Time `gorm:"column:expires_at"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

const bookingCols = `id, user_id, showtime_id, status, total_satang, expires_at, created_at`

// LockStaleBlocking locks (SKIP LOCKED, fixed order) PENDING bookings whose hold has run out
// but that still own an active item on one of the seats. The caller expires them.
func (r *BookingRepository) LockStaleBlocking(ctx context.Context, tx *gorm.DB, seatIDs []string) ([]BookingRow, error) {
	var rows []BookingRow
	err := tx.WithContext(ctx).Raw(
		`SELECT `+bookingCols+` FROM bookings
		  WHERE status = 'PENDING' AND expires_at <= now()
		    AND id IN (SELECT booking_id FROM booking_items WHERE active AND seat_id IN ?)
		  ORDER BY id FOR UPDATE SKIP LOCKED`, seatIDs).Scan(&rows).Error
	return rows, err
}

// DueBookingIDs lists PENDING bookings whose hold expired (no locks; each is re-checked under lock).
func (r *BookingRepository) DueBookingIDs(ctx context.Context, limit int) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Raw(
		`SELECT id FROM bookings WHERE status = 'PENDING' AND expires_at <= now()
		  ORDER BY expires_at LIMIT ?`, limit).Scan(&ids).Error
	return ids, err
}

// LockDue locks one expired PENDING booking; nil, nil when it is gone, no longer due, or locked elsewhere.
func (r *BookingRepository) LockDue(ctx context.Context, tx *gorm.DB, id string) (*BookingRow, error) {
	return r.lockOne(ctx, tx,
		`SELECT `+bookingCols+` FROM bookings
		  WHERE id = ? AND status = 'PENDING' AND expires_at <= now() FOR UPDATE SKIP LOCKED`, id)
}

// Lock locks a booking row (waits for other holders); nil, nil when it does not exist.
func (r *BookingRepository) Lock(ctx context.Context, tx *gorm.DB, id string) (*BookingRow, error) {
	return r.lockOne(ctx, tx, `SELECT `+bookingCols+` FROM bookings WHERE id = ? FOR UPDATE`, id)
}

func (r *BookingRepository) lockOne(ctx context.Context, tx *gorm.DB, sql, id string) (*BookingRow, error) {
	var b BookingRow
	res := tx.WithContext(ctx).Raw(sql, id).Scan(&b)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &b, nil
}

func (r *BookingRepository) SetStatus(ctx context.Context, tx *gorm.DB, id, status string) error {
	return tx.WithContext(ctx).Exec(`UPDATE bookings SET status = ? WHERE id = ?`, status, id).Error
}

func (r *BookingRepository) DeactivateItems(ctx context.Context, tx *gorm.DB, bookingID string) error {
	return tx.WithContext(ctx).Exec(`UPDATE booking_items SET active = false WHERE booking_id = ?`, bookingID).Error
}

func (r *BookingRepository) SeatIDsOfBooking(ctx context.Context, db *gorm.DB, bookingID string) ([]string, error) {
	var ids []string
	err := db.WithContext(ctx).Raw(`SELECT seat_id FROM booking_items WHERE booking_id = ? ORDER BY seat_id`, bookingID).Scan(&ids).Error
	return ids, err
}

// InsertBooking creates a PENDING booking; expires_at is computed by the database clock.
func (r *BookingRepository) InsertBooking(ctx context.Context, tx *gorm.DB, id, userID, showtimeID string, total int64, ttlSeconds int) (*BookingRow, error) {
	var b BookingRow
	err := tx.WithContext(ctx).Raw(
		`INSERT INTO bookings (id, user_id, showtime_id, status, total_satang, expires_at)
		 VALUES (?, ?, ?, 'PENDING', ?, now() + (?::int) * interval '1 second')
		 RETURNING `+bookingCols, id, userID, showtimeID, total, ttlSeconds).Scan(&b).Error
	return &b, err
}

func (r *BookingRepository) InsertItem(ctx context.Context, tx *gorm.DB, bookingID, seatID string, price int64) error {
	return tx.WithContext(ctx).Exec(
		`INSERT INTO booking_items (booking_id, seat_id, price_satang, active) VALUES (?, ?, ?, true)`,
		bookingID, seatID, price).Error
}

// ActiveSeatIDs returns those of seatIDs that currently have an active booking item.
func (r *BookingRepository) ActiveSeatIDs(ctx context.Context, db *gorm.DB, seatIDs []string) ([]string, error) {
	var ids []string
	err := db.WithContext(ctx).Raw(
		`SELECT seat_id FROM booking_items WHERE active AND seat_id IN ? ORDER BY seat_id`, seatIDs).Scan(&ids).Error
	return ids, err
}

type BookingDetailRow struct {
	ID          string    `gorm:"column:id"`
	ShowtimeID  string    `gorm:"column:showtime_id"`
	Status      string    `gorm:"column:status"`
	TotalSatang int64     `gorm:"column:total_satang"`
	ExpiresAt   time.Time `gorm:"column:expires_at"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	EventTitle  string    `gorm:"column:event_title"`
	Venue       string    `gorm:"column:venue"`
	StartsAt    time.Time `gorm:"column:starts_at"`
}

const detailSelect = `SELECT b.id, b.showtime_id, b.status, b.total_satang, b.expires_at, b.created_at,
	e.title AS event_title, e.venue, st.starts_at
	FROM bookings b JOIN showtimes st ON st.id = b.showtime_id JOIN events e ON e.id = st.event_id`

// GetForUser returns nil, nil when the booking does not exist or belongs to someone else.
func (r *BookingRepository) GetForUser(ctx context.Context, id, userID string) (*BookingDetailRow, error) {
	var d BookingDetailRow
	res := r.db.WithContext(ctx).Raw(detailSelect+` WHERE b.id = ? AND b.user_id = ?`, id, userID).Scan(&d)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &d, nil
}

func (r *BookingRepository) ListForUser(ctx context.Context, userID string) ([]BookingDetailRow, error) {
	var rows []BookingDetailRow
	err := r.db.WithContext(ctx).Raw(detailSelect+` WHERE b.user_id = ? ORDER BY b.created_at DESC LIMIT 100`, userID).Scan(&rows).Error
	return rows, err
}

type ItemRow struct {
	BookingID   string `gorm:"column:booking_id"`
	SeatID      string `gorm:"column:seat_id"`
	RowLabel    string `gorm:"column:row_label"`
	SeatNumber  int    `gorm:"column:seat_number"`
	Zone        string `gorm:"column:zone"`
	PriceSatang int64  `gorm:"column:price_satang"`
}

func (r *BookingRepository) ItemsForBookings(ctx context.Context, bookingIDs []string) ([]ItemRow, error) {
	var rows []ItemRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT bi.booking_id, bi.seat_id, s.row_label, s.seat_number, s.zone, bi.price_satang
		   FROM booking_items bi JOIN seats s ON s.id = bi.seat_id
		  WHERE bi.booking_id IN ? ORDER BY s.row_label, s.seat_number`, bookingIDs).Scan(&rows).Error
	return rows, err
}

type reasonRow struct {
	BookingID  string `gorm:"column:booking_id"`
	ReasonCode string `gorm:"column:reason_code"`
}

// LatestReasons maps booking id -> reason_code of its latest EXPIRED/CANCELLED event.
func (r *BookingRepository) LatestReasons(ctx context.Context, bookingIDs []string) (map[string]string, error) {
	var rows []reasonRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT DISTINCT ON (booking_id) booking_id, reason_code FROM booking_events
		  WHERE booking_id IN ? AND event_type IN ('BOOKING_EXPIRED','BOOKING_CANCELLED')
		  ORDER BY booking_id, created_at DESC`, bookingIDs).Scan(&rows).Error
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.BookingID] = row.ReasonCode
	}
	return out, err
}
