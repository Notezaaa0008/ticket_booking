# AGENTS.md: Ticket Booking System

## 0. Mission
Web app: users search showtimes/seats, book, pay, receive tickets; admins manage events, bookings, refunds.
Interview assignment: correctness and reviewability beat feature count. Two non-negotiables:
1. A seat is never sold twice.
2. Every booking and payment operation ends in a recorded outcome (SUCCESS/FAILURE/IGNORED + reason) so any payment can be traced and refunded.

## 1. Stack (do not change without asking)
Next.js (App Router, TypeScript, Tailwind) in /frontend. Go 1.26+, Gin, GORM in /backend. PostgreSQL 16 = source of truth.
Redis 7 (go-redis v9) = seat hold, cache, rate limit only. JWT + bcrypt. SQL migrations (golang-migrate), never AutoMigrate.

## 2. Layout
backend/cmd/server (main, `migrate up|down`), internal/{config,db,cache,domain,audit,model,repository,service,handler,middleware},
migrations/. Schema = backend/migrations/0001_init.up.sql (read it instead of docs). frontend/src/{app,components,lib}.
Already provided by the human, do not rewrite: docker-compose.yml, migrations, seed, internal/domain/codes.go, internal/audit,
config, db, cache, middleware, health handler, scripts/.

## 3. Commands
- Infra: docker compose up -d
- Backend: cd backend && go run ./cmd/server  |  go test ./... -race -count=1  |  go vet ./... && gofmt -l .
- Frontend: cd frontend && npm run dev  |  npm run lint && npx tsc --noEmit && npm run build
- The HUMAN runs scripts/check.sh for full checks. While iterating, run only the tests of the packages you touched.

## 4. Process and token discipline (MANDATORY)
- Work only on the current phase prompt. Do not read docs/plan.md or docs/schema.md unless told to.
- Do not read files unrelated to the task. Do not re-print code or long outputs in chat. Use minimal diffs.
- No long explanations. Plan (when asked) max 15 lines. Final report max 15 lines (section 12).
- Ask a question only if a decision is not covered by the prompt or section 10; otherwise decide and state it in one line.
- Unsure about a library API (Gin, GORM, go-redis, Next.js)? Check Context7. Never invent functions.
- Dependencies: only those the prompt approves. Do not commit or push.

## 5. Backend conventions
- handler -> service -> repository. Handlers have no business logic. Routes under /api/v1.
- Error body: {"error":{"code":"...","message":"..."}} using domain.AppError and handler.RespondError. Codes are domain.ReasonCode constants only.
- context.Context + timeouts on every DB/Redis call. UUID ids. Money = int64 satang (never float). Time = UTC timestamptz.
- Never trust client prices, totals, user ids or roles. Prices come from the DB. Config only from env.
- Never log passwords, tokens or Authorization headers. HTTP: 400 validation, 401, 403, 404, 409 conflict/state, 422 business rule.
- No ignored errors (`_ =`) without a comment.

## 6. Booking and payment safety (NON-NEGOTIABLE)
- Partial unique indexes in the migration are the last line of defence. Never remove or weaken them.
- Seat hold: Redis `SET hold:{seat_id} {booking_id} NX EX ttl` per seat; if any fails, release only keys you own (compare value, e.g. Lua) and return 409.
  Then ONE DB transaction: booking PENDING + booking_items active=true + audit event. Unique violation = conflict: rollback, release keys, 409.
- Redis error (not "key exists") => continue DB-only and log a warning. Never oversell because Redis is down.
- Expiry job every 30s: expired PENDING bookings, FOR UPDATE SKIP LOCKED, set EXPIRED + items active=false + event. Booking creation also lazily expires stale holds.
- Payment confirmation: ONE transaction, SELECT ... FOR UPDATE on payment then booking, re-check status inside. Idempotent (same webhook twice = one ticket set).
  Cancel and expiry also lock the booking FOR UPDATE.
- Webhook: verify HMAC-SHA256 of the RAW body (constant-time compare); invalid => 401 and no state change. Amount must equal payments.amount_satang.
- Money taken but no tickets possible (expired, seat lost, amount mismatch) => payment NEEDS_REFUND. Never silently lost.

## 7. Frontend
- TypeScript strict, no `any`. All API calls via src/lib/api.ts. Every page has loading, empty and error states.
- Booking/payment/refund screens show DISTINCT states (pending, processing, success, failed + reason, expired, cancelled, needs refund). Never a generic error.
- Seat map: available / held / sold / selected. Display server prices only. Simple accessible UI, minimal styling.

## 8. Testing
- Integration tests use the real Postgres/Redis from docker compose and database ticket_booking_test (TEST_DATABASE_URL). Never the dev DB. A TestMain applies migrations.
- No mocks for race/transaction proofs. Never delete or weaken a failing test; fix the cause or ask.
- Each outcome path writes an audit event and has a test asserting event_type, outcome, reason_code and resulting status.

## 9. Definition of Done
Never write "done" without evidence: the tests you ran (names + pass/fail), a criterion table (PASS/FAIL/NOT DONE with file or test as evidence),
and what you skipped, mocked or are unsure about. Report failures honestly. "Should work" is not evidence.
Known failure modes to avoid: claiming tests pass without running them, weakening tests, hard-coding/mocking so a flow only looks working,
inventing library functions, unrequested features, forgetting failure paths, leaving TODO/FIXME.

## 10. STOP and ask before
Schema/migrations/triggers; booking, hold, locking or transaction logic changes beyond the prompt; payment or refund flow rules; audit tables, event types,
reason codes; auth/JWT/permissions; new dependencies; .env or secrets; business rules (hold time, max seats, pricing, rate limits, refund policy);
deleting files or large rewrites; editing AGENTS.md, docs/plan.md, docs/schema.md.

## 11. Git
Do not commit or push. Suggest a conventional commit message at the end.

## 12. Final report (max 15 lines)
Summary (2-3 lines) / files changed / criterion table (PASS/FAIL/NOT DONE + short evidence) / skipped or mocked / risks / commit message.
Do not paste command output unless something failed.

## 13. Audit logging and refund readiness
- Only internal/audit writes events (RecordBookingEvent, RecordPaymentEvent). Pass the transaction so state change and event commit together.
  A failed attempt that rolled back is logged afterwards with the plain connection; if logging fails, log to stderr with the request id and return the original error.
- booking_events and payment_events are append-only (DB trigger). Never UPDATE/DELETE them. Corrections are new events.
- Values live in internal/domain/codes.go. Path -> event:
  booking OK BOOKING_CREATED; booking rejected BOOKING_CREATE_FAILED(reason); expiry BOOKING_EXPIRED (actor SYSTEM, reason HOLD_EXPIRED);
  cancel BOOKING_CANCELLED / BOOKING_CANCEL_REJECTED; payment create PAYMENT_CREATED / PAYMENT_CREATE_FAILED;
  every webhook WEBHOOK_RECEIVED then exactly one of WEBHOOK_REJECTED, PAYMENT_SUCCEEDED, PAYMENT_FAILED, PAYMENT_DUPLICATE_IGNORED (IGNORED),
  PAYMENT_AMOUNT_MISMATCH, PAYMENT_AFTER_EXPIRY; success also BOOKING_PAID + TICKETS_ISSUED;
  refund REFUND_REQUESTED then REFUND_COMPLETED | REFUND_FAILED | REFUND_REJECTED, plus BOOKING_REFUNDED.
- API responses for bookings/payments always carry status and, when relevant, failure_code + failure_message; the frontend maps each to a specific message.
- Traceable chain for every PAID booking: booking -> payment (provider_ref, amount) -> payment_events -> booking_items -> tickets.
  Refunds reference payment_id and booking_id, are idempotent, run in one transaction with FOR UPDATE, total never exceeds the payment amount.
- Never store passwords, tokens, signatures or card data in metadata/raw_payload (signature_valid is a boolean).
