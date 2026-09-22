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

-- name: ListUpcomingEvents :many
SELECT id, host_id, assigned_crew_id, name, amount_minor, duration_seconds,
       status, total_tickets, available_tickets, starts_at, ends_at,
       cancelled_at, created_at, updated_at
FROM events
WHERE status = 'upcoming'
ORDER BY starts_at ASC
LIMIT $1 OFFSET $2;

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
