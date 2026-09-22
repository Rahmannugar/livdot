-- name: InsertWebhookEvent :one
INSERT INTO webhook_events (
    id, provider, provider_event_id, event_type, payload_hash, payload
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (provider, provider_event_id) DO NOTHING
RETURNING id, provider, provider_event_id, event_type, payload_hash, payload,
          attempt_count, next_attempt_at, locked_at, last_error, received_at,
          processed_at, failed_at;

-- name: MarkWebhookProcessed :one
UPDATE webhook_events
SET processed_at = now(),
    locked_at = NULL,
    last_error = NULL
WHERE id = $1
  AND processed_at IS NULL
RETURNING id, provider, provider_event_id, event_type, payload_hash, payload,
          attempt_count, next_attempt_at, locked_at, last_error, received_at,
          processed_at, failed_at;

-- name: MarkWebhookFailed :one
UPDATE webhook_events
SET failed_at = now(),
    locked_at = NULL,
    last_error = $2
WHERE id = $1
  AND processed_at IS NULL
  AND failed_at IS NULL
RETURNING id, provider, provider_event_id, event_type, payload_hash, payload,
          attempt_count, next_attempt_at, locked_at, last_error, received_at,
          processed_at, failed_at;
