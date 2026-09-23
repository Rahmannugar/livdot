-- name: CreateEmailNotification :one
INSERT INTO email_notifications (
    id, notification_type, recipient_account_id, recipient_email,
    template_key, payload, idempotency_key
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id, notification_type, recipient_account_id, recipient_email,
          template_key, payload, idempotency_key, status, attempt_count,
          next_attempt_at, locked_at, delivered_at, last_error, created_at, updated_at;

-- name: ClaimPendingEmailNotifications :many
WITH claim AS (
    SELECT id
    FROM email_notifications
    WHERE status IN ('queued', 'processing')
      AND next_attempt_at <= now()
      AND (locked_at IS NULL OR locked_at < now() - interval '10 minutes')
    ORDER BY next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE email_notifications AS notification
SET status = 'processing',
    locked_at = now(),
    attempt_count = attempt_count + 1,
    updated_at = now()
FROM claim
WHERE notification.id = claim.id
RETURNING notification.id, notification.notification_type,
          notification.recipient_account_id, notification.recipient_email,
          notification.template_key, notification.payload,
          notification.idempotency_key, notification.status,
          notification.attempt_count, notification.next_attempt_at,
          notification.locked_at, notification.delivered_at,
          notification.last_error, notification.created_at, notification.updated_at;

-- name: MarkEmailNotificationDelivered :one
UPDATE email_notifications
SET status = 'delivered', delivered_at = COALESCE(delivered_at, now()),
    locked_at = NULL, last_error = NULL, updated_at = now()
WHERE id = $1 AND status = 'processing'
RETURNING id, notification_type, recipient_account_id, recipient_email,
          template_key, payload, idempotency_key, status, attempt_count,
          next_attempt_at, locked_at, delivered_at, last_error, created_at, updated_at;

-- name: MarkEmailNotificationFailed :one
UPDATE email_notifications
SET status = 'failed', locked_at = NULL, last_error = $2,
    next_attempt_at = $3, updated_at = now()
WHERE id = $1 AND status = 'processing'
RETURNING id, notification_type, recipient_account_id, recipient_email,
          template_key, payload, idempotency_key, status, attempt_count,
          next_attempt_at, locked_at, delivered_at, last_error, created_at, updated_at;
