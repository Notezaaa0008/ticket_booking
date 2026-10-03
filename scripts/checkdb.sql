-- Data-consistency invariants. Every row must show PASS (violations = 0).
-- Run:  docker compose exec postgres psql -U ticket -d ticket_booking -f /scripts/checkdb.sql
SELECT rule, violations, CASE WHEN violations = 0 THEN 'PASS' ELSE 'FAIL' END AS result
FROM (
  SELECT '1  no seat has more than one active booking item' AS rule,
         (SELECT count(*) FROM (SELECT seat_id FROM booking_items WHERE active GROUP BY seat_id HAVING count(*) > 1) x) AS violations
  UNION ALL
  SELECT '2  EXPIRED/CANCELLED bookings have no active items',
         (SELECT count(*) FROM bookings b JOIN booking_items bi ON bi.booking_id = b.id
           WHERE bi.active AND b.status IN ('EXPIRED','CANCELLED'))
  UNION ALL
  SELECT '3  every PAID booking has a matching SUCCEEDED/REFUNDED payment and tickets = items',
         (SELECT count(*) FROM bookings b
           WHERE b.status = 'PAID' AND (
             NOT EXISTS (SELECT 1 FROM payments p WHERE p.booking_id = b.id
                         AND p.status IN ('SUCCEEDED','REFUNDED') AND p.amount_satang = b.total_satang)
             OR (SELECT count(*) FROM tickets t JOIN booking_items bi ON bi.id = t.booking_item_id WHERE bi.booking_id = b.id)
                <> (SELECT count(*) FROM booking_items bi2 WHERE bi2.booking_id = b.id)))
  UNION ALL
  SELECT '4  no PENDING booking is more than 2 minutes past expires_at (expiry job alive)',
         (SELECT count(*) FROM bookings WHERE status = 'PENDING' AND expires_at < now() - interval '2 minutes')
  UNION ALL
  SELECT '5a every SUCCEEDED payment has a PAYMENT_SUCCEEDED event',
         (SELECT count(*) FROM payments p WHERE p.status = 'SUCCEEDED'
           AND NOT EXISTS (SELECT 1 FROM payment_events e WHERE e.payment_id = p.id
                           AND e.event_type = 'PAYMENT_SUCCEEDED' AND e.outcome = 'SUCCESS'))
  UNION ALL
  SELECT '5b every PAID/REFUNDED booking has a BOOKING_PAID event',
         (SELECT count(*) FROM bookings b WHERE b.status IN ('PAID','REFUNDED')
           AND NOT EXISTS (SELECT 1 FROM booking_events e WHERE e.booking_id = b.id
                           AND e.event_type = 'BOOKING_PAID' AND e.outcome = 'SUCCESS'))
  UNION ALL
  SELECT '6a completed refunds never exceed the payment amount',
         (SELECT count(*) FROM payments p
           WHERE (SELECT COALESCE(sum(r.amount_satang), 0) FROM refunds r
                  WHERE r.payment_id = p.id AND r.status = 'COMPLETED') > p.amount_satang)
  UNION ALL
  SELECT '6b every REFUNDED payment has a COMPLETED refund',
         (SELECT count(*) FROM payments p WHERE p.status = 'REFUNDED'
           AND NOT EXISTS (SELECT 1 FROM refunds r WHERE r.payment_id = p.id AND r.status = 'COMPLETED'))
  UNION ALL
  SELECT '7  VOID tickets belong to REFUNDED bookings',
         (SELECT count(*) FROM tickets t
            JOIN booking_items bi ON bi.id = t.booking_item_id
            JOIN bookings b ON b.id = bi.booking_id
           WHERE t.status = 'VOID' AND b.status <> 'REFUNDED')
) checks
ORDER BY rule;

-- Informational: payments still waiting for a refund
SELECT id AS payment_id, booking_id, amount_satang, failure_code, created_at
FROM payments WHERE status = 'NEEDS_REFUND' ORDER BY created_at;
