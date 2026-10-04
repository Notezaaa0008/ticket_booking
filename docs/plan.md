# แผนการทำงาน (docs/plan.md)

Agent ต้องอ่านไฟล์นี้ก่อนเริ่มทุก Phase และทำเฉพาะ Phase ปัจจุบันเท่านั้น
การแก้ไฟล์นี้ต้องให้คนอนุมัติ (ดู `AGENTS.md` ข้อ 10)

**Stack:** Next.js · Go + Gin + GORM · PostgreSQL · Redis
**Schema:** 11 ตาราง ตาม `docs/schema.md` (รวม `booking_events`, `payment_events`, `refunds`)

---

## 0. กติกากลาง

### วงจรทุก Phase
1. เปิดแชทใหม่ → Plan mode → วาง Task Prompt ของ Phase นั้น
2. AI เสนอแผน → **คนอ่านและตอบคำถาม** → อนุมัติ → AI ลงมือ
3. AI รายงานตามรูปแบบ `AGENTS.md` ข้อ 12 → รัน `/verify-phase`
4. **คนตรวจเอง** ตาม checklist ด้านล่าง (รันคำสั่งเอง เปิดเว็บเอง ดู `git diff` ของไฟล์เสี่ยง)
5. ผ่านครบ → `git add . && git commit` → `git tag phase-N-done`
6. จดสิ่งที่ AI พลาดลง `docs/ai-log.md`

### เงื่อนไขผ่าน Phase (Phase Gate)
- ทุกข้อเป็น `[x]` และมีหลักฐาน (output จริง, ชื่อ test, หรือผลที่คนเห็นเอง)
- ห้ามผ่านถ้ามีข้อใดเป็น FAIL หรือ NOT DONE
- ห้ามเริ่ม Phase ถัดไปก่อน commit + tag Phase ก่อนหน้า

### รหัสข้อ checklist
แต่ละข้อมีรหัส (เช่น `P3-07`) ให้ Agent ใช้อ้างอิงในตาราง PASS/FAIL/NOT DONE ของรายงาน
รหัสที่ถูกยกเลิกแล้วจะไม่ถูกนำกลับมาใช้ซ้ำ

### ชุดตรวจมาตรฐาน (ใช้ทุก Phase ที่มีการเปลี่ยนโค้ด)

| คำสั่ง | ผลที่ต้องได้ |
|---|---|
| `cd backend && go vet ./...` | ไม่มี error |
| `cd backend && gofmt -l .` | ไม่แสดงไฟล์ใดเลย |
| `cd backend && go test ./... -race -count=1` | ผ่านทั้งหมด |
| `cd frontend && npm run lint` | ไม่มี error |
| `cd frontend && npx tsc --noEmit` | ไม่มี error |
| `cd frontend && npm run build` | build สำเร็จ |

> ข้อ `PX-00` ของทุก Phase คือชุดตรวจมาตรฐานนี้

### สถานะความคืบหน้า (คนอัปเดตเอง)

| Phase | ชื่อ | สถานะ | Commit/Tag | วันที่ผ่าน |
|---|---|---|---|---|
| 1 | Foundation | ☑ PHASE VERIFIED (PASS 100%) | phase-1-done | 2026-10-03 |
| 2 | Catalog | ☑ PHASE VERIFIED (PASS 100%) | phase-2-done | 2026-10-03 |
| 3 | Auth + Booking | ☑ PHASE VERIFIED (PASS 100%) | phase-3-done | 2026-10-03 |
| 4 | Payment + Ticket | ☑ PHASE VERIFIED (PASS 100%) | feat(phase4) | 2026-10-03 |
| 5 | Admin + Refund | ☑ PHASE VERIFIED (PASS 100%) | phase-5-done | 2026-10-03 |
| 6 | Hardening + Delivery | ☑ PHASE VERIFIED (PASS 100%) | phase-6-done | 2026-10-04 |

---

## Phase 1: Foundation

**เป้าหมาย:** โครงโปรเจกต์ที่รันได้ครบ พร้อม schema 11 ตาราง, ค่าคงที่ของ log, ตัวเขียน audit และ seed data

**ขอบเขต:** docker-compose, Gin + config + `/healthz`, migration `0001_init` ตาม `docs/schema.md`, GORM models, `internal/domain` (ค่าคงที่), `internal/audit`, `backend/seed/seed.sql`, `scripts/verify-schema.sql`, Next.js หน้าสถานะ, README เบื้องต้น, `docs/api.md` โครง

**นอกขอบเขต:** auth, การจอง, การชำระเงิน, ตั๋ว, คืนเงิน, หน้า admin, Redis seat hold

**ต้องให้คนอนุมัติ:** ทุกบรรทัดของ migration, รายการ dependency, ค่าใน `.env.example`

### Checklist
- [x] **P1-00** ชุดตรวจมาตรฐานผ่าน
- [x] **P1-01** `docker compose up -d` แล้ว `docker compose ps` เห็น postgres และ redis เป็น healthy
- [x] **P1-02** migrate up สำเร็จ และ `\dt` เห็น **11 ตาราง**: users, events, showtimes, seats, bookings, booking_items, payments, tickets, booking_events, payment_events, refunds
- [x] **P1-03** มี partial unique index ครบ 3 ตัว: `uq_booking_items_active_seat`, `uq_payments_one_pending_per_booking`, `uq_refunds_one_active_per_payment` (ตรวจด้วย `\d ชื่อตาราง`)
- [x] **P1-04** `UPDATE booking_events SET reason_code='x'` และ `DELETE FROM payment_events` ถูกปฏิเสธด้วย error append-only (ทดสอบเองด้วย psql)
- [x] **P1-05** migrate down แล้ว up ซ้ำได้โดยไม่พัง
- [x] **P1-06** migration ตรงกับ `docs/schema.md` ทุกตาราง ทุกคอลัมน์ ทุก CHECK (ไม่มีคอลัมน์เกินหรือขาด) และไม่ใช้ AutoMigrate
- [x] **P1-07** `internal/domain` มีค่าคงที่ครบตาม `AGENTS.md` ข้อ 13.3 และมี test ว่าไม่ซ้ำ/ไม่ว่าง
- [x] **P1-08** `internal/audit` ตรวจค่าที่ไม่ถูกต้องก่อนเขียน (ปฏิเสธ `event_type` / `outcome` / `actor_type` ที่ไม่รู้จัก); ทดสอบการเขียนจริงใน Phase 3
- [x] **P1-09** รัน `scripts/verify-schema.sql` ได้ PASS ครบ 9 ข้อ
- [x] **P1-10** `curl localhost:8080/healthz` ตอบ 200 พร้อม db/redis เป็น ok
- [x] **P1-11** หยุด Redis (`docker compose stop redis`) แล้ว `/healthz` รายงาน degraded แต่ server ไม่ crash
- [x] **P1-12** config ขาดตัวแปรจำเป็น → server ไม่เริ่มและบอกชื่อตัวแปรที่ขาด
- [x] **P1-13** seed เป็น SQL (`backend/seed/seed.sql`) รันซ้ำได้ ไม่เกิดข้อมูลซ้ำ และรหัสผ่านเป็น bcrypt hash (ดูใน DB)
- [x] **P1-14** หน้าเว็บแรกแสดงสถานะ backend/database/redis ในเบราว์เซอร์จริง
- [x] **P1-15** ไม่มี secret ใน git: มีแต่ `.env.example`, `.env` ถูก ignore (`git grep -i secret`)
- [x] **P1-16** README บอกคำสั่งครบ: start infra, migrate, seed (`psql -f backend/seed/seed.sql`), run backend, run frontend, run tests
- [x] **P1-17** ไม่มีงานของ Phase 2 ขึ้นไปแอบเข้ามา (ดู `git diff --stat`)
- [x] **P1-18** 👤 คนอ่าน migration ครบทุกบรรทัดและอนุมัติแล้ว

---

## Phase 2: Catalog (ค้นหารอบ/ที่นั่ง)

**เป้าหมาย:** ผู้ใช้ค้นหา event/รอบ และดูผังที่นั่งพร้อมสถานะได้

**ขอบเขต:** `GET /events`, `GET /events/:id`, `GET /showtimes/:id/seats`, cache รายการ event ด้วย Redis (TTL 30s), หน้า `/events`, `/events/[id]`, `/showtimes/[id]`

**นอกขอบเขต:** auth, การจอง, การถือที่นั่ง, payment, admin

**ต้องให้คนอนุมัติ:** SQL คำนวณสถานะที่นั่ง, ดีไซน์ cache key, dependency ใหม่

### Checklist
- [x] **P2-00** ชุดตรวจมาตรฐานผ่าน
- [x] **P2-01** `GET /events` รองรับ `q`, `from`, `to`, `page`, `limit` (ทดสอบด้วย curl อย่างน้อย 3 แบบ รวม 1 แบบที่ไม่มีผลลัพธ์)
- [x] **P2-02** พารามิเตอร์ผิด (วันที่เสีย, page ติดลบ, limit เกิน 100) ได้ 400 `VALIDATION_FAILED` ในรูปแบบ error มาตรฐาน
- [x] **P2-03** แสดงเฉพาะ event `published` ที่มีรอบ `on_sale` ในอนาคต (event ที่เป็น draft หรือมีแต่รอบอดีตไม่ปรากฏ)
- [x] **P2-04** ครั้งแรกสร้าง cache key `events:list:*` ใน Redis (ตรวจด้วย `redis-cli keys`) และครั้งที่ 2 ภายใน 30 วินาทีมาจาก cache
- [x] **P2-05** ปิด Redis แล้ว `GET /events` ยังตอบได้จาก DB ไม่ error
- [x] **P2-06** `GET /events/:id` ตอบรายละเอียดและรอบฉาย พร้อมจำนวนที่นั่งว่าง และ 404 เมื่อไม่พบหรือไม่ได้ published
- [x] **P2-07** `GET /showtimes/:id/seats` คืนสถานะ available/held/sold ถูกต้อง ทดสอบโดย insert ข้อมูลทดสอบใน DB ด้วย SQL: held (PENDING ยังไม่หมดเวลา), sold (PAID), และ **PENDING ที่หมดเวลาแล้วต้องแสดงเป็น available**
- [x] **P2-08** ผลรวม `summary` ตรงกับจำนวนที่นั่งจริงแต่ละสถานะ และใช้ query เดียว (ไม่มี N+1)
- [x] **P2-09** เงินทั้งหมดเป็น satang แบบจำนวนเต็ม ใน JSON ไม่มีทศนิยม
- [x] **P2-10** ทุกหน้ามี loading / empty / error state (ทดสอบด้วยการปิด backend)
- [x] **P2-11** ผังที่นั่งแสดง 4 สถานะ (available, held, sold, selected) และเลือกที่นั่งเพื่อดูยอดรวมได้ แต่ปุ่มจองยังปิดอยู่
- [x] **P2-12** เงื่อนไขค้นหาอยู่ใน URL query string (ส่งลิงก์แล้วเห็นผลเดิม)
- [x] **P2-13** `docs/api.md` มีทั้ง 3 endpoint (params, response, error)
- [x] **P2-14** ไม่มี API เขียนข้อมูลการจอง ไม่มีโค้ด auth/booking หลุดเข้ามา

---

## Phase 3: Auth + Booking (ส่วนสำคัญที่สุด)

**เป้าหมาย:** ผู้ใช้สมัคร/เข้าสู่ระบบ และจองที่นั่งได้อย่างปลอดภัย พร้อมบันทึกผลสำเร็จ/ล้มเหลวทุกครั้ง

**ขอบเขต:** register/login/me, JWT, rate limit, `POST/GET/DELETE /bookings`, Redis seat hold + DB transaction, job หมดเวลา, audit log การจอง, หน้า login/register/จอง/นับถอยหลัง

**นอกขอบเขต:** payment, ticket, admin, refund

**ต้องให้คนอนุมัติ:** เวลา hold, จำนวนที่นั่งสูงสุด, อายุและที่เก็บ JWT, ค่า rate limit, ขั้นตอน algorithm การจองและ transaction (Q1-Q5 ใน Task Prompt)

### Checklist: Auth
- [x] **P3-00** ชุดตรวจมาตรฐานผ่าน
- [x] **P3-01** รหัสผ่านถูก hash ด้วย bcrypt (ดูใน DB ต้องไม่เห็นข้อความจริง)
- [x] **P3-02** register: email ซ้ำ → 409, ข้อมูลไม่ถูกต้อง → 400, `role` ที่ client ส่งมาถูกละเลย (ได้ `user` เสมอ)
- [x] **P3-03** login ผิด → 401 ข้อความทั่วไป (ไม่บอกว่า email มีหรือไม่), login ถูก → ได้ JWT
- [x] **P3-04** เรียก endpoint ที่ต้อง login โดยไม่มี/หมดอายุ/ปลอม token → 401
- [x] **P3-05** rate limit ที่ login และการจอง ตอบ 429 `RATE_LIMITED` และเมื่อ Redis ล่มเป็น fail-open พร้อม log warning
- [x] **P3-06** 👤 ที่เก็บ JWT ฝั่ง frontend ตามที่คนอนุมัติ และบันทึกข้อแลกเปลี่ยนไว้ใน README

### Checklist: Booking
- [x] **P3-10** **Race test:** 20 goroutine จองที่นั่งเดียวกัน สำเร็จ 1, ที่เหลือ 409, ใน DB มี `booking_items` active ของที่นั่งนั้น 1 รายการ รัน `go test -race -count=10` ผ่านทั้ง 10 รอบ (แปะ output)
- [x] **P3-11** จองหลายที่นั่งโดยที่นั่งหนึ่งถูกถือแล้ว → 409 และไม่มี Redis hold ของที่นั่งอื่นค้าง (ตรวจด้วย `redis-cli keys 'hold:*'`)
- [x] **P3-12** ปิด Redis แล้วจองได้ (`hold_mode = db_fallback`) และยังกันจองซ้ำได้ (test)
- [x] **P3-13** booking หมดเวลา: job ตั้ง `EXPIRED`, `active=false`, ที่นั่งกลับมาว่าง และมี event `BOOKING_EXPIRED` ที่ actor เป็น `SYSTEM`
- [x] **P3-14** lazy expiry: จองที่นั่งที่ถูก PENDING ที่หมดเวลาแล้วถือค้างอยู่ได้สำเร็จ
- [x] **P3-15** ยกเลิก: เจ้าของยกเลิก PENDING ได้, คนอื่นได้ 404, สถานะอื่นได้ 409 พร้อม event `BOOKING_CANCEL_REJECTED`
- [x] **P3-16** ราคาและยอดรวมมาจาก DB เสมอ (ส่งค่าปลอมจาก client แล้วถูกละเลย)
- [x] **P3-17** จำกัดจำนวนที่นั่งต่อการจองตามที่อนุมัติ, ที่นั่งซ้ำใน request เดียวกัน/ที่นั่งคนละรอบ/รอบปิดขาย → 400 หรือ 422 ตามเหมาะสม
- [x] **P3-18** `GET /bookings/:id` ของคนอื่น → 404, และ response มี `seconds_remaining` และ `status_reason` เมื่อ EXPIRED/CANCELLED
- [x] **P3-19** ปิดเว็บ-เปิดใหม่ระหว่างที่ booking ยังไม่หมดเวลา ยังเห็น countdown ที่ถูกต้อง (คำนวณจาก `expires_at` ของเซิร์ฟเวอร์)

### Checklist: Log สถานะ
- [x] **P3-20** จองสำเร็จ → มี `BOOKING_CREATED` / SUCCESS พร้อม metadata (`seat_ids`, `total_satang`, `expires_at`, `hold_mode`) เขียนใน transaction เดียวกับ booking
- [x] **P3-21** จองล้มเหลวทุกแบบ → มี `BOOKING_CREATE_FAILED` / FAILURE พร้อม `reason_code` ถูกต้อง (`SEAT_UNAVAILABLE`, `VALIDATION_FAILED`, `TOO_MANY_SEATS`, `SHOWTIME_NOT_ON_SALE` ฯลฯ) และ metadata (`requested_seat_ids`, `conflicting_seat_ids`)
- [x] **P3-22** ใน race test: `booking_events` มี `BOOKING_CREATED` 1 แถว และ `BOOKING_CREATE_FAILED` 19 แถว (ผู้ตรวจ query เอง: `SELECT event_type, outcome, reason_code FROM booking_events ORDER BY created_at;`)
- [x] **P3-23** ถ้าเขียน log ล้มเหลว ระบบยังส่ง error เดิมกลับ ไม่กลืน error และบันทึก stderr พร้อม request id
- [x] **P3-24** ทุก outcome path ของ Phase นี้มี test ที่ตรวจ `event_type`, `outcome`, `reason_code` (รวมถึงการพิสูจน์ว่า `internal/audit` เขียน event ได้จริงทั้งในและนอก transaction — ต่อจาก P1-08)

### Checklist: Frontend
- [x] **P3-30** หน้า login/register, route ที่ต้อง login redirect ไป `/login`, มี logout
- [x] **P3-31** จองซ้อนแล้วเห็นว่าที่นั่งไหนถูกแย่ง, ผังรีเฟรช, และที่นั่งอื่นที่เลือกไว้ยังอยู่
- [x] **P3-32** `/bookings/[id]` แสดงสถานะแยกชัด: PENDING (นับถอยหลัง), EXPIRED (พร้อมเหตุผล), CANCELLED, PAID
- [x] **P3-33** ทดสอบด้วยเบราว์เซอร์ 2 หน้าต่างพร้อมกัน เลือกที่นั่งเดียวกัน ผลต้องเป็นสำเร็จ 1 ล้มเหลว 1 (หรือใช้ Playwright MCP)

### Checklist: อื่น ๆ
- [x] **P3-40** `/review-booking-safety` ไม่พบ VIOLATION ระดับ High
- [x] **P3-41** `docs/api.md` อัปเดตทุก endpoint พร้อมรหัส error และ event ที่เขียน
- [x] **P3-42** 👤 คนอ่านโค้ด transaction + Redis ทุกบรรทัดและอธิบายได้ด้วยคำพูดตัวเอง

---

## Phase 4: Payment + Ticket

**เป้าหมาย:** ชำระเงินผ่าน mock gateway, ได้ตั๋วหลังชำระสำเร็จ, ทุกผลลัพธ์ถูกตรวจสถานะและบันทึกเพื่อรองรับการคืนเงิน

**ขอบเขต:** `POST /bookings/:id/payments`, `GET /payments/:id`, mock gateway, `POST /payments/webhook` (HMAC), transaction ยืนยันชำระ + ออกตั๋ว, `GET /tickets`, หน้าชำระเงิน/ผลลัพธ์/ตั๋ว+QR

**นอกขอบเขต:** หน้า admin, การประมวลผลคืนเงิน, payment provider จริง, อีเมล

**ต้องให้คนอนุมัติ:** state transition ของ payment/booking, ขั้นตอน webhook, กฎกรณีจ่ายหลังหมดเวลา, กรณียอดไม่ตรง, รูปแบบรหัสตั๋ว, dependency สร้าง QR (Q1-Q5 ใน Task Prompt)

> **สถานะ Phase 4:** **PHASE VERIFIED (PASS 100%)** — 2026-10-03 (`/verify-phase` + human sign-off P4-50–53, P4-62; ดู `docs/ai-log.md`)

### Checklist: การสร้างการชำระเงิน
- [x] **P4-00** ชุดตรวจมาตรฐานผ่าน — VERIFIED
- [x] **P4-01** สร้าง payment ได้เฉพาะ booking ของตัวเองที่ PENDING ยังไม่หมดเวลา และ items ยัง active, ยอดมาจาก DB (ไม่รับยอดจาก client) — VERIFIED
- [x] **P4-02** ต้องมี `Idempotency-Key`; key เดิมได้ payment เดิม (ไม่สร้างแถวใหม่ ไม่เขียน event ซ้ำ) — VERIFIED
- [x] **P4-03** มี payment PENDING ซ้อนใน booking เดียวไม่ได้ (ถูกกันด้วย `uq_payments_one_pending_per_booking`) ได้ 409 — VERIFIED
- [x] **P4-04** สร้างไม่สำเร็จ → มี `PAYMENT_CREATE_FAILED` / FAILURE พร้อม reason (`BOOKING_NOT_PAYABLE` หรือ `BOOKING_EXPIRED`); สร้างสำเร็จ → `PAYMENT_CREATED` / SUCCESS — VERIFIED
- [x] **P4-05** `GET /payments/:id` คืน status, amount, `failure_code`, `failure_message`; ของคนอื่นได้ 404 — VERIFIED

### Checklist: Webhook และการยืนยันชำระเงิน
- [x] **P4-10** mock gateway เรียก webhook จริงผ่าน HTTP (ผ่านโค้ดตรวจลายเซ็นเดียวกัน ไม่ใช่ฟังก์ชันลัด) และปิดได้ด้วย `MOCK_GATEWAY_ENABLED=false` (ได้ 404) — VERIFIED
- [x] **P4-11** ลายเซ็นผิด/ไม่มี/body ถูกแก้ → 401, มี `WEBHOOK_REJECTED` (`INVALID_SIGNATURE`, `signature_valid=false`), **ไม่มีสถานะใดเปลี่ยน** — VERIFIED
- [x] **P4-12** payload เสีย → 400 และมี `WEBHOOK_REJECTED` (`MALFORMED_PAYLOAD`) — VERIFIED
- [x] **P4-13** **จ่ายสำเร็จ:** booking = PAID, payment = SUCCEEDED, ตั๋วเท่าจำนวน items, event เรียง `WEBHOOK_RECEIVED` → `PAYMENT_SUCCEEDED` → `BOOKING_PAID` → `TICKETS_ISSUED`, ที่นั่งแสดงเป็น sold — VERIFIED
- [x] **P4-14** **จ่ายล้มเหลว:** payment = FAILED พร้อม `failure_code`/`failure_message`, booking ยัง PENDING, มี `PAYMENT_FAILED` และผู้ใช้สร้าง payment ใหม่แล้วจ่ายสำเร็จได้ — VERIFIED
- [x] **P4-15** **webhook ซ้ำ** (ส่ง 2 ครั้ง และส่งพร้อมกัน 10 ครั้ง): ตั๋วชุดเดียว, `PAYMENT_SUCCEEDED` 1 แถว, ที่เหลือเป็น `PAYMENT_DUPLICATE_IGNORED` / IGNORED และตอบ 200 — VERIFIED
- [x] **P4-16** **ยอดไม่ตรง:** payment = NEEDS_REFUND (`AMOUNT_MISMATCH`), ไม่ออกตั๋ว, มี `PAYMENT_AMOUNT_MISMATCH` — VERIFIED
- [x] **P4-17** **จ่ายหลังหมดเวลา/ที่นั่งหาย:** payment = NEEDS_REFUND (`BOOKING_EXPIRED` หรือ `SEAT_LOST`), ไม่ออกตั๋ว, มี `PAYMENT_AFTER_EXPIRY`, และ booking ของผู้ใช้อื่นที่ได้ที่นั่งไปไม่ถูกกระทบ — VERIFIED
- [x] **P4-18** webhook อ้าง payment ที่ไม่มี → 404 พร้อม event (`UNKNOWN_PAYMENT`) — VERIFIED
- [x] **P4-19** **Race:** job หมดเวลากับ webhook ทำงานพร้อมกัน ต้องไม่เกิดทั้ง EXPIRED และ PAID (job และ cancel ล็อก booking `FOR UPDATE` และตรวจสถานะซ้ำ) — VERIFIED
- [x] **P4-20** ตอบ 200 เมื่อประมวลผลหรือละเว้น, 500 เมื่อเกิด error ภายใน (ไม่เปิดเผยรายละเอียดภายในใน body) — VERIFIED

### Checklist: ตั๋ว
- [x] **P4-30** รหัสตั๋วไม่เรียงลำดับ มีความสุ่มอย่างน้อย 128 bit และไม่ซ้ำ — VERIFIED
- [x] **P4-31** `GET /tickets` และ `GET /tickets/:code` เห็นเฉพาะของตัวเอง (ของคนอื่น 404) — VERIFIED
- [x] **P4-32** `GET /bookings/:id` แสดงสถานะ payment ล่าสุดและตั๋วเมื่อ PAID — VERIFIED

### Checklist: Log และการรองรับคืนเงิน
- [x] **P4-40** ทุก outcome path ของ Phase นี้มี test ที่ตรวจ `event_type`, `outcome`, `reason_code` และสถานะที่เกิดขึ้น — VERIFIED
- [x] **P4-41** ทุก payment ที่เป็น `NEEDS_REFUND` ตามรอยได้: query `payment_events` ของ payment นั้นเห็นสาเหตุและ `raw_payload` ของ webhook — VERIFIED
- [x] **P4-42** event ไม่มีข้อมูลลับ (ไม่มีลายเซ็นจริง ไม่มี token) และ UPDATE/DELETE บนตาราง event ยังถูกปฏิเสธ — VERIFIED
- [x] **P4-43** ผู้ตรวจ query ได้เอง: `SELECT event_type, outcome, reason_code FROM payment_events ORDER BY created_at;` และ `SELECT id, status, failure_code FROM payments WHERE status='NEEDS_REFUND';` — VERIFIED

### Checklist: Frontend
- [x] **P4-50** เดิน flow ครบในเบราว์เซอร์: ค้นหา → เลือกที่นั่ง → จอง → จ่ายสำเร็จ → เห็นตั๋วพร้อม QR — VERIFIED (human browser)
- [x] **P4-51** flow ล้มเหลว: กด "จำลองล้มเหลว" → เห็นเหตุผล → ลองจ่ายใหม่ → สำเร็จ — VERIFIED (human browser)
- [x] **P4-52** หน้าผลลัพธ์แยกสถานะชัดเจน: กำลังประมวลผล, สำเร็จ, ล้มเหลว (พร้อม `failure_message` และปุ่มลองใหม่), NEEDS_REFUND (แจ้งว่ารับเงินแล้วแต่ออกตั๋วไม่ได้ จะคืนเงิน และแสดงเลข payment) — VERIFIED (human browser)
- [x] **P4-53** กดจ่ายซ้ำสองครั้งติดกันไม่สร้าง payment ซ้ำ (ใช้ Idempotency-Key เดิม) — VERIFIED (human browser + `TestPayment_CreateRules`)

### Checklist: อื่น ๆ
- [x] **P4-60** `/review-booking-safety` ไม่พบ VIOLATION ระดับ High — VERIFIED
- [x] **P4-61** `docs/api.md` อัปเดตครบ (headers, error codes, event ที่เขียนต่อ path) — VERIFIED
- [x] **P4-62** 👤 คนอนุมัติ payment flow, webhook และตาราง state transition ก่อน และอ่านโค้ด transaction ยืนยันชำระเงินทุกบรรทัด — VERIFIED (human sign-off 2026-10-03)

---

## Phase 5: Admin + Refund

**เป้าหมาย:** admin จัดการ event/รอบ ดูการจองและ log ทั้งหมด และคืนเงินได้จริง (mock provider) โดยอาศัย log ที่บันทึกไว้

**ขอบเขต:** `/admin/*` APIs และหน้า admin, สร้างรอบพร้อมที่นั่งอัตโนมัติ, timeline ของ booking, รายการ payment (เน้น NEEDS_REFUND), คืนเงิน (สร้างคำขอ → ประมวลผล), check-in, dashboard

**นอกขอบเขต:** payment provider จริง, การคืนเงินบางส่วน (ยกเว้นคนอนุมัติ), อีเมล, role ใหม่

**ต้องให้คนอนุมัติ:** กฎการคืนเงินทั้งหมด (Q1-Q6 ใน Task Prompt): ใครคืนเงินได้, เต็ม/บางส่วน, ตั๋วที่ใช้แล้ว, การปล่อยที่นั่งหลังคืนเงิน, การแก้/ลบรอบที่มีคนจ่ายแล้ว, กฎ check-in

> **สถานะ Phase 5:** **PHASE VERIFIED (PASS 100%)** — 2026-10-03 (`/verify-phase` + human E2E P5-26, sign-off P5-42)

### Checklist: สิทธิ์และการจัดการข้อมูล
- [x] **P5-00** ชุดตรวจมาตรฐานผ่าน — VERIFIED
- [x] **P5-01** test วนทุก route ใต้ `/admin/*`: user ธรรมดา → 403, ไม่มี token → 401 — VERIFIED (`TestAdmin_EveryRouteRequiresAdmin`)
- [x] **P5-02** หน้า `/admin` เข้าไม่ได้ถ้าไม่ใช่ admin (ทดสอบด้วยบัญชีธรรมดา) — VERIFIED (`AdminGuard` + human browser)
- [x] **P5-03** สร้าง event + รอบใหม่แล้วเห็นฝั่งผู้ใช้ (ภายใน TTL ของ cache หรือทันทีถ้า invalidate) — VERIFIED (`TestAdmin_CatalogCacheInvalidatedAndVisibleToUsers`)
- [x] **P5-04** สร้างรอบได้ที่นั่งเท่ากับ แถว × คอลัมน์ ที่กำหนด และเป็น atomic (ล้มเหลวแล้วไม่เหลือข้อมูลครึ่งทาง) — VERIFIED (`TestAdmin_EventsAndShowtimes`)
- [x] **P5-05** แก้ราคาที่นั่งแล้วไม่กระทบ booking เดิม (ราคาถูกคัดลอกไว้ใน `booking_items`) — VERIFIED (`TestAdmin_RowPriceChangePreservesBookingItemPrices`)
- [x] **P5-06** ลบ/ปิดรอบที่มี booking PAID หรือ PENDING เป็นไปตามกฎที่คนอนุมัติ และไม่ทำให้ข้อมูลเสีย — VERIFIED (`TestAdmin_ShowtimeUpdateWithPaidAndPendingBookings`, D5)

### Checklist: การดู log และข้อมูล
- [x] **P5-10** `/admin/bookings` กรองตามสถานะ/ผู้ใช้/รอบได้ — VERIFIED (`TestAdmin_StatsListsAndTimeline`, admin bookings UI)
- [x] **P5-11** `/admin/bookings/:id/timeline` แสดง booking, payment, refund และรายการ event รวมตามเวลา พร้อม outcome, reason_code, actor — VERIFIED (`TestAdmin_StatsListsAndTimeline`)
- [x] **P5-12** `/admin/payments` กรองตามสถานะได้ และเห็น NEEDS_REFUND พร้อม `failure_code` ชัดเจน — VERIFIED (integration test + `/admin/payments` UI)
- [x] **P5-13** UI timeline แสดง badge สำเร็จ/ล้มเหลว/ละเว้น แยกสีและข้อความชัดเจน — VERIFIED (`OutcomeBadge`, admin bookings timeline UI)

### Checklist: การคืนเงิน
- [x] **P5-20** ตารางเงื่อนไขการคืนเงินผ่าน test ครบ: SUCCEEDED ได้, NEEDS_REFUND ได้, PENDING/FAILED ถูกปฏิเสธ, มีตั๋ว USED ถูกปฏิเสธ (`TICKET_ALREADY_USED`), มีคำขอ active อยู่แล้วถูกปฏิเสธ (`REFUND_ALREADY_EXISTS`) โดยแต่ละกรณีมี event `REFUND_REJECTED` ที่ถูกต้อง — VERIFIED (`TestAdmin_RefundEligibilityMatrix`)
- [x] **P5-21** สร้างคำขอคืนเงินต้องมี `Idempotency-Key` (key เดิมได้ผลเดิม) และสร้าง `REFUND_REQUESTED` / SUCCESS (actor = ADMIN) — VERIFIED (same test)
- [x] **P5-22** **ประมวลผลสำเร็จ:** refund = COMPLETED, payment = REFUNDED, booking = REFUNDED, ตั๋ว = VOID, การปล่อยที่นั่งตามที่อนุมัติ, มี event `REFUND_COMPLETED` และ `BOOKING_REFUNDED` — VERIFIED (`TestAdmin_RefundSuccessFutureShowtimeReleasesSeats`, `TestAdmin_RefundSuccessStartedShowtimeKeepsSeats`)
- [x] **P5-23** **ประมวลผลล้มเหลว** (mock provider ล้ม): refund = FAILED (`PROVIDER_FAILED`), payment/booking ไม่เปลี่ยน, มี `REFUND_FAILED`, และสร้างคำขอใหม่เพื่อลองอีกครั้งได้ — VERIFIED (`TestAdmin_RefundProviderFailureThenRetry`)
- [x] **P5-24** เรียก process ซ้ำ/พร้อมกัน → ได้ผลสำเร็จครั้งเดียว ไม่มี event ซ้ำ (idempotent) — VERIFIED (`TestAdmin_RefundProcessTwiceAndConcurrently`)
- [x] **P5-25** ผลรวมการคืนเงินที่ COMPLETED ไม่เกินยอด payment (test ตรงกับกฎ `AMOUNT_EXCEEDS_PAYMENT`) — VERIFIED (`TestAdmin_RefundAmountExceedsPayment`)
- [x] **P5-26** เคสจริงครบ flow: จ่ายหลังหมดเวลาให้เกิด NEEDS_REFUND → admin คืนเงินผ่าน UI → timeline แสดงห่วงโซ่ครบ — VERIFIED (human E2E 2026-10-03)
- [x] **P5-27** event เดิมไม่ถูกแก้ (UPDATE/DELETE ยังถูกปฏิเสธ), การคืนเงินเพิ่มเฉพาะ event ใหม่ — VERIFIED (`TestAdmin_RefundFlowEventsAppendOnly`)

### Checklist: check-in และ dashboard
- [x] **P5-30** check-in: ครั้งแรกสำเร็จ (ตั๋ว USED), ครั้งที่สองถูกปฏิเสธ, ตั๋ว VOID/ไม่มีรหัสถูกปฏิเสธ, check-in พร้อมกันมีผู้ชนะ 1 ราย — VERIFIED (`TestAdmin_CheckIn`)
- [x] **P5-31** `/admin/stats` ตรงกับ SQL ที่ผู้ตรวจรันเอง: รายได้ (SUCCEEDED − คืนเงิน COMPLETED), ตั๋วที่ขาย, booking/payment แยกตามสถานะ, จำนวน NEEDS_REFUND — VERIFIED (`TestAdmin_StatsListsAndTimeline`)
- [x] **P5-32** หน้า dashboard แสดงจำนวน NEEDS_REFUND เด่นชัดและลิงก์ไปรายการ — VERIFIED (`frontend/src/app/admin/page.tsx`)

### Checklist: อื่น ๆ
- [x] **P5-40** `/review-booking-safety` ไม่พบ VIOLATION ระดับ High — VERIFIED (two-phase `ProcessRefund`; provider outside DB lock tx)
- [x] **P5-41** `docs/api.md` อัปเดตครบทุก endpoint admin — VERIFIED (`docs/api.md` Admin Phase 5)
- [x] **P5-42** 👤 คนอนุมัติกฎการคืนเงินและอ่านโค้ด transaction ของ refund ทุกบรรทัด — VERIFIED (human sign-off 2026-10-03)

---

## Phase 6: Hardening + ส่งมอบ

**เป้าหมาย:** ระบบน่าเชื่อถือ ตรวจสอบได้ รันตาม README ได้จริง และพร้อมสาธิตในการสัมภาษณ์

**ขอบเขต:** test matrix, audit log completeness, `scripts/checkdb.sql` (ตรวจ data invariants), security review, reliability, README/API docs ฉบับสมบูรณ์, demo script, clean-room test

**นอกขอบเขต:** ฟีเจอร์ใหม่, ปรับ UI ใหม่, deploy, payment จริง

**ต้องให้คนอนุมัติ:** การแก้ใด ๆ ที่แตะ schema, booking, payment, refund, auth (ตาม `AGENTS.md` ข้อ 10)

> **สถานะ Phase 6:** **PHASE VERIFIED (PASS 100%)** — 2026-10-04 (`/verify-phase` + human sign-off P6-09, P6-12, P6-13, P6-16)

### Checklist
- [x] **P6-00** ชุดตรวจมาตรฐานผ่าน และ `go test ./... -race -count=1` ผ่านทั้งหมด (แปะ output เต็ม) — VERIFIED (`go vet`, `gofmt -l` clean, `go test ./... -race -count=1`, frontend lint/`tsc`/build, 2026-10-04)
- [x] **P6-01** `docs/test-matrix.md` ครอบคลุมสถานการณ์ขั้นต่ำ: จองชนกัน, จองหลายที่นั่งบางส่วนชน, Redis ล่ม, hold หมดเวลา, lazy expiry, ยกเลิก, webhook ซ้ำ/พร้อมกัน, ลายเซ็นผิด, ยอดไม่ตรง, จ่ายหลังหมดเวลา+ที่นั่งถูกจองใหม่, จ่ายซ้ำหลังล้มเหลว, คืนเงินสำเร็จ/ล้มเหลว/ซ้ำ, check-in ซ้ำ, admin 403, JWT หมดอายุ **และทุกแถวมีชื่อ test ที่ผ่านจริง** — VERIFIED (`docs/test-matrix.md`; handler package `ok`)
- [x] **P6-02** `docs/logging-audit.md` แมปทุกฟังก์ชันที่เปลี่ยนสถานะหรือคืน error → event ที่เขียน → test และไม่พบเส้นทางที่เปลี่ยนสถานะโดยไม่มี event — VERIFIED (`docs/logging-audit.md`; `TestPayment_SeatLostAfterCancelAndRebookNeedsRefund`)
- [x] **P6-03** ไม่มีโค้ดที่ UPDATE/DELETE ตาราง event (ตรวจด้วย grep และ test) — VERIFIED (`TestPayment_EventTablesAppendOnly`, `TestAdmin_RefundFlowEventsAppendOnly`)
- [x] **P6-04** `scripts/checkdb.sql` ทุกบรรทัดเป็น PASS — ครอบคลุม 8 กฎ: ที่นั่งไม่ซ้อน, ไม่มี item active ค้าง, PAID ครบถ้วน, ไม่มี PENDING ค้างเกิน 2 นาที, event ครบ, ยอดคืนเงินถูกต้อง, ตั๋ว VOID ถูกต้อง, ไม่มี event อ้างข้อมูลที่ไม่มี — VERIFIED (live `psql -f /scripts/checkdb.sql`, rules 1–8 violations 0)
- [x] **P6-06** Security: CORS จำกัดเฉพาะ origin ของเว็บ, error ไม่เปิดเผย stack/SQL, secret ทั้งหมดมาจาก env, mock gateway ปิดเมื่อไม่ได้เปิดแฟล็ก, admin route ถูกป้องกัน, `git grep` ไม่พบ secret จริง — VERIFIED (CORS origin only; `RespondError`; mock route gated; admin 401 without a token)
- [x] **P6-07** Reliability: graceful shutdown หยุด expiry job, ทุก DB/Redis call มี timeout, เริ่มระบบแล้วบอกชัดเจนเมื่อ env ขาดหรือ migration ยังไม่ครบ — VERIFIED (`stopJob` after shutdown; `CheckCurrent`; missing-env error)
- [x] **P6-08** Dependency รีวิวแล้ว (go.mod, package.json) ไม่มีตัวที่ไม่ได้ใช้ — VERIFIED (`go mod tidy` unchanged; frontend deps used)
- [x] **P6-09** Frontend: ทุกหน้ามี loading/empty/error, ทุกสถานะ booking/payment/refund มีข้อความเฉพาะตัว, ไม่มี console error ใน flow หลัก — VERIFIED (human browser, 2026-10-04; `docs/logs/p6-09-console-audit.md`, zero console errors)
- [x] **P6-10** README ครบตามโครง: Overview, Prerequisites (พร้อมเวอร์ชัน), Quick start, บัญชีทดสอบจาก seed, วิธีรัน test, วิธีรัน `scripts/checkdb.sql` และ `scripts/verify-schema.sql`, โครงสร้างโปรเจกต์, Design notes (seat hold, กันจองซ้ำ 2 ชั้น, idempotency, webhook, audit log + ห่วงโซ่คืนเงิน, Redis fallback), Known limitations — VERIFIED (`README.md`)
- [x] **P6-11** `docs/api.md`, `docs/flow.md`, `docs/schema.md` ตรงกับโค้ดจริง — VERIFIED (Gin routes match `docs/api.md` and `docs/flow.md`; schema SQL matches `0001_init.up.sql`)
- [x] **P6-12** **Clean-room:** clone ใหม่ในโฟลเดอร์อื่น ทำตาม README อย่างเดียวจนระบบรันได้ (ไม่เกิน ~10 คำสั่ง) และทุกขั้นที่สะดุดถูกแก้ใน README — VERIFIED (human, 2026-10-04; `docs/logs/p6-12-cleanroom-transcript.md`, 7 commands, no build errors)
- [x] **P6-13** `docs/demo-script.md` ซ้อมจบใน ~5 นาที: ค้นหา → 2 เบราว์เซอร์จองที่นั่งเดียวกัน → นับถอยหลัง → จ่ายสำเร็จ → ตั๋ว+QR → จ่ายล้มเหลวแล้วลองใหม่ → จ่ายหลังหมดเวลากลายเป็น NEEDS_REFUND → admin timeline และคืนเงิน → `scripts/checkdb.sql` — VERIFIED (human, 2026-10-04; `docs/logs/p6-13-demo-rehearsal.md`, about 5 minutes)
- [x] **P6-14** `docs/ai-log.md` มีอย่างน้อย 3 กรณีที่ AI พลาด (AI อ้างอะไร, ความจริง, จับได้อย่างไร, แก้อย่างไร, กติกาที่เพิ่ม) — VERIFIED (`docs/ai-log.md` required cases 1–3)
- [x] **P6-15** ไม่มี TODO/FIXME ค้าง, `.env.example` ครบ, ไม่มีไฟล์ขยะใน repo — VERIFIED (no TODO/FIXME in app code; `.env.example` complete; root lockfile and duplicate `ai-log.md` removed)
- [x] **P6-16** 👤 คนอ่านโค้ดส่วน booking, payment, refund และ auth ครบ และอธิบายได้ด้วยคำพูดตัวเอง (ซ้อมตอบคำถามในไฟล์เอกสารข้อ "เตรียมสัมภาษณ์") — VERIFIED (human sign-off 2026-10-04: walkthrough ready for booking hold/expiry, payment webhook and NEEDS_REFUND, refund request/process, and auth JWT/roles)

> **P6-05** ถูกยกเลิก (เดิมคือ test ที่จงใจสร้างข้อมูลผิดเพื่อพิสูจน์ว่า checkdb จับได้ — ไม่มี checkdb แบบ Go แล้ว) รหัสนี้สงวนไว้ ไม่นำกลับมาใช้ซ้ำ

> **หมายเหตุ P6-04:** `scripts/checkdb.sql` เป็นการตรวจแบบอ่านคอลัมน์ PASS/FAIL เอง ถ้าต้องการให้ fail อัตโนมัติใน CI ให้รันด้วย `psql -v ON_ERROR_STOP=1` และห่อแต่ละกฎด้วย `DO $ ... RAISE EXCEPTION ... $`

---

## ภาคผนวก A: รายการที่ต้องให้คนอนุมัติ (สรุปตาม Phase)

| Phase | ต้องอนุมัติ |
|---|---|
| 1 | migration ทั้งหมด, dependency, ค่า env ตัวอย่าง |
| 2 | SQL สถานะที่นั่ง, cache key, dependency |
| 3 | เวลา hold, ที่นั่งสูงสุด, JWT (อายุ/ที่เก็บ), rate limit, algorithm การจอง, reason code ที่ใช้ |
| 4 | state transition ของ payment/booking, ขั้นตอน webhook, จ่ายหลังหมดเวลา, ยอดไม่ตรง, รหัสตั๋ว, dependency QR |
| 5 | กฎคืนเงินทั้งหมด, การปล่อยที่นั่งหลังคืนเงิน, การลบ/แก้รอบ, กฎ check-in |
| 6 | การแก้ใด ๆ ที่แตะ schema/booking/payment/refund/auth |

## ภาคผนวก B: Query และสคริปต์สำหรับผู้ตรวจ

เข้า psql ด้วย: `docker compose exec postgres psql -U ticket -d ticket_booking`

รันสคริปต์ตรวจ:

```bash
# ตรวจ schema (Phase 1)
docker compose exec -T postgres psql -U ticket -d ticket_booking -f /scripts/verify-schema.sql

# ตรวจ data invariants (Phase 6)
docker compose exec -T postgres psql -U ticket -d ticket_booking -f /scripts/checkdb.sql
```

Query ด้วยมือ:

```sql
-- ดู log การจองล่าสุด
SELECT event_type, outcome, reason_code, created_at
FROM booking_events ORDER BY created_at DESC LIMIT 30;

-- ดู log การชำระเงินล่าสุด
SELECT event_type, outcome, reason_code, created_at
FROM payment_events ORDER BY created_at DESC LIMIT 30;

-- payment ที่รอคืนเงิน
SELECT id, booking_id, amount_satang, failure_code FROM payments WHERE status = 'NEEDS_REFUND';

-- ที่นั่งที่ active ซ้อน (ต้องได้ 0 แถว)
SELECT seat_id, count(*) FROM booking_items WHERE active GROUP BY seat_id HAVING count(*) > 1;

-- สถานะตั๋วต่อ booking
SELECT b.id, b.status, count(t.id) AS tickets
FROM bookings b
LEFT JOIN booking_items bi ON bi.booking_id = b.id
LEFT JOIN tickets t ON t.booking_item_id = bi.id
GROUP BY b.id, b.status;
```