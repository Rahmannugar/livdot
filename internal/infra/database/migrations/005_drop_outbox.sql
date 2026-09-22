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
