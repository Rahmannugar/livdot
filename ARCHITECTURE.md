# Architecture

## Shape

LIV DOT is a domain-organized modular monolith with two deployables sharing one
PostgreSQL database and one Redis instance:

- `cmd/api` serves the HTTP contract.
- `cmd/worker` performs asynchronous and time-based work.
- `cmd/migrate` applies schema migrations as a deploy step.
- `cmd/admin` creates internal administrators out of band.

`cmd/api/main.go` and `cmd/worker/main.go` are direct composition roots. There is
no generic bootstrap package; wiring is explicit per process.

## Domains

Each domain owns its behavior, persistence, handlers, and SQLC queries.

- `authentication` — accounts, role profiles, opaque session tokens, and the
  request identity middleware.
- `crews` — crew profiles and host-facing crew browsing.
- `events` — event lifecycle: create, browse, detail, update, assign, cancel.
- `ticketing` — purchase, reservation, settlement, ticket reads.
- `streaming` — mocked LiveKit room lifecycle and stream failure handling.
- `finance` — refunds, payouts, and the ledger.
- `notifications` — the durable email queue, templates, and delivery.
- `webhooks` — inbound provider callbacks, verification, and dedupe.
- `health` — liveness and readiness probes.

Reusable infrastructure lives in `internal/infra`: `database`, `cache`,
`pagination`, `payment`, `streaming` (provider port), `email`, `lock`,
`ratelimit`, and `openapi`.

## Layering

Handlers parse and validate requests, call a domain service, and shape
responses. Services own workflow and authorization rules. Repositories own SQL
and transactions. Cross-domain reads go through a narrow port owned by the
consumer (`CrewDirectory`, `Directory`, `Refunder`), never through the other
domain's repository. Queries never appear in handlers or services.

## Durable invariants

These are enforced by database constraints so they survive concurrency:

- `events.available_tickets` stays within `[0, total_tickets]`. A reservation is
  an atomic guarded decrement, so two buyers racing for the last slot cannot both
  succeed.
- Ticket purchases are unique per `(event_id, user_id)` and per
  `(user_id, idempotency_key)`; refunds are unique per purchase; payouts are
  unique per event.
- State-consistent columns (`paid_at`, `refunded_at`, `cancelled_at`,
  `issued_at`) must match their status, so a half-applied transition cannot be
  read as valid.
- A ticket is only issued, and membership only granted, while its reservation
  window is open. A payment that settles after the window is refunded instead of
  granting access.

## Money and idempotency

- Every payment, refund, and payout carries a stable idempotency key derived
  from the durable row (`refund:<purchaseId>`, `payout:<eventId>`), so retrying
  one logical movement cannot move money twice.
- `event_refunds` is unique per purchase and `event_payouts` is unique per event.
  Auto-trigger and admin-triggered refunds therefore converge on the same row.
- Refund settlement updates the refund, the purchase, and the ledger row in one
  transaction.
- A provider callback is verified by signature and recorded once in
  `webhook_events` on `(provider, provider_event_id)` before any state change, so
  duplicate or unordered delivery is inert.
- The refund rule is duration-based: a stream failure in the first quarter of the
  scheduled duration refunds automatically; a later failure is left for admin
  review, and an admin refund is refused unless the stream actually failed.

## Asynchronous work

PostgreSQL is the source of truth. Redis is used only for session caching,
distributed rate limiting, and the worker startup lock.

Email is queued in the same transaction as the state change that produces it:
the producer resolves the recipient, writes the `email_notifications` row, and
fires `pg_notify('livdot_email')` inside its own transaction. The worker holds a
`LISTEN` connection and drains the queue on wake; a ticker runs the same drain as
a fallback if a notification is missed. The unique email idempotency key makes a
redelivery a no-op, and delivery retries with backoff.

Time-based work has no trigger, so it is a polling sweep: expired ticket
reservations are claimed with `FOR UPDATE SKIP LOCKED` and their slots returned
to the event.

The worker takes a Redis lock at startup so a one-time boot section never runs
concurrently across instances. Schema migrations are a deploy step, not boot
work.

## Failure handling

- Claim loops use `FOR UPDATE SKIP LOCKED` plus a lease timestamp, so a crashed
  worker's batch is reclaimed by another instead of stalling.
- Provider calls carry idempotency keys; a retry reuses the same key.
- A missing recipient address does not block a ticket or refund; the money action
  proceeds and only the email is skipped.
- `pg_notify` is treated as a hint: missing it costs latency, never correctness,
  because the row is the queue.

## Configuration

Koanf loads `.env` then environment variables with the `LIVDOT_` prefix. Grouped
keys map to struct fields (`LIVDOT_DATABASE_HOST` → `database.host`). Permanent
external configuration, such as the payment webhook secret, is validated at
startup; the development webhook secret is rejected in production. Fixed
transport and policy values (stream name, rate-limit quotas, template keys) live
in code as constants, not environment variables.

## Contract

The OpenAPI document is assembled in `internal/openapi` from per-domain
`contract.go` files. Request schemas are reflected from the same Go request
structs the handlers bind, so the contract cannot drift from acceptance
behavior. Response schemas are maintained by hand because a response is an open
output contract that varies by status and envelope. `cmd/openapi` writes
`openapi.json` deterministically; `task check` regenerates it and fails if the
committed file is stale. The API serves the document at `/api/openapi.json` and
renders it with Scalar at `/api/docs`; Scalar is only the viewer.
