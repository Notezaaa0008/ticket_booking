# sample prompt

Read AGENTS.md only (not docs/). Work only on Phase 2: Catalog. No auth, no booking. Implement directly; no plan stop.
Report max 15 lines (AGENTS.md section 12).

BACKEND (GORM Raw/Scan with small structs is fine; no need for full models)
1. GET /api/v1/events?q=&from=&to=&page=&limit=   (page default 1, limit default 20, max 100)
   - Only events with status 'published' having at least one 'on_sale' showtime in the future (inside from/to if given).
   - q: case-insensitive match on title or venue (ILIKE with % and _ escaped). Sort by earliest upcoming showtime.
   - Item: id, title, venue, poster_url, next_showtime_at, min_price_satang. Wrapper: items, page, limit, total.
   - Bad params (invalid date, page<1, limit>100) => 400 VALIDATION_FAILED.
   - Cache the JSON in Redis, key "events:list:" + normalized params, TTL 30s. Redis error => query the DB, log a warning, never fail.
2. GET /api/v1/events/:id => event + upcoming showtimes (id, starts_at, status, available_seat_count). 404 NOT_FOUND if missing or not published.
3. GET /api/v1/showtimes/:id/seats => seats [{id,row_label,seat_number,zone,price_satang,status}] + summary {total,available,held,sold}. No cache. Use ONE query:

   SELECT s.id, s.row_label, s.seat_number, s.zone, s.price_satang,
     CASE WHEN b.status IN ('PAID','REFUNDED') THEN 'sold'
          WHEN b.status = 'PENDING' AND b.expires_at > now() THEN 'held'
          ELSE 'available' END AS status
   FROM seats s
   LEFT JOIN booking_items bi ON bi.seat_id = s.id AND bi.active
   LEFT JOIN bookings b ON b.id = bi.booking_id
   WHERE s.showtime_id = $1 ORDER BY s.row_label, s.seat_number;

   (An expired PENDING booking therefore reads as available.) 404 if the showtime does not exist.
4. Money is int64 satang in JSON. Wire routes in cmd/server/main.go. Use the existing handler.RespondError and domain codes.

FRONTEND (minimal styling; use existing src/lib/api.ts and format.ts)
- /events: search form (text, date from/to), results list, simple pagination, state in the URL query string.
- /events/[id]: event info + showtimes with available counts.
- /showtimes/[id]: seat map grouped by row with 4 states (available, held, sold, selected). Selecting shows a total using server prices.
  Disabled "Book" button labelled "coming soon". Refetch seats every 10s while the tab is visible.
- Every page: loading, empty, error states.

TESTS (real test DB via TEST_DATABASE_URL, TestMain applies migrations; keep the set small)
- search: q match, date range, no results, bad param => 400
- unpublished event and event with only past showtimes excluded
- seat status: available, held, sold, and expired-PENDING => available; summary counts match
- cache: second call served from cache; endpoint still works when Redis is unreachable

OUT OF SCOPE: auth, booking, payment, admin, docs/api.md.

ACCEPTANCE: the 3 endpoints behave as above; tests pass; lint/tsc/build clean (the human runs scripts/check).