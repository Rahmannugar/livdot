ALTER TABLE tickets ADD COLUMN notified_at timestamptz;
ALTER TABLE event_refunds ADD COLUMN notified_at timestamptz;
ALTER TABLE events ADD COLUMN crew_notified_at timestamptz;

DROP INDEX outbox_events_ready_idx;
DROP TABLE outbox_events;
DROP TYPE outbox_status;

---- create above / drop below ----

CREATE TYPE outbox_status AS ENUM ('pending', 'processing', 'published', 'failed');

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    idempotency_key text NOT NULL,
    status outbox_status NOT NULL DEFAULT 'pending',
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    CONSTRAINT outbox_events_idempotency_unique UNIQUE (idempotency_key)
);

CREATE INDEX outbox_events_ready_idx
    ON outbox_events (next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing');

ALTER TABLE events DROP COLUMN crew_notified_at;
ALTER TABLE event_refunds DROP COLUMN notified_at;
ALTER TABLE tickets DROP COLUMN notified_at;
