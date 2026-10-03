-- Runs only on the FIRST start of an empty volume.
-- If the volume already exists, run manually:
--   docker compose exec postgres psql -U ticket -d postgres -c "CREATE DATABASE ticket_booking_test OWNER ticket;"
CREATE DATABASE ticket_booking_test OWNER ticket;
