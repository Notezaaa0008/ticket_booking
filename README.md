# Ticket Booking System

Search showtimes and seats, book with a timed seat hold, pay through a mock gateway, receive tickets with QR codes. Admin area for events, bookings, payments and refunds.

**Stack:** Next.js (TypeScript) · Go + Gin + GORM · PostgreSQL 16 · Redis 7

## Prerequisites
Go 1.26+, Node.js 20+, Docker Desktop (running), Git.

## Quick start
```bash
# 1. environment files
cp backend/.env.example backend/.env
cp frontend/.env.example frontend/.env.local

# 2. database and cache
docker compose up -d

# 3. schema and demo data
cd backend
go run ./cmd/server migrate up
cd ..
docker compose exec postgres psql -U ticket -d ticket_booking -v ON_ERROR_STOP=1 -f /seed/seed.sql

# 4. backend (http://localhost:8080/healthz)
cd backend && go run ./cmd/server

# 5. frontend, in another terminal (http://localhost:3000)
cd frontend && npm install && npm run dev
```

## Test accounts (from the seed, dev only)
- Admin: admin@example.com / Admin1234!
- User: user@example.com / User1234!

## Tests and checks
```bash
# from the repo root
./scripts/check.sh          # Windows: ./scripts/check.ps1   (NO_RACE=1 if the race detector is unavailable)

# direct checks
(cd backend && go test ./... -race -count=1)
(cd frontend && npm run lint && npx tsc --noEmit)

# schema constraints, then data invariants (every rule must be PASS)
docker compose exec -T postgres psql -U ticket -d ticket_booking -v ON_ERROR_STOP=1 -f /scripts/verify-schema.sql
docker compose exec -T postgres psql -U ticket -d ticket_booking -f /scripts/checkdb.sql
```

## Security (frontend auth)
**Phase 3 decision (human-approved):** JWT in **localStorage** for MVP speed — explicit tradeoff vs httpOnly cookies (favors avoiding CSRF cookie complexity; accepts XSS can exfiltrate the token if script runs on this origin).

Implementation: `frontend/src/lib/api.ts` (`tb_token`) sends `Authorization: Bearer` on API calls. Mitigations: strict CSP, dependency hygiene, input handling, no secrets in client bundles. Revisit httpOnly + SameSite cookies before production hardening if threat model requires it.

## Design notes
- **PostgreSQL is the source of truth.** A partial unique index (`booking_items(seat_id) WHERE active`) makes double booking impossible; Redis `SET NX EX` is only the fast path and TTL. If Redis is down the system falls back to the database alone.
- **Seat status** (available / held / sold) is computed from the database, so it is correct without Redis.
- **Payments:** idempotency keys, one PENDING payment per booking, signed (HMAC-SHA256) webhooks, amount check, and one transaction with row locks for confirmation, so a duplicate webhook never issues a second set of tickets.
- **Payment after expiry or seat lost** ends as `NEEDS_REFUND`; it is never silently lost.
- **Audit log:** every booking and payment outcome (success, failure, ignored) is stored in append-only tables protected by database triggers. Refunds are built on that chain: booking -> payment -> events -> tickets.
- **Money** is integer satang; times are UTC and shown in Asia/Bangkok.

## Project structure
`backend/` Go API (cmd, internal, migrations, seed) · `frontend/` Next.js app · `scripts/` checks and SQL verification · `docs/` plan, schema, demo script, AI log

## Known limitations
Mock payment gateway only; no email; single instance.
