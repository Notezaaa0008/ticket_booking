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
| 422 | `REFUND_NOT_ELIGIBLE` | Payment status not refundable |
| 422 | `TICKET_ALREADY_USED` | Refund blocked or check-in on used ticket |
| 422 | `TICKET_VOID` | Check-in on void ticket or non-PAID booking |
| 422 | `AMOUNT_EXCEEDS_PAYMENT` | Refund total would exceed payment |
| 409 | `REFUND_ALREADY_EXISTS` | Active refund already on payment |
| 502 | `PROVIDER_FAILED` | Mock refund provider failed (refund row → FAILED) |
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

## Admin (Phase 5)

All routes under `/admin/*` require **JWT with role `admin`**.  
401: `UNAUTHENTICATED`. 403: `FORBIDDEN` for non-admin users.

Paginated list responses: `{ "items": [...], "page", "limit", "total" }` (default `limit` 20).

### GET /admin/stats
**200:**
```json
{
  "revenue_satang": 1380000,
  "tickets_sold": 14,
  "bookings_by_status": { "PAID": 9 },
  "payments_by_status": { "SUCCEEDED": 9 },
  "needs_refund_count": 0,
  "bookings_today": 9
}
```
`revenue_satang` = sum of `SUCCEEDED` + `REFUNDED` payments minus sum of **COMPLETED** refunds.

---

### Events & showtimes

#### GET /admin/events
**200:** `{ "items": [ admin_event, ... ] }` — each event includes nested `showtimes[]` with `seat_count`, `booking_count`.

#### POST /admin/events
Body: `{ "title", "venue", "description?", "poster_url?", "status?" }` (`draft` | `published` | `archived`, default `published`).  
**201:** event object. Invalidates Redis `events:list:*`.

#### PUT /admin/events/{id}
Body: partial fields as POST. **200:** updated event. Invalidates event list cache.

#### DELETE /admin/events/{id}
If the event has showtimes → **200** `{ "result": "archived" }` (status set to `archived`).  
If no showtimes → **200** `{ "result": "deleted" }`.

#### POST /admin/events/{id}/showtimes
Body:
```json
{
  "starts_at": "2026-12-01T19:00:00+07:00",
  "rows": 5,
  "seats_per_row": 10,
  "price_satang": 15000,
  "price_satang_by_row": { "A": 30000 }
}
```
Max 20 rows, 30 seats per row; row labels `A`–`T`. Creates showtime + all seats in one transaction.  
**201:** showtime with `seat_count`. Invalidates event list cache.

#### PUT /admin/showtimes/{id}
Body: `{ "status"?: "on_sale"|"closed"|"cancelled", "starts_at"?, "price_satang_by_row"?: { "B": 5000 } }`.  
Showtimes are never deleted (D5). Row price changes affect **future** bookings only.  
**200:** showtime object.

---

### GET /admin/bookings
Query: `status`, `user_id` (UUID), `email` (case-insensitive exact match), `showtime_id` (UUID), `page` (default 1).  
**200:** page of bookings with `user_email`, `event_title`, `starts_at`, `seats[]`, `latest_payment_status`, `total_satang`.  
**400:** unknown `status`, invalid UUID filters.

#### GET /admin/bookings/{id}/timeline
**200:**
```json
{
  "booking": { ... },
  "payments": [ ... ],
  "refunds": [ ... ],
  "events": [
    {
      "source": "booking" | "payment",
      "event_type": "BOOKING_CREATED",
      "outcome": "SUCCESS" | "FAILURE" | "IGNORED",
      "reason_code": "",
      "actor_type": "USER" | "ADMIN" | "SYSTEM" | "GATEWAY",
      "from_status": "",
      "to_status": "PENDING",
      "created_at": "..."
    }
  ]
}
```
Events are `booking_events` ∪ `payment_events` ordered by `created_at`.  
**404:** unknown booking id.

---

### GET /admin/payments
Query: `status`, `page`.  
**200:** payments with `user_email`, `booking_status`, `event_title`, `provider_ref`, `failure_code`, `failure_message`.  
Filter `status=NEEDS_REFUND` for payments that require manual refund.

---

### Refunds

#### POST /admin/payments/{id}/refunds
**Header:** `Idempotency-Key` (required). Same key → **200** with existing refund.  
Body: `{ "reason_code": "CUSTOMER_REQUEST", "note": "..." }` — `reason_code` must match `^[A-Z_]{1,64}$`.  
Full refund only (amount = payment amount). Eligible payment statuses: `SUCCEEDED`, `NEEDS_REFUND`.  
**201:** refund object. Rejected cases write `REFUND_REJECTED` audit event (no refund row).

**422:** `REFUND_NOT_ELIGIBLE`, `TICKET_ALREADY_USED`.  
**409:** `REFUND_ALREADY_EXISTS`.

**Audit (success):** `REFUND_REQUESTED` / SUCCESS, actor `ADMIN`; idempotency key stored in event `raw_payload`.

#### POST /admin/refunds/{id}/process
Calls mock refund provider (`RefundProvider` in server).  
**200:** refund with `status` `COMPLETED` or `FAILED`.  
On success: payment → `REFUNDED`, booking → `REFUNDED` if was `PAID`, tickets → `VOID`, seats released if showtime not started.  
On provider failure: refund → `FAILED` (`failure_code`: `PROVIDER_FAILED`); payment/booking unchanged; retry with a new refund request.  
Re-processing a `COMPLETED` refund is idempotent (no new events).

**Audit:** `REFUND_COMPLETED` + `BOOKING_REFUNDED`, or `REFUND_FAILED`.

#### GET /admin/refunds
Query: `status`, `page`. **200:** page of refund rows.

---

### POST /admin/tickets/{code}/check-in
Ticket must be `VALID` and booking `PAID`. First check-in sets `USED` + `checked_in_at`.  
**200:** `{ "result": "CHECKED_IN", "ticket": { ... } }`  
**422:** `TICKET_ALREADY_USED`, `TICKET_VOID`.  
**404:** unknown code.

---

### Admin audit notes
- Refund rejections after rollback: `REFUND_REJECTED` written on plain connection (same pattern as payment create failures).
- Event tables remain append-only; admin flows append new events only.
