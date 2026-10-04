package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ticketbooking/internal/repository"
	"ticketbooking/internal/service"
)

const payWebhookSecret = "payment-webhook-test-secret-payment-webhook-test"

type payEnv struct {
	*bkEnv
	srv *httptest.Server
}

// newPayEnv adds the payment routes to a fresh booking env. The router is also served over real
// HTTP so the mock gateway delivers its webhook through the network stack.
func newPayEnv(t *testing.T, mockGateway bool, o bkOpts) *payEnv {
	t.Helper()
	env := newBkEnv(t, o)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { env.r.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)

	payRepo := repository.NewPaymentRepository(env.gdb)
	psvc := service.NewPaymentService(payRepo, repository.NewBookingRepository(env.gdb), env.svc, payWebhookSecret)
	deps := PaymentDeps{Payment: NewPaymentHandler(psvc), Ticket: NewTicketHandler(psvc), Verify: env.auth.VerifyToken}
	if mockGateway {
		deps.MockGateway = NewMockGatewayHandler(service.NewMockGateway(payRepo, payWebhookSecret, srv.URL+"/api/v1/payments/webhook"))
	}
	v1 := env.r.Group("/api/v1")
	RegisterPaymentRoutes(v1, deps)
	catalogSvc := service.NewCatalogService(repository.NewCatalogRepository(env.gdb), env.rdb)
	v1.GET("/showtimes/:id/seats", NewCatalogHandler(catalogSvc).GetShowtimeSeats)
	return &payEnv{bkEnv: env, srv: srv}
}

func (e *payEnv) raw(method, path, token string, body []byte, headers map[string]string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m) // non-JSON bodies leave m nil, which tests handle
	return w.Code, m
}

func (e *payEnv) createPayment(token, bookingID, key string) (int, map[string]any) {
	h := map[string]string{}
	if key != "" {
		h["Idempotency-Key"] = key
	}
	return e.raw(http.MethodPost, "/api/v1/bookings/"+bookingID+"/payments", token, nil, h)
}

func (e *payEnv) webhook(body []byte, sig string) (int, map[string]any) {
	h := map[string]string{}
	if sig != "" {
		h["X-Signature"] = sig
	}
	return e.raw(http.MethodPost, "/api/v1/payments/webhook", "", body, h)
}

func successBody(t *testing.T, paymentID string, amount int64) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"event_id": uuid.NewString(), "payment_id": paymentID, "status": "succeeded",
		"amount_satang": amount, "provider_ref": "test_ref",
	})
	require.NoError(t, err)
	return b
}

func (e *payEnv) signedWebhook(body []byte) (int, map[string]any) {
	return e.webhook(body, service.Sign([]byte(payWebhookSecret), body))
}

func (e *payEnv) mockPay(paymentID, result string) (int, map[string]any) {
	return e.do(http.MethodPost, "/api/v1/mock-gateway/"+paymentID+"/pay", "", map[string]any{"result": result})
}

// bookAndPay books seats and starts a payment; returns booking id and payment id.
func (e *payEnv) bookAndPay(t *testing.T, tok string, seats ...string) (string, string) {
	t.Helper()
	code, b := e.book(tok, bkShowtime, seats...)
	require.Equal(t, http.StatusCreated, code, b)
	code, p := e.createPayment(tok, str(b, "id"), uuid.NewString())
	require.Equal(t, http.StatusCreated, code, p)
	return str(b, "id"), str(p, "id")
}

type payEvRow struct {
	PaymentID      *string
	BookingID      *string
	EventType      string
	Outcome        string
	ReasonCode     string
	FromStatus     string
	ToStatus       string
	SignatureValid *bool
	RawPayload     string
	CreatedAt      time.Time
}

func (e *payEnv) payEvents(t *testing.T, where string, args ...any) []payEvRow {
	t.Helper()
	var rows []payEvRow
	require.NoError(t, e.gdb.Raw(
		`SELECT payment_id, booking_id, event_type, outcome, reason_code, from_status, to_status, signature_valid,
		        raw_payload::text AS raw_payload, created_at
		   FROM payment_events WHERE `+where+` ORDER BY created_at, id`, args...).Scan(&rows).Error)
	return rows
}

func (e *payEnv) status(t *testing.T, table, id string) string {
	t.Helper()
	var s string
	require.NoError(t, e.gdb.Raw(`SELECT status FROM `+table+` WHERE id = ?`, id).Scan(&s).Error)
	return s
}

func (e *payEnv) ticketCount(t *testing.T, bookingID string) int64 {
	return e.count(t, `SELECT COUNT(*) FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id
		WHERE bi.booking_id = ?`, bookingID)
}

func assertPayEvent(t *testing.T, ev payEvRow, eventType, outcome, reason, toStatus string) {
	t.Helper()
	assert.Equal(t, eventType, ev.EventType)
	assert.Equal(t, outcome, ev.Outcome)
	assert.Equal(t, reason, ev.ReasonCode)
	assert.Equal(t, toStatus, ev.ToStatus)
}

func (e *payEnv) seatStatusInShowtime(t *testing.T, showtimeID, seatID string) string {
	t.Helper()
	code, m := e.do(http.MethodGet, "/api/v1/showtimes/"+showtimeID+"/seats", "", nil)
	require.Equal(t, http.StatusOK, code)
	seats, _ := m["seats"].([]any)
	for _, s := range seats {
		sm, _ := s.(map[string]any)
		if str(sm, "id") == seatID {
			return str(sm, "status")
		}
	}
	t.Fatalf("seat %s not found in showtime %s", seatID, showtimeID)
	return ""
}

func assertWebhookRawPayloadStored(t *testing.T, env *payEnv, paymentID string, amountSatang int64) {
	t.Helper()
	recv := env.payEvents(t, `payment_id = ? AND event_type = 'WEBHOOK_RECEIVED'`, paymentID)
	require.Len(t, recv, 1)
	require.NotNil(t, recv[0].SignatureValid)
	assert.True(t, *recv[0].SignatureValid)
	assert.Contains(t, recv[0].RawPayload, paymentID)
	assert.Contains(t, recv[0].RawPayload, `"status": "succeeded"`)
	assert.Contains(t, recv[0].RawPayload, strconv.FormatInt(amountSatang, 10))
	assert.NotContains(t, recv[0].RawPayload, "X-Signature")
}

type paySuccessSeqRow struct {
	EventType  string
	Outcome    string
	ReasonCode string
	FromStatus string
	ToStatus   string
	CreatedAt  time.Time
	Tbl        int
	Xmin       string
}

// paySuccessProcessingSequence returns the four P4-13 success-path audit events in commit order.
func (e *payEnv) paySuccessProcessingSequence(t *testing.T, paymentID, bookingID string) []paySuccessSeqRow {
	t.Helper()
	var rows []paySuccessSeqRow
	require.NoError(t, e.gdb.Raw(`
		SELECT event_type, outcome, reason_code, from_status, to_status, created_at, tbl, xmin::text AS xmin FROM (
			SELECT event_type, outcome, reason_code, from_status, to_status, created_at, xmin, 1 AS tbl
			  FROM payment_events
			 WHERE payment_id = ? AND event_type IN ('WEBHOOK_RECEIVED', 'PAYMENT_SUCCEEDED')
			UNION ALL
			SELECT event_type, outcome, reason_code, from_status, to_status, created_at, xmin, 2 AS tbl
			  FROM booking_events
			 WHERE booking_id = ? AND event_type IN ('BOOKING_PAID', 'TICKETS_ISSUED')
		) q
		ORDER BY created_at, tbl, xmin`, paymentID, bookingID).Scan(&rows).Error)
	return rows
}

func assertP413SuccessEventOrder(t *testing.T, seq []paySuccessSeqRow) {
	t.Helper()
	require.Len(t, seq, 4, "expected WEBHOOK_RECEIVED → PAYMENT_SUCCEEDED → BOOKING_PAID → TICKETS_ISSUED")
	want := []string{"WEBHOOK_RECEIVED", "PAYMENT_SUCCEEDED", "BOOKING_PAID", "TICKETS_ISSUED"}
	for i, w := range want {
		assert.Equalf(t, w, seq[i].EventType, "event order at index %d", i)
	}
	assert.True(t, seq[0].CreatedAt.Before(seq[1].CreatedAt),
		"WEBHOOK_RECEIVED must be committed before the payment success transaction")
	for i := 2; i < 4; i++ {
		assert.True(t, seq[i].CreatedAt.Equal(seq[1].CreatedAt),
			"%s should share the PAYMENT_SUCCEEDED transaction timestamp", seq[i].EventType)
	}
	assertPayEvent(t, payEvRow{EventType: seq[1].EventType, Outcome: seq[1].Outcome, ReasonCode: seq[1].ReasonCode, ToStatus: seq[1].ToStatus},
		"PAYMENT_SUCCEEDED", "SUCCESS", "", "SUCCEEDED")
	assert.Equal(t, "PENDING", seq[1].FromStatus)
	assertPayEvent(t, payEvRow{EventType: seq[2].EventType, Outcome: seq[2].Outcome, ReasonCode: seq[2].ReasonCode, ToStatus: seq[2].ToStatus},
		"BOOKING_PAID", "SUCCESS", "", "PAID")
	assertPayEvent(t, payEvRow{EventType: seq[3].EventType, Outcome: seq[3].Outcome, ReasonCode: seq[3].ReasonCode, ToStatus: seq[3].ToStatus},
		"TICKETS_ISSUED", "SUCCESS", "", "PAID")
}

// ---------- tests ----------

func TestPayment_SuccessIssuesTickets(t *testing.T) {
	env := newPayEnv(t, true, bkOpts{})
	_, tok := env.newUser(t, "pay1@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok, bkSeatA1, bkSeatA2)
	assert.Equal(t, "held", env.seatStatusInShowtime(t, bkShowtime, bkSeatA1))
	assert.Equal(t, "held", env.seatStatusInShowtime(t, bkShowtime, bkSeatA2))

	created := env.payEvents(t, `payment_id = ? AND event_type = 'PAYMENT_CREATED'`, paymentID)
	require.Len(t, created, 1)
	assertPayEvent(t, created[0], "PAYMENT_CREATED", "SUCCESS", "", "PENDING")

	code, p := env.do(http.MethodGet, "/api/v1/payments/"+paymentID, tok, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "PENDING", str(p, "status"))
	assert.EqualValues(t, 30000, p["amount_satang"], "amount is the DB sum of item prices")
	assert.Equal(t, "/pay/"+paymentID, str(p, "pay_url"))

	code, res := env.mockPay(paymentID, "success")
	require.Equal(t, http.StatusOK, code, res)
	assert.EqualValues(t, 200, res["webhook_status"])

	assert.Equal(t, "SUCCEEDED", env.status(t, "payments", paymentID))
	assert.Equal(t, "PAID", env.status(t, "bookings", bookingID))
	assert.EqualValues(t, 2, env.ticketCount(t, bookingID))
	assert.EqualValues(t, 2, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, bookingID))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM payments WHERE id = ? AND paid_at IS NOT NULL AND provider_ref LIKE 'mock_%'`, paymentID))
	assert.False(t, env.holdExists(t, bkSeatA1), "hold key released after payment")
	assert.False(t, env.holdExists(t, bkSeatA2))
	assert.Equal(t, "sold", env.seatStatusInShowtime(t, bkShowtime, bkSeatA1))
	assert.Equal(t, "sold", env.seatStatusInShowtime(t, bkShowtime, bkSeatA2))

	assertWebhookRawPayloadStored(t, env, paymentID, 30000)
	assertP413SuccessEventOrder(t, env.paySuccessProcessingSequence(t, paymentID, bookingID))

	code, bk := env.do(http.MethodGet, "/api/v1/bookings/"+bookingID, tok, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "PAID", str(bk, "status"))
	pay, _ := bk["payment"].(map[string]any)
	assert.Equal(t, "SUCCEEDED", str(pay, "status"))
	tickets, _ := bk["tickets"].([]any)
	require.Len(t, tickets, 2)

	code, list := env.do(http.MethodGet, "/api/v1/tickets", tok, nil)
	require.Equal(t, http.StatusOK, code)
	items, _ := list["items"].([]any)
	require.Len(t, items, 2)
	first, _ := items[0].(map[string]any)
	ticketCode := str(first, "code")
	assert.Len(t, ticketCode, 22, "16 random bytes, base64url without padding")
	code, tk := env.do(http.MethodGet, "/api/v1/tickets/"+ticketCode, tok, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "Test Event", str(tk, "event_title"))
	assert.Equal(t, "A1", str(tk, "seat_label"))
	assert.Equal(t, "VALID", str(tk, "status"))
}

func TestPayment_GatewayFailureThenRetry(t *testing.T) {
	env := newPayEnv(t, true, bkOpts{})
	_, tok := env.newUser(t, "fail@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok, bkSeatA1)

	code, res := env.mockPay(paymentID, "fail")
	require.Equal(t, http.StatusOK, code, res)
	code, p := env.do(http.MethodGet, "/api/v1/payments/"+paymentID, tok, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "FAILED", str(p, "status"))
	assert.Equal(t, "GATEWAY_DECLINED", str(p, "failure_code"))
	assert.NotEmpty(t, str(p, "failure_message"))
	assert.Equal(t, "PENDING", env.status(t, "bookings", bookingID))
	assert.EqualValues(t, 0, env.ticketCount(t, bookingID))
	evs := env.payEvents(t, `payment_id = ? AND event_type = 'PAYMENT_FAILED'`, paymentID)
	require.Len(t, evs, 1)
	assertPayEvent(t, evs[0], "PAYMENT_FAILED", "FAILURE", "GATEWAY_DECLINED", "FAILED")

	code, p2 := env.createPayment(tok, bookingID, uuid.NewString())
	require.Equal(t, http.StatusCreated, code, p2)
	retryID := str(p2, "id")
	assert.NotEqual(t, paymentID, retryID)
	code, _ = env.mockPay(retryID, "success")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "SUCCEEDED", env.status(t, "payments", retryID))
	assert.Equal(t, "FAILED", env.status(t, "payments", paymentID))
	assert.Equal(t, "PAID", env.status(t, "bookings", bookingID))
	assert.EqualValues(t, 1, env.ticketCount(t, bookingID))
	assertP413SuccessEventOrder(t, env.paySuccessProcessingSequence(t, retryID, bookingID))
}

func TestPayment_DuplicateWebhookSequential(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "dup@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok, bkSeatA1, bkSeatA2)
	body := successBody(t, paymentID, 30000)

	code, r1 := env.signedWebhook(body)
	require.Equal(t, http.StatusOK, code, r1)
	assert.Equal(t, "processed", str(r1, "result"))
	code, r2 := env.signedWebhook(body)
	require.Equal(t, http.StatusOK, code, r2)
	assert.Equal(t, "ignored", str(r2, "result"))

	assert.EqualValues(t, 2, env.ticketCount(t, bookingID))
	dup := env.payEvents(t, `payment_id = ? AND event_type = 'PAYMENT_DUPLICATE_IGNORED'`, paymentID)
	require.Len(t, dup, 1)
	assertPayEvent(t, dup[0], "PAYMENT_DUPLICATE_IGNORED", "IGNORED", "DUPLICATE_EVENT", "SUCCEEDED")
	assert.Equal(t, "SUCCEEDED", dup[0].FromStatus)
	assert.Len(t, env.payEvents(t, `payment_id = ? AND event_type = 'PAYMENT_SUCCEEDED'`, paymentID), 1)
	seq := env.paySuccessProcessingSequence(t, paymentID, bookingID)
	require.GreaterOrEqual(t, len(seq), 4)
	assertP413SuccessEventOrder(t, seq[:4])
}

func TestPayment_DuplicateWebhookConcurrent(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "dupc@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok, bkSeatA1, bkSeatA2, bkSeatA3)
	body := successBody(t, paymentID, 60000)

	const n = 10
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i], _ = env.signedWebhook(body)
		}(i)
	}
	close(start)
	wg.Wait()
	for _, c := range codes {
		assert.Equal(t, http.StatusOK, c)
	}

	assert.EqualValues(t, 3, env.ticketCount(t, bookingID), "exactly one ticket set")
	assert.Len(t, env.payEvents(t, `payment_id = ? AND event_type = 'WEBHOOK_RECEIVED'`, paymentID), n)
	assert.Len(t, env.payEvents(t, `payment_id = ? AND event_type = 'PAYMENT_SUCCEEDED'`, paymentID), 1)
	assert.Len(t, env.payEvents(t, `payment_id = ? AND event_type = 'PAYMENT_DUPLICATE_IGNORED' AND outcome = 'IGNORED'`, paymentID), n-1)
	assert.Len(t, env.events(t, `booking_id = ? AND event_type = 'BOOKING_PAID'`, bookingID), 1)
	assert.Len(t, env.events(t, `booking_id = ? AND event_type = 'TICKETS_ISSUED'`, bookingID), 1)
	assert.Equal(t, "PAID", env.status(t, "bookings", bookingID))
}

func TestPayment_InvalidSignatureRejected(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "sig@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok, bkSeatA1)
	body := successBody(t, paymentID, 10000)
	goodSig := service.Sign([]byte(payWebhookSecret), body)
	tampered := successBody(t, paymentID, 1)

	cases := []struct {
		name string
		body []byte
		sig  string
	}{
		{"missing", body, ""},
		{"wrong secret", body, service.Sign([]byte("other-secret"), body)},
		{"not hex", body, "zz-not-hex"},
		{"tampered body", tampered, goodSig},
	}
	for i, tc := range cases {
		code, m := env.webhook(tc.body, tc.sig)
		assert.Equal(t, http.StatusUnauthorized, code, tc.name)
		assert.Equal(t, "INVALID_SIGNATURE", errCode(m), tc.name)
		rej := env.payEvents(t, `event_type = 'WEBHOOK_REJECTED' AND reason_code = 'INVALID_SIGNATURE'`)
		require.Len(t, rej, i+1, tc.name)
		assertPayEvent(t, rej[i], "WEBHOOK_REJECTED", "FAILURE", "INVALID_SIGNATURE", "")
		require.NotNil(t, rej[i].SignatureValid)
		assert.False(t, *rej[i].SignatureValid)
		assert.Contains(t, rej[i].RawPayload, paymentID, "raw payload kept for tracing")
		assert.NotContains(t, rej[i].RawPayload, goodSig, "signature never stored")
		recv := env.payEvents(t, `event_type = 'WEBHOOK_RECEIVED' AND reason_code = ''`)
		require.Len(t, recv, i+1, tc.name)
		require.NotNil(t, recv[i].SignatureValid)
		assert.False(t, *recv[i].SignatureValid, "received event records that the signature was invalid")
	}
	assert.Equal(t, "PENDING", env.status(t, "payments", paymentID))
	assert.Equal(t, "PENDING", env.status(t, "bookings", bookingID))
	assert.EqualValues(t, 0, env.ticketCount(t, bookingID))
}

func TestPayment_MalformedAndUnknownWebhook(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})

	code, m := env.signedWebhook([]byte(`{not json`))
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "MALFORMED_PAYLOAD", errCode(m))
	code, _ = env.signedWebhook([]byte(`{"payment_id":"x","status":"weird"}`))
	assert.Equal(t, http.StatusBadRequest, code)
	rej := env.payEvents(t, `event_type = 'WEBHOOK_REJECTED' AND reason_code = 'MALFORMED_PAYLOAD'`)
	require.Len(t, rej, 2)
	assertPayEvent(t, rej[0], "WEBHOOK_REJECTED", "FAILURE", "MALFORMED_PAYLOAD", "")
	require.NotNil(t, rej[0].SignatureValid)
	assert.True(t, *rej[0].SignatureValid)
	assert.Contains(t, rej[0].RawPayload, "raw_text")
	malformedRecv := env.payEvents(t, `event_type = 'WEBHOOK_RECEIVED' AND reason_code = ''`)
	require.Len(t, malformedRecv, 2)
	require.NotNil(t, malformedRecv[0].SignatureValid)
	assert.True(t, *malformedRecv[0].SignatureValid)

	unknown := uuid.NewString()
	code, m = env.signedWebhook(successBody(t, unknown, 100))
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, "UNKNOWN_PAYMENT", errCode(m))
	assert.Len(t, env.payEvents(t, `event_type = 'WEBHOOK_RECEIVED' AND payment_id IS NULL AND raw_payload::text LIKE ?`, "%"+unknown+"%"), 1)
	unk := env.payEvents(t, `event_type = 'WEBHOOK_REJECTED' AND reason_code = 'UNKNOWN_PAYMENT'`)
	require.Len(t, unk, 1)
	assertPayEvent(t, unk[0], "WEBHOOK_REJECTED", "FAILURE", "UNKNOWN_PAYMENT", "")
	assert.Nil(t, unk[0].PaymentID)
}

func TestPayment_AmountMismatchNeedsRefund(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "amt@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok, bkSeatA1)
	body := successBody(t, paymentID, 9999)

	code, _ := env.signedWebhook(body)
	require.Equal(t, http.StatusOK, code)
	assertWebhookRawPayloadStored(t, env, paymentID, 9999)
	code, p := env.do(http.MethodGet, "/api/v1/payments/"+paymentID, tok, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "NEEDS_REFUND", str(p, "status"))
	assert.Equal(t, "AMOUNT_MISMATCH", str(p, "failure_code"))
	assert.NotEmpty(t, str(p, "failure_message"))
	assert.Equal(t, "PENDING", env.status(t, "bookings", bookingID))
	assert.EqualValues(t, 0, env.ticketCount(t, bookingID))
	evs := env.payEvents(t, `payment_id = ? AND event_type = 'PAYMENT_AMOUNT_MISMATCH'`, paymentID)
	require.Len(t, evs, 1)
	assertPayEvent(t, evs[0], "PAYMENT_AMOUNT_MISMATCH", "FAILURE", "AMOUNT_MISMATCH", "NEEDS_REFUND")
}

func TestPayment_AfterExpirySeatRebookedNeedsRefund(t *testing.T) {
	env := newPayEnv(t, true, bkOpts{ttl: 2 * time.Second})
	_, tok1 := env.newUser(t, "late1@example.com", "user")
	_, tok2 := env.newUser(t, "late2@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok1, bkSeatA1)

	time.Sleep(2500 * time.Millisecond) // hold (Redis TTL and expires_at) runs out
	code, other := env.book(tok2, bkShowtime, bkSeatA1)
	require.Equal(t, http.StatusCreated, code, other)
	otherID := str(other, "id")
	require.Equal(t, "EXPIRED", env.status(t, "bookings", bookingID), "lazily expired by the new booking")

	code, res := env.mockPay(paymentID, "success")
	require.Equal(t, http.StatusOK, code, res)
	assertWebhookRawPayloadStored(t, env, paymentID, 10000)
	code, p := env.do(http.MethodGet, "/api/v1/payments/"+paymentID, tok1, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "NEEDS_REFUND", str(p, "status"))
	assert.Equal(t, "BOOKING_EXPIRED", str(p, "failure_code"))
	assert.EqualValues(t, 0, env.ticketCount(t, bookingID))
	evs := env.payEvents(t, `payment_id = ? AND event_type = 'PAYMENT_AFTER_EXPIRY'`, paymentID)
	require.Len(t, evs, 1)
	assertPayEvent(t, evs[0], "PAYMENT_AFTER_EXPIRY", "FAILURE", "BOOKING_EXPIRED", "NEEDS_REFUND")

	assert.Equal(t, "PENDING", env.status(t, "bookings", otherID), "other user's booking untouched")
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, otherID))
	assert.EqualValues(t, 0, env.ticketCount(t, otherID))
}

func TestPayment_SeatLostAfterCancelAndRebookNeedsRefund(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok1 := env.newUser(t, "lost1@example.com", "user")
	_, tok2 := env.newUser(t, "lost2@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok1, bkSeatA1)

	code, cancelled := env.do(http.MethodDelete, "/api/v1/bookings/"+bookingID, tok1, nil)
	require.Equal(t, http.StatusOK, code, cancelled)
	require.Equal(t, "CANCELLED", env.status(t, "bookings", bookingID))

	code, other := env.book(tok2, bkShowtime, bkSeatA1)
	require.Equal(t, http.StatusCreated, code, other)
	otherID := str(other, "id")

	code, res := env.signedWebhook(successBody(t, paymentID, 10000))
	require.Equal(t, http.StatusOK, code, res)
	code, p := env.do(http.MethodGet, "/api/v1/payments/"+paymentID, tok1, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "NEEDS_REFUND", str(p, "status"))
	assert.Equal(t, "SEAT_LOST", str(p, "failure_code"))
	assert.EqualValues(t, 0, env.ticketCount(t, bookingID))
	assert.Equal(t, "CANCELLED", env.status(t, "bookings", bookingID))
	evs := env.payEvents(t, `payment_id = ? AND event_type = 'PAYMENT_AFTER_EXPIRY'`, paymentID)
	require.Len(t, evs, 1)
	assertPayEvent(t, evs[0], "PAYMENT_AFTER_EXPIRY", "FAILURE", "SEAT_LOST", "NEEDS_REFUND")

	assert.Equal(t, "PENDING", env.status(t, "bookings", otherID))
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, otherID))
}

func TestPayment_JustPastExpiresAtStillAccepted(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "d1@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok, bkSeatA1)
	require.NoError(t, env.gdb.Exec(`UPDATE bookings SET expires_at = now() - interval '1 second' WHERE id = ?`, bookingID).Error)

	code, _ := env.signedWebhook(successBody(t, paymentID, 10000))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "SUCCEEDED", env.status(t, "payments", paymentID))
	assert.Equal(t, "PAID", env.status(t, "bookings", bookingID))
	assert.EqualValues(t, 1, env.ticketCount(t, bookingID))
}

func TestPayment_ExpiryJobVsWebhookRace(t *testing.T) {
	seats := []string{bkSeatA1, bkSeatA2, bkSeatA3, bkSeatA4, bkSeatA5, bkSeatA6}
	env := newPayEnv(t, false, bkOpts{})
	prices := map[string]int64{bkSeatA1: 10000, bkSeatA2: 20000, bkSeatA3: 30000, bkSeatA4: 40000, bkSeatA5: 50000, bkSeatA6: 60000}
	paid, expired := 0, 0
	for i, seat := range seats {
		_, tok := env.newUser(t, "race"+uuid.NewString()[:8]+"@example.com", "user")
		bookingID, paymentID := env.bookAndPay(t, tok, seat)
		require.NoError(t, env.gdb.Exec(`UPDATE bookings SET expires_at = now() - interval '1 second' WHERE id = ?`, bookingID).Error)
		body := successBody(t, paymentID, prices[seat])

		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := env.svc.ExpireDue(context.Background())
			assert.NoError(t, err)
		}()
		go func() {
			defer wg.Done()
			<-start
			code, _ := env.signedWebhook(body)
			assert.Equal(t, http.StatusOK, code)
		}()
		close(start)
		wg.Wait()

		bs := env.status(t, "bookings", bookingID)
		ps := env.status(t, "payments", paymentID)
		nPaid := len(env.events(t, `booking_id = ? AND event_type = 'BOOKING_PAID'`, bookingID))
		nExp := len(env.events(t, `booking_id = ? AND event_type = 'BOOKING_EXPIRED'`, bookingID))
		assert.Equal(t, 1, nPaid+nExp, "iteration %d: exactly one of PAID / EXPIRED", i)
		switch bs {
		case "PAID":
			paid++
			assert.Equal(t, "SUCCEEDED", ps)
			assert.EqualValues(t, 1, env.ticketCount(t, bookingID))
		case "EXPIRED":
			expired++
			assert.Equal(t, "NEEDS_REFUND", ps)
			assert.EqualValues(t, 0, env.ticketCount(t, bookingID))
			assert.EqualValues(t, 0, env.count(t, `SELECT COUNT(*) FROM booking_items WHERE booking_id = ? AND active`, bookingID))
		default:
			t.Fatalf("iteration %d: unexpected booking status %s", i, bs)
		}
	}
	t.Logf("race outcomes: paid=%d expired=%d", paid, expired)
}

func TestPayment_CreateRules(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "rules@example.com", "user")
	code, b := env.book(tok, bkShowtime, bkSeatA1)
	require.Equal(t, http.StatusCreated, code)
	bookingID := str(b, "id")

	code, m := env.createPayment(tok, bookingID, "")
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "IDEMPOTENCY_KEY_REQUIRED", errCode(m))

	key := uuid.NewString()
	code, p1 := env.createPayment(tok, bookingID, key)
	require.Equal(t, http.StatusCreated, code, p1)
	assert.Equal(t, "PENDING", str(p1, "status"))
	code, p1again := env.createPayment(tok, bookingID, key)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, str(p1, "id"), str(p1again, "id"), "same key returns the same payment")
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM payments WHERE booking_id = ?`, bookingID))
	assert.Len(t, env.payEvents(t, `event_type = 'PAYMENT_CREATED'`), 1)

	code, m = env.createPayment(tok, bookingID, uuid.NewString())
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "BOOKING_NOT_PAYABLE", errCode(m))

	// expired booking (other user so the one-pending-per-showtime rule does not interfere)
	_, tok2 := env.newUser(t, "rules2@example.com", "user")
	code, b2 := env.book(tok2, bkShowtime, bkSeatA2)
	require.Equal(t, http.StatusCreated, code)
	require.NoError(t, env.gdb.Exec(`UPDATE bookings SET expires_at = now() - interval '1 second' WHERE id = ?`, str(b2, "id")).Error)
	code, m = env.createPayment(tok2, str(b2, "id"), uuid.NewString())
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "BOOKING_EXPIRED", errCode(m))

	// key already used for another booking
	code, m = env.createPayment(tok2, str(b2, "id"), key)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "VALIDATION_FAILED", errCode(m))

	failed := env.payEvents(t, `event_type = 'PAYMENT_CREATE_FAILED'`)
	reasons := []string{}
	for _, ev := range failed {
		assert.Equal(t, "FAILURE", ev.Outcome)
		reasons = append(reasons, ev.ReasonCode)
	}
	assert.Equal(t, []string{"VALIDATION_FAILED", "BOOKING_NOT_PAYABLE", "BOOKING_EXPIRED", "VALIDATION_FAILED"}, reasons)
	assert.Nil(t, failed[0].BookingID, "missing key is rejected before the booking is loaded")
	require.NotNil(t, failed[1].BookingID)
	assert.Equal(t, bookingID, *failed[1].BookingID)
}

func TestPayment_CancelledBookingNotPayable(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "cxl@example.com", "user")
	code, b := env.book(tok, bkShowtime, bkSeatA1)
	require.Equal(t, http.StatusCreated, code)
	code, _ = env.do(http.MethodDelete, "/api/v1/bookings/"+str(b, "id"), tok, nil)
	require.Equal(t, http.StatusOK, code)
	code, m := env.createPayment(tok, str(b, "id"), uuid.NewString())
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "BOOKING_NOT_PAYABLE", errCode(m))
	evs := env.payEvents(t, `event_type = 'PAYMENT_CREATE_FAILED'`)
	require.Len(t, evs, 1)
	assertPayEvent(t, evs[0], "PAYMENT_CREATE_FAILED", "FAILURE", "BOOKING_NOT_PAYABLE", "")
}

func TestPayment_OwnershipAndDisabledGateway(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok1 := env.newUser(t, "own1@example.com", "user")
	_, tok2 := env.newUser(t, "own2@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok1, bkSeatA1)
	code, _ := env.signedWebhook(successBody(t, paymentID, 10000))
	require.Equal(t, http.StatusOK, code)
	var ticketCode string
	require.NoError(t, env.gdb.Raw(`SELECT t.code FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id WHERE bi.booking_id = ?`, bookingID).Scan(&ticketCode).Error)

	code, m := env.do(http.MethodGet, "/api/v1/payments/"+paymentID, tok2, nil)
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, "NOT_FOUND", errCode(m))
	code, _ = env.do(http.MethodGet, "/api/v1/tickets/"+ticketCode, tok2, nil)
	assert.Equal(t, http.StatusNotFound, code)
	code, list := env.do(http.MethodGet, "/api/v1/tickets", tok2, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, list["items"])
	code, m = env.createPayment(tok2, bookingID, uuid.NewString())
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, "BOOKING_NOT_FOUND", errCode(m))
	code, _ = env.do(http.MethodGet, "/api/v1/payments/"+paymentID, "", nil)
	assert.Equal(t, http.StatusUnauthorized, code)

	code, _ = env.mockPay(paymentID, "success")
	assert.Equal(t, http.StatusNotFound, code, "mock gateway route absent when disabled")
}

func TestPayment_WebhookLookupFailureIsAudited(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "byid@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok, bkSeatA1)
	body := successBody(t, paymentID, 10000)
	psvc := service.NewPaymentService(repository.NewPaymentRepository(env.gdb), repository.NewBookingRepository(env.gdb), env.svc, payWebhookSecret)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := psvc.HandleWebhook(ctx, body, service.Sign([]byte(payWebhookSecret), body))
	require.Error(t, err)
	assert.Equal(t, "PENDING", env.status(t, "payments", paymentID))
	assert.Equal(t, "PENDING", env.status(t, "bookings", bookingID))
	assert.EqualValues(t, 0, env.ticketCount(t, bookingID))
	rej := env.payEvents(t, `event_type = 'WEBHOOK_REJECTED' AND reason_code = 'INTERNAL_ERROR' AND payment_id IS NULL`)
	require.Len(t, rej, 1)
	assertPayEvent(t, rej[0], "WEBHOOK_REJECTED", "FAILURE", "INTERNAL_ERROR", "")
	require.NotNil(t, rej[0].SignatureValid)
	assert.True(t, *rej[0].SignatureValid)
	recv := env.payEvents(t, `event_type = 'WEBHOOK_RECEIVED' AND payment_id IS NULL AND raw_payload::text LIKE ?`, "%"+paymentID+"%")
	require.Len(t, recv, 1)
	assert.Empty(t, recv[0].ReasonCode)
}

func TestPayment_WebhookInternalError500(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "wh500@example.com", "user")
	bookingID, paymentID := env.bookAndPay(t, tok, bkSeatA1)
	var itemID string
	require.NoError(t, env.gdb.Raw(`SELECT id FROM booking_items WHERE booking_id = ? LIMIT 1`, bookingID).Scan(&itemID).Error)
	require.NoError(t, env.gdb.Exec(`INSERT INTO tickets (booking_item_id, code) VALUES (?, 'preflight-ticket-block')`, itemID).Error)

	code, m := env.signedWebhook(successBody(t, paymentID, 10000))
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, "INTERNAL_ERROR", errCode(m))
	assert.Equal(t, "PENDING", env.status(t, "payments", paymentID))
	assert.Equal(t, "PENDING", env.status(t, "bookings", bookingID))
	assert.EqualValues(t, 0, env.count(t, `SELECT COUNT(*) FROM payment_events WHERE payment_id = ? AND event_type = 'PAYMENT_SUCCEEDED'`, paymentID))
	recv := env.payEvents(t, `payment_id = ? AND event_type = 'WEBHOOK_RECEIVED'`, paymentID)
	require.Len(t, recv, 1)
	rej := env.payEvents(t, `payment_id = ? AND event_type = 'WEBHOOK_REJECTED' AND reason_code = 'INTERNAL_ERROR'`, paymentID)
	require.Len(t, rej, 1)
	assertPayEvent(t, rej[0], "WEBHOOK_REJECTED", "FAILURE", "INTERNAL_ERROR", "")
}

func TestPayment_EventTablesAppendOnly(t *testing.T) {
	env := newPayEnv(t, false, bkOpts{})
	_, tok := env.newUser(t, "append@example.com", "user")
	_, paymentID := env.bookAndPay(t, tok, bkSeatA1)
	var payEvID, bookEvID string
	require.NoError(t, env.gdb.Raw(`SELECT id FROM payment_events WHERE payment_id = ? LIMIT 1`, paymentID).Scan(&payEvID).Error)
	require.NoError(t, env.gdb.Raw(`SELECT id FROM booking_events LIMIT 1`).Scan(&bookEvID).Error)

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
