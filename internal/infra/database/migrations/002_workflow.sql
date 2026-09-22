CREATE TYPE event_status AS ENUM ('upcoming', 'live', 'ended', 'cancelled');
CREATE TYPE ticket_status AS ENUM ('temporarily_reserved', 'reservation_expired', 'issued', 'revoked');
CREATE TYPE purchase_status AS ENUM ('initiated', 'processing', 'paid', 'refunded', 'failed');
CREATE TYPE stream_status AS ENUM ('live', 'failed', 'ended');
CREATE TYPE membership_status AS ENUM ('active', 'revoked');
CREATE TYPE refund_status AS ENUM ('processing', 'refunded', 'failed');
CREATE TYPE payout_status AS ENUM ('processing', 'paid', 'failed');
CREATE TYPE ledger_entry_type AS ENUM ('charge', 'refund', 'payout');
CREATE TYPE email_notification_status AS ENUM ('queued', 'processing', 'delivered', 'failed');

CREATE TABLE provider_accounts (
    id uuid PRIMARY KEY,
    owner_account_id uuid NOT NULL REFERENCES authentication_accounts (id) ON DELETE CASCADE,
    provider text NOT NULL,
    provider_account_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT provider_accounts_provider_not_blank CHECK (length(btrim(provider)) > 0),
    CONSTRAINT provider_accounts_provider_account_id_not_blank CHECK (length(btrim(provider_account_id)) > 0),
    CONSTRAINT provider_accounts_owner_provider_unique UNIQUE (owner_account_id, provider),
    CONSTRAINT provider_accounts_provider_id_unique UNIQUE (provider, provider_account_id)
);

CREATE TABLE events (
    id uuid PRIMARY KEY,
    host_id uuid NOT NULL REFERENCES hosts (account_id) ON DELETE RESTRICT,
    assigned_crew_id uuid REFERENCES crews (account_id) ON DELETE SET NULL,
    name text NOT NULL,
    amount_minor bigint NOT NULL,
    duration_seconds integer NOT NULL,
    status event_status NOT NULL DEFAULT 'upcoming',
    total_tickets integer NOT NULL,
    available_tickets integer NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    cancelled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT events_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT events_amount_non_negative CHECK (amount_minor >= 0),
    CONSTRAINT events_duration_positive CHECK (duration_seconds > 0),
    CONSTRAINT events_total_tickets_positive CHECK (total_tickets > 0),
    CONSTRAINT events_available_tickets_valid CHECK (available_tickets BETWEEN 0 AND total_tickets),
    CONSTRAINT events_schedule_valid CHECK (ends_at > starts_at),
    CONSTRAINT events_cancelled_at_consistent CHECK (
        (status = 'cancelled' AND cancelled_at IS NOT NULL)
        OR (status <> 'cancelled' AND cancelled_at IS NULL)
    )
);

CREATE INDEX events_status_starts_idx ON events (status, starts_at);
CREATE INDEX events_host_idx ON events (host_id, starts_at DESC);
CREATE INDEX events_crew_idx ON events (assigned_crew_id, starts_at DESC);

CREATE TABLE event_purchases (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES events (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES users (account_id) ON DELETE RESTRICT,
    amount_minor bigint NOT NULL,
    status purchase_status NOT NULL DEFAULT 'initiated',
    provider text NOT NULL,
    provider_payment_id text,
    idempotency_key text NOT NULL,
    checkout_url text,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    paid_at timestamptz,
    refunded_at timestamptz,
    CONSTRAINT event_purchases_amount_non_negative CHECK (amount_minor >= 0),
    CONSTRAINT event_purchases_provider_not_blank CHECK (length(btrim(provider)) > 0),
    CONSTRAINT event_purchases_idempotency_not_blank CHECK (length(btrim(idempotency_key)) > 0),
    CONSTRAINT event_purchases_attempt_count_non_negative CHECK (attempt_count >= 0),
    CONSTRAINT event_purchases_provider_payment_not_blank CHECK (
        provider_payment_id IS NULL OR length(btrim(provider_payment_id)) > 0
    ),
    CONSTRAINT event_purchases_checkout_url_not_blank CHECK (
        checkout_url IS NULL OR length(btrim(checkout_url)) > 0
    ),
    CONSTRAINT event_purchases_paid_at_consistent CHECK (
        (status IN ('paid', 'refunded') AND paid_at IS NOT NULL)
        OR (status NOT IN ('paid', 'refunded') AND paid_at IS NULL)
    ),
    CONSTRAINT event_purchases_refunded_at_consistent CHECK (
        (status = 'refunded' AND refunded_at IS NOT NULL)
        OR (status <> 'refunded' AND refunded_at IS NULL)
    ),
    CONSTRAINT event_purchases_last_error_not_blank CHECK (
        last_error IS NULL OR length(btrim(last_error)) > 0
    ),
    CONSTRAINT event_purchases_user_event_unique UNIQUE (event_id, user_id),
    CONSTRAINT event_purchases_user_idempotency_unique UNIQUE (user_id, idempotency_key)
);

CREATE UNIQUE INDEX event_purchases_provider_payment_idx
    ON event_purchases (provider, provider_payment_id)
    WHERE provider_payment_id IS NOT NULL;
CREATE INDEX event_purchases_event_status_idx ON event_purchases (event_id, status);
CREATE INDEX event_purchases_ready_idx
    ON event_purchases (next_attempt_at, created_at)
    WHERE status IN ('initiated', 'processing');

CREATE TABLE tickets (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES events (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES users (account_id) ON DELETE RESTRICT,
    purchase_id uuid NOT NULL UNIQUE REFERENCES event_purchases (id) ON DELETE RESTRICT,
    status ticket_status NOT NULL DEFAULT 'temporarily_reserved',
    reserved_at timestamptz NOT NULL DEFAULT now(),
    reservation_expires_at timestamptz NOT NULL,
    issued_at timestamptz,
    revoked_at timestamptz,
    CONSTRAINT tickets_reservation_window_valid CHECK (reservation_expires_at > reserved_at),
    CONSTRAINT tickets_issued_at_consistent CHECK (
        (status = 'issued' AND issued_at IS NOT NULL)
        OR (status <> 'issued' AND issued_at IS NULL)
    ),
    CONSTRAINT tickets_revoked_at_consistent CHECK (
        (status = 'revoked' AND revoked_at IS NOT NULL)
        OR (status <> 'revoked' AND revoked_at IS NULL)
    ),
    CONSTRAINT tickets_user_event_unique UNIQUE (event_id, user_id)
);

CREATE INDEX tickets_reservation_expiry_idx
    ON tickets (reservation_expires_at)
    WHERE status = 'temporarily_reserved';

CREATE TABLE event_members (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES events (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES users (account_id) ON DELETE RESTRICT,
    ticket_id uuid NOT NULL UNIQUE REFERENCES tickets (id) ON DELETE RESTRICT,
    status membership_status NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    CONSTRAINT event_members_revoked_at_consistent CHECK (
        (status = 'revoked' AND revoked_at IS NOT NULL)
        OR (status <> 'revoked' AND revoked_at IS NULL)
    ),
    CONSTRAINT event_members_user_event_unique UNIQUE (event_id, user_id)
);

CREATE TABLE event_streams (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL UNIQUE REFERENCES events (id) ON DELETE RESTRICT,
    livekit_room_id text NOT NULL UNIQUE,
    viewer_count integer NOT NULL DEFAULT 0,
    status stream_status NOT NULL DEFAULT 'live',
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    failed_at timestamptz,
    failure_reason text,
    CONSTRAINT event_streams_room_not_blank CHECK (length(btrim(livekit_room_id)) > 0),
    CONSTRAINT event_streams_viewer_count_non_negative CHECK (viewer_count >= 0),
    CONSTRAINT event_streams_finished_at_consistent CHECK (
        (status IN ('ended', 'failed') AND finished_at IS NOT NULL)
        OR (status = 'live' AND finished_at IS NULL)
    ),
    CONSTRAINT event_streams_failed_at_consistent CHECK (
        (status = 'failed' AND failed_at IS NOT NULL AND failure_reason IS NOT NULL)
        OR (status <> 'failed' AND failed_at IS NULL AND failure_reason IS NULL)
    )
);

CREATE TABLE event_stream_members (
    id uuid PRIMARY KEY,
    stream_id uuid NOT NULL REFERENCES event_streams (id) ON DELETE CASCADE,
    event_member_id uuid NOT NULL REFERENCES event_members (id) ON DELETE RESTRICT,
    joined_at timestamptz NOT NULL DEFAULT now(),
    left_at timestamptz,
    CONSTRAINT event_stream_members_left_at_valid CHECK (left_at IS NULL OR left_at >= joined_at),
    CONSTRAINT event_stream_members_unique_join UNIQUE (stream_id, event_member_id)
);

CREATE TABLE event_refunds (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES events (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES users (account_id) ON DELETE RESTRICT,
    purchase_id uuid NOT NULL UNIQUE REFERENCES event_purchases (id) ON DELETE RESTRICT,
    amount_minor bigint NOT NULL,
    status refund_status NOT NULL DEFAULT 'processing',
    provider text NOT NULL,
    provider_refund_id text,
    linked_refund_id uuid REFERENCES event_refunds (id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    last_error text,
    processed_at timestamptz,
    retried_at timestamptz,
    refunded_at timestamptz,
    CONSTRAINT event_refunds_amount_positive CHECK (amount_minor > 0),
    CONSTRAINT event_refunds_provider_not_blank CHECK (length(btrim(provider)) > 0),
    CONSTRAINT event_refunds_idempotency_not_blank CHECK (length(btrim(idempotency_key)) > 0),
    CONSTRAINT event_refunds_attempt_count_non_negative CHECK (attempt_count >= 0),
    CONSTRAINT event_refunds_last_error_not_blank CHECK (
        last_error IS NULL OR length(btrim(last_error)) > 0
    ),
    CONSTRAINT event_refunds_processed_at_consistent CHECK (
        (status IN ('refunded', 'failed') AND processed_at IS NOT NULL)
        OR (status = 'processing' AND processed_at IS NULL)
    ),
    CONSTRAINT event_refunds_refunded_at_consistent CHECK (
        (status = 'refunded' AND refunded_at IS NOT NULL)
        OR (status <> 'refunded' AND refunded_at IS NULL)
    ),
    CONSTRAINT event_refunds_provider_refund_not_blank CHECK (
        provider_refund_id IS NULL OR length(btrim(provider_refund_id)) > 0
    ),
    CONSTRAINT event_refunds_idempotency_unique UNIQUE (provider, idempotency_key)
);

CREATE UNIQUE INDEX event_refunds_provider_refund_idx
    ON event_refunds (provider, provider_refund_id)
    WHERE provider_refund_id IS NOT NULL;
CREATE INDEX event_refunds_ready_idx
    ON event_refunds (next_attempt_at, id)
    WHERE status = 'processing';

CREATE TABLE event_payouts (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL UNIQUE REFERENCES events (id) ON DELETE RESTRICT,
    host_id uuid NOT NULL REFERENCES hosts (account_id) ON DELETE RESTRICT,
    amount_minor bigint NOT NULL,
    status payout_status NOT NULL DEFAULT 'processing',
    provider text NOT NULL,
    provider_payout_id text,
    linked_payout_id uuid REFERENCES event_payouts (id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    last_error text,
    processed_at timestamptz,
    retried_at timestamptz,
    paid_at timestamptz,
    CONSTRAINT event_payouts_amount_positive CHECK (amount_minor > 0),
    CONSTRAINT event_payouts_provider_not_blank CHECK (length(btrim(provider)) > 0),
    CONSTRAINT event_payouts_idempotency_not_blank CHECK (length(btrim(idempotency_key)) > 0),
    CONSTRAINT event_payouts_attempt_count_non_negative CHECK (attempt_count >= 0),
    CONSTRAINT event_payouts_last_error_not_blank CHECK (
        last_error IS NULL OR length(btrim(last_error)) > 0
    ),
    CONSTRAINT event_payouts_processed_at_consistent CHECK (
        (status IN ('paid', 'failed') AND processed_at IS NOT NULL)
        OR (status = 'processing' AND processed_at IS NULL)
    ),
    CONSTRAINT event_payouts_paid_at_consistent CHECK (
        (status = 'paid' AND paid_at IS NOT NULL)
        OR (status <> 'paid' AND paid_at IS NULL)
    ),
    CONSTRAINT event_payouts_provider_payout_not_blank CHECK (
        provider_payout_id IS NULL OR length(btrim(provider_payout_id)) > 0
    ),
    CONSTRAINT event_payouts_idempotency_unique UNIQUE (provider, idempotency_key)
);

CREATE UNIQUE INDEX event_payouts_provider_payout_idx
    ON event_payouts (provider, provider_payout_id)
    WHERE provider_payout_id IS NOT NULL;
CREATE INDEX event_payouts_ready_idx
    ON event_payouts (next_attempt_at, id)
    WHERE status = 'processing';

CREATE TABLE webhook_events (
    id uuid PRIMARY KEY,
    provider text NOT NULL,
    provider_event_id text NOT NULL,
    event_type text NOT NULL,
    payload_hash text NOT NULL,
    payload jsonb NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    last_error text,
    received_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    failed_at timestamptz,
    CONSTRAINT webhook_events_provider_not_blank CHECK (length(btrim(provider)) > 0),
    CONSTRAINT webhook_events_provider_event_id_not_blank CHECK (length(btrim(provider_event_id)) > 0),
    CONSTRAINT webhook_events_type_not_blank CHECK (length(btrim(event_type)) > 0),
    CONSTRAINT webhook_events_payload_hash_not_blank CHECK (length(btrim(payload_hash)) > 0),
    CONSTRAINT webhook_events_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT webhook_events_attempt_count_non_negative CHECK (attempt_count >= 0),
    CONSTRAINT webhook_events_last_error_not_blank CHECK (
        last_error IS NULL OR length(btrim(last_error)) > 0
    ),
    CONSTRAINT webhook_events_provider_event_unique UNIQUE (provider, provider_event_id)
);
CREATE INDEX webhook_events_ready_idx
    ON webhook_events (next_attempt_at, received_at)
    WHERE processed_at IS NULL;

CREATE TABLE ledger_entries (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES events (id) ON DELETE RESTRICT,
    entry_type ledger_entry_type NOT NULL,
    amount_minor bigint NOT NULL,
    purchase_id uuid REFERENCES event_purchases (id) ON DELETE RESTRICT,
    refund_id uuid REFERENCES event_refunds (id) ON DELETE RESTRICT,
    payout_id uuid REFERENCES event_payouts (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_amount_positive CHECK (amount_minor > 0),
    CONSTRAINT ledger_entries_reference_present CHECK (
        (entry_type = 'charge' AND purchase_id IS NOT NULL AND refund_id IS NULL AND payout_id IS NULL)
        OR (entry_type = 'refund' AND purchase_id IS NULL AND refund_id IS NOT NULL AND payout_id IS NULL)
        OR (entry_type = 'payout' AND purchase_id IS NULL AND refund_id IS NULL AND payout_id IS NOT NULL)
    ),
    CONSTRAINT ledger_entries_purchase_unique UNIQUE (purchase_id),
    CONSTRAINT ledger_entries_refund_unique UNIQUE (refund_id),
    CONSTRAINT ledger_entries_payout_unique UNIQUE (payout_id)
);

CREATE TABLE email_notifications (
    id uuid PRIMARY KEY,
    notification_type text NOT NULL,
    recipient_user_id uuid REFERENCES users (account_id) ON DELETE SET NULL,
    recipient_email text NOT NULL,
    template_key text NOT NULL,
    payload jsonb NOT NULL,
    idempotency_key text NOT NULL,
    status email_notification_status NOT NULL DEFAULT 'queued',
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    delivered_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT email_notifications_type_not_blank CHECK (length(btrim(notification_type)) > 0),
    CONSTRAINT email_notifications_recipient_email_not_blank CHECK (length(btrim(recipient_email)) > 0),
    CONSTRAINT email_notifications_template_not_blank CHECK (length(btrim(template_key)) > 0),
    CONSTRAINT email_notifications_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT email_notifications_idempotency_not_blank CHECK (length(btrim(idempotency_key)) > 0),
    CONSTRAINT email_notifications_attempt_count_non_negative CHECK (attempt_count >= 0),
    CONSTRAINT email_notifications_last_error_not_blank CHECK (
        last_error IS NULL OR length(btrim(last_error)) > 0
    ),
    CONSTRAINT email_notifications_delivered_at_consistent CHECK (
        (status = 'delivered' AND delivered_at IS NOT NULL)
        OR (status <> 'delivered' AND delivered_at IS NULL)
    ),
    CONSTRAINT email_notifications_idempotency_unique UNIQUE (idempotency_key)
);

CREATE INDEX email_notifications_ready_idx
    ON email_notifications (next_attempt_at, created_at)
    WHERE status IN ('queued', 'processing');

---- create above / drop below ----

DROP INDEX email_notifications_ready_idx;
DROP TABLE email_notifications;
DROP TABLE ledger_entries;
DROP INDEX webhook_events_ready_idx;
DROP TABLE webhook_events;
DROP INDEX event_payouts_ready_idx;
DROP INDEX event_payouts_provider_payout_idx;
DROP TABLE event_payouts;
DROP INDEX event_refunds_ready_idx;
DROP INDEX event_refunds_provider_refund_idx;
DROP TABLE event_refunds;
DROP TABLE event_stream_members;
DROP TABLE event_streams;
DROP TABLE event_members;
DROP INDEX tickets_reservation_expiry_idx;
DROP TABLE tickets;
DROP INDEX event_purchases_ready_idx;
DROP INDEX event_purchases_event_status_idx;
DROP INDEX event_purchases_provider_payment_idx;
DROP TABLE event_purchases;
DROP INDEX events_crew_idx;
DROP INDEX events_host_idx;
DROP INDEX events_status_starts_idx;
DROP TABLE events;
DROP TABLE provider_accounts;
DROP TYPE ledger_entry_type;
DROP TYPE payout_status;
DROP TYPE refund_status;
DROP TYPE membership_status;
DROP TYPE stream_status;
DROP TYPE purchase_status;
DROP TYPE ticket_status;
DROP TYPE event_status;
DROP TYPE email_notification_status;
