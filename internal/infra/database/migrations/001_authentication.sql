CREATE TYPE authentication_account_type AS ENUM ('user', 'host', 'crew', 'internal_admin');

CREATE TABLE authentication_accounts (
    id uuid PRIMARY KEY,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    account_type authentication_account_type NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT authentication_accounts_email_normalized CHECK (
        email = lower(btrim(email)) AND length(email) BETWEEN 3 AND 254
    ),
    CONSTRAINT authentication_accounts_password_hash_not_blank CHECK (
        length(btrim(password_hash)) > 0
    )
);

CREATE TABLE users (
    account_id uuid PRIMARY KEY REFERENCES authentication_accounts (id) ON DELETE CASCADE,
    full_name text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_full_name_not_blank CHECK (length(btrim(full_name)) > 0)
);

CREATE TABLE hosts (
    account_id uuid PRIMARY KEY REFERENCES authentication_accounts (id) ON DELETE CASCADE,
    full_name text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT hosts_full_name_not_blank CHECK (length(btrim(full_name)) > 0)
);

CREATE TYPE crew_availability_status AS ENUM ('available', 'unavailable');

CREATE TABLE crews (
    account_id uuid PRIMARY KEY REFERENCES authentication_accounts (id) ON DELETE CASCADE,
    crew_name text NOT NULL,
    crew_list jsonb NOT NULL DEFAULT '[]'::jsonb,
    availability_status crew_availability_status NOT NULL DEFAULT 'available',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT crews_name_not_blank CHECK (length(btrim(crew_name)) > 0),
    CONSTRAINT crews_list_array CHECK (jsonb_typeof(crew_list) = 'array')
);

CREATE TYPE internal_admin_role AS ENUM ('admin', 'subadmin');

CREATE TABLE internal_admins (
    account_id uuid PRIMARY KEY REFERENCES authentication_accounts (id) ON DELETE CASCADE,
    full_name text NOT NULL,
    role internal_admin_role NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT internal_admins_full_name_not_blank CHECK (length(btrim(full_name)) > 0)
);

CREATE TABLE authentication_sessions (
    token_hash bytea PRIMARY KEY,
    id uuid NOT NULL UNIQUE,
    subject_id uuid NOT NULL REFERENCES authentication_accounts (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    extended_at timestamptz,
    revoked_at timestamptz,
    CONSTRAINT authentication_sessions_token_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT authentication_sessions_expiry_valid CHECK (expires_at > created_at),
    CONSTRAINT authentication_sessions_extended_at_valid CHECK (
        extended_at IS NULL OR extended_at >= created_at
    ),
    CONSTRAINT authentication_sessions_revoked_at_valid CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE INDEX authentication_sessions_subject_created_idx
    ON authentication_sessions (subject_id, created_at DESC);

---- create above / drop below ----

DROP TABLE authentication_sessions;
DROP TABLE internal_admins;
DROP TYPE internal_admin_role;
DROP TABLE crews;
DROP TYPE crew_availability_status;
DROP TABLE hosts;
DROP TABLE users;
DROP TABLE authentication_accounts;
DROP TYPE authentication_account_type;
