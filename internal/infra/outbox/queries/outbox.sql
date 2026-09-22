-- name: EnqueueOutboxEvent :one
INSERT INTO outbox_events (
    id, aggregate_type, aggregate_id, event_type, payload, idempotency_key
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id, aggregate_type, aggregate_id, event_type, payload, idempotency_key,
          status, attempt_count, next_attempt_at, locked_at, last_error,
          created_at, published_at;

-- name: ClaimPendingOutboxEvents :many
WITH claim AS (
    SELECT id
    FROM outbox_events
    WHERE status IN ('pending', 'processing')
      AND next_attempt_at <= now()
      AND (locked_at IS NULL OR locked_at < now() - interval '10 minutes')
    ORDER BY next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE outbox_events AS event
SET status = 'processing',
    locked_at = now(),
    attempt_count = attempt_count + 1
FROM claim
WHERE event.id = claim.id
RETURNING event.id, event.aggregate_type, event.aggregate_id, event.event_type,
          event.payload, event.idempotency_key, event.status, event.attempt_count,
          event.next_attempt_at, event.locked_at, event.last_error,
          event.created_at, event.published_at;

-- name: MarkOutboxEventPublished :one
UPDATE outbox_events
SET status = 'published',
    published_at = COALESCE(published_at, now()),
    locked_at = NULL,
    last_error = NULL
WHERE id = $1
  AND published_at IS NULL
RETURNING id, aggregate_type, aggregate_id, event_type, payload, idempotency_key,
          status, attempt_count, next_attempt_at, locked_at, last_error,
          created_at, published_at;

-- name: MarkOutboxEventFailed :one
UPDATE outbox_events
SET status = 'failed',
    locked_at = NULL,
    last_error = $2,
    next_attempt_at = $3
WHERE id = $1
  AND published_at IS NULL
RETURNING id, aggregate_type, aggregate_id, event_type, payload, idempotency_key,
          status, attempt_count, next_attempt_at, locked_at, last_error,
          created_at, published_at;
