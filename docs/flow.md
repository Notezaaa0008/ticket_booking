# Flow

Derived from the Go services and the Gin routes. Money is int64 satang. Time is UTC.

## 1. Architecture

```mermaid
flowchart LR
    U[User / Admin Browser] --> FE[Next.js Frontend]
    FE -->|REST /api/v1| BE[Go Gin Backend]
    BE -->|GORM + SQL| PG[(PostgreSQL)]
    BE -->|hold / cache / rate limit| RD[(Redis)]
    FE -->|redirect| MG[Mock Payment Page]
    MG -->|signed webhook| BE
```

## 2. Booking and payment

```mermaid
sequenceDiagram
    actor User
    participant FE as Next.js
    participant BE as Gin API
    participant RD as Redis
    participant PG as PostgreSQL

    User->>FE: ค้นหา event / รอบ
    FE->>BE: GET /events, GET /showtimes/:id/seats
    BE->>PG: query (cache รายการ event ใน Redis)
    BE-->>FE: ผังที่นั่ง available / held / sold

    User->>FE: เลือกที่นั่ง กดจอง
    FE->>BE: POST /bookings (JWT)
    BE->>RD: SET hold:seat NX EX 600 (ทุกที่นั่ง)
    alt ที่นั่งถูกจองแล้ว
        BE-->>FE: 409 SEAT_UNAVAILABLE (ปล่อย hold ที่ได้มาแล้ว)
    else ได้ครบ
        BE->>PG: TX: insert booking PENDING + items (unique index กันซ้ำ)
        BE-->>FE: 201 booking + expires_at
    end

    User->>FE: กดชำระเงิน
    FE->>BE: POST /bookings/:id/payments (Idempotency-Key)
    BE->>PG: insert payment PENDING
    BE-->>FE: payment_id + pay_url (mock gateway)
    User->>FE: กดจ่ายสำเร็จ/ล้มเหลวที่หน้า mock
    FE->>BE: mock gateway -> POST /payments/webhook (HMAC signature)
    BE->>PG: TX FOR UPDATE: booking PAID, payment SUCCEEDED, สร้าง tickets
    BE->>RD: ลบ hold keys
    FE->>BE: GET /bookings/:id (poll)
    BE-->>FE: PAID + tickets (code, QR)
```

## 3. States

```mermaid
stateDiagram-v2
    [*] --> PENDING: สร้างการจอง (ถือที่นั่ง 10 นาที)
    PENDING --> PAID: webhook สำเร็จ ก่อนหมดเวลา
    PENDING --> EXPIRED: หมดเวลา (job / lazy expiry)
    PENDING --> CANCELLED: ผู้ใช้ยกเลิก
    PENDING --> PENDING: payment ล้มเหลว (ลองจ่ายใหม่ได้ ถ้ายังไม่หมดเวลา)
    PAID --> [*]
    EXPIRED --> [*]
    CANCELLED --> [*]
```

## 4. Edge cases

- 2 คนจองที่นั่งเดียวกันพร้อมกัน: สำเร็จ 1 ราย อีกรายได้ 409
- เลือก 3 ที่นั่ง แต่ที่ 3 ถูกคนอื่นถือ: ต้องปล่อยที่ 1-2 ที่ถือไปแล้ว
- Redis ล่ม: ระบบยังกันจองซ้ำได้ด้วย unique index
- หมดเวลาแล้วที่นั่งกลับมาว่าง
- webhook ซ้ำ: ไม่ออกตั๋วซ้ำ
- webhook ลายเซ็นผิด: 401
- จ่ายสำเร็จหลังหมดเวลา: บันทึกเป็น `NEEDS_REFUND` ไม่ปล่อยให้หายเงียบ ๆ (ต้องให้คนตัดสิน business rule)
- user ธรรมดาเรียก admin API: 403


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
