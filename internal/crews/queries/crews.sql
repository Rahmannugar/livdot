-- name: GetCrewProfile :one
SELECT account_id, crew_name, crew_list, availability_status, updated_at
FROM crews
WHERE account_id = $1;

-- name: UpdateCrewProfile :one
UPDATE crews
SET crew_name = $2,
    crew_list = $3,
    availability_status = $4,
    updated_at = now()
WHERE account_id = $1
RETURNING account_id, crew_name, crew_list, availability_status, updated_at;

-- name: ListCrews :many
SELECT account_id, crew_name, crew_list, availability_status, updated_at
FROM crews
WHERE (sqlc.narg('name')::text IS NULL OR crew_name ILIKE '%' || sqlc.narg('name')::text || '%')
  AND (
      sqlc.narg('availability')::crew_availability_status IS NULL
      OR availability_status = sqlc.narg('availability')::crew_availability_status
  )
  AND (
      sqlc.narg('cursor_crew_name')::text IS NULL
      OR (crew_name, account_id) > (
          sqlc.narg('cursor_crew_name')::text,
          sqlc.narg('cursor_id')::uuid
      )
  )
ORDER BY crew_name ASC, account_id ASC
LIMIT sqlc.arg('page_size');
