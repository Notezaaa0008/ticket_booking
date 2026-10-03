-- Dev seed data. Safe to run more than once (ON CONFLICT DO NOTHING).
-- Run:  docker compose exec postgres psql -U ticket -d ticket_booking -v ON_ERROR_STOP=1 -f /seed/seed.sql
-- Accounts (DEV ONLY):  admin@example.com / Admin1234!    user@example.com / User1234!
-- Passwords are bcrypt hashes made by pgcrypto (compatible with Go's bcrypt).

INSERT INTO users (id, email, password_hash, name, role) VALUES
  ('aaaaaaaa-0000-0000-0000-000000000001', 'admin@example.com', crypt('Admin1234!', gen_salt('bf', 10)), 'Admin', 'admin'),
  ('aaaaaaaa-0000-0000-0000-000000000002', 'user@example.com',  crypt('User1234!',  gen_salt('bf', 10)), 'Test User', 'user')
ON CONFLICT (email) DO NOTHING;

INSERT INTO events (id, title, description, venue, poster_url, status) VALUES
  ('eeeeeeee-0000-0000-0000-000000000001', 'Bangkok Jazz Night',  'An evening of live jazz.',         'Lumpini Hall',      '', 'published'),
  ('eeeeeeee-0000-0000-0000-000000000002', 'Rock the Riverside',  'Open-air rock concert by the river.', 'Riverside Arena', '', 'published'),
  ('eeeeeeee-0000-0000-0000-000000000003', 'Stand-up Comedy Live','Three comedians, one stage.',        'Siam Theatre',      '', 'published')
ON CONFLICT (id) DO NOTHING;

INSERT INTO showtimes (id, event_id, starts_at, status) VALUES
  ('55555555-0000-0000-0000-000000000001', 'eeeeeeee-0000-0000-0000-000000000001', date_trunc('day', now()) + interval '7 days'  + interval '19 hours', 'on_sale'),
  ('55555555-0000-0000-0000-000000000002', 'eeeeeeee-0000-0000-0000-000000000001', date_trunc('day', now()) + interval '8 days'  + interval '19 hours', 'on_sale'),
  ('55555555-0000-0000-0000-000000000003', 'eeeeeeee-0000-0000-0000-000000000002', date_trunc('day', now()) + interval '10 days' + interval '18 hours', 'on_sale'),
  ('55555555-0000-0000-0000-000000000004', 'eeeeeeee-0000-0000-0000-000000000002', date_trunc('day', now()) + interval '11 days' + interval '18 hours', 'on_sale'),
  ('55555555-0000-0000-0000-000000000005', 'eeeeeeee-0000-0000-0000-000000000003', date_trunc('day', now()) + interval '5 days'  + interval '20 hours', 'on_sale'),
  ('55555555-0000-0000-0000-000000000006', 'eeeeeeee-0000-0000-0000-000000000003', date_trunc('day', now()) + interval '6 days'  + interval '20 hours', 'on_sale')
ON CONFLICT (id) DO NOTHING;

-- 50 seats per showtime: rows A-E x seats 1-10
INSERT INTO seats (showtime_id, row_label, seat_number, zone, price_satang)
SELECT st.id, r.row_label, n, r.zone, r.price
FROM showtimes st
CROSS JOIN (VALUES ('A','front',150000), ('B','front',120000), ('C','middle',100000), ('D','middle',80000), ('E','back',50000))
       AS r(row_label, zone, price)
CROSS JOIN generate_series(1, 10) AS n
WHERE st.id::text LIKE '55555555-0000-0000-0000-00000000000%'
ON CONFLICT (showtime_id, row_label, seat_number) DO NOTHING;
