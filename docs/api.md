# API

Base: `http://localhost:8080`. JSON. Money is int64 satang. Time is RFC 3339 UTC. Auth is `Authorization: Bearer <jwt>` (HS256, claims: user id + role).

Error body: `{"error":{"code":"...","message":"..."}}`. `SEAT_UNAVAILABLE` may add `seat_ids`. Unknown failures are `500 INTERNAL_ERROR` with message `internal server error` (no stack or SQL).

| HTTP | codes |
|---|---|
| 400 | `VALIDATION_FAILED`, `IDEMPOTENCY_KEY_REQUIRED`, `MALFORMED_PAYLOAD` |
| 401 | `UNAUTHENTICATED`, `INVALID_CREDENTIALS`, `INVALID_SIGNATURE` |
| 403 | `FORBIDDEN` |
| 404 | `NOT_FOUND`, `BOOKING_NOT_FOUND`, `UNKNOWN_PAYMENT` |
| 409 | `EMAIL_TAKEN`, `SEAT_UNAVAILABLE`, `BOOKING_NOT_PENDING`, `BOOKING_NOT_PAYABLE`, `BOOKING_EXPIRED`, `REFUND_ALREADY_EXISTS` |
| 422 | `TOO_MANY_SEATS`, `SHOWTIME_NOT_ON_SALE`, `AMOUNT_MISMATCH`, `REFUND_NOT_ELIGIBLE`, `TICKET_ALREADY_USED`, `TICKET_VOID`, `AMOUNT_EXCEEDS_PAYMENT` |
| 429 | `RATE_LIMITED` |
| 500 | `INTERNAL_ERROR` |
| 502 | `PROVIDER_FAILED` |

One pending booking per user per showtime is `409 VALIDATION_FAILED`.

## GET /healthz

Auth: none. 200 `{status, db, redis}` (`ok`/`down`; Redis down is `status: degraded`, still 200). 503 when the database is down.

## GET /api/v1/events

Auth: none. Query: `q`, `from`, `to`, `page` (default 1), `limit` (default 20, max 100). 200 `{items[], page, limit, total}`. 400 `VALIDATION_FAILED`.

## GET /api/v1/events/{id}

Auth: none. 200 event plus upcoming `on_sale` showtimes (`id, title, description, venue, poster_url, showtimes[]`). 404 `NOT_FOUND` (draft, archived, or no future on-sale showtime).

## GET /api/v1/showtimes/{id}/seats

Auth: none. 200 `{seats[{id, row_label, seat_number, zone, price_satang, status}], summary}`. `status`: `available` | `held` | `sold` (from the database). 404 `NOT_FOUND`.

## POST /api/v1/auth/register

Auth: none. Body: `{email, password (≥8), name}`. Role is always `user`. 201 `{user: {id, email, name, role, created_at}}`. 400 `VALIDATION_FAILED`. 409 `EMAIL_TAKEN`.

## POST /api/v1/auth/login

Auth: none. Body: `{email, password}`. 200 `{token, user}`. 401 `INVALID_CREDENTIALS` (same message for unknown email and wrong password). 429 `RATE_LIMITED` (5/min per IP+email).

## GET /api/v1/me

Auth: user. 200 `{user}`. 401 `UNAUTHENTICATED`.

## POST /api/v1/bookings

Auth: user. Rate limit 10/min per user. Body: `{showtime_id, seat_ids}` (1–6). Client prices are ignored. 201 booking (`id, showtime_id, event_title, venue, starts_at, status, total_satang, expires_at, seconds_remaining, created_at, items[{seat_id, row_label, seat_number, zone, price_satang}]`). 400 `VALIDATION_FAILED`. 409 `SEAT_UNAVAILABLE` (+ `seat_ids`) or existing pending booking. 422 `TOO_MANY_SEATS`, `SHOWTIME_NOT_ON_SALE`. 429 `RATE_LIMITED`.

## GET /api/v1/bookings

Auth: user. 200 `{items: [booking]}` (owner only).

## GET /api/v1/bookings/{id}

Auth: user. 200 booking; `status_reason` when expired or cancelled; when `PAID`, also `payment` and `tickets`. 404 `BOOKING_NOT_FOUND` (unknown or not owner).

## DELETE /api/v1/bookings/{id}

Auth: user (owner, `PENDING` only). 200 cancelled booking. 404 `BOOKING_NOT_FOUND`. 409 `BOOKING_NOT_PENDING`.

## POST /api/v1/bookings/{id}/payments

Auth: user (owner). Header: `Idempotency-Key` (required, max 200). Body: none. Amount comes from the booking. 201 new payment, 200 same key on the same booking: `{id, booking_id, status, amount_satang, failure_code, failure_message, paid_at, created_at, pay_url}`. Reusing a key on another booking: 409 `VALIDATION_FAILED`. 400 `IDEMPOTENCY_KEY_REQUIRED`. 404 `BOOKING_NOT_FOUND`. 409 `BOOKING_NOT_PAYABLE`, `BOOKING_EXPIRED`.

## GET /api/v1/payments/{id}

Auth: user (owner). 200 payment (same shape as create). Status: `PENDING` | `SUCCEEDED` | `FAILED` | `NEEDS_REFUND` | `REFUNDED`. 404 `NOT_FOUND`.

## POST /api/v1/payments/webhook

Auth: none. Header: `X-Signature` = hex HMAC-SHA256 of the raw body (`PAYMENT_WEBHOOK_SECRET`). Body: `{event_id, payment_id, status: succeeded|failed, amount_satang, provider_ref, failure_code}`. `amount_satang` required when `succeeded` and must match the payment row. 200 `{result: processed|ignored, payment_status}`. 401 `INVALID_SIGNATURE`, 400 `MALFORMED_PAYLOAD`, and a lookup failure each commit `WEBHOOK_RECEIVED` then `WEBHOOK_REJECTED` with that reason code and do not change payment state. 404 `UNKNOWN_PAYMENT`. 500 `INTERNAL_ERROR` after the webhook was accepted.

## POST /api/v1/mock-gateway/{payment_id}/pay

Auth: none. Registered only when `MOCK_GATEWAY_ENABLED=true`; otherwise 404. Body: `{result: success|fail}`. Signs a webhook from the database amount and POSTs it to `/api/v1/payments/webhook`. 200 `{webhook_status, webhook_body}`. 400 `VALIDATION_FAILED`. 404 `NOT_FOUND`. 502 `PROVIDER_FAILED` if the webhook POST fails.

## GET /api/v1/tickets

Auth: user. 200 `{items: [ticket]}`. Ticket: `{code, status, booking_id, event_title, venue, starts_at, seat_label, zone, issued_at}`.

## GET /api/v1/tickets/{code}

Auth: user. 200 ticket. 404 `NOT_FOUND` (unknown or not owner).

## Admin

Every `/api/v1/admin/*` route requires an admin JWT. 401 `UNAUTHENTICATED`. 403 `FORBIDDEN` for any other role. List routes return `{items, page, limit, total}` (`page` default 1).

## GET /api/v1/admin/stats

Auth: admin. 200 `{revenue_satang, tickets_sold, bookings_by_status, payments_by_status, needs_refund_count, bookings_today}`.

## GET /api/v1/admin/events

Auth: admin. 200 `{items}` events with nested showtimes (`seat_count`, `booking_count`).

## POST /api/v1/admin/events

Auth: admin. Body: `{title, venue, description?, poster_url?, status?}` (`draft`|`published`|`archived`, default `published`). 201 event. 400 `VALIDATION_FAILED`.

## PUT /api/v1/admin/events/{id}

Auth: admin. Body: same fields as create (partial). 200 event. 400 `VALIDATION_FAILED`. 404 `NOT_FOUND`.

## DELETE /api/v1/admin/events/{id}

Auth: admin. 200 `{result: archived}` if the event has showtimes, otherwise `{result: deleted}`. 404 `NOT_FOUND`.

## POST /api/v1/admin/events/{id}/showtimes

Auth: admin. Body: `{starts_at, rows (1–20), seats_per_row (1–30), price_satang, price_satang_by_row?}`. 201 showtime with `seat_count`. 400 `VALIDATION_FAILED`. 404 `NOT_FOUND`.

## PUT /api/v1/admin/showtimes/{id}

Auth: admin. Body: `{status?: on_sale|closed|cancelled, starts_at?, price_satang_by_row?}`. Showtimes are not deleted. Row prices apply to future bookings only. 200 showtime. 400 `VALIDATION_FAILED`. 404 `NOT_FOUND`.

## GET /api/v1/admin/bookings

Auth: admin. Query: `status`, `user_id`, `email`, `showtime_id`, `page`. 200 page of bookings (`user_email`, `event_title`, `starts_at`, `seats`, `latest_payment_status`, `total_satang`). 400 `VALIDATION_FAILED`.

## GET /api/v1/admin/bookings/{id}/timeline

Auth: admin. 200 `{booking, payments, refunds, events[{source, event_type, outcome, reason_code, actor_type, from_status, to_status, created_at}]}`. 404 `NOT_FOUND`.

## GET /api/v1/admin/payments

Auth: admin. Query: `status`, `page`. 200 payments with `user_email`, `booking_status`, `event_title`, `provider_ref`, `failure_code`, `failure_message`. 400 `VALIDATION_FAILED`.

## POST /api/v1/admin/payments/{id}/refunds

Auth: admin. Header: `Idempotency-Key`. Body: `{reason_code, note}` (`reason_code` matches `^[A-Z_]{1,64}$`). Full refund of a `SUCCEEDED` or `NEEDS_REFUND` payment. 201 refund, 200 replay of the same key: `{id, payment_id, booking_id, amount_satang, status, reason_code, note, provider_ref, failure_code, created_at, completed_at}`. 400 `IDEMPOTENCY_KEY_REQUIRED` / `VALIDATION_FAILED`. 404 `NOT_FOUND`. 409 `REFUND_ALREADY_EXISTS`. 422 `REFUND_NOT_ELIGIBLE`, `TICKET_ALREADY_USED`, `AMOUNT_EXCEEDS_PAYMENT`.

## GET /api/v1/admin/refunds

Auth: admin. Query: `status`, `page`. 200 page of refunds. 400 `VALIDATION_FAILED`.

## POST /api/v1/admin/refunds/{id}/process

Auth: admin. No body. 200 refund `COMPLETED` (payment `REFUNDED`, booking `REFUNDED` if it was `PAID`, tickets `VOID`, seats released if the showtime has not started) or `FAILED` (`failure_code: PROVIDER_FAILED`, payment and booking unchanged). Re-processing a finished refund returns it unchanged. 404 `NOT_FOUND` writes `REFUND_REJECTED`. An unexpected `provider_ref` is 500 `INTERNAL_ERROR` and writes `REFUND_FAILED`; the refund stays `REQUESTED`.

## POST /api/v1/admin/tickets/{code}/check-in

Auth: admin. No body. Ticket must be `VALID` on a `PAID` booking. 200 `{result: CHECKED_IN, ticket}` with `TICKET_CHECKED_IN` in the same transaction. 404 `NOT_FOUND`. 422 `TICKET_ALREADY_USED`, `TICKET_VOID`. Rejections write `TICKET_CHECK_IN_REJECTED`.
