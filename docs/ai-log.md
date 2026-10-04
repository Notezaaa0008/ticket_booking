# AI Log

## Required cases

### 1. Money was proposed as a float

1. **AI Claim / Proposal:** Store and add prices as `float64` baht, including a client-supplied total.
2. **Truth / Fact:** Money is `int64` satang. Prices and totals come from the database (`price_satang`, `amount_satang`). A float cannot represent 0.10 baht exactly, and a client total can be forged.
3. **How it was Detected:** Code review against `AGENTS.md` and `TestPayment_SuccessIssuesTickets`, which asserts `amount_satang` equals the sum of booking-item prices, not the request body.
4. **Fix / Solution:** Payment, seat, and booking-item amounts are `int64`. `PaymentRepository.ItemStats` sums `price_satang` in SQL. API fields are `*_satang`.
5. **Added Rule / Guideline:** Never use `float32` or `float64` for money. Persist and calculate satang as `int64`. Ignore any price or total sent by the client.

### 2. An audit correction was proposed as an update

1. **AI Claim / Proposal:** Fix a wrong `reason_code` by `UPDATE` on `payment_events` or `booking_events`, or `DELETE` the bad row and insert a replacement.
2. **Truth / Fact:** Those tables are append-only. Triggers `payment_events_append_only` and `booking_events_append_only` reject `UPDATE` and `DELETE`. A correction is a new event.
3. **How it was Detected:** `TestPayment_EventTablesAppendOnly` and `TestAdmin_RefundFlowEventsAppendOnly` run the update and delete and require an error containing `append-only`.
4. **Fix / Solution:** `internal/audit` only `INSERT`s, inside the same transaction as the state change. Failed attempts that roll back are logged afterward with a new row, not by editing an old one.
5. **Added Rule / Guideline:** Never `UPDATE` or `DELETE` `booking_events` or `payment_events`. If a logged reason is wrong, append another event.

### 3. Seat confirmation skipped the row lock

1. **AI Claim / Proposal:** A Redis `SET NX` hold is enough. Load the booking with a plain `SELECT`, then insert items, with no `FOR UPDATE`.
2. **Truth / Fact:** Two requests can pass a check-then-act before either commits. Redis can also be down. The booking row must be locked with `SELECT ... FOR UPDATE` inside the transaction, and the partial unique index on active booking items is the last line of defence.
3. **How it was Detected:** `go test ./... -race -count=1`, specifically `TestBooking_ConcurrentSameSeat`: exactly one of the concurrent bookings for the same seat succeeds.
4. **Fix / Solution:** `repository/booking.go` locks the booking with `FOR UPDATE` and locks due rows with `FOR UPDATE SKIP LOCKED` for the expiry job. Payment confirmation locks the payment row the same way before it re-checks status.
5. **Added Rule / Guideline:** Any transaction that sells, expires, cancels, pays, or refunds a booking locks the row with `FOR UPDATE` and re-checks status inside that transaction. Do not treat the Redis hold as the guarantee.

## Phase 2 — Catalog (session summary)

### 1. Overview

Phase 2 added the **catalog system**: three read-only APIs (`GET /events`, `GET /events/:id`, `GET /showtimes/:id/seats`), Redis-backed list caching (30s TTL), integration tests against Postgres/Redis, and Next.js pages `/events`, `/events/[id]`, `/showtimes/[id]`. Work followed `AGENTS.md` (handler → service → repository, satang integers, standard error JSON) and the attached Phase 2 plan. The human ran `/verify-phase` multiple times; the agent implemented code, ran `./scripts/check.sh` and curl where possible, and the human manually confirmed **P2-10** with the backend stopped.

---

### 2. Actual issues and solutions

| Issue | How it showed up | What we did |
|--------|------------------|-------------|
| **P2-13 — missing API doc** | `/verify-phase`: no `docs/api.md` for the three catalog endpoints. | Added/updated **`docs/api.md`** with query params, response shapes, error codes, cache key notes, and curl examples for all three catalog routes. |
| **P2-03 / P2-07 — catalog tests skipped** | Plain `go test ./...` skipped all `TestCatalog*` when `TEST_DATABASE_URL` was unset; `./scripts/check.sh` initially used a wrong default DB URL so integration tests did not run reliably. | Updated **`scripts/check.sh`**: export `TEST_DATABASE_URL` (default `postgres://ticket:ticket@localhost:5432/ticket_booking_test?sslmode=disable`) and `REDIS_URL`, preflight postgres/redis, verbose test log, and a gate that fails if `TestCatalog*` is skipped when `REQUIRE_INTEGRATION=1`. |
| **List query omitted some events** | Verify red-flag: list SQL used **`INNER JOIN seats`**, so published events with future `on_sale` showtimes but **no seat rows** never appeared. | Refactored **`backend/internal/repository/catalog.go`** to **`LEFT JOIN seats`** on list/count queries so those events still qualify; `min_price_satang` comes from `MIN(s.price_satang)` over joined rows (no seats → SQL `MIN` is null; Go scan yields `0` unless later tightened with `COALESCE`). |
| **P2-10 — UI with backend down** | Checklist requires loading / empty / error when the API is unreachable; automated verify could not complete browser proof. | Frontend pages already had loading, empty, and error branches; **eslint** issues on `useEffect` + `setState` were fixed (keyed search form, async fetch in effects, loader subcomponents). **Human manual test** (backend stopped, `npm run dev` on :3000): all three routes showed fetch failure UI with **Retry** (`/events`, `/events/[id]`, `/showtimes/[id]`). |
| **Integration test seed vs GORM** | Catalog tests failed with `there is no parameter $1` because GORM treated `$1` in multi-statement seed SQL as bind placeholders. | Seed SQL in **`catalog_integration_test.go`** switched to **literal UUID strings** in the script instead of `$1` placeholders. |
| **Doc vs live JSON (minor)** | Some early `docs/api.md` examples used field names that differ from the API (`row_label` / `seat_number` vs doc `row` / `code`). | Catalog sections were aligned toward actual JSON where updated; worth re-diffing doc against curl when editing. |

Other Phase 2 deliverables from the same session (not “fixes” but core scope): `repository/catalog.go`, `service/catalog.go` (validation, ILIKE escape, Redis cache), `handler/catalog.go`, routes in `cmd/server/main.go`, `frontend/src/lib/api.ts`, and **`catalog_integration_test.go`** (search, filters, unpublished/past exclusion, seat statuses + summary, cache, Redis unreachable).

---

### 3. Prompts and fix steps (this project chat)

1. **Implement Phase 2 Catalog** — User pasted the plan (no plan stop): backend three endpoints + Redis cache, frontend three pages, integration tests; final report ≤15 lines per `AGENTS.md` §12.
2. **Plan mode → implement** — Agent added repository/service/handler, wired routes, tests with `TestMain`, frontend URL-driven search and seat map with 10s refresh and disabled “Book — coming soon”.
3. **`/verify-phase` (first runs)** — Agent quoted P2 checklist, ran docker/go/frontend checks, curl when server up; flagged **P2-13** missing doc, **P2-03/P2-07** skips on bare `go test`, **P2-10** not browser-tested, **INNER JOIN seats** risk.
4. **Follow-ups** — User asked for brief verify summaries; backend curl helper process was started/stopped for verification.
5. **Manual P2-10** — User requested confirmation with backend stopped; agent used browser on localhost:3000 and documented error states on all three pages.
6. **`docs/ai-log.md`** — User asked for this English log of Phase 2 interaction and fixes (this file).

Typical fix loop: run `./scripts/check.sh` or targeted `go test ./internal/handler/... -run Catalog` → fix seed/SQL/lint → re-run verify → human manual UI for P2-10.

---

### 4. Verification status (P2-00 … P2-14)

Evidence types used: **`./scripts/check.sh`** (with default test DB URL), **curl** against running backend, **docker compose exec redis redis-cli KEYS 'events:list:*'`**, integration test names, **browser manual test** for P2-10, and **`docs/api.md`**.

| ID | Status | Primary evidence |
|----|--------|------------------|
| P2-00 | Pass | `go vet`, `gofmt`, `go test -race`, frontend lint/tsc/build via `check.sh` |
| P2-01 | Pass | curl: `q`, `from`/`to`, empty `q` |
| P2-02 | Pass | curl: bad `page`, `from`, `limit` → `VALIDATION_FAILED` |
| P2-03 | Pass | `TestCatalogExcludesUnpublishedAndPast` (via `check.sh`) |
| P2-04 | Pass | Redis keys `events:list:*`; `TestCatalogListCache` |
| P2-05 | Pass | curl list 200 after `docker compose stop redis`; `TestCatalogListWorksWhenRedisUnreachable` |
| P2-06 | Pass | curl detail + 404; `available_seat_count` in JSON |
| P2-07 | Pass | `TestCatalogSeatStatusesAndSummary` (held/sold/expired PENDING → available) |
| P2-08 | Pass | Single SQL in `ListShowtimeSeats`; summary from scanned rows |
| P2-09 | Pass | Integer `*_satang` in API JSON |
| P2-10 | Pass | Manual: backend off → loading then error + Retry on all three pages |
| P2-11 | Pass | Seat map: available/held/sold/selected + total + disabled book (code + UI) |
| P2-12 | Pass | `useSearchParams` + `router.push` on `/events` |
| P2-13 | Pass | `docs/api.md` sections for all three catalog endpoints |
| P2-14 | Pass | Catalog routes GET-only; no auth/booking write APIs in Phase 2 scope |

**Note:** Running only `cd backend && go test ./... -race -count=1` **without** exporting `TEST_DATABASE_URL` still **skips** catalog integration tests while exiting 0. Treat **`./scripts/check.sh`** as the authoritative integration run for Phase 2, or export the same env vars before `go test`.

**Phase 2 gate:** With `check.sh` passing, curl checks, manual P2-10, and `docs/api.md` in place, **P2-00 through P2-14 are satisfied** per the checklist in `docs/plan.md`. Human still owns commit/tag `phase-2-done` and any final diff review before Phase 3.

---

## Phase 3 — Auth + Booking (gate notes)

### Human sign-offs (checklist exceptions)

| ID | Status | Note |
|----|--------|------|
| **P3-06** | **PASS** | Human sign-off: Approved **localStorage** as an explicit Phase 3 tradeoff for fast prototyping/MVP. JWT stored in localStorage with awareness of **XSS** (token readable by script) vs **CSRF** (not using cookie session). Documented in `README.md` Security. |
| **P3-42** | **PASS** | Human sign-off: Approved Redis seat holds + DB transaction implementation (lazy expiry, cancel, audit). |
| **P3-32 (PAID)** | **DEFERRED** | UI includes PAID state panel; end-to-end PAID proof deferred to **Phase 4** (payment integration prerequisite). PENDING / EXPIRED / CANCELLED covered by integration tests and `scripts/phase3-browser-e2e.mjs`. |

**P3-40:** `/review-booking-safety` on Phase 3 booking code — no **High** violations; payment/webhook items N/A until Phase 4.

### Final verification status (Phase 3 complete)

**Verdict:** **PHASE VERIFIED** (`/verify-phase` with human gate notes below).

| Area | Result | Evidence |
|------|--------|----------|
| Standard checks (P3-00) | Pass | `go vet`, `gofmt`, `go test ./... -race -count=1`; frontend `lint`, `tsc`, `build` |
| Auth hydration (P3-30–33, P3-19) | Pass | `frontend/src/lib/auth.tsx` — `useSyncExternalStore` + `getToken()` redirect; fixed protected-route hang on `/bookings` |
| Rate limiter fail-open (P3-05) | Pass | `TestRateLimiter_FailsOpenAndLogsWhenRedisDown` in `internal/middleware/ratelimit_test.go` (asserts `rate limit warning (failing open)`) |
| Booking safety (P3-40) | Pass | `/review-booking-safety`: no **High** in Phase 3 booking scope |
| Browser E2E | Pass | `node scripts/phase3-browser-e2e.mjs` — **14/14** (P3-30, P3-31, P3-32 PENDING/CANCELLED/EXPIRED, P3-33, P3-19) |
| Integration / race | Pass | Handler integration tests; `TestBooking_ConcurrentSameSeat` with `-race -count=10` |

**Follow-up fixes included in final verify:** audit test constraint `defer` cleanup; `integrationDBMu` on catalog TRUNCATE; `scripts/check.sh` uses `go test -p 1`; root `package.json` for Playwright.

**Tag:** Commit and `git tag phase-3-done`; human updates progress table in `docs/plan.md`.

---

## Phase 4 — Payment + Ticket (verification & resolution log)

**Date:** October 3, 2026  
**Commit:** `f8eab09` — `feat(phase4): complete payment integration, webhooks, and ticket issuance`  
**Final status:** **PHASE VERIFIED (PASS 100%)** — P4-00 through P4-62 (see `docs/plan.md` progress table).

### 1. Overview

Phase 4 added mock-gateway payments, HMAC webhooks, ticket issuance, and frontend pay/tickets/QR flows. Backend: `PaymentService`, `MockGateway`, `PaymentRepository`, handlers, and **`payment_integration_test.go`** (16 integration tests). Frontend: `/pay/[paymentId]`, `/tickets`, booking Pay + idempotency, `docs/api.md` Phase 4 section. Multiple **`/verify-phase`** runs preceded final sign-off; gaps were closed before the phase commit.

### 2. Issues resolved during Phase 4 verification

| Issue | How it showed up | Resolution |
|--------|------------------|------------|
| **Auth context stuck on `loading`** | Protected routes (e.g. bookings/pay) never left loading when no JWT; `useRequireAuth` waited indefinitely. | **`frontend/src/lib/auth.tsx`**: treat missing token as `anonymous` immediately instead of staying in `loading`. Restored NavBar login/register/logout and home redirect alongside Phase 4 routes. |
| **`gofmt` dirty file** | `/verify-phase` P4-00 fail: `gofmt -l .` listed `payment_integration_test.go`. | Ran `gofmt -w` on handler tests; standard checks clean before final commit. |
| **P4-13 — strict audit event order** | Verify reported incomplete proof: only partial timestamp checks, not full sequence `WEBHOOK_RECEIVED` → `PAYMENT_SUCCEEDED` → `BOOKING_PAID` → `TICKETS_ISSUED`. | Added **`paySuccessProcessingSequence`** (order by `created_at`, table, `xmin`) and **`assertP413SuccessEventOrder`** in `payment_integration_test.go`; used in `TestPayment_SuccessIssuesTickets`, retry success, and first success quadruple in duplicate-webhook test. |
| **`docs/api.md` Phase 4 gap** | Early verify: payment/webhook/ticket endpoints not documented. | Extended **`docs/api.md`** (paths, headers, errors, events); webhook path `/payments/webhook` documented explicitly. |
| **Pay page success UX** | After mock pay, user had to navigate to tickets manually. | **`frontend/src/app/pay/[paymentId]/page.tsx`**: poll while `PENDING`; on `SUCCEEDED`, brief message then `router.replace("/tickets")`. |
| **NEEDS_REFUND traceability tests** | Plan P4-41 needed `raw_payload` on webhook-received rows for refund forensics. | **`assertWebhookRawPayloadStored`**; used on amount-mismatch and after-expiry tests. |
| **Append-only / webhook 500 paths** | P4-20 and P4-42 needed explicit coverage. | **`TestPayment_WebhookInternalError500`**, **`TestPayment_EventTablesAppendOnly`**. |

Other deliverables (not “fixes”): sold-seat assertions after pay, concurrent duplicate webhook race, expiry-job vs webhook race, mock gateway disabled 404, D1 pay-after-`expires_at` when items still active (`TestPayment_JustPastExpiresAtStillAccepted`).

### 3. `/verify-phase` runs (October 3, 2026)

| Run | Outcome | Notes |
|-----|---------|--------|
| Mid-phase | **PHASE NOT VERIFIED** | gofmt dirty; P4-13 order; browser P4-50–53 and human P4-62 pending. |
| After P4-13 + gofmt | **PHASE NOT VERIFIED** | Automated backend/frontend green; browser and P4-60 log still open. |
| Post sign-off + commit `f8eab09` | **PHASE VERIFIED** | `docker compose ps` healthy; `go vet` / `gofmt -l .` / `go test ./... -race -count=1`; frontend lint/tsc/build; curl `/healthz`, `/tickets`, unsigned webhook → `401 INVALID_SIGNATURE`. |

### Human sign-offs

| ID | Status | Note |
|----|--------|------|
| **P4-50–P4-53** | **PASS** | Human browser verification (2026-10-03): full pay flow on `npm run dev` — search → seat map → book → `/pay/[paymentId]` mock success/fail → `/tickets` with QR; distinct payment states on pay page; double Pay reuses Idempotency-Key (`bookings/[id]/page.tsx` `payKey` ref). |
| **P4-62** | **PASS** | Human sign-off (2026-10-03): Approved payment/booking state transitions, webhook HMAC flow, and `PaymentService.applyWebhook` transaction (FOR UPDATE on payment + booking, amount from DB, NEEDS_REFUND paths). |

### P4-60 — `/review-booking-safety` (Phase 4 scope)

**Verdict: no High-severity violations.**

| Rule | Result | Evidence |
|------|--------|----------|
| Partial unique index on active seats | OK (Phase 1) | Migration `uq_booking_items_active_seat`; ticket insert unique on `booking_item_id` |
| Redis holds + DB transaction on book | OK (Phase 3) | Unchanged; payment success releases holds after commit (`payment.go` L374–380) |
| Payment confirmation `FOR UPDATE` + status check | OK | `LockPayment` (`repository/payment.go` L51–53), `bookings.Lock` (`booking.go` L114–115), terminal status guard (`payment.go` L392–396) |
| Webhook HMAC before state change | OK | `validSignature` + `hmac.Equal` (`payment.go` L272–279, L305–311); invalid → no `WEBHOOK_RECEIVED` on payment row / no tx |
| Amount from server/DB only | OK | `InsertPayment` uses `ItemStats.Total` (`payment.go` L183–183); webhook compares `*in.AmountSatang != p.AmountSatang` (`L415–416`) |
| Idempotent webhook / one ticket set | OK | `TestPayment_DuplicateWebhookSequential`, `TestPayment_DuplicateWebhookConcurrent` |
| Pay after expiry / seat lost → NEEDS_REFUND | OK | `TestPayment_AfterExpirySeatRebookedNeedsRefund`, `TestPayment_AmountMismatchNeedsRefund` |
| Audit: no signature/token in events | OK | `TestPayment_InvalidSignatureRejected` (signature not in raw_payload); `signature_valid` boolean only |
| Append-only event tables | OK | `TestPayment_EventTablesAppendOnly` |

**Medium (accepted for MVP):** mock gateway routes unauthenticated when `MOCK_GATEWAY_ENABLED=true` (dev-only; disable in production). **`WEBHOOK_RECEIVED`** commits before the processing transaction (`payment.go` L335–338); success path events `PAYMENT_SUCCEEDED` → `BOOKING_PAID` → `TICKETS_ISSUED` share one transaction.

**Low:** Refund idempotency rules (item 13) — Phase 5.

### 4. Final verification status (P4-00 … P4-62)

**Verdict:** **PHASE VERIFIED (PASS 100%)** — October 3, 2026.

| ID / area | Result | Primary evidence |
|-----------|--------|------------------|
| P4-00 | Pass | `gofmt -l .` empty; `go vet ./...`; `go test ./... -race -count=1`; frontend lint/tsc/build |
| P4-01–P4-05 | Pass | `TestPayment_CreateRules`, `TestPayment_CancelledBookingNotPayable`, ownership tests |
| P4-10–P4-20 | Pass | 16× `TestPayment_*`; `gateway.go` HTTP webhook; curl unsigned webhook → 401 |
| P4-13 | Pass | `assertP413SuccessEventOrder` + `paySuccessProcessingSequence` |
| P4-30–P4-32 | Pass | `newTicketCode` (128-bit); ticket list/get; booking GET payment + tickets when PAID |
| P4-40–P4-43 | Pass | Outcome/reason assertions; NEEDS_REFUND + `raw_payload`; append-only trigger test |
| P4-50–P4-53 | Pass | Human browser sign-off (table above); idempotency UI + `TestPayment_CreateRules` |
| P4-60 | Pass | Safety review — **no High-severity violations** in money/seat/webhook logic (table above) |
| P4-61 | Pass | `docs/api.md` § Payments & tickets |
| P4-62 | Pass | Human transaction/webhook approval (table above) |

**Note:** Phase 4 has no Playwright script yet (unlike Phase 3 `scripts/phase3-browser-e2e.mjs`); browser criteria rely on documented human verification.

**Suggested tag:** `git tag phase-4-done` on `f8eab09` (or after this doc commit).

---

## Log Entry: Phase 5 Verification & Final Sign-Off

- **Date:** 2026-10-04
- **Phase:** Phase 5 (Admin & Refund Management)
- **Status:** Completed (`phase-5-done`)

### Summary of Activities & Fixes

1. **Verification failures & fixes**
   - **P5-40:** Refactored `ProcessRefund` so `RefundProvider` runs **outside** the active database transaction (claim in tx → provider call → finalize in a second tx). Avoids holding row locks during mock/provider I/O and matches `/review-booking-safety` expectations. Evidence: `backend/internal/service/refund.go` (claim/finalize split); `TestAdmin_RefundProviderFailureThenRetry`, concurrent/idempotency tests.
   - **Frontend lint/build:** Resolved `react-hooks/set-state-in-effect` on `frontend/src/app/admin/payments/page.tsx` by deriving `status` and `page` from URL search params instead of syncing with `setState` in `useEffect`.

2. **E2E & manual verification**
   - **P5-26:** End-to-end refund workflow verified in Admin UI: `NEEDS_REFUND` payment → Dashboard → request/process refund → booking timeline shows full event chain (human run, 2026-10-03/04).
   - **P5-42:** Human code review and sign-off on refund transaction logic, safety bounds, and business rules (eligibility, idempotency, seat release vs started showtime, append-only audit).

3. **Automated checks**
   - **Backend:** `go vet ./...`, `gofmt -l .` (clean), `go test ./... -race -count=1` — all packages OK; admin/refund coverage in `admin_integration_test.go` (`TestAdmin_*`).
   - **Frontend:** `npm run lint`, `npx tsc --noEmit`, `npm run build` — zero errors.
   - **`/verify-phase` (2026-10-03):** **PHASE VERIFIED** — P5-00 through P5-42 with test names, curl `/healthz` + `/admin/stats`, human items attested.

### Human sign-offs (Phase 5)

| ID | Status | Note |
|----|--------|------|
| **P5-02** | **PASS** | Non-admin blocked on `/admin/*` via `AdminGuard` + browser check with regular user. |
| **P5-26** | **PASS** | Full NEEDS_REFUND → admin refund → timeline (UI). |
| **P5-42** | **PASS** | Refund rules and transaction code reviewed line-by-line. |

### Deliverables (scope reminder)

Admin APIs and UI (`/admin/stats`, events/showtimes CRUD, bookings/timeline, payments/refunds, check-in), `docs/api.md` § Admin (Phase 5), mock `RefundProvider` (tests override for failure paths). Out of scope: real payment provider, partial refunds unless approved.

**Gate:** Commit Phase 5 work and `git tag phase-5-done` before Phase 6 (`docs/plan.md` §0).

---

## Log Entry: Phase 6 Verification & Final Sign-Off

- **Date:** 2026-10-04
- **Phase:** Phase 6 (Hardening + Delivery)
- **Status:** **PHASE VERIFIED (PASS 100%)**

Human sign-off on the items left open by the automated `/verify-phase` run:

| ID | Status | Note |
|----|--------|------|
| **P6-09** | **PASS** | Every page's loading, empty, and error states checked in the browser. Booking, payment, and refund states have their own messages. No console errors on the main flow. |
| **P6-12** | **PASS** | Clean-room clone in a fresh directory, following only `README.md`. Setup succeeded. |
| **P6-13** | **PASS** | Demo from `docs/demo-script.md` rehearsed and finished in about 5 minutes. |
| **P6-16** | **PASS** | Booking, payment, refund, and auth code reviewed. Ready for a walkthrough. |

Automated checks the same day: `docker compose ps` healthy; `go vet`, `gofmt -l` clean, `go test ./... -race -count=1`; frontend lint, `tsc`, and build; curl `/healthz`, `/api/v1/events`, unauthenticated `/api/v1/admin/stats` → 401, bad webhook signature → 401; `scripts/checkdb.sql` rules 1–8 PASS. P6-00 through P6-16 are checked in `docs/plan.md`. Commit and tag are not done yet.
