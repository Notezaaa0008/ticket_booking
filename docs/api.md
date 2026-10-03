# Ticket Booking API

Base URL: `http://localhost:8080/api/v1`

## Shared conventions

### Money
Fields ending in `_satang` are **integers** (satang). No floats in JSON. `150000` = 1,500.00 THB.

### Time
RFC 3339 with offset, e.g. `2026-10-11T02:00:00+07:00`. Stored in UTC on the server.

### Auth
Protected routes require `Authorization: Bearer <jwt>` (HS256, 24h, claims: user id + role).

### Standard error body
```json
{ "error": { "code": "VALIDATION_FAILED", "message": "invalid page" } }
```

`POST /bookings` may add `seat_ids` on `SEAT_UNAVAILABLE`:
```json
{ "error": { "code": "SEAT_UNAVAILABLE", "message": "...", "seat_ids": ["uuid", "..."] } }
```

### Common HTTP / codes

| HTTP | code | Meaning |
|---|---|---|
| 400 | `VALIDATION_FAILED` | Invalid input |
| 400 | `IDEMPOTENCY_KEY_REQUIRED` | Missing `Idempotency-Key` on payment create |
| 400 | `MALFORMED_PAYLOAD` | Webhook JSON invalid |
| 401 | `UNAUTHENTICATED` | Missing or invalid token |
| 401 | `INVALID_CREDENTIALS` | Wrong email or password (generic message) |
| 401 | `INVALID_SIGNATURE` | Webhook HMAC invalid or missing |
| 403 | `FORBIDDEN` | Admin-only route |
| 404 | `NOT_FOUND` / `BOOKING_NOT_FOUND` | Resource hidden or missing |
| 404 | `UNKNOWN_PAYMENT` | Webhook references unknown payment id |
| 409 | `EMAIL_TAKEN` | Register duplicate email |
| 409 | `SEAT_UNAVAILABLE` | Seat held or sold |
| 409 | `BOOKING_NOT_PENDING` | Cancel on non-pending booking |
| 409 | `BOOKING_NOT_PAYABLE` | Payment create rejected (not pending, another pending payment, etc.) |
| 409 | `BOOKING_EXPIRED` | Booking hold expired |
| 422 | `TOO_MANY_SEATS` | More than 6 seats |
| 422 | `SHOWTIME_NOT_ON_SALE` | Showtime closed or in the past |
| 422 | `AMOUNT_MISMATCH` | Webhook amount ≠ payment row (stored on payment as `failure_code`) |
| 429 | `RATE_LIMITED` | Login (5/min per IP+email) or booking (10/min per user) |
| 500 | `INTERNAL_ERROR` | Server error (generic message; webhook processing failure after receive) |

One pending booking per user per showtime returns **409** with code `VALIDATION_FAILED` and message `you already have a pending booking for this showtime`.

---

## Catalog (read-only)

### GET /events
Query: `q`, `from`, `to`, `page` (default 1), `limit` (default 20, max 100).  
200: `{ items[], page, limit, total }`. Empty list is still 200.

### GET /events/{id}
200: event + upcoming `on_sale` showtimes. Draft / no future on_sale → 404 `NOT_FOUND`.

### GET /showtimes/{id}/seats
200: `{ seats[], summary }`. Seat `status`: `available` | `held` | `sold` (from DB, not Redis).

---

## Auth

### POST /auth/register
Body: `{ "email", "password" (≥8), "name" }`. Role is always `user` (client `role` ignored).  
201: `{ "user": { id, email, name, role, created_at } }`  
409: `EMAIL_TAKEN`. 400: validation.

**Audit:** none.

### POST /auth/login
Body: `{ "email", "password" }`.  
200: `{ "token", "user" }`.  
401: `INVALID_CREDENTIALS` (same message for unknown email and wrong password).  
429: `RATE_LIMITED`.

**Audit:** none.

### GET /me
Auth required. 200: `{ "user" }`. 401: `UNAUTHENTICATED`.

---

## Bookings

All booking routes require auth unless noted.

### POST /bookings
Body: `{ "showtime_id": "uuid", "seat_ids": ["uuid", ...] }` (1–6 unique seats).  
Client `price_satang` / `total_satang` are **ignored**; prices come from `seats` in DB.

201: booking object (see GET).  
409: `SEAT_UNAVAILABLE` (+ `seat_ids`), or pending-booking rule above.  
422: `TOO_MANY_SEATS`, `SHOWTIME_NOT_ON_SALE`.  
400: bad ids, seats not in showtime, etc.

**Audit (success, same DB transaction as booking):**  
`BOOKING_CREATED` / SUCCESS — metadata: `seat_ids`, `total_satang`, `expires_at`, `hold_mode` (`redis` | `db_fallback`).

**Audit (failure, after rollback, plain connection):**  
`BOOKING_CREATE_FAILED` / FAILURE — `reason_code` e.g. `SEAT_UNAVAILABLE`, `VALIDATION_FAILED`, `TOO_MANY_SEATS`, `SHOWTIME_NOT_ON_SALE`, `SEAT_NOT_IN_SHOWTIME`, `INTERNAL_ERROR`; metadata: `requested_seat_ids`, optional `conflicting_seat_ids`.

### GET /bookings
200: `{ "items": [ booking, ... ] }` (owner only).

### GET /bookings/{id}
200: booking with `items`, `total_satang`, `expires_at`, `seconds_remaining` (if PENDING), `status_reason` (if EXPIRED → e.g. `HOLD_EXPIRED`, if CANCELLED → `USER_CANCELLED`).  
When **PAID**, also includes latest `payment` (summary) and `tickets[]` issued for this booking.  
404: `BOOKING_NOT_FOUND` for other users or unknown id.

### DELETE /bookings/{id}
Owner + `PENDING` only. 200: cancelled booking.  
404: not owner. 409: `BOOKING_NOT_PENDING`.

**Audit (success, same transaction):** `BOOKING_CANCELLED` / SUCCESS, `reason_code`: `USER_CANCELLED`.  
**Audit (reject):** `BOOKING_CANCEL_REJECTED` / FAILURE — `BOOKING_NOT_FOUND`, `BOOKING_NOT_PENDING`.

### Background expiry (every 30s)
For expired PENDING bookings: status `EXPIRED`, items `active=false`, Redis holds released.

**Audit:** `BOOKING_EXPIRED` / SUCCESS, actor `SYSTEM`, `reason_code`: `HOLD_EXPIRED`.

Lazy expiry on a new booking that needs seats blocked by stale PENDING rows uses the same event in-transaction.

---

## Booking response shape (example)

```json
{
  "id": "uuid",
  "showtime_id": "uuid",
  "event_title": "Bangkok Jazz Night",
  "venue": "Lumpini Hall",
  "starts_at": "2026-10-11T12:00:00+07:00",
  "status": "PENDING",
  "status_reason": "",
  "total_satang": 150000,
  "expires_at": "2026-10-03T07:00:00Z",
  "seconds_remaining": 598,
  "created_at": "2026-10-03T06:50:00Z",
  "items": [
    {
      "seat_id": "uuid",
      "row_label": "A",
      "seat_number": 1,
      "zone": "front",
      "price_satang": 150000
    }
  ],
  "payment": { "id": "uuid", "status": "SUCCEEDED", "amount_satang": 150000, "failure_code": "", "failure_message": "", "paid_at": "...", "pay_url": "/pay/uuid" },
  "tickets": []
}
```

---

## Payments & tickets (Phase 4)

Amounts are always computed on the server from `booking_items.price_satang`. Clients never send payment totals.

### POST /bookings/{id}/payments
Auth required (booking owner).  
**Header:** `Idempotency-Key: <string>` (required, max 200 chars). Same key on the same booking returns the existing payment (**200**); reusing a key for a different booking → **409** `VALIDATION_FAILED`.

**Body:** none.

**201** (new payment) / **200** (idempotent replay):
```json
{
  "id": "uuid",
  "booking_id": "uuid",
  "status": "PENDING",
  "amount_satang": 30000,
  "failure_code": "",
  "failure_message": "",
  "paid_at": null,
  "created_at": "2026-10-03T12:00:00Z",
  "pay_url": "/pay/{id}"
}
```

**409:** `BOOKING_NOT_PAYABLE` (not pending, seats lost, or another `PENDING` payment exists), `BOOKING_EXPIRED`.  
**404:** `BOOKING_NOT_FOUND` (not owner).  
**400:** `IDEMPOTENCY_KEY_REQUIRED`.

**Audit (success, same transaction):** `PAYMENT_CREATED` / SUCCESS.  
**Audit (failure, after rollback):** `PAYMENT_CREATE_FAILED` / FAILURE — `BOOKING_NOT_PAYABLE`, `BOOKING_EXPIRED`, `VALIDATION_FAILED`, etc.

### GET /payments/{id}
Auth required (owner via booking).  
**200:** same shape as create response; includes `failure_code` / `failure_message` when failed or needs refund.  
**404:** `NOT_FOUND` for other users.

Payment `status`: `PENDING` | `SUCCEEDED` | `FAILED` | `NEEDS_REFUND` | `REFUNDED`.

---

### POST /payments/webhook
Gateway callback (**no JWT**). Verifies **HMAC-SHA256** of the **raw** request body.

**Header:** `X-Signature: <hex>` — HMAC-SHA256 of the body using `PAYMENT_WEBHOOK_SECRET`.

**Body (JSON):**
```json
{
  "event_id": "uuid",
  "payment_id": "uuid",
  "status": "succeeded",
  "amount_satang": 30000,
  "provider_ref": "provider-charge-id",
  "failure_code": "GATEWAY_DECLINED"
}
```
- `status`: `succeeded` | `failed` (on `failed`, optional `failure_code`, e.g. `GATEWAY_DECLINED`).
- `amount_satang` required when `status` is `succeeded`; must match the payment row.

**200:** `{ "result": "processed" | "ignored", "payment_status": "..." }`  
**401:** `INVALID_SIGNATURE` — no state change; audit `WEBHOOK_REJECTED` / FAILURE.  
**400:** `MALFORMED_PAYLOAD`.  
**404:** `UNKNOWN_PAYMENT`.  
**500:** `INTERNAL_ERROR` (generic body) if processing fails after `WEBHOOK_RECEIVED`.

**Audit:** always `WEBHOOK_RECEIVED` / SUCCESS (valid signature + JSON) before processing. Then one of:  
`PAYMENT_SUCCEEDED`, `PAYMENT_FAILED`, `PAYMENT_AMOUNT_MISMATCH`, `PAYMENT_AFTER_EXPIRY`, `PAYMENT_DUPLICATE_IGNORED` (with booking events `BOOKING_PAID`, `TICKETS_ISSUED` on success).  
Duplicate terminal webhooks → `PAYMENT_DUPLICATE_IGNORED` / IGNORED, **200**, no further state change.

> Alias note: there is no `/webhooks/payment` route; providers must POST to **`/payments/webhook`**.

---

### POST /mock-gateway/{payment_id}/pay
Dev only when `MOCK_GATEWAY_ENABLED=true`; otherwise **404**. **No auth.**  
Builds a signed webhook payload from DB amount and POSTs to `/payments/webhook` over HTTP (same path as production).

**Body:** `{ "result": "success" | "fail" }`

**200:** `{ "webhook_status": 200, "webhook_body": { "result": "processed", "payment_status": "..." } }`  
**404:** unknown payment.

---

### GET /tickets
Auth required.  
**200:** `{ "items": [ ticket, ... ] }` (owner only, newest showtimes first).

### GET /tickets/{code}
Auth required.  
**200:** single ticket (same fields as list item).  
**404:** `NOT_FOUND` if code unknown or not owned by caller.

**Ticket object:**
```json
{
  "code": "base64url-22-chars",
  "status": "VALID",
  "booking_id": "uuid",
  "event_title": "Bangkok Jazz Night",
  "venue": "Lumpini Hall",
  "starts_at": "2026-10-11T12:00:00+07:00",
  "seat_label": "A1",
  "zone": "standard",
  "issued_at": "2026-10-03T12:00:00Z"
}
```

After successful payment, seats show as **`sold`** on `GET /showtimes/{id}/seats` (booking `PAID`).

---

## Out of scope in this document
Admin, refund processing (Phase 5+).
