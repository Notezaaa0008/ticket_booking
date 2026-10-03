package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ticketbooking/internal/repository"
	"ticketbooking/internal/service"
)

type adminEnv struct {
	*payEnv
	adminID  string
	adminTok string
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	env := newPayEnv(t, false, bkOpts{})
	v1 := env.r.Group("/api/v1")
	catalogSvc := service.NewCatalogService(repository.NewCatalogRepository(env.gdb), env.rdb)
	v1.GET("/events", NewCatalogHandler(catalogSvc).ListEvents)
	svc := service.NewAdminService(repository.NewAdminRepository(env.gdb), repository.NewPaymentRepository(env.gdb),
		repository.NewBookingRepository(env.gdb), env.rdb)
	RegisterAdminRoutes(v1, AdminDeps{Admin: NewAdminHandler(svc), Verify: env.auth.VerifyToken})
	id, tok := env.newUser(t, "admin@test.local", "admin")
	return &adminEnv{payEnv: env, adminID: id, adminTok: tok}
}

func setRefundProvider(t *testing.T, fn func(context.Context, string, int64) (string, error)) {
	t.Helper()
	orig := service.RefundProvider
	service.RefundProvider = fn
	t.Cleanup(func() { service.RefundProvider = orig })
}

// paidBooking books seats on bkShowtime for a new user and pays them via a signed webhook.
func (e *adminEnv) paidBooking(t *testing.T, email string, seats ...string) (bookingID, paymentID string) {
	t.Helper()
	_, tok := e.newUser(t, email, "user")
	bookingID, paymentID = e.bookAndPay(t, tok, seats...)
	code, m := e.signedWebhook(successBody(t, paymentID, e.amount(t, paymentID)))
	require.Equal(t, http.StatusOK, code, m)
	require.Equal(t, "SUCCEEDED", e.status(t, "payments", paymentID))
	return bookingID, paymentID
}

func (e *adminEnv) amount(t *testing.T, paymentID string) int64 {
	return e.count(t, `SELECT amount_satang FROM payments WHERE id = ?`, paymentID)
}

func (e *adminEnv) requestRefund(paymentID, key string) (int, map[string]any) {
	h := map[string]string{}
	if key != "" {
		h["Idempotency-Key"] = key
	}
	return e.raw(http.MethodPost, "/api/v1/admin/payments/"+paymentID+"/refunds", e.adminTok,
		[]byte(`{"reason_code":"CUSTOMER_REQUEST","note":"test"}`), h)
}

func (e *adminEnv) process(refundID string) (int, map[string]any) {
	return e.do(http.MethodPost, "/api/v1/admin/refunds/"+refundID+"/process", e.adminTok, nil)
}

func (e *adminEnv) checkIn(code string) (int, map[string]any) {
	return e.do(http.MethodPost, "/api/v1/admin/tickets/"+code+"/check-in", e.adminTok, nil)
}

func (e *adminEnv) ticketCodes(t *testing.T, bookingID string) []string {
	t.Helper()
	var codes []string
	require.NoError(t, e.gdb.Raw(`SELECT t.code FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id
		WHERE bi.booking_id = ? ORDER BY t.code`, bookingID).Scan(&codes).Error)
	return codes
}

func (e *adminEnv) payEventCount(t *testing.T, paymentID, eventType string) int64 {
	return e.count(t, `SELECT COUNT(*) FROM payment_events WHERE payment_id = ? AND event_type = ?`, paymentID, eventType)
}

func (e *adminEnv) lastPayEvent(t *testing.T, paymentID, eventType string) payEvRow {
	t.Helper()
	evs := e.payEvents(t, `payment_id = ? AND event_type = ?`, paymentID, eventType)
	require.NotEmpty(t, evs, "no %s event", eventType)
	return evs[len(evs)-1]
}

func TestAdmin_EveryRouteRequiresAdmin(t *testing.T) {
	env := newAdminEnv(t)
	_, userTok := env.newUser(t, "plain@test.local", "user")
	n := 0
	for _, rt := range env.r.Routes() {
		if !strings.HasPrefix(rt.Path, "/api/v1/admin") {
			continue
		}
		n++
		path := strings.NewReplacer(":id", uuid.NewString(), ":code", "somecode").Replace(rt.Path)
		code, m := env.do(rt.Method, path, "", map[string]any{})
		assert.Equal(t, http.StatusUnauthorized, code, "%s %s without token", rt.Method, rt.Path)
		assert.Equal(t, "UNAUTHENTICATED", errCode(m))
		code, m = env.do(rt.Method, path, userTok, map[string]any{})
		assert.Equal(t, http.StatusForbidden, code, "%s %s as user", rt.Method, rt.Path)
		assert.Equal(t, "FORBIDDEN", errCode(m))
	}
	assert.Equal(t, 14, n, "admin route count")
}

func TestAdmin_EventsAndShowtimes(t *testing.T) {
	env := newAdminEnv(t)
	ctx := context.Background()
	require.NoError(t, env.rdb.Set(ctx, "events:list:stale", "x", time.Minute).Err())

	code, ev := env.do(http.MethodPost, "/api/v1/admin/events", env.adminTok, map[string]any{"title": "Admin Show", "venue": "Hall"})
	require.Equal(t, http.StatusCreated, code, ev)
	eventID := str(ev, "id")
	n, err := env.rdb.Exists(ctx, "events:list:stale").Result()
	require.NoError(t, err)
	assert.Zero(t, n, "events list cache must be invalidated")

	starts := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	code, st := env.do(http.MethodPost, "/api/v1/admin/events/"+eventID+"/showtimes", env.adminTok, map[string]any{
		"starts_at": starts, "rows": 3, "seats_per_row": 4, "price_satang": 15000, "price_satang_by_row": map[string]int64{"A": 30000},
	})
	require.Equal(t, http.StatusCreated, code, st)
	showID := str(st, "id")
	assert.EqualValues(t, 12, st["seat_count"])
	assert.EqualValues(t, 12, env.count(t, `SELECT COUNT(*) FROM seats WHERE showtime_id = ?`, showID))
	assert.EqualValues(t, 4, env.count(t, `SELECT COUNT(*) FROM seats WHERE showtime_id = ? AND row_label = 'A' AND price_satang = 30000`, showID))
	assert.EqualValues(t, 8, env.count(t, `SELECT COUNT(*) FROM seats WHERE showtime_id = ? AND price_satang = 15000`, showID))

	for _, bad := range []map[string]any{
		{"starts_at": starts, "rows": 21, "seats_per_row": 1, "price_satang": 1},
		{"starts_at": starts, "rows": 1, "seats_per_row": 31, "price_satang": 1},
		{"starts_at": starts, "rows": 1, "seats_per_row": 1, "price_satang": -1},
		{"starts_at": starts, "rows": 1, "seats_per_row": 1, "price_satang": 1, "price_satang_by_row": map[string]int64{"Z": 1}},
	} {
		code, m := env.do(http.MethodPost, "/api/v1/admin/events/"+eventID+"/showtimes", env.adminTok, bad)
		assert.Equal(t, http.StatusBadRequest, code, m)
	}

	// Atomicity: make the 30th seat of every row violate a temporary constraint.
	require.NoError(t, env.gdb.Exec(`ALTER TABLE seats ADD CONSTRAINT test_block_seat_30 CHECK (seat_number <> 30) NOT VALID`).Error)
	t.Cleanup(func() {
		require.NoError(t, env.gdb.Exec(`ALTER TABLE seats DROP CONSTRAINT IF EXISTS test_block_seat_30`).Error)
	})
	before := env.count(t, `SELECT COUNT(*) FROM showtimes WHERE event_id = ?`, eventID)
	seatsBefore := env.count(t, `SELECT COUNT(*) FROM seats`)
	code, _ = env.do(http.MethodPost, "/api/v1/admin/events/"+eventID+"/showtimes", env.adminTok, map[string]any{
		"starts_at": starts, "rows": 20, "seats_per_row": 30, "price_satang": 100,
	})
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, before, env.count(t, `SELECT COUNT(*) FROM showtimes WHERE event_id = ?`, eventID), "showtime rolled back")
	assert.Equal(t, seatsBefore, env.count(t, `SELECT COUNT(*) FROM seats`), "no seats inserted")
	require.NoError(t, env.gdb.Exec(`ALTER TABLE seats DROP CONSTRAINT test_block_seat_30`).Error)

	code, m := env.do(http.MethodPost, "/api/v1/admin/events/"+eventID+"/showtimes", env.adminTok, map[string]any{
		"starts_at": starts, "rows": 20, "seats_per_row": 30, "price_satang": 100,
	})
	require.Equal(t, http.StatusCreated, code, m)
	assert.EqualValues(t, 600, env.count(t, `SELECT COUNT(*) FROM seats WHERE showtime_id = ?`, str(m, "id")))

	// D5: showtimes are edited (status/starts_at/price), never deleted.
	code, m = env.do(http.MethodPut, "/api/v1/admin/showtimes/"+showID, env.adminTok, map[string]any{
		"status": "closed", "price_satang_by_row": map[string]int64{"B": 5000},
	})
	require.Equal(t, http.StatusOK, code, m)
	assert.Equal(t, "closed", str(m, "status"))
	assert.EqualValues(t, 4, env.count(t, `SELECT COUNT(*) FROM seats WHERE showtime_id = ? AND row_label = 'B' AND price_satang = 5000`, showID))
	code, _ = env.do(http.MethodPut, "/api/v1/admin/showtimes/"+showID, env.adminTok, map[string]any{"status": "deleted"})
	assert.Equal(t, http.StatusBadRequest, code)

	// Delete archives an event that has showtimes and removes one that has none.
	code, m = env.do(http.MethodDelete, "/api/v1/admin/events/"+eventID, env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, m)
	assert.Equal(t, "archived", str(m, "result"))
	assert.Equal(t, "archived", env.status(t, "events", eventID))
	_, empty := env.do(http.MethodPost, "/api/v1/admin/events", env.adminTok, map[string]any{"title": "Empty", "venue": "V"})
	code, m = env.do(http.MethodDelete, "/api/v1/admin/events/"+str(empty, "id"), env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, m)
	assert.Equal(t, "deleted", str(m, "result"))
	assert.Zero(t, env.count(t, `SELECT COUNT(*) FROM events WHERE id = ?`, str(empty, "id")))
}

func TestAdmin_RefundEligibilityMatrix(t *testing.T) {
	env := newAdminEnv(t)

	assertRejected := func(t *testing.T, paymentID, wantStatus string, code int, m map[string]any, httpStatus int, reason string) {
		t.Helper()
		assert.Equal(t, httpStatus, code, m)
		assert.Equal(t, reason, errCode(m))
		ev := env.lastPayEvent(t, paymentID, "REFUND_REJECTED")
		assertPayEvent(t, ev, "REFUND_REJECTED", "FAILURE", reason, wantStatus)
		assert.Equal(t, wantStatus, env.status(t, "payments", paymentID))
	}

	t.Run("succeeded ok, replay idempotent, second active rejected", func(t *testing.T) {
		_, pid := env.paidBooking(t, "s1@test.local", bkSeatA1)
		key := uuid.NewString()
		code, r := env.requestRefund(pid, key)
		require.Equal(t, http.StatusCreated, code, r)
		assert.Equal(t, "REQUESTED", str(r, "status"))
		assert.EqualValues(t, env.amount(t, pid), r["amount_satang"])
		ev := env.lastPayEvent(t, pid, "REFUND_REQUESTED")
		assertPayEvent(t, ev, "REFUND_REQUESTED", "SUCCESS", "", "SUCCEEDED")
		assert.Contains(t, ev.RawPayload, str(r, "id"))

		code, again := env.requestRefund(pid, key)
		require.Equal(t, http.StatusOK, code, again)
		assert.Equal(t, str(r, "id"), str(again, "id"))
		assert.EqualValues(t, 1, env.payEventCount(t, pid, "REFUND_REQUESTED"))

		code, m := env.requestRefund(pid, uuid.NewString())
		assertRejected(t, pid, "SUCCEEDED", code, m, http.StatusConflict, "REFUND_ALREADY_EXISTS")
		assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM refunds WHERE payment_id = ?`, pid))
	})

	t.Run("needs_refund ok", func(t *testing.T) {
		_, tok := env.newUser(t, "nr@test.local", "user")
		_, pid := env.bookAndPay(t, tok, bkSeatA2)
		code, m := env.signedWebhook(successBody(t, pid, env.amount(t, pid)+1))
		require.Equal(t, http.StatusOK, code, m)
		require.Equal(t, "NEEDS_REFUND", env.status(t, "payments", pid))
		code, r := env.requestRefund(pid, uuid.NewString())
		require.Equal(t, http.StatusCreated, code, r)
		assertPayEvent(t, env.lastPayEvent(t, pid, "REFUND_REQUESTED"), "REFUND_REQUESTED", "SUCCESS", "", "NEEDS_REFUND")
	})

	t.Run("pending rejected", func(t *testing.T) {
		_, tok := env.newUser(t, "pend@test.local", "user")
		_, pid := env.bookAndPay(t, tok, bkSeatA3)
		code, m := env.requestRefund(pid, uuid.NewString())
		assertRejected(t, pid, "PENDING", code, m, http.StatusUnprocessableEntity, "REFUND_NOT_ELIGIBLE")
	})

	t.Run("failed rejected", func(t *testing.T) {
		_, tok := env.newUser(t, "fail@test.local", "user")
		_, pid := env.bookAndPay(t, tok, bkSeatA4)
		code, m := env.signedWebhook([]byte(`{"event_id":"` + uuid.NewString() + `","payment_id":"` + pid + `","status":"failed","provider_ref":"r"}`))
		require.Equal(t, http.StatusOK, code, m)
		require.Equal(t, "FAILED", env.status(t, "payments", pid))
		code, m = env.requestRefund(pid, uuid.NewString())
		assertRejected(t, pid, "FAILED", code, m, http.StatusUnprocessableEntity, "REFUND_NOT_ELIGIBLE")
	})

	t.Run("used ticket rejected", func(t *testing.T) {
		bid, pid := env.paidBooking(t, "used@test.local", bkSeatA5)
		codes := env.ticketCodes(t, bid)
		require.Len(t, codes, 1)
		code, m := env.checkIn(codes[0])
		require.Equal(t, http.StatusOK, code, m)
		code, m = env.requestRefund(pid, uuid.NewString())
		assertRejected(t, pid, "SUCCEEDED", code, m, http.StatusUnprocessableEntity, "TICKET_ALREADY_USED")
	})

	t.Run("missing idempotency key rejected", func(t *testing.T) {
		_, pid := env.paidBooking(t, "nokey@test.local", bkSeatA6)
		code, m := env.requestRefund(pid, "")
		assert.Equal(t, http.StatusBadRequest, code, m)
		assert.Equal(t, "IDEMPOTENCY_KEY_REQUIRED", errCode(m))
		assert.Zero(t, env.count(t, `SELECT COUNT(*) FROM refunds WHERE payment_id = ?`, pid))
		evs := env.payEvents(t, `event_type = 'REFUND_REJECTED' AND reason_code = 'VALIDATION_FAILED' AND payment_id IS NULL`)
		assert.Len(t, evs, 1, "rejection before the payment is loaded is still audited")
	})
}

func TestAdmin_RefundSuccessFutureShowtimeReleasesSeats(t *testing.T) {
	env := newAdminEnv(t)
	bid, pid := env.paidBooking(t, "ok@test.local", bkSeatA1, bkSeatA2)
	code, r := env.requestRefund(pid, uuid.NewString())
	require.Equal(t, http.StatusCreated, code, r)

	code, done := env.process(str(r, "id"))
	require.Equal(t, http.StatusOK, code, done)
	assert.Equal(t, "COMPLETED", str(done, "status"))
	assert.NotEmpty(t, str(done, "provider_ref"))
	assert.NotNil(t, done["completed_at"])

	assert.Equal(t, "REFUNDED", env.status(t, "payments", pid))
	assert.Equal(t, "REFUNDED", env.status(t, "bookings", bid))
	assert.EqualValues(t, 2, env.count(t, `SELECT COUNT(*) FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id
		WHERE bi.booking_id = ? AND t.status = 'VOID'`, bid))
	assert.Zero(t, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, bid))

	assertPayEvent(t, env.lastPayEvent(t, pid, "REFUND_COMPLETED"), "REFUND_COMPLETED", "SUCCESS", "", "REFUNDED")
	bev := env.events(t, `booking_id = ? AND event_type = 'BOOKING_REFUNDED'`, bid)
	require.Len(t, bev, 1)
	assert.Equal(t, "SUCCESS", bev[0].Outcome)
	assert.Equal(t, "PAID", bev[0].FromStatus)
	assert.Equal(t, "REFUNDED", bev[0].ToStatus)
	assert.Equal(t, "ADMIN", bev[0].ActorType)
	assert.Contains(t, bev[0].Metadata, `"seats_released": true`)

	_, tok := env.newUser(t, "next@test.local", "user")
	code, m := env.book(tok, bkShowtime, bkSeatA1)
	assert.Equal(t, http.StatusCreated, code, m, "released seat can be booked again")

	code, m = env.requestRefund(pid, uuid.NewString())
	assert.Equal(t, http.StatusUnprocessableEntity, code, m)
	assert.Equal(t, "REFUND_NOT_ELIGIBLE", errCode(m))
}

func TestAdmin_RefundSuccessStartedShowtimeKeepsSeats(t *testing.T) {
	env := newAdminEnv(t)
	bid, pid := env.paidBooking(t, "late@test.local", bkSeatA3)
	require.NoError(t, env.gdb.Exec(`UPDATE showtimes SET starts_at = now() - interval '1 hour' WHERE id = ?`, bkShowtime).Error)
	_, r := env.requestRefund(pid, uuid.NewString())
	code, done := env.process(str(r, "id"))
	require.Equal(t, http.StatusOK, code, done)
	assert.Equal(t, "COMPLETED", str(done, "status"))
	assert.Equal(t, "REFUNDED", env.status(t, "payments", pid))
	assert.Equal(t, "REFUNDED", env.status(t, "bookings", bid))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, bid))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id
		WHERE bi.booking_id = ? AND t.status = 'VOID'`, bid))
	bev := env.events(t, `booking_id = ? AND event_type = 'BOOKING_REFUNDED'`, bid)
	require.Len(t, bev, 1)
	assert.Contains(t, bev[0].Metadata, `"seats_released": false`)
}

func TestAdmin_RefundProviderFailureThenRetry(t *testing.T) {
	env := newAdminEnv(t)
	bid, pid := env.paidBooking(t, "pf@test.local", bkSeatA1)
	setRefundProvider(t, func(context.Context, string, int64) (string, error) { return "", errors.New("provider down") })

	_, r := env.requestRefund(pid, uuid.NewString())
	code, failed := env.process(str(r, "id"))
	require.Equal(t, http.StatusOK, code, failed)
	assert.Equal(t, "FAILED", str(failed, "status"))
	assert.Equal(t, "PROVIDER_FAILED", str(failed, "failure_code"))
	assert.Equal(t, "SUCCEEDED", env.status(t, "payments", pid))
	assert.Equal(t, "PAID", env.status(t, "bookings", bid))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id
		WHERE bi.booking_id = ? AND t.status = 'VALID'`, bid))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, bid))
	assertPayEvent(t, env.lastPayEvent(t, pid, "REFUND_FAILED"), "REFUND_FAILED", "FAILURE", "PROVIDER_FAILED", "SUCCEEDED")

	code, again := env.process(str(r, "id"))
	require.Equal(t, http.StatusOK, code, again)
	assert.Equal(t, "FAILED", str(again, "status"))
	assert.EqualValues(t, 1, env.payEventCount(t, pid, "REFUND_FAILED"), "re-processing a FAILED refund adds no event")

	service.RefundProvider = func(_ context.Context, id string, _ int64) (string, error) { return "retry_" + id, nil }
	code, r2 := env.requestRefund(pid, uuid.NewString())
	require.Equal(t, http.StatusCreated, code, r2)
	code, done := env.process(str(r2, "id"))
	require.Equal(t, http.StatusOK, code, done)
	assert.Equal(t, "COMPLETED", str(done, "status"))
	assert.Equal(t, "REFUNDED", env.status(t, "payments", pid))
	assert.Equal(t, "REFUNDED", env.status(t, "bookings", bid))
}

func TestAdmin_RefundProcessTwiceAndConcurrently(t *testing.T) {
	env := newAdminEnv(t)
	bid, pid := env.paidBooking(t, "cc@test.local", bkSeatA1, bkSeatA2)
	var calls atomic.Int32
	setRefundProvider(t, func(_ context.Context, id string, _ int64) (string, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return "ref_" + id, nil
	})
	_, r := env.requestRefund(pid, uuid.NewString())
	refundID := str(r, "id")

	const n = 8
	var wg sync.WaitGroup
	statuses := make([]string, n)
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, r := env.process(refundID)
			codes[i], statuses[i] = c, str(r, "status")
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		assert.Equal(t, http.StatusOK, codes[i])
		assert.Equal(t, "COMPLETED", statuses[i])
	}
	assert.EqualValues(t, 1, calls.Load(), "provider called exactly once")

	code, m := env.process(refundID)
	require.Equal(t, http.StatusOK, code, m)
	assert.Equal(t, "COMPLETED", str(m, "status"))
	assert.EqualValues(t, 1, calls.Load())
	assert.EqualValues(t, 1, env.payEventCount(t, pid, "REFUND_COMPLETED"))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_events WHERE booking_id = ? AND event_type = 'BOOKING_REFUNDED'`, bid))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM refunds WHERE payment_id = ? AND status = 'COMPLETED'`, pid))
}

func TestAdmin_CheckIn(t *testing.T) {
	env := newAdminEnv(t)
	bid, _ := env.paidBooking(t, "ci@test.local", bkSeatA1, bkSeatA2, bkSeatA3)
	codes := env.ticketCodes(t, bid)
	require.Len(t, codes, 3)

	code, m := env.checkIn(codes[0])
	require.Equal(t, http.StatusOK, code, m)
	assert.Equal(t, "CHECKED_IN", str(m, "result"))
	ticket, _ := m["ticket"].(map[string]any)
	assert.Equal(t, "USED", str(ticket, "status"))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM tickets WHERE code = ? AND status = 'USED' AND checked_in_at IS NOT NULL`, codes[0]))

	code, m = env.checkIn(codes[0])
	assert.Equal(t, http.StatusUnprocessableEntity, code, m)
	assert.Equal(t, "TICKET_ALREADY_USED", errCode(m))

	require.NoError(t, env.gdb.Exec(`UPDATE tickets SET status = 'VOID' WHERE code = ?`, codes[1]).Error)
	code, m = env.checkIn(codes[1])
	assert.Equal(t, http.StatusUnprocessableEntity, code, m)
	assert.Equal(t, "TICKET_VOID", errCode(m))

	code, m = env.checkIn("does-not-exist")
	assert.Equal(t, http.StatusNotFound, code, m)
	assert.Equal(t, "NOT_FOUND", errCode(m))

	const n = 10
	var wg sync.WaitGroup
	results := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = env.checkIn(codes[2])
		}(i)
	}
	wg.Wait()
	ok, used := 0, 0
	for _, c := range results {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusUnprocessableEntity:
			used++
		}
	}
	assert.Equal(t, 1, ok, "exactly one concurrent check-in wins")
	assert.Equal(t, n-1, used)
}

func TestAdmin_StatsListsAndTimeline(t *testing.T) {
	env := newAdminEnv(t)
	// refunded booking (A1 10000)
	refBid, refPid := env.paidBooking(t, "st1@test.local", bkSeatA1)
	_, r := env.requestRefund(refPid, uuid.NewString())
	code, m := env.process(str(r, "id"))
	require.Equal(t, http.StatusOK, code, m)
	// paid booking with one used ticket (A2 20000)
	paidBid, _ := env.paidBooking(t, "st2@test.local", bkSeatA2)
	code, m = env.checkIn(env.ticketCodes(t, paidBid)[0])
	require.Equal(t, http.StatusOK, code, m)
	// NEEDS_REFUND payment (A3) and a plain pending booking (A4)
	_, tok3 := env.newUser(t, "st3@test.local", "user")
	_, nrPid := env.bookAndPay(t, tok3, bkSeatA3)
	code, m = env.signedWebhook(successBody(t, nrPid, 1))
	require.Equal(t, http.StatusOK, code, m)
	_, tok4 := env.newUser(t, "st4@test.local", "user")
	code, m = env.book(tok4, bkShowtime, bkSeatA4)
	require.Equal(t, http.StatusCreated, code, m)

	code, s := env.do(http.MethodGet, "/api/v1/admin/stats", env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, s)

	var want struct {
		Revenue int64
		Tickets int64
		NR      int64
		Today   int64
	}
	require.NoError(t, env.gdb.Raw(`SELECT
		(SELECT COALESCE(SUM(amount_satang),0) FROM payments WHERE status IN ('SUCCEEDED','REFUNDED'))
		  - (SELECT COALESCE(SUM(amount_satang),0) FROM refunds WHERE status='COMPLETED') AS revenue,
		(SELECT COUNT(*) FROM tickets WHERE status IN ('VALID','USED')) AS tickets,
		(SELECT COUNT(*) FROM payments WHERE status = 'NEEDS_REFUND') AS nr,
		(SELECT COUNT(*) FROM bookings WHERE (created_at AT TIME ZONE 'UTC')::date = (now() AT TIME ZONE 'UTC')::date) AS today`).Scan(&want).Error)
	assert.EqualValues(t, want.Revenue, s["revenue_satang"])
	assert.EqualValues(t, 20000, s["revenue_satang"], "10000 + 20000 paid - 10000 refunded")
	assert.EqualValues(t, want.Tickets, s["tickets_sold"])
	assert.EqualValues(t, 1, s["tickets_sold"])
	assert.EqualValues(t, want.NR, s["needs_refund_count"])
	assert.EqualValues(t, 1, s["needs_refund_count"])
	assert.EqualValues(t, want.Today, s["bookings_today"])
	assert.EqualValues(t, 4, s["bookings_today"])
	assert.Equal(t, map[string]any{"PAID": 1.0, "REFUNDED": 1.0, "PENDING": 2.0}, s["bookings_by_status"])
	assert.Equal(t, map[string]any{"SUCCEEDED": 1.0, "REFUNDED": 1.0, "NEEDS_REFUND": 1.0}, s["payments_by_status"])

	code, p := env.do(http.MethodGet, "/api/v1/admin/payments?status=NEEDS_REFUND", env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, p)
	items := p["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, nrPid, str(items[0].(map[string]any), "id"))
	assert.Equal(t, "AMOUNT_MISMATCH", str(items[0].(map[string]any), "failure_code"))
	code, _ = env.do(http.MethodGet, "/api/v1/admin/payments?status=BOGUS", env.adminTok, nil)
	assert.Equal(t, http.StatusBadRequest, code)

	code, b := env.do(http.MethodGet, "/api/v1/admin/bookings?status=PAID", env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, b)
	items = b["items"].([]any)
	require.Len(t, items, 1)
	row := items[0].(map[string]any)
	assert.Equal(t, paidBid, str(row, "id"))
	assert.Equal(t, "st2@test.local", str(row, "user_email"))
	assert.Equal(t, []any{"A2"}, row["seats"])
	assert.Equal(t, "SUCCEEDED", str(row, "latest_payment_status"))

	code, b = env.do(http.MethodGet, "/api/v1/admin/bookings?email=st2@test.local", env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, b)
	assert.Len(t, b["items"].([]any), 1)
	code, b = env.do(http.MethodGet, "/api/v1/admin/bookings?showtime_id="+bkShowtime, env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, b)
	assert.GreaterOrEqual(t, len(b["items"].([]any)), 4)
	var uid string
	require.NoError(t, env.gdb.Raw(`SELECT id FROM users WHERE email = ?`, "st2@test.local").Scan(&uid).Error)
	code, b = env.do(http.MethodGet, "/api/v1/admin/bookings?user_id="+uid+"&status=PAID", env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, b)
	require.Len(t, b["items"].([]any), 1)
	code, _ = env.do(http.MethodGet, "/api/v1/admin/bookings?showtime_id=not-a-uuid", env.adminTok, nil)
	assert.Equal(t, http.StatusBadRequest, code)

	code, refunds := env.do(http.MethodGet, "/api/v1/admin/refunds?status=COMPLETED", env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, refunds)
	assert.Len(t, refunds["items"].([]any), 1)

	code, tl := env.do(http.MethodGet, "/api/v1/admin/bookings/"+refBid+"/timeline", env.adminTok, nil)
	require.Equal(t, http.StatusOK, code, tl)
	assert.Equal(t, "REFUNDED", str(tl["booking"].(map[string]any), "status"))
	assert.Len(t, tl["payments"].([]any), 1)
	assert.Len(t, tl["refunds"].([]any), 1)
	var types []string
	for _, e := range tl["events"].([]any) {
		em := e.(map[string]any)
		types = append(types, str(em, "source")+":"+str(em, "event_type"))
	}
	// Events of one transaction share created_at (DEFAULT now()), so only the set and the first entry are fixed.
	assert.ElementsMatch(t, []string{
		"booking:BOOKING_CREATED", "payment:PAYMENT_CREATED", "payment:WEBHOOK_RECEIVED", "payment:PAYMENT_SUCCEEDED",
		"booking:BOOKING_PAID", "booking:TICKETS_ISSUED", "payment:REFUND_REQUESTED", "payment:REFUND_COMPLETED",
		"booking:BOOKING_REFUNDED",
	}, types)
	require.NotEmpty(t, types)
	assert.Equal(t, "booking:BOOKING_CREATED", types[0])
	code, _ = env.do(http.MethodGet, "/api/v1/admin/bookings/"+uuid.NewString()+"/timeline", env.adminTok, nil)
	assert.Equal(t, http.StatusNotFound, code)
}

func TestAdmin_CatalogCacheInvalidatedAndVisibleToUsers(t *testing.T) {
	env := newAdminEnv(t)
	ctx := context.Background()
	const listKey = "events:list:from=&limit=20&page=1&q=&to="
	staleList := `{"items":[],"page":1,"limit":20,"total":0}`
	require.NoError(t, env.rdb.Set(ctx, listKey, staleList, time.Minute).Err())
	require.NoError(t, env.rdb.Set(ctx, "events:list:stale", "x", time.Minute).Err())

	code, cached := env.do(http.MethodGet, "/api/v1/events", "", nil)
	require.Equal(t, http.StatusOK, code, cached)
	assert.EqualValues(t, 0, cached["total"], "poisoned cache should hide events until invalidation")

	title := "CacheVisibleConcert-" + uuid.NewString()[:8]
	starts := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	code, ev := env.do(http.MethodPost, "/api/v1/admin/events", env.adminTok, map[string]any{
		"title": title, "venue": "Hall", "status": "published",
	})
	require.Equal(t, http.StatusCreated, code, ev)
	eventID := str(ev, "id")
	code, st := env.do(http.MethodPost, "/api/v1/admin/events/"+eventID+"/showtimes", env.adminTok, map[string]any{
		"starts_at": starts, "rows": 2, "seats_per_row": 5, "price_satang": 12000,
	})
	require.Equal(t, http.StatusCreated, code, st)

	n, err := env.rdb.Exists(ctx, "events:list:stale", listKey).Result()
	require.NoError(t, err)
	assert.Zero(t, n, "admin create must invalidate events:list:* keys")

	code, list := env.do(http.MethodGet, "/api/v1/events?q="+title, "", nil)
	require.Equal(t, http.StatusOK, code, list)
	items, _ := list["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, title, str(items[0].(map[string]any), "title"))
}

func TestAdmin_RowPriceChangePreservesBookingItemPrices(t *testing.T) {
	env := newAdminEnv(t)
	bid, _ := env.paidBooking(t, "price@test.local", bkSeatA1)
	var itemPrice int64
	require.NoError(t, env.gdb.Raw(
		`SELECT bi.price_satang FROM booking_items bi WHERE bi.booking_id = ? AND bi.seat_id = ?`, bid, bkSeatA1).Scan(&itemPrice).Error)
	require.EqualValues(t, 10000, itemPrice)

	code, m := env.do(http.MethodPut, "/api/v1/admin/showtimes/"+bkShowtime, env.adminTok, map[string]any{
		"price_satang_by_row": map[string]int64{"A": 99999},
	})
	require.Equal(t, http.StatusOK, code, m)
	assert.EqualValues(t, 99999, env.count(t,
		`SELECT price_satang FROM seats WHERE showtime_id = ? AND row_label = 'A' AND seat_number = 1`, bkShowtime))

	var after int64
	require.NoError(t, env.gdb.Raw(
		`SELECT bi.price_satang FROM booking_items bi WHERE bi.booking_id = ? AND bi.seat_id = ?`, bid, bkSeatA1).Scan(&after).Error)
	assert.EqualValues(t, itemPrice, after, "booking_items keep the price captured at booking time")
}

func TestAdmin_RefundAmountExceedsPayment(t *testing.T) {
	env := newAdminEnv(t)
	bid, pid := env.paidBooking(t, "exceed@test.local", bkSeatA2)
	amount := env.amount(t, pid)
	require.EqualValues(t, 20000, amount)

	// Simulate legacy partial completed refund total (full refunds are payment-sized; sum + new full refund exceeds cap).
	require.NoError(t, env.gdb.Exec(
		`INSERT INTO refunds (payment_id, booking_id, amount_satang, status, reason_code, requested_by, completed_at)
		 VALUES (?, ?, ?, 'COMPLETED', 'LEGACY_PARTIAL', ?::uuid, now())`,
		pid, bid, amount/2, env.adminID).Error)

	code, m := env.requestRefund(pid, uuid.NewString())
	assert.Equal(t, http.StatusUnprocessableEntity, code, m)
	assert.Equal(t, "AMOUNT_EXCEEDS_PAYMENT", errCode(m))
	ev := env.lastPayEvent(t, pid, "REFUND_REJECTED")
	assertPayEvent(t, ev, "REFUND_REJECTED", "FAILURE", "AMOUNT_EXCEEDS_PAYMENT", "SUCCEEDED")
	assert.Zero(t, env.count(t, `SELECT COUNT(*) FROM refunds WHERE payment_id = ? AND status = 'REQUESTED'`, pid))
}

func TestAdmin_ShowtimeUpdateWithPaidAndPendingBookings(t *testing.T) {
	env := newAdminEnv(t)
	require.NoError(t, env.gdb.Exec(`ALTER TABLE seats DROP CONSTRAINT IF EXISTS test_block_seat_30`).Error)
	starts := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	code, ev := env.do(http.MethodPost, "/api/v1/admin/events", env.adminTok, map[string]any{"title": "D5 Show", "venue": "Hall"})
	require.Equal(t, http.StatusCreated, code, ev)
	eventID := str(ev, "id")
	code, st := env.do(http.MethodPost, "/api/v1/admin/events/"+eventID+"/showtimes", env.adminTok, map[string]any{
		"starts_at": starts, "rows": 2, "seats_per_row": 4, "price_satang": 10000,
	})
	require.Equal(t, http.StatusCreated, code, st)
	showID := str(st, "id")
	var seats []string
	require.NoError(t, env.gdb.Raw(
		`SELECT id FROM seats WHERE showtime_id = ? ORDER BY row_label, seat_number LIMIT 2`, showID).Scan(&seats).Error)
	require.Len(t, seats, 2)

	_, tokPaid := env.newUser(t, "d5paid@test.local", "user")
	code, bPaid := env.book(tokPaid, showID, seats[0])
	require.Equal(t, http.StatusCreated, code, bPaid)
	paidBid := str(bPaid, "id")
	code, pPaid := env.createPayment(tokPaid, paidBid, uuid.NewString())
	require.Equal(t, http.StatusCreated, code, pPaid)
	pid := str(pPaid, "id")
	code, m := env.signedWebhook(successBody(t, pid, env.amount(t, pid)))
	require.Equal(t, http.StatusOK, code, m)
	require.Equal(t, "PAID", env.status(t, "bookings", paidBid))
	require.Equal(t, "SUCCEEDED", env.status(t, "payments", pid))

	_, tokPend := env.newUser(t, "d5pend@test.local", "user")
	code, bPend := env.book(tokPend, showID, seats[1])
	require.Equal(t, http.StatusCreated, code, bPend)
	pendBid := str(bPend, "id")
	require.Equal(t, "PENDING", env.status(t, "bookings", pendBid))

	code, m = env.do(http.MethodPut, "/api/v1/admin/showtimes/"+showID, env.adminTok, map[string]any{"status": "closed"})
	require.Equal(t, http.StatusOK, code, m)
	assert.Equal(t, "closed", str(m, "status"))

	assert.Equal(t, "PAID", env.status(t, "bookings", paidBid))
	assert.Equal(t, "PENDING", env.status(t, "bookings", pendBid))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, paidBid))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, pendBid))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id
		WHERE bi.booking_id = ? AND t.status = 'VALID'`, paidBid))

	code, _ = env.do(http.MethodPut, "/api/v1/admin/showtimes/"+showID, env.adminTok, map[string]any{"status": "deleted"})
	assert.Equal(t, http.StatusBadRequest, code)

	code, m = env.do(http.MethodPut, "/api/v1/admin/showtimes/"+showID, env.adminTok, map[string]any{"status": "cancelled"})
	require.Equal(t, http.StatusOK, code, m)
	assert.Equal(t, "cancelled", str(m, "status"))
	assert.Equal(t, "PAID", env.status(t, "bookings", paidBid))
	assert.Equal(t, "PENDING", env.status(t, "bookings", pendBid))
}

func TestAdmin_RefundFlowEventsAppendOnly(t *testing.T) {
	env := newAdminEnv(t)
	bid, pid := env.paidBooking(t, "append-refund@test.local", bkSeatA1)
	code, r := env.requestRefund(pid, uuid.NewString())
	require.Equal(t, http.StatusCreated, code, r)
	refundID := str(r, "id")
	code, done := env.process(refundID)
	require.Equal(t, http.StatusOK, code, done)
	assert.Equal(t, "COMPLETED", str(done, "status"))

	var payEvID, bookEvID string
	require.NoError(t, env.gdb.Raw(
		`SELECT id FROM payment_events WHERE payment_id = ? AND event_type = 'REFUND_COMPLETED' LIMIT 1`, pid).Scan(&payEvID).Error)
	require.NoError(t, env.gdb.Raw(
		`SELECT id FROM booking_events WHERE booking_id = ? AND event_type = 'BOOKING_REFUNDED' LIMIT 1`, bid).Scan(&bookEvID).Error)
	require.NotEmpty(t, payEvID)
	require.NotEmpty(t, bookEvID)

	err := env.gdb.Exec(`UPDATE payment_events SET reason_code = 'tamper' WHERE id = ?`, payEvID).Error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append-only")
	err = env.gdb.Exec(`DELETE FROM payment_events WHERE id = ?`, payEvID).Error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append-only")

	err = env.gdb.Exec(`UPDATE booking_events SET reason_code = 'tamper' WHERE id = ?`, bookEvID).Error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append-only")
	err = env.gdb.Exec(`DELETE FROM booking_events WHERE id = ?`, bookEvID).Error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append-only")
}
