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

-- name: GetEventForFinance :one
SELECT id, host_id, status, amount_minor, duration_seconds, total_tickets,
       available_tickets, starts_at, ends_at
FROM events
WHERE id = $1;

-- name: ListPaidPurchasesForEvent :many
SELECT id, event_id, user_id, amount_minor, provider, provider_payment_id
FROM event_purchases
WHERE event_id = $1
  AND status = 'paid'
ORDER BY id;

-- name: MarkPurchaseRefunded :one
UPDATE event_purchases
SET status = 'refunded',
    refunded_at = COALESCE(refunded_at, now()),
    updated_at = now()
WHERE id = $1
  AND status = 'paid'
RETURNING id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
          idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
          last_error, created_at, updated_at, paid_at, refunded_at;

-- name: SumPaidPurchases :one
SELECT COALESCE(SUM(amount_minor), 0)::bigint AS amount_minor
FROM event_purchases
WHERE event_id = $1 AND status = 'paid';

-- name: SumRefundedPurchases :one
SELECT COALESCE(SUM(amount_minor), 0)::bigint AS amount_minor
FROM event_refunds
WHERE event_id = $1 AND status = 'refunded';

-- name: GetRefundByID :one
SELECT id, event_id, user_id, purchase_id, amount_minor, status, provider,
       provider_refund_id, linked_refund_id, idempotency_key, attempt_count,
       next_attempt_at, locked_at, last_error, processed_at, retried_at, refunded_at
FROM event_refunds
WHERE id = $1;

-- name: ListRefunds :many
SELECT id, event_id, user_id, purchase_id, amount_minor, status, provider,
       provider_refund_id, linked_refund_id, idempotency_key, attempt_count,
       next_attempt_at, locked_at, last_error, processed_at, retried_at, refunded_at
FROM event_refunds
WHERE (sqlc.narg('event_id')::uuid IS NULL OR event_id = sqlc.narg('event_id')::uuid)
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor')::uuid)
ORDER BY id ASC
LIMIT sqlc.arg('page_size');

-- name: GetPayoutByID :one
SELECT id, event_id, host_id, amount_minor, status, provider, provider_payout_id,
       linked_payout_id, idempotency_key, attempt_count, next_attempt_at, locked_at,
       last_error, processed_at, retried_at, paid_at
FROM event_payouts
WHERE id = $1;

-- name: ListPayouts :many
SELECT id, event_id, host_id, amount_minor, status, provider, provider_payout_id,
       linked_payout_id, idempotency_key, attempt_count, next_attempt_at, locked_at,
       last_error, processed_at, retried_at, paid_at
FROM event_payouts
WHERE (sqlc.narg('event_id')::uuid IS NULL OR event_id = sqlc.narg('event_id')::uuid)
  AND (sqlc.narg('host_id')::uuid IS NULL OR host_id = sqlc.narg('host_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor')::uuid)
ORDER BY id ASC
LIMIT sqlc.arg('page_size');

-- name: CreateLedgerEntry :one
INSERT INTO ledger_entries (id, event_id, entry_type, amount_minor, purchase_id, refund_id, payout_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT DO NOTHING
RETURNING id, event_id, entry_type, amount_minor, purchase_id, refund_id, payout_id, created_at;

-- name: GetEventStreamStatus :one
SELECT status
FROM event_streams
WHERE event_id = $1;
