-- name: CreateSession :exec
INSERT INTO authentication_sessions (
    token_hash,
    id,
    subject_id,
    created_at,
    expires_at,
    extended_at,
    revoked_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: FindSessionByTokenHash :one
SELECT token_hash, id, subject_id, created_at, expires_at, extended_at, revoked_at
FROM authentication_sessions
WHERE token_hash = $1;

-- name: ListSessionsBySubject :many
SELECT token_hash, id, subject_id, created_at, expires_at, extended_at, revoked_at
FROM authentication_sessions
WHERE subject_id = $1
ORDER BY created_at DESC;

-- name: ExtendSession :one
UPDATE authentication_sessions
SET extended_at = $2,
    expires_at = $3
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > $2
  AND expires_at < $3
RETURNING token_hash, id, subject_id, created_at, expires_at, extended_at, revoked_at;

-- name: RevokeSession :execrows
UPDATE authentication_sessions
SET revoked_at = COALESCE(revoked_at, $2)
WHERE token_hash = $1;

-- name: RevokeSessionsBySubject :many
UPDATE authentication_sessions
SET revoked_at = COALESCE(revoked_at, $2)
WHERE subject_id = $1
RETURNING token_hash, id, subject_id, created_at, expires_at, extended_at, revoked_at;
