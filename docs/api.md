# Ticket Booking API — Catalog (Phase 2)

Base URL: `http://localhost:8080/api/v1`
เนื้อหาเฉพาะ endpoint สำหรับ **อ่านข้อมูล** (read-only) ยังไม่มี API เขียนการจองใน Phase นี้

## ข้อตกลงร่วม

### หน่วยเงิน
ทุกฟิลด์ที่ลงท้ายด้วย `_satang` เป็น **จำนวนเต็ม (integer) หน่วยสตางค์** เสมอ
ไม่มีทศนิยมใน JSON เด็ดขาด — `150000` = 1,500.00 บาท การแปลงเป็นบาทเป็นหน้าที่ของ client

### เวลา
เป็น RFC 3339 พร้อม offset เช่น `2026-10-11T02:00:00+07:00`

### รูปแบบ error มาตรฐาน
ทุก error ตอบด้วยโครงเดียวกัน:

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "invalid page"
  }
}
```

| HTTP | code | ความหมาย |
|---|---|---|
| 400 | `VALIDATION_FAILED` | พารามิเตอร์ไม่ถูกต้อง |
| 404 | `NOT_FOUND` | ไม่พบทรัพยากร หรือยังไม่ `published` |
| 500 | `INTERNAL_ERROR` | ข้อผิดพลาดฝั่งเซิร์ฟเวอร์ |

### เกณฑ์การมองเห็นข้อมูล (visibility)
Event จะปรากฏใน API ก็ต่อเมื่อ:
- `events.status = 'published'` **และ**
- มีรอบฉายอย่างน้อย 1 รอบที่ `showtimes.status = 'on_sale'` และ `starts_at > now()`

Event ที่เป็น `draft` หรือมีแต่รอบในอดีต จะไม่ปรากฏทั้งในลิสต์และหน้ารายละเอียด (ตอบ 404)

---

## 1. GET /events

ค้นหารายการ event พร้อมแบ่งหน้า

### Query parameters

| ชื่อ | ชนิด | ค่าเริ่มต้น | ข้อจำกัด | คำอธิบาย |
|---|---|---|---|---|
| `q` | string | `""` | - | ค้นหาจากชื่อ event / สถานที่ (case-insensitive, partial match) |
| `from` | date | - | `YYYY-MM-DD` | กรองรอบฉายที่เริ่มตั้งแต่วันนี้เป็นต้นไป |
| `to` | date | - | `YYYY-MM-DD` | กรองรอบฉายที่เริ่มไม่เกินวันนี้ |
| `page` | integer | `1` | ≥ 1 | เลขหน้า |
| `limit` | integer | `20` | 1–100 | จำนวนรายการต่อหน้า |

### Response 200

```json
{
  "items": [
    {
      "id": "eeeeeeee-0000-0000-0000-000000000001",
      "title": "Bangkok Jazz Night",
      "venue": "Lumpini Hall",
      "poster_url": "",
      "next_showtime_at": "2026-10-11T02:00:00+07:00",
      "min_price_satang": 50000
    }
  ],
  "page": 1,
  "limit": 2,
  "total": 1
}
```

| ฟิลด์ | ชนิด | หมายเหตุ |
|---|---|---|
| `items[].next_showtime_at` | string | รอบ `on_sale` ที่ใกล้ที่สุดในอนาคต |
| `items[].min_price_satang` | integer | ราคาต่ำสุดของที่นั่งในรอบที่ยังขายอยู่ |
| `total` | integer | จำนวนผลลัพธ์ทั้งหมด (ไม่ใช่จำนวนในหน้านี้) |

ไม่พบผลลัพธ์ → ยังคงเป็น **200** พร้อม `items: []` ไม่ใช่ 404

### ตัวอย่าง

```bash
curl -s "http://localhost:8080/api/v1/events?q=jazz&limit=2"
curl -s "http://localhost:8080/api/v1/events?from=2026-10-01&to=2026-12-31"
curl -s "http://localhost:8080/api/v1/events?q=zzzznomatch999"
# => {"items":[],"page":1,"limit":20,"total":0}
```

### Error

| กรณี | HTTP | message |
|---|---|---|
| `page=0` หรือติดลบ | 400 | `invalid page` |
| `limit=101` หรือ `limit=0` | 400 | `invalid limit` |
| `from=not-a-date` | 400 | `invalid from date` |
| `to` รูปแบบผิด | 400 | `invalid to date` |

### Cache
ผลลัพธ์ถูก cache ใน Redis ด้วย key รูปแบบ
`events:list:from=<from>&limit=<limit>&page=<page>&q=<q>&to=<to>` อายุ 30 วินาที

หาก Redis ไม่พร้อมใช้งาน API จะ **fallback ไปอ่านจากฐานข้อมูลโดยตรง** และยังตอบ 200 ตามปกติ

---

## 2. GET /events/{id}

รายละเอียด event พร้อมรายการรอบฉาย

### Path parameter

| ชื่อ | ชนิด | คำอธิบาย |
|---|---|---|
| `id` | UUID | รหัส event |

### Response 200

```json
{
  "id": "eeeeeeee-0000-0000-0000-000000000001",
  "title": "Bangkok Jazz Night",
  "description": "An evening of live jazz.",
  "venue": "Lumpini Hall",
  "poster_url": "",
  "showtimes": [
    {
      "id": "55555555-0000-0000-0000-000000000001",
      "starts_at": "2026-10-11T02:00:00+07:00",
      "status": "on_sale",
      "min_price_satang": 150000,
      "available_seat_count": 50
    }
  ]
}
```

- `showtimes` มีเฉพาะรอบ `on_sale` ที่ยังไม่เริ่ม เรียงตาม `starts_at` จากน้อยไปมาก
- `available_seat_count` นับที่นั่งที่ยังว่าง ณ เวลาที่เรียก (ที่นั่งซึ่ง hold หมดอายุแล้วถือว่าว่าง)

### Error

| กรณี | HTTP | code | message |
|---|---|---|---|
| ไม่มี event นี้ | 404 | `NOT_FOUND` | `event not found` |
| event เป็น `draft` | 404 | `NOT_FOUND` | `event not found` |
| event ไม่มีรอบ `on_sale` ในอนาคต | 404 | `NOT_FOUND` | `event not found` |
| `id` ไม่ใช่ UUID | 400 | `VALIDATION_FAILED` | `invalid event id` |

ใช้ 404 กับ event ที่ยังไม่ published โดยตั้งใจ เพื่อไม่เปิดเผยว่ามี draft อยู่จริง

### ตัวอย่าง

```bash
curl -s "http://localhost:8080/api/v1/events/eeeeeeee-0000-0000-0000-000000000001"
curl -s -w "\nHTTP:%{http_code}\n" \
  "http://localhost:8080/api/v1/events/eeeeeeee-0000-0000-0000-000000000099"
# => {"error":{"code":"NOT_FOUND","message":"event not found"}}  HTTP:404
```

---

## 3. GET /showtimes/{id}/seats

ผังที่นั่งของรอบฉาย พร้อมสรุปจำนวนแต่ละสถานะ

### Path parameter

| ชื่อ | ชนิด | คำอธิบาย |
|---|---|---|
| `id` | UUID | รหัสรอบฉาย (showtime) |

### สถานะที่นั่ง

| status | เงื่อนไข |
|---|---|
| `available` | ไม่มี booking ที่ถือครองอยู่ **หรือ** มีแต่เป็น `PENDING` ที่ `expires_at <= now()` |
| `held` | มี booking สถานะ `PENDING` ที่ `expires_at > now()` |
| `sold` | มี booking สถานะ `PAID` |

`selected` เป็นสถานะฝั่ง UI เท่านั้น API ไม่ส่งค่านี้

### Response 200

```json
{
  "showtime_id": "55555555-0000-0000-0000-000000000001",
  "seats": [
    { "id": "...", "code": "A1", "row": "A", "number": 1, "price_satang": 150000, "status": "held" },
    { "id": "...", "code": "A2", "row": "A", "number": 2, "price_satang": 150000, "status": "sold" },
    { "id": "...", "code": "A3", "row": "A", "number": 3, "price_satang": 150000, "status": "available" }
  ],
  "summary": {
    "total": 50,
    "available": 48,
    "held": 1,
    "sold": 1
  }
}
```

`summary.available + summary.held + summary.sold` ต้องเท่ากับ `summary.total` เสมอ
ข้อมูลทั้งชุดดึงด้วย query เดียว ไม่มีการ query ต่อที่นั่ง (ไม่มี N+1)

### Error

| กรณี | HTTP | code | message |
|---|---|---|---|
| ไม่มีรอบนี้ หรือ event ยังไม่ published | 404 | `NOT_FOUND` | `showtime not found` |
| `id` ไม่ใช่ UUID | 400 | `VALIDATION_FAILED` | `invalid showtime id` |

### ตัวอย่าง

```bash
curl -s "http://localhost:8080/api/v1/showtimes/55555555-0000-0000-0000-000000000001/seats"
```

---

## ขอบเขตของ Phase 2

API ชุดนี้เป็น read-only ทั้งหมด ยังไม่มี endpoint สำหรับสร้าง/แก้ไขการจอง และยังไม่มีการยืนยันตัวตน
ปุ่มจองบน UI ถูกปิดไว้โดยตั้งใจ (`Book — coming soon`) จนกว่าจะถึง Phase 3