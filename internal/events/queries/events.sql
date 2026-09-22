-- name: CreateEvent :one
INSERT INTO events (
    id,
    host_id,
    assigned_crew_id,
    name,
    amount_minor,
    duration_seconds,
    total_tickets,
    available_tickets,
    starts_at,
    ends_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8, $9)
RETURNING id, host_id, assigned_crew_id, name, amount_minor, duration_seconds,
          status, total_tickets, available_tickets, starts_at, ends_at,
          cancelled_at, created_at, updated_at;

-- name: GetEvent :one
SELECT id, host_id, assigned_crew_id, name, amount_minor, duration_seconds,
       status, total_tickets, available_tickets, starts_at, ends_at,
       cancelled_at, created_at, updated_at
FROM events
WHERE id = $1;

-- name: GetEventDetail :one
SELECT e.id, e.host_id, e.assigned_crew_id, e.name, e.amount_minor,
       e.duration_seconds, e.status, e.total_tickets, e.available_tickets,
       e.starts_at, e.ends_at, e.cancelled_at, e.created_at, e.updated_at,
       c.crew_name, c.availability_status
FROM events AS e
LEFT JOIN crews AS c ON c.account_id = e.assigned_crew_id
WHERE e.id = $1;

-- name: ListEvents :many
SELECT id, host_id, assigned_crew_id, name, amount_minor, duration_seconds,
       status, total_tickets, available_tickets, starts_at, ends_at,
       cancelled_at, created_at, updated_at
FROM events
WHERE (sqlc.narg('name')::text IS NULL OR name ILIKE '%' || sqlc.narg('name')::text || '%')
  AND (sqlc.narg('status')::event_status IS NULL OR status = sqlc.narg('status')::event_status)
  AND (
      sqlc.narg('duration_gte')::integer IS NULL
      OR duration_seconds >= sqlc.narg('duration_gte')::integer
  )
  AND (
      sqlc.narg('duration_lte')::integer IS NULL
      OR duration_seconds <= sqlc.narg('duration_lte')::integer
  )
  AND (
      sqlc.narg('amount_gte')::bigint IS NULL
      OR amount_minor >= sqlc.narg('amount_gte')::bigint
  )
  AND (
      sqlc.narg('amount_lte')::bigint IS NULL
      OR amount_minor <= sqlc.narg('amount_lte')::bigint
  )
  AND (
      sqlc.narg('cursor_starts_at')::timestamptz IS NULL
      OR (starts_at, id) > (
          sqlc.narg('cursor_starts_at')::timestamptz,
          sqlc.narg('cursor_id')::uuid
      )
  )
ORDER BY starts_at ASC, id ASC
LIMIT sqlc.arg('page_size');

-- name: UpdateEvent :one
UPDATE events
SET name = $2,
    amount_minor = $3,
    duration_seconds = $4,
    total_tickets = $5,
    available_tickets = $5 - (total_tickets - available_tickets),
    assigned_crew_id = $6,
    starts_at = $7,
    ends_at = $8,
    updated_at = now()
WHERE id = $1
  AND status = 'upcoming'
  AND $5 >= total_tickets - available_tickets
RETURNING id, host_id, assigned_crew_id, name, amount_minor, duration_seconds,
          status, total_tickets, available_tickets, starts_at, ends_at,
          cancelled_at, created_at, updated_at;

-- name: CancelEvent :one
UPDATE events
SET status = 'cancelled',
    cancelled_at = now(),
    updated_at = now()
WHERE id = $1
  AND status = 'upcoming'
RETURNING id, host_id, assigned_crew_id, name, amount_minor, duration_seconds,
          status, total_tickets, available_tickets, starts_at, ends_at,
          cancelled_at, created_at, updated_at;

-- name: ReserveEventTicket :one
UPDATE events
SET available_tickets = available_tickets - 1,
    updated_at = now()
WHERE id = $1
  AND status = 'upcoming'
  AND available_tickets > 0
RETURNING id, host_id, assigned_crew_id, name, amount_minor, duration_seconds,
          status, total_tickets, available_tickets, starts_at, ends_at,
          cancelled_at, created_at, updated_at;

-- name: UpdateEventStatus :one
UPDATE events
SET status = $2,
    cancelled_at = CASE WHEN $2 = 'cancelled' THEN now() ELSE NULL END,
    updated_at = now()
WHERE id = $1
RETURNING id, host_id, assigned_crew_id, name, amount_minor, duration_seconds,
          status, total_tickets, available_tickets, starts_at, ends_at,
          cancelled_at, created_at, updated_at;
