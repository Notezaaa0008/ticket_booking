# /review-booking-safety

Act as a skeptical reviewer, not the author. Review the booking and payment code for
race conditions and money/seat integrity. Do not modify code; only report.

Check each item and answer with file:line evidence or "VIOLATION":
1. Is there a partial unique index on booking_items(seat_id) WHERE active = true in a migration?
2. Does booking creation acquire Redis holds with SET NX EX, and release only keys it owns?
3. Is booking + booking_items creation inside ONE DB transaction? What happens on unique violation?
4. If Redis is down or returns an error, does the code still prevent double booking?
5. Is every active=false transition covered: expiry job, cancel, payment failure?
6. Does payment confirmation use SELECT ... FOR UPDATE and a status check inside a transaction?
7. Is webhook handling idempotent (same event twice gives one ticket set)? Is the signature verified
   with a constant-time compare?
8. What happens if payment succeeds AFTER the hold expired and another user already took the seat?
   Is the outcome explicit (e.g. payment flagged NEEDS_REFUND) and not silently lost?
9. Are prices read from the DB, never from client input? Is money stored as integers?
10. Which tests prove items 1-8? Name them. Which items have no test?
Finish with a ranked list of risks (High/Medium/Low) and a suggested test for each missing proof.