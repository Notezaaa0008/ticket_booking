package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ticketbooking/internal/middleware"
)

func TestBooking_ConcurrentSameSeat(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	const n = 20
	tokens := make([]string, n)
	for i := range tokens {
		_, tokens[i] = env.newUser(t, fmt.Sprintf("racer%d@example.com", i), "user")
	}

	codes := make([]int, n)
	errCodes := make([]string, n)
	bookingIDs := make([]string, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			code, m := env.book(tokens[i], bkShowtime, bkSeatA1)
			codes[i], errCodes[i], bookingIDs[i] = code, errCode(m), str(m, "id")
		}(i)
	}
	close(start)
	wg.Wait()

	var ok, conflict int
	var winner string
	for i := range codes {
		switch codes[i] {
		case http.StatusCreated:
			ok++
			winner = bookingIDs[i]
		case http.StatusConflict:
			conflict++
			assert.Equal(t, "SEAT_UNAVAILABLE", errCodes[i])
		default:
			t.Errorf("unexpected status %d (%s)", codes[i], errCodes[i])
		}
	}
	require.Equal(t, 1, ok)
	require.Equal(t, 19, conflict)
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE seat_id = ? AND active`, bkSeatA1))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM bookings`))
	assert.EqualValues(t, 1, env.countEvents(t, "BOOKING_CREATED", "SUCCESS", ""))
	assert.EqualValues(t, 19, env.countEvents(t, "BOOKING_CREATE_FAILED", "FAILURE", "SEAT_UNAVAILABLE"))
	assert.Equal(t, winner, env.holdOwner(t, bkSeatA1))
}

func TestBooking_PartialHold_ReleasesOwnKeys(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	_, tok1 := env.newUser(t, "holder@example.com", "user")
	u2, tok2 := env.newUser(t, "partial@example.com", "user")

	code, first := env.book(tok1, bkShowtime, bkSeatA3)
	require.Equal(t, http.StatusCreated, code)

	code, m := env.book(tok2, bkShowtime, bkSeatA1, bkSeatA2, bkSeatA3)
	require.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "SEAT_UNAVAILABLE", errCode(m))
	em, _ := m["error"].(map[string]any)
	assert.Equal(t, []any{bkSeatA3}, em["seat_ids"])

	assert.False(t, env.holdExists(t, bkSeatA1), "A1 key must be released")
	assert.False(t, env.holdExists(t, bkSeatA2), "A2 key must be released")
	assert.Equal(t, str(first, "id"), env.holdOwner(t, bkSeatA3), "A3 must stay with its owner")
	assert.EqualValues(t, 0, env.count(t, `SELECT COUNT(*) FROM bookings WHERE user_id = ?`, u2))

	evs := env.events(t, `event_type = 'BOOKING_CREATE_FAILED'`)
	require.Len(t, evs, 1)
	assert.Equal(t, "FAILURE", evs[0].Outcome)
	assert.Equal(t, "SEAT_UNAVAILABLE", evs[0].ReasonCode)
	var meta map[string][]string
	require.NoError(t, json.Unmarshal([]byte(evs[0].Metadata), &meta))
	assert.Equal(t, []string{bkSeatA3}, meta["conflicting_seat_ids"])
	assert.ElementsMatch(t, []string{bkSeatA1, bkSeatA2, bkSeatA3}, meta["requested_seat_ids"])
}

func TestBooking_RedisDown_DBFallback(t *testing.T) {
	bad := unreachableRedis(t)
	env := newBkEnv(t, bkOpts{rdb: bad})
	_, tok1 := env.newUser(t, "fb1@example.com", "user")
	_, tok2 := env.newUser(t, "fb2@example.com", "user")

	code, _ := env.book(tok1, bkShowtime, bkSeatA1)
	require.Equal(t, http.StatusCreated, code)
	evs := env.events(t, `event_type = 'BOOKING_CREATED'`)
	require.Len(t, evs, 1)
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(evs[0].Metadata), &meta))
	assert.Equal(t, "db_fallback", meta["hold_mode"])

	code, m := env.book(tok2, bkShowtime, bkSeatA1)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "SEAT_UNAVAILABLE", errCode(m))
	assert.EqualValues(t, 1, env.countEvents(t, "BOOKING_CREATE_FAILED", "FAILURE", "SEAT_UNAVAILABLE"))

	// Concurrent racers with Redis down: the unique index alone must allow exactly one.
	const n = 10
	tokens := make([]string, n)
	for i := range tokens {
		_, tokens[i] = env.newUser(t, fmt.Sprintf("fbrace%d@example.com", i), "user")
	}
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _ = env.book(tokens[i], bkShowtime, bkSeatA2)
		}(i)
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else {
			assert.Equal(t, http.StatusConflict, c)
		}
	}
	assert.Equal(t, 1, created)
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE seat_id = ? AND active`, bkSeatA2))

	// The rate limiter fails open when Redis is unreachable.
	assert.True(t, middleware.NewRateLimiter(bad).Allow(context.Background(), "rl:test", 1, time.Minute))
	assert.True(t, middleware.NewRateLimiter(bad).Allow(context.Background(), "rl:test", 1, time.Minute))
}

func TestBooking_ExpiryJob(t *testing.T) {
	env := newBkEnv(t, bkOpts{ttl: time.Second})
	_, tok1 := env.newUser(t, "exp1@example.com", "user")
	_, tok2 := env.newUser(t, "exp2@example.com", "user")

	code, b := env.book(tok1, bkShowtime, bkSeatA1, bkSeatA2)
	require.Equal(t, http.StatusCreated, code)
	id := str(b, "id")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); env.svc.RunExpiryJob(ctx, 100*time.Millisecond) }()
	t.Cleanup(func() { cancel(); <-done })

	require.Eventually(t, func() bool {
		return env.count(t, `SELECT COUNT(*) FROM bookings WHERE id = ? AND status = 'EXPIRED'`, id) == 1
	}, 8*time.Second, 100*time.Millisecond)

	assert.EqualValues(t, 0, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, id))
	evs := env.events(t, `event_type = 'BOOKING_EXPIRED'`)
	require.Len(t, evs, 1)
	assert.Equal(t, "SUCCESS", evs[0].Outcome)
	assert.Equal(t, "HOLD_EXPIRED", evs[0].ReasonCode)
	assert.Equal(t, "SYSTEM", evs[0].ActorType)
	assert.Equal(t, "PENDING", evs[0].FromStatus)
	assert.Equal(t, "EXPIRED", evs[0].ToStatus)

	code, got := env.do(http.MethodGet, "/api/v1/bookings/"+id, tok1, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "EXPIRED", str(got, "status"))
	assert.Equal(t, "HOLD_EXPIRED", str(got, "status_reason"))
	assert.EqualValues(t, 0, got["seconds_remaining"])

	code, _ = env.book(tok2, bkShowtime, bkSeatA1)
	assert.Equal(t, http.StatusCreated, code, "seat must be bookable again after expiry")
}

func TestBooking_LazyExpiryOnNewBooking(t *testing.T) {
	env := newBkEnv(t, bkOpts{ttl: time.Second})
	_, tok1 := env.newUser(t, "lazy1@example.com", "user")
	_, tok2 := env.newUser(t, "lazy2@example.com", "user")

	code, b := env.book(tok1, bkShowtime, bkSeatA1)
	require.Equal(t, http.StatusCreated, code)
	oldID := str(b, "id")
	time.Sleep(1500 * time.Millisecond) // hold runs out; no expiry job is running

	code, _ = env.book(tok2, bkShowtime, bkSeatA1)
	require.Equal(t, http.StatusCreated, code)

	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM bookings WHERE id = ? AND status = 'EXPIRED'`, oldID))
	assert.EqualValues(t, 0, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, oldID))
	evs := env.events(t, `event_type = 'BOOKING_EXPIRED'`)
	require.Len(t, evs, 1)
	assert.Equal(t, oldID, *evs[0].BookingID)
	assert.Equal(t, "SYSTEM", evs[0].ActorType)
	assert.Equal(t, "HOLD_EXPIRED", evs[0].ReasonCode)
	assert.Equal(t, "SUCCESS", evs[0].Outcome)
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE seat_id = ? AND active`, bkSeatA1))
}

func TestBooking_Cancel(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	_, owner := env.newUser(t, "owner@example.com", "user")
	_, other := env.newUser(t, "other@example.com", "user")

	code, b := env.book(owner, bkShowtime, bkSeatA1)
	require.Equal(t, http.StatusCreated, code)
	id := str(b, "id")
	path := "/api/v1/bookings/" + id

	code, m := env.do(http.MethodDelete, path, other, nil)
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, "BOOKING_NOT_FOUND", errCode(m))
	assert.EqualValues(t, 1, env.countEvents(t, "BOOKING_CANCEL_REJECTED", "FAILURE", "BOOKING_NOT_FOUND"))
	assert.True(t, env.holdExists(t, bkSeatA1), "other user's cancel must not touch the hold")

	code, m = env.do(http.MethodDelete, path, owner, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "CANCELLED", str(m, "status"))
	assert.Equal(t, "USER_CANCELLED", str(m, "status_reason"))
	assert.False(t, env.holdExists(t, bkSeatA1))
	assert.EqualValues(t, 0, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, id))
	evs := env.events(t, `event_type = 'BOOKING_CANCELLED'`)
	require.Len(t, evs, 1)
	assert.Equal(t, "SUCCESS", evs[0].Outcome)
	assert.Equal(t, "USER_CANCELLED", evs[0].ReasonCode)
	assert.Equal(t, "USER", evs[0].ActorType)
	assert.Equal(t, "CANCELLED", evs[0].ToStatus)

	code, m = env.do(http.MethodDelete, path, owner, nil)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "BOOKING_NOT_PENDING", errCode(m))
	rej := env.events(t, `event_type = 'BOOKING_CANCEL_REJECTED' AND reason_code = 'BOOKING_NOT_PENDING'`)
	require.Len(t, rej, 1)
	assert.Equal(t, "FAILURE", rej[0].Outcome)
	assert.Equal(t, "CANCELLED", rej[0].FromStatus)
	assert.Equal(t, id, *rej[0].BookingID)

	code, _ = env.book(other, bkShowtime, bkSeatA1)
	assert.Equal(t, http.StatusCreated, code, "cancelled seat must be bookable again")
}

func TestBooking_CreateViewsAndAudit(t *testing.T) {
	env := newBkEnv(t, bkOpts{ttl: 600 * time.Second})
	_, tok1 := env.newUser(t, "view1@example.com", "user")
	_, tok2 := env.newUser(t, "view2@example.com", "user")

	code, _ := env.do(http.MethodPost, "/api/v1/bookings", "", map[string]any{})
	assert.Equal(t, http.StatusUnauthorized, code)

	code, b := env.book(tok1, bkShowtime, bkSeatA1, bkSeatA2)
	require.Equal(t, http.StatusCreated, code)
	id := str(b, "id")
	assert.Equal(t, "PENDING", str(b, "status"))
	assert.EqualValues(t, 30000, b["total_satang"])
	secs, _ := b["seconds_remaining"].(float64)
	assert.InDelta(t, 600, secs, 5)

	evs := env.events(t, `event_type = 'BOOKING_CREATED'`)
	require.Len(t, evs, 1)
	assert.Equal(t, "SUCCESS", evs[0].Outcome)
	assert.Equal(t, "USER", evs[0].ActorType)
	assert.Equal(t, "PENDING", evs[0].ToStatus)
	assert.Equal(t, id, *evs[0].BookingID)
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(evs[0].Metadata), &meta))
	assert.Equal(t, "redis", meta["hold_mode"])
	assert.EqualValues(t, 30000, meta["total_satang"])
	assert.ElementsMatch(t, []any{bkSeatA1, bkSeatA2}, meta["seat_ids"])
	assert.NotEmpty(t, meta["expires_at"])
	assert.Equal(t, id, env.holdOwner(t, bkSeatA1))

	code, got := env.do(http.MethodGet, "/api/v1/bookings/"+id, tok1, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, got["items"], 2)
	code, _ = env.do(http.MethodGet, "/api/v1/bookings/"+id, tok2, nil)
	assert.Equal(t, http.StatusNotFound, code, "other users must get 404")

	code, list := env.do(http.MethodGet, "/api/v1/bookings", tok1, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, list["items"], 1)
	code, list = env.do(http.MethodGet, "/api/v1/bookings", tok2, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, list["items"], 0)
}

func TestBooking_ClientPriceIgnored(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	_, tok := env.newUser(t, "price@example.com", "user")
	payload := `{"showtime_id":"` + bkShowtime + `","seat_ids":["` + bkSeatA1 + `","` + bkSeatA2 + `"],
		"price_satang":1,"total_satang":1,"items":[{"seat_id":"` + bkSeatA1 + `","price_satang":1}],"user_id":"x","status":"PAID"}`
	code, b := env.do(http.MethodPost, "/api/v1/bookings", tok, payload)
	require.Equal(t, http.StatusCreated, code)
	assert.EqualValues(t, 30000, b["total_satang"])
	assert.Equal(t, "PENDING", str(b, "status"))
	assert.EqualValues(t, 30000, env.count(t, `SELECT total_satang FROM bookings`))
	assert.EqualValues(t, 30000, env.count(t, `SELECT SUM(price_satang) FROM booking_items`))
}

func TestBooking_SixSeatsAllowedSevenRejected(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	_, tok := env.newUser(t, "six@example.com", "user")
	code, _ := env.book(tok, bkShowtime, bkSeatA1, bkSeatA2, bkSeatA3, bkSeatA4, bkSeatA5, bkSeatA6)
	assert.Equal(t, http.StatusCreated, code)
}

func TestBooking_ValidationFailuresAreAudited(t *testing.T) {
	const unknownShowtime = "a0000000-0000-0000-0000-0000000000ff"
	cases := []struct {
		name       string
		body       any
		wantStatus int
		wantCode   string
		showtimeNo bool // event has no showtime_id (showtime unknown)
	}{
		{"no seats", map[string]any{"showtime_id": bkShowtime, "seat_ids": []string{}}, 400, "VALIDATION_FAILED", false},
		{"too many seats", map[string]any{"showtime_id": bkShowtime, "seat_ids": []string{bkSeatA1, bkSeatA2, bkSeatA3, bkSeatA4, bkSeatA5, bkSeatA6, bkSeatA7}}, 422, "TOO_MANY_SEATS", false},
		{"duplicate seats", map[string]any{"showtime_id": bkShowtime, "seat_ids": []string{bkSeatA1, bkSeatA1}}, 400, "VALIDATION_FAILED", false},
		{"bad seat id", map[string]any{"showtime_id": bkShowtime, "seat_ids": []string{"nope"}}, 400, "VALIDATION_FAILED", false},
		{"seat of another showtime", map[string]any{"showtime_id": bkShowtime, "seat_ids": []string{bkSeatOther}}, 400, "SEAT_NOT_IN_SHOWTIME", false},
		{"closed showtime", map[string]any{"showtime_id": bkClosedShow, "seat_ids": []string{bkSeatClosed}}, 422, "SHOWTIME_NOT_ON_SALE", false},
		{"past showtime", map[string]any{"showtime_id": bkPastShow, "seat_ids": []string{bkSeatPast}}, 422, "SHOWTIME_NOT_ON_SALE", false},
		{"unknown showtime", map[string]any{"showtime_id": unknownShowtime, "seat_ids": []string{bkSeatA1}}, 400, "VALIDATION_FAILED", true},
		{"malformed json", "{not json", 400, "VALIDATION_FAILED", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newBkEnv(t, bkOpts{})
			_, tok := env.newUser(t, "val@example.com", "user")
			code, m := env.do(http.MethodPost, "/api/v1/bookings", tok, tc.body)
			assert.Equal(t, tc.wantStatus, code)
			assert.Equal(t, tc.wantCode, errCode(m))
			assert.EqualValues(t, 0, env.count(t, `SELECT COUNT(*) FROM bookings`))
			evs := env.events(t, `event_type = 'BOOKING_CREATE_FAILED'`)
			require.Len(t, evs, 1)
			assert.Equal(t, "FAILURE", evs[0].Outcome)
			assert.Equal(t, tc.wantCode, evs[0].ReasonCode)
			assert.Equal(t, "USER", evs[0].ActorType)
			if tc.showtimeNo {
				assert.Nil(t, evs[0].ShowtimeID)
			}
		})
	}
}

func TestBooking_OnePendingPerShowtime(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	_, tok := env.newUser(t, "twice@example.com", "user")
	code, _ := env.book(tok, bkShowtime, bkSeatA1)
	require.Equal(t, http.StatusCreated, code)

	code, m := env.book(tok, bkShowtime, bkSeatA2)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "VALIDATION_FAILED", errCode(m))
	assert.Equal(t, "you already have a pending booking for this showtime", errMessage(m))
	assert.False(t, env.holdExists(t, bkSeatA2), "no hold may be taken for a rejected request")
	assert.EqualValues(t, 1, env.countEvents(t, "BOOKING_CREATE_FAILED", "FAILURE", "VALIDATION_FAILED"))

	// The same user may still book in another showtime.
	code, _ = env.book(tok, bkShowtime2, bkSeatOther)
	assert.Equal(t, http.StatusCreated, code)
}

const auditBlockConstraint = "booking_events_block_create_failed"

func dropAuditBlockConstraint(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	require.NoError(t, gdb.Exec(
		`ALTER TABLE booking_events DROP CONSTRAINT IF EXISTS `+auditBlockConstraint).Error)
}

func TestBooking_AuditFailurePreservesError(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	dropAuditBlockConstraint(t, env.gdb)
	defer dropAuditBlockConstraint(t, env.gdb)

	_, tok := env.newUser(t, "auditfail@example.com", "user")
	require.NoError(t, env.gdb.Exec(
		`ALTER TABLE booking_events ADD CONSTRAINT `+auditBlockConstraint+` CHECK (event_type <> 'BOOKING_CREATE_FAILED')`).Error)
	defer dropAuditBlockConstraint(t, env.gdb)

	oldErr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	code, m := env.do(http.MethodPost, "/api/v1/bookings", tok, map[string]any{"showtime_id": bkShowtime, "seat_ids": []string{}})

	require.NoError(t, w.Close())
	os.Stderr = oldErr
	stderr, _ := io.ReadAll(r)

	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "VALIDATION_FAILED", errCode(m))
	assert.Contains(t, string(stderr), "audit write failed")
	assert.Contains(t, string(stderr), "request_id=")
	assert.EqualValues(t, 0, env.count(t, `SELECT COUNT(*) FROM bookings`))
}

func TestBooking_RateLimit(t *testing.T) {
	env := newBkEnv(t, bkOpts{bookingLimit: 2})
	_, tok := env.newUser(t, "limited@example.com", "user")
	code, _ := env.book(tok, bkShowtime, bkSeatA1)
	assert.Equal(t, http.StatusCreated, code)
	code, _ = env.book(tok, bkShowtime, bkSeatA2) // rejected by one-pending rule but still counts
	assert.Equal(t, http.StatusConflict, code)
	code, m := env.book(tok, bkShowtime, bkSeatA2)
	assert.Equal(t, http.StatusTooManyRequests, code)
	assert.Equal(t, "RATE_LIMITED", errCode(m))
}
