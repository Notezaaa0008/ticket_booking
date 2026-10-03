CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- users
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    name          text NOT NULL,
    role          text NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- events / showtimes / seats
CREATE TABLE events (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title       text NOT NULL,
    description text NOT NULL DEFAULT '',
    venue       text NOT NULL,
    poster_url  text NOT NULL DEFAULT '',
    status      text NOT NULL DEFAULT 'published'
                CHECK (status IN ('draft','published','archived')),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE showtimes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    starts_at  timestamptz NOT NULL,
    status     text NOT NULL DEFAULT 'on_sale'
               CHECK (status IN ('on_sale','closed','cancelled')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_showtimes_event  ON showtimes(event_id);
CREATE INDEX idx_showtimes_starts ON showtimes(starts_at);

CREATE TABLE seats (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    showtime_id  uuid NOT NULL REFERENCES showtimes(id) ON DELETE CASCADE,
    row_label    text NOT NULL,
    seat_number  int  NOT NULL,
    zone         text NOT NULL DEFAULT 'standard',
    price_satang bigint NOT NULL CHECK (price_satang >= 0),
    UNIQUE (showtime_id, row_label, seat_number)
);

-- bookings / booking_items
CREATE TABLE bookings (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id),
    showtime_id  uuid NOT NULL REFERENCES showtimes(id),
    status       text NOT NULL DEFAULT 'PENDING'
                 CHECK (status IN ('PENDING','PAID','EXPIRED','CANCELLED','REFUNDED')),
    total_satang bigint NOT NULL CHECK (total_satang >= 0),
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_bookings_user ON bookings(user_id);
CREATE INDEX idx_bookings_showtime ON bookings(showtime_id);
CREATE INDEX idx_bookings_pending_expiry ON bookings(expires_at) WHERE status = 'PENDING';

CREATE TABLE booking_items (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id   uuid NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    seat_id      uuid NOT NULL REFERENCES seats(id),
    price_satang bigint NOT NULL CHECK (price_satang >= 0),
    active       boolean NOT NULL DEFAULT true
);
-- One seat can have only ONE active item: the last line of defence against double booking.
CREATE UNIQUE INDEX uq_booking_items_active_seat
    ON booking_items(seat_id) WHERE active = true;
CREATE INDEX idx_booking_items_booking ON booking_items(booking_id);

-- payments
CREATE TABLE payments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id      uuid NOT NULL REFERENCES bookings(id),
    amount_satang   bigint NOT NULL CHECK (amount_satang >= 0),
    status          text NOT NULL DEFAULT 'PENDING'
                    CHECK (status IN ('PENDING','SUCCEEDED','FAILED','NEEDS_REFUND','REFUNDED')),
    provider_ref    text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL UNIQUE,
    failure_code    text NOT NULL DEFAULT '',
    failure_message text NOT NULL DEFAULT '',
    paid_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_payments_booking ON payments(booking_id);
CREATE INDEX idx_payments_status ON payments(status);
CREATE UNIQUE INDEX uq_payments_one_pending_per_booking
    ON payments(booking_id) WHERE status = 'PENDING';

-- tickets
CREATE TABLE tickets (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_item_id uuid NOT NULL UNIQUE REFERENCES booking_items(id),
    code            text NOT NULL UNIQUE,
    status          text NOT NULL DEFAULT 'VALID'
                    CHECK (status IN ('VALID','USED','VOID')),
    issued_at       timestamptz NOT NULL DEFAULT now(),
    checked_in_at   timestamptz
);

-- booking_events (append-only log)
CREATE TABLE booking_events (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id  uuid REFERENCES bookings(id),
    user_id     uuid REFERENCES users(id),
    showtime_id uuid REFERENCES showtimes(id),
    event_type  text NOT NULL,
    outcome     text NOT NULL CHECK (outcome IN ('SUCCESS','FAILURE','IGNORED')),
    reason_code text NOT NULL DEFAULT '',
    from_status text NOT NULL DEFAULT '',
    to_status   text NOT NULL DEFAULT '',
    actor_type  text NOT NULL CHECK (actor_type IN ('USER','ADMIN','SYSTEM','GATEWAY')),
    actor_id    uuid,
    metadata    jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_booking_events_booking ON booking_events(booking_id, created_at);
CREATE INDEX idx_booking_events_user    ON booking_events(user_id, created_at);

-- payment_events (append-only log)
CREATE TABLE payment_events (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id      uuid REFERENCES payments(id),
    booking_id      uuid REFERENCES bookings(id),
    event_type      text NOT NULL,
    outcome         text NOT NULL CHECK (outcome IN ('SUCCESS','FAILURE','IGNORED')),
    reason_code     text NOT NULL DEFAULT '',
    from_status     text NOT NULL DEFAULT '',
    to_status       text NOT NULL DEFAULT '',
    amount_satang   bigint,
    provider_ref    text NOT NULL DEFAULT '',
    signature_valid boolean,
    actor_type      text NOT NULL CHECK (actor_type IN ('USER','ADMIN','SYSTEM','GATEWAY')),
    actor_id        uuid,
    raw_payload     jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_payment_events_payment ON payment_events(payment_id, created_at);
CREATE INDEX idx_payment_events_booking ON payment_events(booking_id, created_at);

-- Logs are append-only: corrections are new events, never UPDATE/DELETE.
CREATE FUNCTION forbid_event_modification() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER booking_events_append_only
    BEFORE UPDATE OR DELETE ON booking_events
    FOR EACH ROW EXECUTE FUNCTION forbid_event_modification();

CREATE TRIGGER payment_events_append_only
    BEFORE UPDATE OR DELETE ON payment_events
    FOR EACH ROW EXECUTE FUNCTION forbid_event_modification();

-- refunds
CREATE TABLE refunds (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id    uuid NOT NULL REFERENCES payments(id),
    booking_id    uuid NOT NULL REFERENCES bookings(id),
    amount_satang bigint NOT NULL CHECK (amount_satang > 0),
    status        text NOT NULL DEFAULT 'REQUESTED'
                  CHECK (status IN ('REQUESTED','COMPLETED','FAILED','REJECTED')),
    reason_code   text NOT NULL,
    note          text NOT NULL DEFAULT '',
    requested_by  uuid NOT NULL REFERENCES users(id),
    processed_by  uuid REFERENCES users(id),
    provider_ref  text NOT NULL DEFAULT '',
    failure_code  text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    completed_at  timestamptz
);
CREATE INDEX idx_refunds_payment ON refunds(payment_id);
CREATE INDEX idx_refunds_status  ON refunds(status);
CREATE UNIQUE INDEX uq_refunds_one_active_per_payment
    ON refunds(payment_id) WHERE status IN ('REQUESTED','COMPLETED');
