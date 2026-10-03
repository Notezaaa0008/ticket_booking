package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"ticketbooking/internal/cache"
	"ticketbooking/internal/db"
	"ticketbooking/internal/repository"
	"ticketbooking/internal/service"
)

var (
	intTestDB  *gorm.DB
	intTestRDB *redis.Client
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)

	// อ่านค่า TEST_DATABASE_URL หากไม่มีให้ fallback ไปใช้ postgres:postgres
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:postgres@localhost:5432/ticket_booking_test?sslmode=disable"
	}

	if err := db.Migrate(url, "up"); err != nil {
		panic(err)
	}
	var err error
	intTestDB, err = db.Connect(url)
	if err != nil {
		panic(err)
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379/1"
	}
	intTestRDB, err = cache.Connect(redisURL)
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

func requireIntegration(t *testing.T) (*gorm.DB, *redis.Client) {
	t.Helper()
	if intTestDB == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if intTestRDB == nil {
		t.Skip("REDIS_URL not set")
	}
	return intTestDB, intTestRDB
}

func setupCatalogRouter(t *testing.T, gdb *gorm.DB, rdb *redis.Client) *gin.Engine {
	t.Helper()
	repo := repository.NewCatalogRepository(gdb)
	svc := service.NewCatalogService(repo, rdb)
	h := NewCatalogHandler(svc)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.GET("/events", h.ListEvents)
	v1.GET("/events/:id", h.GetEvent)
	v1.GET("/showtimes/:id/seats", h.GetShowtimeSeats)
	return r
}

// integrationDBMu serialises TRUNCATE across parallel tests in this package (shared test DB).
var integrationDBMu sync.Mutex

func resetCatalogData(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	integrationDBMu.Lock()
	defer integrationDBMu.Unlock()
	ctx := context.Background()
	//nolint:gosec // test fixture reset
	err := gdb.WithContext(ctx).Exec(`
TRUNCATE booking_events, payment_events, refunds, tickets, payments,
  booking_items, bookings, seats, showtimes, events, users CASCADE`).Error
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

const (
	testUserID      = "aaaaaaaa-0000-0000-0000-000000000099"
	testPubEventID  = "eeeeeeee-0000-0000-0000-000000000101"
	testDraftEvent  = "eeeeeeee-0000-0000-0000-000000000102"
	testPastEvent   = "eeeeeeee-0000-0000-0000-000000000103"
	testShowFuture  = "55555555-0000-0000-0000-000000000101"
	testShowSeats   = "55555555-0000-0000-0000-000000000102"
	testSeatAvail   = "66666666-0000-0000-0000-000000000001"
	testSeatHeld    = "66666666-0000-0000-0000-000000000002"
	testSeatSold    = "66666666-0000-0000-0000-000000000003"
	testSeatExpired = "66666666-0000-0000-0000-000000000004"
)

func seedCatalogFixtures(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	resetCatalogData(t, gdb)
	ctx := context.Background()
	// Literal UUIDs only: GORM Exec treats $n as bind placeholders.
	sql := `
INSERT INTO users (id, email, password_hash, name, role) VALUES
  ('` + testUserID + `', 'catalog-test@example.com', 'x', 'Catalog Test', 'user');

INSERT INTO events (id, title, description, venue, poster_url, status) VALUES
  ('` + testPubEventID + `', 'Bangkok Jazz Night', 'desc', 'Lumpini Hall', 'http://poster/jazz', 'published'),
  ('` + testDraftEvent + `', 'Draft Show', 'desc', 'Secret Venue', '', 'draft'),
  ('` + testPastEvent + `', 'Past Only Fest', 'desc', 'Old Arena', '', 'published');

INSERT INTO showtimes (id, event_id, starts_at, status) VALUES
  ('` + testShowFuture + `', '` + testPubEventID + `', now() + interval '7 days', 'on_sale'),
  ('` + testShowSeats + `', '` + testPubEventID + `', now() + interval '8 days', 'on_sale'),
  ('55555555-0000-0000-0000-000000000103', '` + testPastEvent + `', now() - interval '1 day', 'on_sale');

INSERT INTO seats (id, showtime_id, row_label, seat_number, zone, price_satang) VALUES
  ('` + testSeatAvail + `', '` + testShowSeats + `', 'A', 1, 'front', 100000),
  ('` + testSeatHeld + `', '` + testShowSeats + `', 'A', 2, 'front', 100000),
  ('` + testSeatSold + `', '` + testShowSeats + `', 'A', 3, 'front', 100000),
  ('` + testSeatExpired + `', '` + testShowSeats + `', 'A', 4, 'front', 100000);

INSERT INTO bookings (id, user_id, showtime_id, status, total_satang, expires_at) VALUES
  ('bbbbbbbb-0000-0000-0000-000000000001', '` + testUserID + `', '` + testShowSeats + `', 'PENDING', 100000, now() + interval '10 minutes'),
  ('bbbbbbbb-0000-0000-0000-000000000002', '` + testUserID + `', '` + testShowSeats + `', 'PAID', 100000, now() + interval '10 minutes'),
  ('bbbbbbbb-0000-0000-0000-000000000003', '` + testUserID + `', '` + testShowSeats + `', 'PENDING', 100000, now() - interval '10 minutes');

INSERT INTO booking_items (booking_id, seat_id, price_satang, active) VALUES
  ('bbbbbbbb-0000-0000-0000-000000000001', '` + testSeatHeld + `', 100000, true),
  ('bbbbbbbb-0000-0000-0000-000000000002', '` + testSeatSold + `', 100000, true),
  ('bbbbbbbb-0000-0000-0000-000000000003', '` + testSeatExpired + `', 100000, true);
`
	if err := gdb.WithContext(ctx).Exec(sql).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func catalogGET(t *testing.T, r *gin.Engine, path string) (int, json.RawMessage) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w.Code, json.RawMessage(w.Body.Bytes())
}

func TestCatalogListSearchAndValidation(t *testing.T) {
	gdb, rdb := requireIntegration(t)
	seedCatalogFixtures(t, gdb)
	r := setupCatalogRouter(t, gdb, rdb)
	ctx := context.Background()
	_ = rdb.FlushDB(ctx).Err()

	code, body := catalogGET(t, r, "/api/v1/events?q=jazz")
	if code != 200 {
		t.Fatalf("search: %d %s", code, body)
	}
	var list struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].Title != "Bangkok Jazz Night" {
		t.Fatalf("unexpected list: %+v", list)
	}

	code, _ = catalogGET(t, r, "/api/v1/events?q=zzzznomatch")
	if code != 200 {
		t.Fatalf("no match status: %d", code)
	}

	from := time.Now().UTC().Add(6 * 24 * time.Hour).Format("2006-01-02")
	to := time.Now().UTC().Add(9 * 24 * time.Hour).Format("2006-01-02")
	code, body = catalogGET(t, r, "/api/v1/events?from="+from+"&to="+to)
	if code != 200 {
		t.Fatalf("date range: %d", code)
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 {
		t.Fatalf("date filter total: %d", list.Total)
	}

	code, body = catalogGET(t, r, "/api/v1/events?page=0")
	if code != 400 {
		t.Fatalf("bad page: %d", code)
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &errBody)
	if errBody.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("bad page code: %+v", errBody)
	}

	code, _ = catalogGET(t, r, "/api/v1/events?from=not-a-date")
	if code != 400 {
		t.Fatalf("bad from: %d", code)
	}
}

func TestCatalogExcludesUnpublishedAndPast(t *testing.T) {
	gdb, rdb := requireIntegration(t)
	seedCatalogFixtures(t, gdb)
	r := setupCatalogRouter(t, gdb, rdb)
	_ = rdb.FlushDB(context.Background()).Err()

	code, body := catalogGET(t, r, "/api/v1/events")
	if code != 200 {
		t.Fatalf("list: %d", code)
	}
	var list struct {
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 published future event, got %d", len(list.Items))
	}
	if list.Items[0].ID.String() != testPubEventID {
		t.Fatalf("wrong event id: %s", list.Items[0].ID)
	}

	code, _ = catalogGET(t, r, "/api/v1/events/"+testDraftEvent)
	if code != 404 {
		t.Fatalf("draft event: %d", code)
	}
}

func TestCatalogSeatStatusesAndSummary(t *testing.T) {
	gdb, rdb := requireIntegration(t)
	seedCatalogFixtures(t, gdb)
	r := setupCatalogRouter(t, gdb, rdb)

	code, body := catalogGET(t, r, "/api/v1/showtimes/"+testShowSeats+"/seats")
	if code != 200 {
		t.Fatalf("seats: %d %s", code, body)
	}
	var resp struct {
		Seats []struct {
			ID     uuid.UUID `json:"id"`
			Status string    `json:"status"`
		} `json:"seats"`
		Summary struct {
			Total     int `json:"total"`
			Available int `json:"available"`
			Held      int `json:"held"`
			Sold      int `json:"sold"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	statusByID := map[string]string{}
	for _, s := range resp.Seats {
		statusByID[s.ID.String()] = s.Status
	}
	if statusByID[testSeatAvail] != "available" {
		t.Fatalf("avail: %v", statusByID[testSeatAvail])
	}
	if statusByID[testSeatHeld] != "held" {
		t.Fatalf("held: %v", statusByID[testSeatHeld])
	}
	if statusByID[testSeatSold] != "sold" {
		t.Fatalf("sold: %v", statusByID[testSeatSold])
	}
	if statusByID[testSeatExpired] != "available" {
		t.Fatalf("expired pending: %v", statusByID[testSeatExpired])
	}
	if resp.Summary.Total != 4 || resp.Summary.Available != 2 || resp.Summary.Held != 1 || resp.Summary.Sold != 1 {
		t.Fatalf("summary: %+v", resp.Summary)
	}
}

func TestCatalogListCache(t *testing.T) {
	gdb, rdb := requireIntegration(t)
	seedCatalogFixtures(t, gdb)
	r := setupCatalogRouter(t, gdb, rdb)
	ctx := context.Background()
	_ = rdb.FlushDB(ctx).Err()

	path := "/api/v1/events?q=jazz&page=1&limit=20"
	code, body1 := catalogGET(t, r, path)
	if code != 200 {
		t.Fatalf("first: %d", code)
	}
	keys, err := rdb.Keys(ctx, "events:list:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected cache key, got %v", keys)
	}
	code, body2 := catalogGET(t, r, path)
	if code != 200 {
		t.Fatalf("second: %d", code)
	}
	if string(body1) != string(body2) {
		t.Fatal("cached response mismatch")
	}
}

func TestCatalogListWorksWhenRedisUnreachable(t *testing.T) {
	gdb, _ := requireIntegration(t)
	seedCatalogFixtures(t, gdb)
	broken := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6399", DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = broken.Close() })
	r := setupCatalogRouter(t, gdb, broken)

	code, body := catalogGET(t, r, "/api/v1/events")
	if code != 200 {
		t.Fatalf("list without redis: %d %s", code, body)
	}
}
