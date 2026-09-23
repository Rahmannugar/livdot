-- name: CreatePurchase :one
INSERT INTO event_purchases (
    id, event_id, user_id, amount_minor, status, provider, idempotency_key, checkout_url
)
VALUES ($1, $2, $3, $4, 'initiated', $5, $6, $7)
ON CONFLICT (event_id, user_id) DO NOTHING
RETURNING id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
          idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
          last_error, created_at, updated_at, paid_at, refunded_at;

-- name: GetPurchaseByID :one
SELECT id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
       idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
       last_error, created_at, updated_at, paid_at, refunded_at
FROM event_purchases
WHERE id = $1;

-- name: GetPurchaseByIdempotencyKey :one
SELECT id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
       idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
       last_error, created_at, updated_at, paid_at, refunded_at
FROM event_purchases
WHERE user_id = $1 AND idempotency_key = $2;

-- name: MarkPurchaseProcessing :one
UPDATE event_purchases
SET status = CASE
        WHEN status = 'initiated' THEN 'processing'::purchase_status
        ELSE status
    END,
    updated_at = now()
WHERE id = $1
RETURNING id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
          idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
          last_error, created_at, updated_at, paid_at, refunded_at;

-- name: MarkPurchasePaid :one
UPDATE event_purchases
SET status = 'paid',
    provider_payment_id = $2,
    paid_at = COALESCE(paid_at, now()),
    updated_at = now()
WHERE id = $1
  AND status IN ('initiated', 'processing')
RETURNING id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
          idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
          last_error, created_at, updated_at, paid_at, refunded_at;

-- name: MarkPurchaseFailed :one
UPDATE event_purchases
SET status = 'failed',
    updated_at = now()
WHERE id = $1
  AND status IN ('initiated', 'processing')
RETURNING id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
          idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
          last_error, created_at, updated_at, paid_at, refunded_at;

-- name: ClaimPendingPurchases :many
WITH claim AS (
    SELECT id
    FROM event_purchases
    WHERE status IN ('initiated', 'processing')
      AND next_attempt_at <= now()
      AND (locked_at IS NULL OR locked_at < now() - interval '10 minutes')
    ORDER BY next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE event_purchases AS purchase
SET locked_at = now(),
    attempt_count = attempt_count + 1,
    updated_at = now()
FROM claim
WHERE purchase.id = claim.id
RETURNING purchase.id, purchase.event_id, purchase.user_id, purchase.amount_minor,
          purchase.status, purchase.provider, purchase.provider_payment_id,
          purchase.idempotency_key, purchase.checkout_url, purchase.attempt_count,
          purchase.next_attempt_at, purchase.locked_at, purchase.last_error,
          purchase.created_at, purchase.updated_at, purchase.paid_at, purchase.refunded_at;

-- name: CreateTemporaryTicket :one
INSERT INTO tickets (
    id, event_id, user_id, purchase_id, reserved_at, reservation_expires_at
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, event_id, user_id, purchase_id, status, reserved_at,
          reservation_expires_at, issued_at, revoked_at;

-- name: ExpireTicketReservation :one
UPDATE tickets
SET status = 'reservation_expired',
    reservation_expires_at = LEAST(reservation_expires_at, now())
WHERE id = $1
  AND status = 'temporarily_reserved'
  AND reservation_expires_at <= now()
RETURNING id, event_id, user_id, purchase_id, status, reserved_at,
          reservation_expires_at, issued_at, revoked_at;

-- name: IssueTicket :one
UPDATE tickets
SET status = 'issued',
    issued_at = COALESCE(issued_at, now())
WHERE id = $1
  AND status = 'temporarily_reserved'
  AND reservation_expires_at > now()
RETURNING id, event_id, user_id, purchase_id, status, reserved_at,
          reservation_expires_at, issued_at, revoked_at;

-- name: CreateEventMember :one
INSERT INTO event_members (id, event_id, user_id, ticket_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (event_id, user_id) DO NOTHING
RETURNING id, event_id, user_id, ticket_id, status, created_at, revoked_at;

-- name: GetEventMemberByUser :one
SELECT id, event_id, user_id, ticket_id, status, created_at, revoked_at
FROM event_members
WHERE event_id = $1 AND user_id = $2;

-- name: GetEventForReservation :one
SELECT id, host_id, status, amount_minor, total_tickets, available_tickets, starts_at, ends_at
FROM events
WHERE id = $1;

-- name: ReserveEventTicket :one
UPDATE events
SET available_tickets = available_tickets - 1,
    updated_at = now()
WHERE id = $1
  AND status = 'upcoming'
  AND available_tickets > 0
RETURNING id, host_id, status, amount_minor, total_tickets, available_tickets, starts_at, ends_at;

-- name: ReleaseEventTicket :one
UPDATE events
SET available_tickets = available_tickets + 1,
    updated_at = now()
WHERE id = $1
  AND available_tickets < total_tickets
RETURNING id, host_id, status, amount_minor, total_tickets, available_tickets, starts_at, ends_at;

-- name: GetPurchaseByEventAndUser :one
SELECT id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
       idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
       last_error, created_at, updated_at, paid_at, refunded_at
FROM event_purchases
WHERE event_id = $1 AND user_id = $2;

-- name: UpdatePurchaseCheckout :one
UPDATE event_purchases
SET checkout_url = $2,
    updated_at = now()
WHERE id = $1
RETURNING id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
          idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
          last_error, created_at, updated_at, paid_at, refunded_at;

-- name: GetTicketByID :one
SELECT id, event_id, user_id, purchase_id, status, reserved_at,
       reservation_expires_at, issued_at, revoked_at
FROM tickets
WHERE id = $1;

-- name: GetTicketByPurchase :one
SELECT id, event_id, user_id, purchase_id, status, reserved_at,
       reservation_expires_at, issued_at, revoked_at
FROM tickets
WHERE purchase_id = $1;

-- name: IssueTicketByPurchase :one
UPDATE tickets
SET status = 'issued',
    issued_at = COALESCE(issued_at, now())
WHERE purchase_id = $1
  AND status = 'temporarily_reserved'
  AND reservation_expires_at > now()
RETURNING id, event_id, user_id, purchase_id, status, reserved_at,
          reservation_expires_at, issued_at, revoked_at;

-- name: ExpireTicketByPurchase :one
UPDATE tickets
SET status = 'reservation_expired',
    reservation_expires_at = LEAST(reservation_expires_at, now())
WHERE purchase_id = $1
  AND status = 'temporarily_reserved'
RETURNING id, event_id, user_id, purchase_id, status, reserved_at,
          reservation_expires_at, issued_at, revoked_at;

-- name: ClaimExpiredTicketReservations :many
WITH claim AS (
    SELECT id
    FROM tickets
    WHERE status = 'temporarily_reserved'
      AND reservation_expires_at <= now()
    ORDER BY reservation_expires_at
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE tickets AS ticket
SET status = 'reservation_expired'
FROM claim
WHERE ticket.id = claim.id
RETURNING ticket.id, ticket.event_id, ticket.user_id, ticket.purchase_id,
          ticket.status, ticket.reserved_at, ticket.reservation_expires_at,
          ticket.issued_at, ticket.revoked_at;

-- name: ResetPurchaseForRetry :one
UPDATE event_purchases
SET idempotency_key = $2,
    status = 'initiated',
    checkout_url = NULL,
    provider_payment_id = NULL,
    paid_at = NULL,
    refunded_at = NULL,
    attempt_count = 0,
    next_attempt_at = now(),
    locked_at = NULL,
    last_error = NULL,
    updated_at = now()
WHERE id = $1
  AND status IN ('failed', 'refunded')
RETURNING id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
          idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
          last_error, created_at, updated_at, paid_at, refunded_at;

-- name: ResetTicketForRetry :one
UPDATE tickets
SET status = 'temporarily_reserved',
    reserved_at = $2,
    reservation_expires_at = $3,
    issued_at = NULL,
    revoked_at = NULL
WHERE id = $1
RETURNING id, event_id, user_id, purchase_id, status, reserved_at,
          reservation_expires_at, issued_at, revoked_at;

-- name: MarkPurchaseFailedForTicket :one
UPDATE event_purchases
SET status = 'failed',
    updated_at = now()
WHERE id = $1
  AND status IN ('initiated', 'processing')
RETURNING id, event_id, user_id, amount_minor, status, provider, provider_payment_id,
          idempotency_key, checkout_url, attempt_count, next_attempt_at, locked_at,
          last_error, created_at, updated_at, paid_at, refunded_at;

-- name: ListActiveMemberships :many
SELECT event_id
FROM event_members
WHERE user_id = $1
  AND status = 'active'
  AND event_id = ANY(sqlc.arg('event_ids')::uuid[]);
