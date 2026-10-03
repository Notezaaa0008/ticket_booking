### 1 สถาปัตยกรรม

```mermaid
flowchart LR
    U[User / Admin Browser] --> FE[Next.js Frontend]
    FE -->|REST /api/v1| BE[Go Gin Backend]
    BE -->|GORM + SQL| PG[(PostgreSQL)]
    BE -->|hold / cache / rate limit| RD[(Redis)]
    FE -->|redirect| MG[Mock Payment Page]
    MG -->|signed webhook| BE
```

### 2 Flow การจองและชำระเงิน

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

### 3 State ของ Booking

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

### 4 Edge cases ที่ต้องมี test

- 2 คนจองที่นั่งเดียวกันพร้อมกัน: สำเร็จ 1 ราย อีกรายได้ 409
- เลือก 3 ที่นั่ง แต่ที่ 3 ถูกคนอื่นถือ: ต้องปล่อยที่ 1-2 ที่ถือไปแล้ว
- Redis ล่ม: ระบบยังกันจองซ้ำได้ด้วย unique index
- หมดเวลาแล้วที่นั่งกลับมาว่าง
- webhook ซ้ำ: ไม่ออกตั๋วซ้ำ
- webhook ลายเซ็นผิด: 401
- จ่ายสำเร็จหลังหมดเวลา: บันทึกเป็น `NEEDS_REFUND` ไม่ปล่อยให้หายเงียบ ๆ (ต้องให้คนตัดสิน business rule)
- user ธรรมดาเรียก admin API: 403

### 5 รายการ API (ตั้งต้น)

| Method | Path | สิทธิ์ | หมายเหตุ |
|---|---|---|---|
| GET | `/healthz` | public | เช็ก DB + Redis |
| POST | `/api/v1/auth/register`, `/auth/login` | public | |
| GET | `/api/v1/me` | user | |
| GET | `/api/v1/events?q=&from=&to=` | public | cache 30 วินาที |
| GET | `/api/v1/events/:id` | public | พร้อมรอบฉาย |
| GET | `/api/v1/showtimes/:id/seats` | public | สถานะที่นั่ง |
| POST | `/api/v1/bookings` | user | `{showtime_id, seat_ids[]}` |
| GET | `/api/v1/bookings`, `/bookings/:id` | user | เฉพาะของตัวเอง |
| DELETE | `/api/v1/bookings/:id` | user | ยกเลิกได้เฉพาะ PENDING |
| POST | `/api/v1/bookings/:id/payments` | user | ต้องมี `Idempotency-Key` |
| POST | `/api/v1/payments/webhook` | gateway | ตรวจ HMAC |
| POST | `/api/v1/mock-gateway/:payment_id/pay` | dev only | `{result: success\|fail}` |
| GET | `/api/v1/tickets`, `/tickets/:code` | user | |
| CRUD | `/api/v1/admin/events` | admin | |
| POST/PUT/DELETE | `/api/v1/admin/events/:id/showtimes`, `/admin/showtimes/:id` | admin | สร้างรอบ + สร้างที่นั่งอัตโนมัติ (แถว x คอลัมน์) |
| GET | `/api/v1/admin/bookings`, `/admin/stats` | admin | |
| POST | `/api/v1/admin/tickets/:code/check-in` | admin | |
| GET | `/api/v1/payments/:id` | user (เจ้าของ) | status + failure_code + failure_message |
| GET | `/api/v1/admin/bookings/:id/timeline` | admin | booking_events + payment_events รวมตามเวลา |
| GET | `/api/v1/admin/payments?status=` | admin | รวมรายการ NEEDS_REFUND |
| POST | `/api/v1/admin/payments/:id/refunds` | admin | สร้างคำขอคืนเงิน (Idempotency-Key) |
| POST | `/api/v1/admin/refunds/:id/process` | admin | ประมวลผลผ่าน mock provider |
| GET | `/api/v1/admin/refunds` | admin | |
