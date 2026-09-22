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
    CONSTRAINT outbox_events_aggregate_type_not_blank CHECK (length(btrim(aggregate_type)) > 0),
    CONSTRAINT outbox_events_event_type_not_blank CHECK (length(btrim(event_type)) > 0),
    CONSTRAINT outbox_events_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT outbox_events_idempotency_not_blank CHECK (length(btrim(idempotency_key)) > 0),
    CONSTRAINT outbox_events_attempt_count_non_negative CHECK (attempt_count >= 0),
    CONSTRAINT outbox_events_last_error_not_blank CHECK (
        last_error IS NULL OR length(btrim(last_error)) > 0
    ),
    CONSTRAINT outbox_events_idempotency_unique UNIQUE (idempotency_key),
    CONSTRAINT outbox_events_published_at_consistent CHECK (
        (status = 'published' AND published_at IS NOT NULL)
        OR (status <> 'published' AND published_at IS NULL)
    )
);

CREATE INDEX outbox_events_ready_idx
    ON outbox_events (next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing');

---- create above / drop below ----

DROP INDEX outbox_events_ready_idx;
DROP TABLE outbox_events;
DROP TYPE outbox_status;
