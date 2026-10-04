# Flow

Derived from the Go services and the Gin routes. Money is int64 satang. Time is UTC.

## 1. Architecture

```mermaid
flowchart LR
    Browser[Next.js browser] -->|REST JSON| API[Go Gin API]
    API -->|GORM| PG[(PostgreSQL 16)]
    API -->|holds cache rate limit| Redis[(Redis 7)]
    Browser -->|pay_url /pay/id| PayPage[Mock pay page]
    PayPage -->|POST mock-gateway when enabled| API
    API -->|HMAC webhook to itself| API
```

- Frontend: Next.js App Router. All API calls go through `frontend/src/lib/api.ts`. QR codes are drawn in the browser from `ticket.code`.
- Backend: Gin under `/api/v1`, plus `GET /healthz`. Handlers call services; services call repositories. JWT (HS256) and bcrypt. CORS is the single `CORS_ORIGIN` value (`*` is rejected).
- PostgreSQL is the source of truth. SQL migrations only. Startup fails if required env is missing or the schema is unversioned, dirty, or behind the embedded migrations.
- Redis is only seat holds (`hold:{seat_id}`), the public event-list cache (30s), and rate limits. A Redis error while taking holds falls through to the database. The partial unique index on active seat items is what prevents a double sale.
- The mock gateway route exists only when `MOCK_GATEWAY_ENABLED=true`. It reads the amount from the database, signs the raw body, and POSTs `/api/v1/payments/webhook`.
- An expiry job runs every 30s. Shutdown stops that job after the HTTP server drains.

## 2. Booking and payment

```mermaid
sequenceDiagram
    actor User
    participant FE as Next.js
    participant API as Gin
    participant Redis
    participant PG as PostgreSQL

    User->>FE: Search events and open a seat map
    FE->>API: GET /events, GET /events/:id, GET /showtimes/:id/seats
    API->>PG: Read published events and seat status
    API-->>FE: available, held, or sold from the database

    User->>FE: Book 1 to 6 seats
    FE->>API: POST /bookings
    API->>Redis: SET hold:seat booking NX EX ttl
    alt Any seat already held
        API->>Redis: Delete only keys this booking owns
        API-->>FE: 409 SEAT_UNAVAILABLE
    else Holds acquired, or Redis is down
        API->>PG: One transaction
        Note over PG: Lazy-expire stale PENDING rows on these seats
        Note over PG: Insert PENDING booking and active items
        Note over PG: BOOKING_CREATED
        API-->>FE: 201 booking
    end

    User->>FE: Pay
    FE->>API: POST /bookings/:id/payments
    API->>PG: PENDING payment, amount from booking items, PAYMENT_CREATED
    API-->>FE: pay_url /pay/:id
    FE->>API: POST /mock-gateway/:id/pay
    API->>API: POST /payments/webhook with X-Signature
    API->>PG: Lock payment and booking
    alt succeeded and seats still held
        Note over PG: Payment SUCCEEDED, booking PAID, tickets issued
    else succeeded but expired, seats lost, or amount differs
        Note over PG: Payment NEEDS_REFUND, booking unchanged
    else status failed
        Note over PG: Payment FAILED, booking stays PENDING
    end
    API->>Redis: Release holds after a successful commit
```

Hold TTL is `SEAT_HOLD_TTL_SECONDS` (default 600). Client prices are ignored. One live PENDING booking per user per showtime. Booking create is limited to 10/min per user. Login is 5/min per IP and email.

Expiry: the job locks due PENDING rows (`FOR UPDATE SKIP LOCKED`), sets `EXPIRED`, deactivates items, writes `BOOKING_EXPIRED` (`HOLD_EXPIRED`, actor `SYSTEM`), then releases Redis holds. Creating a booking does the same lazy expiry inside its transaction when a stale hold blocks the requested seats.

## 3. States

```mermaid
stateDiagram-v2
    [*] --> PENDING
    PENDING --> PAID: webhook succeeded and items still active
    PENDING --> EXPIRED: expiry job or lazy expiry
    PENDING --> CANCELLED: owner cancel
    PAID --> REFUNDED: refund completed
    PENDING --> PENDING: payment FAILED, a new payment is allowed
```

```mermaid
stateDiagram-v2
    [*] --> PayPending: POST payments
    PayPending --> SUCCEEDED: amount matches and booking still PENDING
    PayPending --> FAILED: gateway status failed
    PayPending --> NEEDS_REFUND: amount mismatch, booking expired, or seats lost
    SUCCEEDED --> REFUNDED: refund completed
    NEEDS_REFUND --> REFUNDED: refund completed
```

A duplicate webhook on `SUCCEEDED`, `FAILED`, `NEEDS_REFUND`, or `REFUNDED` changes nothing.

```mermaid
stateDiagram-v2
    [*] --> REQUESTED: full refund of SUCCEEDED or NEEDS_REFUND
    REQUESTED --> COMPLETED: provider ok
    REQUESTED --> FAILED: provider error
    FAILED --> [*]
    COMPLETED --> [*]
```

`COMPLETED` sets the payment to `REFUNDED`, voids tickets, and sets the booking to `REFUNDED` only if it was `PAID`. Seats are released only when the showtime has not started. A `FAILED` refund leaves the payment and booking as they were. A later attempt is a new refund request. Re-processing a finished refund returns the same row and writes no new event.

`REFUND_REJECTED` is an audit event for a refused request. It does not insert a refund row.

Check-in sets a `VALID` ticket of a `PAID` booking to `USED`. The booking status does not change. A used ticket blocks a later refund request.

## 4. Audit events

`booking_events` and `payment_events` are append-only. A state change and its event commit in one transaction. A rolled-back failure is written afterward on the plain connection. Webhook rejections that never change a payment (`INVALID_SIGNATURE`, `MALFORMED_PAYLOAD`, lookup failure) commit `WEBHOOK_RECEIVED` and `WEBHOOK_REJECTED` together.

| When | Event | Outcome | reason_code |
|---|---|---|---|
| Booking created | `BOOKING_CREATED` | SUCCESS | |
| Booking rejected | `BOOKING_CREATE_FAILED` | FAILURE | `SEAT_UNAVAILABLE`, `VALIDATION_FAILED`, `TOO_MANY_SEATS`, `SHOWTIME_NOT_ON_SALE`, `SEAT_NOT_IN_SHOWTIME`, … |
| Hold expired | `BOOKING_EXPIRED` | SUCCESS | `HOLD_EXPIRED` |
| Owner cancel | `BOOKING_CANCELLED` | SUCCESS | `USER_CANCELLED` |
| Cancel refused | `BOOKING_CANCEL_REJECTED` | FAILURE | `BOOKING_NOT_FOUND`, `BOOKING_NOT_PENDING` |
| Payment created | `PAYMENT_CREATED` | SUCCESS | |
| Payment create refused | `PAYMENT_CREATE_FAILED` | FAILURE | `BOOKING_NOT_PAYABLE`, `BOOKING_EXPIRED`, `VALIDATION_FAILED`, … |
| Webhook accepted for processing | `WEBHOOK_RECEIVED` | SUCCESS | |
| Bad signature, bad JSON, unknown id, or lookup/processing error | `WEBHOOK_REJECTED` | FAILURE | `INVALID_SIGNATURE`, `MALFORMED_PAYLOAD`, `UNKNOWN_PAYMENT`, `INTERNAL_ERROR` |
| Webhook paid | `PAYMENT_SUCCEEDED`, then `BOOKING_PAID`, then `TICKETS_ISSUED` | SUCCESS | |
| Gateway declined | `PAYMENT_FAILED` | FAILURE | `GATEWAY_DECLINED` |
| Same terminal webhook again | `PAYMENT_DUPLICATE_IGNORED` | IGNORED | `DUPLICATE_EVENT` |
| Amount differs | `PAYMENT_AMOUNT_MISMATCH` | FAILURE | `AMOUNT_MISMATCH` |
| Paid after expiry, or seats no longer held | `PAYMENT_AFTER_EXPIRY` | FAILURE | `BOOKING_EXPIRED` or `SEAT_LOST` |
| Refund opened | `REFUND_REQUESTED` | SUCCESS | |
| Refund refused or id missing | `REFUND_REJECTED` | FAILURE | `REFUND_NOT_ELIGIBLE`, `TICKET_ALREADY_USED`, `AMOUNT_EXCEEDS_PAYMENT`, `REFUND_ALREADY_EXISTS`, `VALIDATION_FAILED`, `UNKNOWN_PAYMENT`, `NOT_FOUND` |
| Provider ok | `REFUND_COMPLETED` and `BOOKING_REFUNDED` | SUCCESS | |
| Provider error, or unexpected `provider_ref` | `REFUND_FAILED` | FAILURE | `PROVIDER_FAILED` or `INTERNAL_ERROR` |
| Check-in | `TICKET_CHECKED_IN` | SUCCESS | |
| Check-in refused | `TICKET_CHECK_IN_REJECTED` | FAILURE | `TICKET_ALREADY_USED`, `TICKET_VOID`, `NOT_FOUND` |

`NEEDS_REFUND` is a payment status. The money is recorded and no tickets are issued. An admin refunds it with the same request/process routes as a successful payment.

## 5. API

Paths match `docs/api.md` and the routes registered in `main.go`, `routes.go`, and `admin.go`. Admin routes require an admin JWT: 401 `UNAUTHENTICATED`, 403 `FORBIDDEN`.

| Method | Path | Auth |
|---|---|---|
| GET | `/healthz` | public |
| GET | `/api/v1/events` | public |
| GET | `/api/v1/events/:id` | public |
| GET | `/api/v1/showtimes/:id/seats` | public |
| POST | `/api/v1/auth/register` | public |
| POST | `/api/v1/auth/login` | public |
| GET | `/api/v1/me` | user |
| POST | `/api/v1/bookings` | user |
| GET | `/api/v1/bookings` | user |
| GET | `/api/v1/bookings/:id` | user |
| DELETE | `/api/v1/bookings/:id` | user |
| POST | `/api/v1/bookings/:id/payments` | user |
| GET | `/api/v1/payments/:id` | user |
| POST | `/api/v1/payments/webhook` | gateway HMAC |
| POST | `/api/v1/mock-gateway/:payment_id/pay` | none; route absent unless mock gateway is enabled |
| GET | `/api/v1/tickets` | user |
| GET | `/api/v1/tickets/:code` | user |
| GET | `/api/v1/admin/stats` | admin |
| GET | `/api/v1/admin/events` | admin |
| POST | `/api/v1/admin/events` | admin |
| PUT | `/api/v1/admin/events/:id` | admin |
| DELETE | `/api/v1/admin/events/:id` | admin |
| POST | `/api/v1/admin/events/:id/showtimes` | admin |
| PUT | `/api/v1/admin/showtimes/:id` | admin |
| GET | `/api/v1/admin/bookings` | admin |
| GET | `/api/v1/admin/bookings/:id/timeline` | admin |
| GET | `/api/v1/admin/payments` | admin |
| POST | `/api/v1/admin/payments/:id/refunds` | admin |
| GET | `/api/v1/admin/refunds` | admin |
| POST | `/api/v1/admin/refunds/:id/process` | admin |
| POST | `/api/v1/admin/tickets/:code/check-in` | admin |

There is no showtime delete route. Request and error details are in `docs/api.md`.
