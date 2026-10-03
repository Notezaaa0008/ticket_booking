# AI Log

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
