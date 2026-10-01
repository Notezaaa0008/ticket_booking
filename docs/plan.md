### กติกากลางทุก Phase

- เปิดแชทใหม่ → Plan mode → อนุมัติแผน → ทำ → `/verify-phase` → คนตรวจ → commit + tag
- ผ่าน Phase ไม่ได้ถ้ามีข้อใดใน checklist เป็น FAIL หรือ NOT DONE

### Phase 1: Foundation

**เป้าหมาย:** โครงโปรเจกต์ที่รันได้ครบ พร้อม schema และ seed data

งานหลัก
- `docker-compose.yml` (Postgres 16 + Redis 7, healthcheck, volume)
- `backend`: Gin server, config จาก env, เชื่อม Postgres (GORM) และ Redis, `/healthz`, CORS, request id, error shape กลาง
- Migration `0001_init` ตาม `docs/schema.md` ทั้งหมด + คำสั่งรัน migration
- `cmd/seed`: admin 1 คน, user ทดสอบ 1 คน, event 3 รายการ, รอบฉาย 2-3 รอบต่อ event, ที่นั่งรอบละ ~50 ที่ (5 แถว x 10)
- `frontend`: Next.js + TS + Tailwind, หน้าแรกเรียก `/healthz` แล้วแสดงสถานะ, `lib/api.ts`
- `.env.example` ทั้งสองฝั่ง, README เบื้องต้น (วิธีรัน)

**Checklist ตรวจงาน**
- [ ] `docker compose up -d` แล้ว `docker compose ps` เห็น postgres และ redis เป็น healthy
- [ ] รัน migration แล้วครบ 8 ตาราง (ตรวจด้วย `\dt` หรือ Postgres MCP)
- [ ] มี partial unique index `uq_booking_items_active_seat` จริง (ตรวจด้วย `\d booking_items`)
- [ ] migration มี `.down.sql` และรัน down แล้ว up ซ้ำได้ไม่พัง
- [ ] `curl localhost:8080/healthz` ตอบ 200 และแสดง db/redis เป็น ok
- [ ] หยุด Redis แล้ว `/healthz` รายงาน redis ล่ม แต่ server ไม่ crash
- [ ] seed รันซ้ำได้โดยไม่เกิดข้อมูลซ้ำ (idempotent) และรหัสผ่าน admin ถูก hash ด้วย bcrypt
- [ ] `go vet`, `gofmt -l .` (ต้องว่าง), `go test ./... -race` ผ่าน
- [ ] `npm run lint`, `npx tsc --noEmit`, `npm run build` ผ่าน
- [ ] เปิดหน้าเว็บเห็นสถานะ backend
- [ ] ไม่มี secret ใน git (`git grep -i password` ไม่เจอของจริง, `.env` ถูก ignore)
- [ ] คนอ่าน migration ครบทุกบรรทัดและอนุมัติแล้ว

### Phase 2: Catalog (ค้นหารอบ/ที่นั่ง)

งานหลัก: API events/showtimes/seats, ค้นหา (ชื่อ, ช่วงวันที่), cache รายการ event ด้วย Redis (TTL 30 วินาที), หน้า list events, หน้า event detail + รอบ, หน้าผังที่นั่งแสดงสถานะ (ยังเลือกที่นั่งเพื่อดูยอดรวมได้ แต่ยังไม่จอง)

- [ ] `GET /events` รองรับ `q`, `from`, `to` และ pagination ทดสอบด้วย curl จริง
- [ ] ผลค้นหาถูกต้องกับข้อมูล seed (ลองคำค้น 3 แบบ รวมคำที่ไม่มีผลลัพธ์)
- [ ] ครั้งที่ 2 ที่เรียกภายใน 30 วินาทีมาจาก cache (ดูจาก log หรือ `redis-cli keys`) และปิด Redis แล้วยังตอบได้
- [ ] `/showtimes/:id/seats` ตอบสถานะที่นั่ง 3 แบบถูกต้อง (ทดสอบโดยแก้ข้อมูลใน DB ด้วยมือ)
- [ ] ราคาที่แสดงมาจากเซิร์ฟเวอร์ เงินเป็น satang แสดงผลเป็นบาทถูกต้อง
- [ ] หน้าเว็บมี loading / empty / error ครบ
- [ ] มี test ของ service ค้นหาและการคำนวณสถานะที่นั่ง
- [ ] lint/tsc/build/test ผ่านพร้อม output จริง

### Phase 3: Auth + Booking (ส่วนสำคัญที่สุด)

งานหลัก: register/login/me (bcrypt + JWT), middleware auth, `POST /bookings` พร้อม Redis hold + DB transaction ตาม `.cursorrules` ข้อ 6, ยกเลิก booking, job หมดเวลาทุก 30 วินาที, หน้า login/register, เลือกที่นั่ง → จอง → หน้านับถอยหลัง

- [ ] รหัสผ่านถูก hash (ดูใน DB ต้องไม่เห็นข้อความจริง), login ผิดได้ 401, email ซ้ำได้ 409
- [ ] เรียก `/bookings` โดยไม่มี token ได้ 401
- [ ] **Test race:** 20 goroutine จองที่นั่งเดียวกัน ต้องสำเร็จ 1 รายการพอดี รันซ้ำ 10 ครั้งผ่านทุกครั้ง (`-race -count=10`)
- [ ] จองหลายที่นั่งโดยที่ 1 ที่ถูกถืออยู่: ได้ 409 และไม่มี hold ค้างของที่นั่งอื่น
- [ ] ปิด Redis แล้วจองยังได้ และยังกันซ้ำได้ (test)
- [ ] booking หมดเวลา: job ตั้ง EXPIRED, `active=false`, ที่นั่งกลับมาว่าง (test ด้วย TTL สั้น)
- [ ] ยกเลิก booking ของคนอื่นได้ 403/404
- [ ] ราคาที่ผู้ใช้ส่งมา (ถ้ามี) ถูกละเลย ใช้ราคาจาก DB เสมอ
- [ ] จำกัดจำนวนที่นั่งต่อการจอง (ค่าที่คนอนุมัติ) และ rate limit ที่ login/booking
- [ ] ผ่าน `/review-booking-safety` โดยไม่มี VIOLATION ระดับ High
- [ ] คนอ่านโค้ด transaction + Redis ทุกบรรทัดและอนุมัติ
- [ ] ทดสอบด้วยเบราว์เซอร์ 2 หน้าต่างพร้อมกัน เลือกที่นั่งเดียวกัน (หรือใช้ Playwright MCP)

### Phase 4: Payment + Ticket

งานหลัก: สร้าง payment (Idempotency-Key), mock gateway (หน้าให้กด success/fail), webhook ที่ตรวจ HMAC, transaction ยืนยันชำระเงินและออกตั๋ว (code สุ่มที่เดาไม่ได้), หน้า "ตั๋วของฉัน" พร้อม QR (สร้างฝั่งเว็บด้วย library เช่น `qrcode.react` จากค่า code)

- [ ] จ่ายสำเร็จ: booking = PAID, payment = SUCCEEDED, มี ticket เท่าจำนวนที่นั่ง, ที่นั่งเป็น sold
- [ ] จ่ายล้มเหลว: booking ยัง PENDING จ่ายใหม่ได้ภายในเวลา
- [ ] ส่ง webhook ซ้ำ 2 ครั้ง: ticket ไม่ซ้ำ (test)
- [ ] webhook ลายเซ็นผิดหรือไม่มี: 401 และข้อมูลไม่เปลี่ยน (test)
- [ ] ส่ง Idempotency-Key เดิม: ได้ payment เดิม ไม่สร้างใหม่ (test)
- [ ] จ่ายสำเร็จหลังหมดเวลา: payment = NEEDS_REFUND ไม่ออกตั๋ว และเห็นในรายการ (test)
- [ ] ผู้ใช้ดูตั๋วของคนอื่นไม่ได้ (403/404)
- [ ] ticket code ไม่ใช่ตัวเลขเรียงลำดับ (ใช้ random อย่างน้อย 128 bit หรือ UUID)
- [ ] mock gateway ปิดได้ด้วย env (`MOCK_GATEWAY_ENABLED=false`)
- [ ] เดินครบทั้ง flow ในเบราว์เซอร์จริง (ค้นหา → จอง → จ่าย → เห็นตั๋ว+QR)
- [ ] ผ่าน `/review-booking-safety`
- [ ] คนอนุมัติ payment flow และ webhook

### Phase 5: Admin

งานหลัก: middleware admin, CRUD events, สร้าง/แก้/ปิดรอบ (สร้างที่นั่งอัตโนมัติจาก แถว x คอลัมน์ x ราคา), รายการ booking ทั้งหมด (กรองตามสถานะ), dashboard (รายได้, ตั๋วที่ขาย, booking ตามสถานะ), check-in ตั๋วด้วย code, หน้า admin ในเว็บ

- [ ] user ธรรมดาเรียก `/admin/*` ได้ 403 ทุก endpoint (test)
- [ ] หน้า admin เข้าไม่ได้ถ้าไม่ใช่ admin (ทดสอบด้วยบัญชีธรรมดา)
- [ ] สร้าง event + รอบใหม่ แล้วเห็นฝั่งผู้ใช้ทันที (cache ไม่ทำให้ค้างนานเกิน TTL)
- [ ] สร้างรอบแล้วได้จำนวนที่นั่งตรงตามที่กำหนด
- [ ] ลบ/ปิดรอบที่มี booking PAID แล้วถูกป้องกัน (คนตัดสิน rule) ไม่ทำให้ข้อมูลเสีย
- [ ] ตัวเลข dashboard ตรงกับข้อมูลจริงใน DB (เทียบกับ query ด้วยมือ)
- [ ] check-in: ใช้ครั้งแรกสำเร็จ ครั้งที่สองถูกปฏิเสธ (test)
- [ ] ไม่มีการแก้ราคาที่กระทบ booking เดิม
- [ ] lint/tsc/build/test ผ่านพร้อม output จริง

### Phase 6: Hardening + ส่งมอบ

งานหลัก: ไล่ edge case ทั้งหมดใน 4.4, เก็บกวาด error handling, README ฉบับสมบูรณ์, `docs/api.md`, demo script สำหรับสัมภาษณ์, ตรวจ security พื้นฐาน

- [ ] ทำตาม README บนเครื่องที่สะอาด (clone ใหม่ในโฟลเดอร์อื่น) แล้วรันได้จริง ภายในไม่เกิน ~10 คำสั่ง
- [ ] `go test ./... -race -count=1` ผ่านทั้งหมด และมี test ครบรายการใน `.cursorrules` ข้อ 8
- [ ] ไม่มี TODO/FIXME ที่ค้าง, ไม่มี secret ใน repo, มี `.env.example` ครบ
- [ ] CORS จำกัดเฉพาะ origin ของเว็บ, JWT secret มาจาก env, error ไม่เปิดเผย stack/SQL
- [ ] Demo script ซ้อมแล้วจบใน ~5 นาที: ค้นหา → จอง 2 คนชนกัน → จ่าย → ตั๋ว → admin dashboard
- [ ] `docs/ai-log.md` มีบันทึกอย่างน้อย 3 กรณีที่ AI พลาดและวิธีที่จับได้
- [ ] คนอ่านโค้ดส่วน booking/payment/auth ครบ และอธิบายได้ด้วยคำพูดตัวเอง

### เทมเพลต README (ให้ Agent เขียนใน Phase 6 โดยยึดโครงนี้)

```
# Ticket Booking System
## Overview และ Tech stack
## Prerequisites (Go, Node, Docker + เวอร์ชัน)
## Quick start
1. cp backend/.env.example backend/.env && cp frontend/.env.example frontend/.env.local
2. docker compose up -d
3. cd backend && go run ./cmd/server migrate up
4. cd backend && go run ./cmd/seed
5. cd backend && go run ./cmd/server        (http://localhost:8080)
6. cd frontend && npm install && npm run dev (http://localhost:3000)
## Test accounts (admin / user จาก seed)
## Running tests
## Project structure
## Design notes (seat hold, idempotency, fallback เมื่อ Redis ล่ม)
## Known limitations
```