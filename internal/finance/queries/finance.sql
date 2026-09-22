-- name: CreateEventRefund :one
INSERT INTO event_refunds (
    id, event_id, user_id, purchase_id, amount_minor, provider, idempotency_key
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (purchase_id) DO NOTHING
RETURNING id, event_id, user_id, purchase_id, amount_minor, status, provider,
          provider_refund_id, linked_refund_id, idempotency_key, attempt_count,
          next_attempt_at, locked_at, last_error, processed_at, retried_at, refunded_at;

-- name: CreateEventPayout :one
INSERT INTO event_payouts (
    id, event_id, host_id, amount_minor, provider, idempotency_key
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (event_id) DO NOTHING
RETURNING id, event_id, host_id, amount_minor, status, provider,
          provider_payout_id, linked_payout_id, idempotency_key, attempt_count,
          next_attempt_at, locked_at, last_error, processed_at, retried_at, paid_at;

-- name: MarkRefunded :one
UPDATE event_refunds
SET status = 'refunded',
    provider_refund_id = $2,
    attempt_count = attempt_count + 1,
    processed_at = COALESCE(processed_at, now()),
    refunded_at = COALESCE(refunded_at, now())
WHERE id = $1
  AND status = 'processing'
RETURNING id, event_id, user_id, purchase_id, amount_minor, status, provider,
          provider_refund_id, linked_refund_id, idempotency_key, attempt_count,
          next_attempt_at, locked_at, last_error, processed_at, retried_at, refunded_at;

-- name: MarkPayoutPaid :one
UPDATE event_payouts
SET status = 'paid',
    provider_payout_id = $2,
    attempt_count = attempt_count + 1,
    processed_at = COALESCE(processed_at, now()),
    paid_at = COALESCE(paid_at, now())
WHERE id = $1
  AND status = 'processing'
RETURNING id, event_id, host_id, amount_minor, status, provider,
          provider_payout_id, linked_payout_id, idempotency_key, attempt_count,
          next_attempt_at, locked_at, last_error, processed_at, retried_at, paid_at;

-- name: ClaimPendingRefunds :many
WITH claim AS (
    SELECT id
    FROM event_refunds
    WHERE status = 'processing'
      AND next_attempt_at <= now()
      AND (locked_at IS NULL OR locked_at < now() - interval '10 minutes')
    ORDER BY next_attempt_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE event_refunds AS refund
SET locked_at = now(),
    attempt_count = attempt_count + 1
FROM claim
WHERE refund.id = claim.id
RETURNING refund.id, refund.event_id, refund.user_id, refund.purchase_id,
          refund.amount_minor, refund.status, refund.provider,
          refund.provider_refund_id, refund.linked_refund_id, refund.idempotency_key,
          refund.attempt_count, refund.next_attempt_at, refund.locked_at,
          refund.last_error, refund.processed_at, refund.retried_at, refund.refunded_at;

-- name: ClaimPendingPayouts :many
WITH claim AS (
    SELECT id
    FROM event_payouts
    WHERE status = 'processing'
      AND next_attempt_at <= now()
      AND (locked_at IS NULL OR locked_at < now() - interval '10 minutes')
    ORDER BY next_attempt_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE event_payouts AS payout
SET locked_at = now(),
    attempt_count = attempt_count + 1
FROM claim
WHERE payout.id = claim.id
RETURNING payout.id, payout.event_id, payout.host_id, payout.amount_minor,
          payout.status, payout.provider, payout.provider_payout_id,
          payout.linked_payout_id, payout.idempotency_key, payout.attempt_count,
          payout.next_attempt_at, payout.locked_at, payout.last_error,
          payout.processed_at, payout.retried_at, payout.paid_at;
