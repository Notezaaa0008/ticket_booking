# Demo script (about 5 minutes)

Prepare: `docker compose up -d`, backend and frontend running, database freshly migrated and seeded.
Accounts: admin@example.com / Admin1234!, user@example.com / User1234! (register a second user for step 3).

1. **Search** (30s): open /events, search by name and by date range, open an event, open a showtime and show the seat map (available / held / sold).
2. **Book** (30s): log in as the user, pick 2 seats, press Book. Show the countdown that comes from the server's `expires_at`.
3. **Collision** (60s): in a second browser (private window, second user) try the same seat. Show the 409 message that names the taken seat. Say: "Redis SET NX is the fast path; the partial unique index in PostgreSQL is the guarantee, and a race test proves exactly one wins."
4. **Pay** (45s): press Pay, then "Pay successfully" on the mock gateway. Show the PAID booking and tickets with QR codes.
5. **Failure and retry** (45s): book again, press "Simulate failure". Show the failure reason on screen, then retry and succeed.
6. **Payment after expiry** (45s): book, wait for expiry (or lower SEAT_HOLD_TTL_SECONDS), pay anyway. Show the NEEDS_REFUND message.
7. **Admin** (60s): log in as admin. Open that booking's timeline (events with SUCCESS / FAILURE / IGNORED and reason codes). Refund the NEEDS_REFUND payment and show the new state.
8. **Integrity** (15s): run `docker compose exec postgres psql -U ticket -d ticket_booking -f /scripts/checkdb.sql` and show all PASS.

Talking points: PostgreSQL is the source of truth; Redis failure degrades gracefully; webhooks are signed and idempotent; every outcome is logged append-only so refunds are traceable; how the AI agent was supervised (see docs/ai-log.md).
