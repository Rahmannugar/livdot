-- name: CreateAccount :one
INSERT INTO authentication_accounts (id, email, password_hash, account_type)
VALUES ($1, $2, $3, $4)
RETURNING id, email, password_hash, account_type, created_at, updated_at;

-- name: FindAccountByEmail :one
SELECT id, email, password_hash, account_type, created_at, updated_at
FROM authentication_accounts
WHERE email = $1;

-- name: FindAccountByID :one
SELECT id, email, password_hash, account_type, created_at, updated_at
FROM authentication_accounts
WHERE id = $1;

-- name: FindAccountType :one
SELECT account_type
FROM authentication_accounts
WHERE id = $1;

-- name: ReplacePasswordHash :execrows
UPDATE authentication_accounts
SET password_hash = $1,
    updated_at = $2
WHERE id = $3
  AND account_type = $4
  AND password_hash = $5;

-- name: CreateUserProfile :one
INSERT INTO users (account_id, full_name)
VALUES ($1, $2)
RETURNING account_id, full_name, updated_at;

-- name: CreateHostProfile :one
INSERT INTO hosts (account_id, full_name)
VALUES ($1, $2)
RETURNING account_id, full_name, updated_at;

-- name: CreateCrewProfile :one
INSERT INTO crews (account_id, crew_name)
VALUES ($1, $2)
RETURNING account_id, crew_name, crew_list, availability_status, updated_at;

-- name: CreateInternalAdminProfile :one
INSERT INTO internal_admins (account_id, full_name, role)
VALUES ($1, $2, $3)
RETURNING account_id, full_name, role, updated_at;

-- name: FindAccountEmail :one
SELECT email
FROM authentication_accounts
WHERE id = $1;
