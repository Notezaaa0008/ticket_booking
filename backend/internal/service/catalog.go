package service

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"ticketbooking/internal/domain"
	"ticketbooking/internal/repository"
)

const eventsListCacheTTL = 30 * time.Second
const eventsListCachePrefix = "events:list:"
const catalogOpTimeout = 10 * time.Second

type CatalogService struct {
	repo *repository.CatalogRepository
	rdb  *redis.Client
}

func NewCatalogService(repo *repository.CatalogRepository, rdb *redis.Client) *CatalogService {
	return &CatalogService{repo: repo, rdb: rdb}
}

type EventListItem struct {
	ID             uuid.UUID `json:"id"`
	Title          string    `json:"title"`
	Venue          string    `json:"venue"`
	PosterURL      string    `json:"poster_url"`
	NextShowtimeAt time.Time `json:"next_showtime_at"`
	MinPriceSatang int64     `json:"min_price_satang"`
}

type EventListResponse struct {
	Items []EventListItem `json:"items"`
	Page  int             `json:"page"`
	Limit int             `json:"limit"`
	Total int64           `json:"total"`
}

type ListEventsInput struct {
	Q     string
	From  string
	To    string
	Page  string
	Limit string
}

type ShowtimeItem struct {
	ID                 uuid.UUID `json:"id"`
	StartsAt           time.Time `json:"starts_at"`
	Status             string    `json:"status"`
	AvailableSeatCount int64     `json:"available_seat_count"`
}

type EventDetailResponse struct {
	ID          uuid.UUID      `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Venue       string         `json:"venue"`
	PosterURL   string         `json:"poster_url"`
	Showtimes   []ShowtimeItem `json:"showtimes"`
}

type SeatItem struct {
	ID          uuid.UUID `json:"id"`
	RowLabel    string    `json:"row_label"`
	SeatNumber  int       `json:"seat_number"`
	Zone        string    `json:"zone"`
	PriceSatang int64     `json:"price_satang"`
	Status      string    `json:"status"`
}

type SeatSummary struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Held      int `json:"held"`
	Sold      int `json:"sold"`
}

type ShowtimeSeatsResponse struct {
	Seats   []SeatItem  `json:"seats"`
	Summary SeatSummary `json:"summary"`
}

func (s *CatalogService) ListEvents(ctx context.Context, in ListEventsInput) (*EventListResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogOpTimeout)
	defer cancel()
	page, limit, err := parsePageLimit(in.Page, in.Limit)
	if err != nil {
		return nil, err
	}
	from, to, err := parseDateRange(in.From, in.To)
	if err != nil {
		return nil, err
	}

	cacheKey := eventsListCachePrefix + normalizedListCacheKey(in.Q, in.From, in.To, page, limit)

	if s.rdb != nil {
		cached, cerr := s.rdb.Get(ctx, cacheKey).Bytes()
		if cerr == nil {
			var resp EventListResponse
			if json.Unmarshal(cached, &resp) == nil {
				return &resp, nil
			}
		} else if cerr != redis.Nil {
			log.Printf("catalog list cache get warning: %v", cerr)
		}
	}

	pattern := ""
	if strings.TrimSpace(in.Q) != "" {
		pattern = "%" + escapeILIKE(strings.TrimSpace(in.Q)) + "%"
	}

	offset := (page - 1) * limit
	rows, total, err := s.repo.ListEvents(ctx, pattern, from, to, limit, offset)
	if err != nil {
		return nil, err
	}

	items := make([]EventListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, EventListItem{
			ID:             row.ID,
			Title:          row.Title,
			Venue:          row.Venue,
			PosterURL:      row.PosterURL,
			NextShowtimeAt: row.NextShowtimeAt,
			MinPriceSatang: row.MinPriceSatang,
		})
	}

	resp := &EventListResponse{Items: items, Page: page, Limit: limit, Total: total}

	if s.rdb != nil {
		if b, err := json.Marshal(resp); err == nil {
			if err := s.rdb.Set(ctx, cacheKey, b, eventsListCacheTTL).Err(); err != nil {
				log.Printf("catalog list cache set warning: %v", err)
			}
		}
	}

	return resp, nil
}

func (s *CatalogService) GetEvent(ctx context.Context, id uuid.UUID) (*EventDetailResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogOpTimeout)
	defer cancel()
	ev, err := s.repo.GetPublishedEvent(ctx, id)
	if err != nil {
		return nil, err
	}
	if ev == nil {
		return nil, domain.NewError(domain.ReasonNotFound, "event not found")
	}

	showtimes, err := s.repo.ListUpcomingShowtimes(ctx, id)
	if err != nil {
		return nil, err
	}

	stItems := make([]ShowtimeItem, 0, len(showtimes))
	for _, st := range showtimes {
		stItems = append(stItems, ShowtimeItem{
			ID:                 st.ID,
			StartsAt:           st.StartsAt,
			Status:             st.Status,
			AvailableSeatCount: st.AvailableSeatCount,
		})
	}

	return &EventDetailResponse{
		ID:          ev.ID,
		Title:       ev.Title,
		Description: ev.Description,
		Venue:       ev.Venue,
		PosterURL:   ev.PosterURL,
		Showtimes:   stItems,
	}, nil
}

func (s *CatalogService) GetShowtimeSeats(ctx context.Context, showtimeID uuid.UUID) (*ShowtimeSeatsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogOpTimeout)
	defer cancel()
	ok, err := s.repo.ShowtimeExists(ctx, showtimeID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.NewError(domain.ReasonNotFound, "showtime not found")
	}

	rows, err := s.repo.ListShowtimeSeats(ctx, showtimeID)
	if err != nil {
		return nil, err
	}

	var summary SeatSummary
	seats := make([]SeatItem, 0, len(rows))
	for _, row := range rows {
		seats = append(seats, SeatItem{
			ID:          row.ID,
			RowLabel:    row.RowLabel,
			SeatNumber:  row.SeatNumber,
			Zone:        row.Zone,
			PriceSatang: row.PriceSatang,
			Status:      row.Status,
		})
		summary.Total++
		switch row.Status {
		case "available":
			summary.Available++
		case "held":
			summary.Held++
		case "sold":
			summary.Sold++
		}
	}

	return &ShowtimeSeatsResponse{Seats: seats, Summary: summary}, nil
}

func parsePageLimit(pageStr, limitStr string) (page, limit int, err error) {
	page = 1
	limit = 20
	if pageStr != "" {
		page, err = strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			return 0, 0, domain.NewError(domain.ReasonValidationFailed, "invalid page")
		}
	}
	if limitStr != "" {
		limit, err = strconv.Atoi(limitStr)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, domain.NewError(domain.ReasonValidationFailed, "invalid limit")
		}
	}
	return page, limit, nil
}

func parseDateRange(fromStr, toStr string) (from, to *time.Time, err error) {
	if fromStr != "" {
		t, e := parseDateParam(fromStr, false)
		if e != nil {
			return nil, nil, domain.NewError(domain.ReasonValidationFailed, "invalid from date")
		}
		from = &t
	}
	if toStr != "" {
		t, e := parseDateParam(toStr, true)
		if e != nil {
			return nil, nil, domain.NewError(domain.ReasonValidationFailed, "invalid to date")
		}
		to = &t
	}
	return from, to, nil
}

func parseDateParam(s string, endOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	t = t.UTC()
	if endOfDay {
		return t.Add(24*time.Hour - time.Nanosecond), nil
	}
	return t, nil
}

func escapeILIKE(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func normalizedListCacheKey(q, from, to string, page, limit int) string {
	parts := []string{
		"from=" + strings.TrimSpace(from),
		"limit=" + strconv.Itoa(limit),
		"page=" + strconv.Itoa(page),
		"q=" + strings.TrimSpace(q),
		"to=" + strings.TrimSpace(to),
	}
	return strings.Join(parts, "&")
}
