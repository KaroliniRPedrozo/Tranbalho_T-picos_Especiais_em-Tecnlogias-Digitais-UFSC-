CREATE TABLE IF NOT EXISTS events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    venue       TEXT        NOT NULL,
    city        TEXT        NOT NULL,
    starts_at   TIMESTAMPTZ NOT NULL,
    capacity    INTEGER     NOT NULL CHECK (capacity > 0),
    price_cents BIGINT      NOT NULL CHECK (price_cents >= 0),
    status      TEXT        NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft', 'published', 'cancelled')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_events_status_starts_at ON events (status, starts_at);
CREATE INDEX IF NOT EXISTS idx_events_city ON events (lower(city));
