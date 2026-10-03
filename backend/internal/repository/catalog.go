package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CatalogRepository struct {
	db *gorm.DB
}

func NewCatalogRepository(db *gorm.DB) *CatalogRepository {
	return &CatalogRepository{db: db}
}

type EventListRow struct {
	ID             uuid.UUID `gorm:"column:id"`
	Title          string    `gorm:"column:title"`
	Venue          string    `gorm:"column:venue"`
	PosterURL      string    `gorm:"column:poster_url"`
	NextShowtimeAt time.Time `gorm:"column:next_showtime_at"`
	MinPriceSatang int64     `gorm:"column:min_price_satang"`
}

type EventDetailRow struct {
	ID          uuid.UUID `gorm:"column:id"`
	Title       string    `gorm:"column:title"`
	Description string    `gorm:"column:description"`
	Venue       string    `gorm:"column:venue"`
	PosterURL   string    `gorm:"column:poster_url"`
}

type ShowtimeRow struct {
	ID                 uuid.UUID `gorm:"column:id"`
	StartsAt           time.Time `gorm:"column:starts_at"`
	Status             string    `gorm:"column:status"`
	AvailableSeatCount int64     `gorm:"column:available_seat_count"`
}

type SeatRow struct {
	ID          uuid.UUID `gorm:"column:id"`
	RowLabel    string    `gorm:"column:row_label"`
	SeatNumber  int       `gorm:"column:seat_number"`
	Zone        string    `gorm:"column:zone"`
	PriceSatang int64     `gorm:"column:price_satang"`
	Status      string    `gorm:"column:status"`
}

const seatStatusCase = `CASE WHEN b.status IN ('PAID','REFUNDED') THEN 'sold'
          WHEN b.status = 'PENDING' AND b.expires_at > now() THEN 'held'
          ELSE 'available' END`

const qualifyingShowtime = `st.status = 'on_sale' AND st.starts_at > now()`

func (r *CatalogRepository) ListEvents(ctx context.Context, q string, from, to *time.Time, limit, offset int) ([]EventListRow, int64, error) {
	where := `e.status = 'published' AND ` + qualifyingShowtime
	args := []any{}
	argN := 1

	if from != nil {
		where += ` AND st.starts_at >= $` + itoa(argN)
		args = append(args, *from)
		argN++
	}
	if to != nil {
		where += ` AND st.starts_at <= $` + itoa(argN)
		args = append(args, *to)
		argN++
	}
	if q != "" {
		where += ` AND (e.title ILIKE $` + itoa(argN) + ` ESCAPE '\' OR e.venue ILIKE $` + itoa(argN) + ` ESCAPE '\')`
		args = append(args, q)
		argN++
	}

	countSQL := `SELECT COUNT(DISTINCT e.id) FROM events e
JOIN showtimes st ON st.event_id = e.id
LEFT JOIN seats s ON s.showtime_id = st.id
WHERE ` + where

	var total int64
	if err := r.db.WithContext(ctx).Raw(countSQL, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	listSQL := `SELECT e.id, e.title, e.venue, e.poster_url,
       MIN(st.starts_at) AS next_showtime_at,
       MIN(s.price_satang) AS min_price_satang
FROM events e
JOIN showtimes st ON st.event_id = e.id
LEFT JOIN seats s ON s.showtime_id = st.id
WHERE ` + where + `
GROUP BY e.id, e.title, e.venue, e.poster_url
ORDER BY next_showtime_at
LIMIT $` + itoa(argN) + ` OFFSET $` + itoa(argN+1)

	listArgs := append(append([]any{}, args...), limit, offset)
	var rows []EventListRow
	if err := r.db.WithContext(ctx).Raw(listSQL, listArgs...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	if rows == nil {
		rows = []EventListRow{}
	}
	return rows, total, nil
}

func (r *CatalogRepository) GetPublishedEvent(ctx context.Context, id uuid.UUID) (*EventDetailRow, error) {
	var row EventDetailRow
	tx := r.db.WithContext(ctx).Raw(
		`SELECT id, title, description, venue, poster_url FROM events WHERE id = $1 AND status = 'published'`,
		id,
	).Scan(&row)
	if tx.Error != nil {
		return nil, tx.Error
	}
	if tx.RowsAffected == 0 {
		return nil, nil
	}
	return &row, nil
}

func (r *CatalogRepository) ListUpcomingShowtimes(ctx context.Context, eventID uuid.UUID) ([]ShowtimeRow, error) {
	sql := `SELECT st.id, st.starts_at, st.status,
  (SELECT COUNT(*)::bigint FROM seats s
   LEFT JOIN booking_items bi ON bi.seat_id = s.id AND bi.active
   LEFT JOIN bookings b ON b.id = bi.booking_id
   WHERE s.showtime_id = st.id AND (` + seatStatusCase + `) = 'available') AS available_seat_count
FROM showtimes st
WHERE st.event_id = $1 AND ` + qualifyingShowtime + `
ORDER BY st.starts_at`

	var rows []ShowtimeRow
	if err := r.db.WithContext(ctx).Raw(sql, eventID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []ShowtimeRow{}
	}
	return rows, nil
}

func (r *CatalogRepository) ShowtimeExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var n int64
	tx := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM showtimes WHERE id = $1`, id).Scan(&n)
	if tx.Error != nil {
		return false, tx.Error
	}
	return n > 0, nil
}

func (r *CatalogRepository) ListShowtimeSeats(ctx context.Context, showtimeID uuid.UUID) ([]SeatRow, error) {
	sql := `SELECT s.id, s.row_label, s.seat_number, s.zone, s.price_satang,
     ` + seatStatusCase + ` AS status
   FROM seats s
   LEFT JOIN booking_items bi ON bi.seat_id = s.id AND bi.active
   LEFT JOIN bookings b ON b.id = bi.booking_id
   WHERE s.showtime_id = $1 ORDER BY s.row_label, s.seat_number`

	var rows []SeatRow
	if err := r.db.WithContext(ctx).Raw(sql, showtimeID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []SeatRow{}
	}
	return rows, nil
}

func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = digits[n%10]
		n /= 10
	}
	return string(b[i:])
}
