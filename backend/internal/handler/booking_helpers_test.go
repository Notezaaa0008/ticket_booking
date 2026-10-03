package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ticketbooking/internal/middleware"
	"ticketbooking/internal/repository"
	"ticketbooking/internal/service"
)

const (
	bkEvent      = "e0000000-0000-0000-0000-000000000001"
	bkShowtime   = "a0000000-0000-0000-0000-000000000001"
	bkShowtime2  = "a0000000-0000-0000-0000-000000000002"
	bkClosedShow = "a0000000-0000-0000-0000-000000000003"
	bkPastShow   = "a0000000-0000-0000-0000-000000000004"
	bkSeatA1     = "b0000000-0000-0000-0000-000000000001" // 10000 satang
	bkSeatA2     = "b0000000-0000-0000-0000-000000000002" // 20000
	bkSeatA3     = "b0000000-0000-0000-0000-000000000003" // 30000
	bkSeatA4     = "b0000000-0000-0000-0000-000000000004"
	bkSeatA5     = "b0000000-0000-0000-0000-000000000005"
	bkSeatA6     = "b0000000-0000-0000-0000-000000000006"
	bkSeatA7     = "b0000000-0000-0000-0000-000000000007"
	bkSeatOther  = "b0000000-0000-0000-0000-0000000000b1" // belongs to bkShowtime2
	bkSeatClosed = "b0000000-0000-0000-0000-0000000000c1"
	bkSeatPast   = "b0000000-0000-0000-0000-0000000000d1"
	bkSecret     = "integration-test-secret-integration-test-secret"
)

type bkOpts struct {
	ttl          time.Duration
	rdb          *redis.Client // override for hold + rate limiter (e.g. unreachable Redis)
	bookingLimit int
	loginLimit   int
}

type bkEnv struct {
	gdb  *gorm.DB
	rdb  *redis.Client
	r    *gin.Engine
	auth *service.AuthService
	svc  *service.BookingService
}

// newBkEnv resets the test database and Redis keys, seeds fixtures and builds the router.
func newBkEnv(t *testing.T, o bkOpts) *bkEnv {
	t.Helper()
	gdb, realRDB := requireIntegration(t)
	if o.ttl == 0 {
		o.ttl = 10 * time.Minute
	}
	if o.bookingLimit == 0 {
		o.bookingLimit = 1000
	}
	if o.loginLimit == 0 {
		o.loginLimit = 1000
	}
	rdb := o.rdb
	if rdb == nil {
		rdb = realRDB
	}
	resetCatalogData(t, gdb)
	flushRedisPrefix(t, realRDB, "hold:*", "rl:*")
	seedBookingFixtures(t, gdb)

	auth := service.NewAuthService(repository.NewUserRepository(gdb), bkSecret)
	svc := service.NewBookingService(repository.NewBookingRepository(gdb), rdb, o.ttl)
	limiter := middleware.NewRateLimiter(rdb)
	r := gin.New()
	r.Use(middleware.RequestID())
	RegisterAuthBookingRoutes(r.Group("/api/v1"), AuthBookingDeps{
		Auth:         NewAuthHandler(auth, limiter, o.loginLimit),
		Booking:      NewBookingHandler(svc),
		Verify:       auth.VerifyToken,
		Limiter:      limiter,
		BookingLimit: o.bookingLimit,
	})
	return &bkEnv{gdb: gdb, rdb: realRDB, r: r, auth: auth, svc: svc}
}

func flushRedisPrefix(t *testing.T, rdb *redis.Client, patterns ...string) {
	t.Helper()
	ctx := context.Background()
	for _, p := range patterns {
		iter := rdb.Scan(ctx, 0, p, 200).Iterator()
		for iter.Next(ctx) {
			require.NoError(t, rdb.Del(ctx, iter.Val()).Err())
		}
		require.NoError(t, iter.Err())
	}
}

func seedBookingFixtures(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	sql := `
INSERT INTO events (id, title, venue, status) VALUES ('` + bkEvent + `', 'Test Event', 'Test Venue', 'published');
INSERT INTO showtimes (id, event_id, starts_at, status) VALUES
  ('` + bkShowtime + `',   '` + bkEvent + `', now() + interval '30 days', 'on_sale'),
  ('` + bkShowtime2 + `',  '` + bkEvent + `', now() + interval '31 days', 'on_sale'),
  ('` + bkClosedShow + `', '` + bkEvent + `', now() + interval '32 days', 'closed'),
  ('` + bkPastShow + `',   '` + bkEvent + `', now() - interval '1 day',   'on_sale');
INSERT INTO seats (id, showtime_id, row_label, seat_number, zone, price_satang) VALUES
  ('` + bkSeatA1 + `', '` + bkShowtime + `', 'A', 1, 'standard', 10000),
  ('` + bkSeatA2 + `', '` + bkShowtime + `', 'A', 2, 'standard', 20000),
  ('` + bkSeatA3 + `', '` + bkShowtime + `', 'A', 3, 'standard', 30000),
  ('` + bkSeatA4 + `', '` + bkShowtime + `', 'A', 4, 'standard', 40000),
  ('` + bkSeatA5 + `', '` + bkShowtime + `', 'A', 5, 'standard', 50000),
  ('` + bkSeatA6 + `', '` + bkShowtime + `', 'A', 6, 'standard', 60000),
  ('` + bkSeatA7 + `', '` + bkShowtime + `', 'A', 7, 'standard', 70000),
  ('` + bkSeatOther + `',  '` + bkShowtime2 + `',  'B', 1, 'standard', 10000),
  ('` + bkSeatClosed + `', '` + bkClosedShow + `', 'C', 1, 'standard', 10000),
  ('` + bkSeatPast + `',   '` + bkPastShow + `',   'D', 1, 'standard', 10000);`
	require.NoError(t, gdb.Exec(sql).Error)
}

// newUser inserts a user directly (fast) and returns its id and a valid token.
func (e *bkEnv) newUser(t *testing.T, email, role string) (id, token string) {
	t.Helper()
	require.NoError(t, e.gdb.Raw(
		`INSERT INTO users (email, password_hash, name, role) VALUES (?, 'x', 'Test', ?) RETURNING id`,
		email, role).Scan(&id).Error)
	tok, err := e.auth.IssueToken(id, role, time.Now())
	require.NoError(t, err)
	return id, tok
}

// do sends a request through the router. body may be nil, a string (raw) or any JSON-able value.
func (e *bkEnv) do(method, path, token string, body any) (int, map[string]any) {
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		j, _ := json.Marshal(b) // test payloads are always marshalable
		rd = bytes.NewReader(j)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m) // empty/non-JSON bodies leave m nil, which tests handle
	return w.Code, m
}

func (e *bkEnv) book(token, showtime string, seats ...string) (int, map[string]any) {
	return e.do(http.MethodPost, "/api/v1/bookings", token, map[string]any{"showtime_id": showtime, "seat_ids": seats})
}

func errCode(m map[string]any) string {
	em, _ := m["error"].(map[string]any)
	s, _ := em["code"].(string)
	return s
}

func errMessage(m map[string]any) string {
	em, _ := m["error"].(map[string]any)
	s, _ := em["message"].(string)
	return s
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

type evRow struct {
	BookingID  *string
	ShowtimeID *string
	EventType  string
	Outcome    string
	ReasonCode string
	FromStatus string
	ToStatus   string
	ActorType  string
	Metadata   string
}

func (e *bkEnv) events(t *testing.T, where string, args ...any) []evRow {
	t.Helper()
	var rows []evRow
	require.NoError(t, e.gdb.Raw(
		`SELECT booking_id, showtime_id, event_type, outcome, reason_code, from_status, to_status, actor_type,
		        metadata::text AS metadata
		   FROM booking_events WHERE `+where+` ORDER BY created_at, id`, args...).Scan(&rows).Error)
	return rows
}

func (e *bkEnv) countEvents(t *testing.T, eventType, outcome, reason string) int64 {
	t.Helper()
	return e.count(t, `SELECT COUNT(*) FROM booking_events WHERE event_type = ? AND outcome = ? AND reason_code = ?`,
		eventType, outcome, reason)
}

func (e *bkEnv) count(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, e.gdb.Raw(sql, args...).Scan(&n).Error)
	return n
}

func (e *bkEnv) holdExists(t *testing.T, seatID string) bool {
	t.Helper()
	n, err := e.rdb.Exists(context.Background(), "hold:"+seatID).Result()
	require.NoError(t, err)
	return n == 1
}

func (e *bkEnv) holdOwner(t *testing.T, seatID string) string {
	t.Helper()
	v, err := e.rdb.Get(context.Background(), "hold:"+seatID).Result()
	require.NoError(t, err)
	return v
}

func unreachableRedis(t *testing.T) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() }) // best-effort close of a client that never connected
	return c
}
