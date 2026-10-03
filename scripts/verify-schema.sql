-- Verifies the constraints that protect against double booking and tampering with logs.
-- Safe to run on the dev DB: everything is rolled back at the end.
-- Run:  docker compose exec postgres psql -U ticket -d ticket_booking -v ON_ERROR_STOP=1 -f /scripts/verify-schema.sql
-- Expected: 9 lines starting with "PASS" and none starting with "FAIL".
BEGIN;
DO $$
DECLARE
  u   uuid := gen_random_uuid();
  e   uuid := gen_random_uuid();
  st  uuid := gen_random_uuid();
  s1  uuid := gen_random_uuid();
  b1  uuid := gen_random_uuid();
  b2  uuid := gen_random_uuid();
  p1  uuid := gen_random_uuid();
  bev uuid;
  pev uuid;
BEGIN
  INSERT INTO users(id, email, password_hash, name) VALUES (u, 'verify-' || u || '@test.local', 'x', 'Verify');
  INSERT INTO events(id, title, venue) VALUES (e, 'E', 'V');
  INSERT INTO showtimes(id, event_id, starts_at) VALUES (st, e, now() + interval '1 day');
  INSERT INTO seats(id, showtime_id, row_label, seat_number, price_satang) VALUES (s1, st, 'A', 1, 1000);
  INSERT INTO bookings(id, user_id, showtime_id, total_satang, expires_at)
    VALUES (b1, u, st, 1000, now() + interval '10 minutes'), (b2, u, st, 1000, now() + interval '10 minutes');
  INSERT INTO booking_items(booking_id, seat_id, price_satang, active) VALUES (b1, s1, 1000, true);

  -- T1: a second ACTIVE item on the same seat must be rejected
  BEGIN
    INSERT INTO booking_items(booking_id, seat_id, price_satang, active) VALUES (b2, s1, 1000, true);
    RAISE NOTICE 'FAIL T1: duplicate active seat was allowed';
  EXCEPTION WHEN unique_violation THEN
    RAISE NOTICE 'PASS T1: duplicate active seat rejected';
  END;

  -- T2: an INACTIVE item on the same seat must be allowed (released seats can be rebooked)
  BEGIN
    INSERT INTO booking_items(booking_id, seat_id, price_satang, active) VALUES (b2, s1, 1000, false);
    RAISE NOTICE 'PASS T2: inactive item allowed';
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'FAIL T2: inactive item rejected (%)', SQLERRM;
  END;

  -- T3: a second PENDING payment for the same booking must be rejected
  INSERT INTO payments(id, booking_id, amount_satang, idempotency_key) VALUES (p1, b1, 1000, 'k1-' || p1);
  BEGIN
    INSERT INTO payments(booking_id, amount_satang, idempotency_key) VALUES (b1, 1000, 'k2-' || p1);
    RAISE NOTICE 'FAIL T3: two PENDING payments allowed';
  EXCEPTION WHEN unique_violation THEN
    RAISE NOTICE 'PASS T3: second PENDING payment rejected';
  END;

  -- T4: a repeated idempotency key must be rejected
  BEGIN
    INSERT INTO payments(booking_id, amount_satang, status, idempotency_key) VALUES (b2, 1000, 'FAILED', 'k1-' || p1);
    RAISE NOTICE 'FAIL T4: duplicate idempotency key allowed';
  EXCEPTION WHEN unique_violation THEN
    RAISE NOTICE 'PASS T4: duplicate idempotency key rejected';
  END;

  -- T5: a second active refund for the same payment must be rejected
  INSERT INTO refunds(payment_id, booking_id, amount_satang, reason_code, requested_by) VALUES (p1, b1, 1000, 'TEST', u);
  BEGIN
    INSERT INTO refunds(payment_id, booking_id, amount_satang, reason_code, requested_by) VALUES (p1, b1, 1000, 'TEST', u);
    RAISE NOTICE 'FAIL T5: two active refunds allowed';
  EXCEPTION WHEN unique_violation THEN
    RAISE NOTICE 'PASS T5: second active refund rejected';
  END;

  -- T6/T7: booking_events is append-only
  INSERT INTO booking_events(booking_id, user_id, showtime_id, event_type, outcome, actor_type)
    VALUES (b1, u, st, 'BOOKING_CREATED', 'SUCCESS', 'USER') RETURNING id INTO bev;
  BEGIN
    UPDATE booking_events SET reason_code = 'x' WHERE id = bev;
    RAISE NOTICE 'FAIL T6: UPDATE on booking_events allowed';
  EXCEPTION WHEN raise_exception THEN
    RAISE NOTICE 'PASS T6: UPDATE on booking_events rejected';
  END;
  BEGIN
    DELETE FROM booking_events WHERE id = bev;
    RAISE NOTICE 'FAIL T7: DELETE on booking_events allowed';
  EXCEPTION WHEN raise_exception THEN
    RAISE NOTICE 'PASS T7: DELETE on booking_events rejected';
  END;

  -- T8/T9: payment_events is append-only
  INSERT INTO payment_events(payment_id, booking_id, event_type, outcome, actor_type)
    VALUES (p1, b1, 'PAYMENT_CREATED', 'SUCCESS', 'USER') RETURNING id INTO pev;
  BEGIN
    UPDATE payment_events SET reason_code = 'x' WHERE id = pev;
    RAISE NOTICE 'FAIL T8: UPDATE on payment_events allowed';
  EXCEPTION WHEN raise_exception THEN
    RAISE NOTICE 'PASS T8: UPDATE on payment_events rejected';
  END;
  BEGIN
    DELETE FROM payment_events WHERE id = pev;
    RAISE NOTICE 'FAIL T9: DELETE on payment_events allowed';
  EXCEPTION WHEN raise_exception THEN
    RAISE NOTICE 'PASS T9: DELETE on payment_events rejected';
  END;
END $$;
ROLLBACK;
