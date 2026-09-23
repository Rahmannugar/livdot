# Architecture

## System shape

LIV DOT is a modular monolith with four deployables sharing one PostgreSQL
database and one Redis instance.

```text
cmd/api       HTTP contract
cmd/worker    asynchronous and time-based work
cmd/migrate   schema migrations (deploy step)
cmd/admin     internal administrator creation
```

`cmd/api/main.go` and `cmd/worker/main.go` are composition roots. Each process
wires its own dependencies, and there is no shared bootstrap package.

## Domains

Each domain owns its behavior, persistence, handlers, and SQLC queries.

- `authentication` — accounts, role profiles, opaque session tokens, request identity middleware.
- `crews` — crew profiles and host-facing browsing.
- `events` — event lifecycle.
- `ticketing` — purchase, reservation, settlement, tickets.
- `streaming` — mocked LiveKit rooms and stream failure.
- `finance` — refunds, payouts, ledger.
- `notifications` — durable email queue, templates, delivery.
- `webhooks` — provider callbacks, verification, dedupe.
- `health` — liveness and readiness.

Shared infrastructure lives in `internal/infra`: database, cache, pagination,
payment, streaming provider, email, lock, ratelimit, and openapi.

## Layering

Handlers parse and validate requests, call a service, and shape responses.
Services own workflow and authorization. Repositories own SQL and transactions.
A domain that needs another domain's data depends on a narrow port it owns
(`CrewDirectory`, `Directory`, `Refunder`) and never reads the other domain's
repository. Queries stay out of handlers and services.

## Durable invariants

Database constraints enforce these, so they hold under concurrency.

- `events.available_tickets` stays between zero and `total_tickets`. Reserving a
  ticket is a guarded decrement, so two buyers cannot take the last slot.
- A purchase is unique per `(event_id, user_id)` and per
  `(user_id, idempotency_key)`. A refund is unique per purchase. A payout is
  unique per event.
- Timestamps that describe state (`paid_at`, `refunded_at`, `cancelled_at`,
  `issued_at`) must agree with their status, so a half-applied transition cannot
  read as valid.
- A ticket is issued, and membership granted, only while the reservation window
  is open. A payment that settles late is refunded instead of granted.

## Money and idempotency

- Every payment, refund, and payout carries an idempotency key derived from its
  row (`refund:<purchaseId>`, `payout:<eventId>`). A retry cannot move money
  twice.
- Auto-triggered and admin-triggered refunds converge on the same unique row.
- Refund settlement writes the refund, the purchase, and the ledger row in one
  transaction.
- A provider callback is verified by signature and recorded once in
  `webhook_events` on `(provider, provider_event_id)` before any state change, so
  duplicate or unordered delivery does nothing.
- Refund eligibility is duration-based. A stream failure in the first quarter of
  the scheduled duration refunds automatically; a later failure goes to admin
  review, and an admin refund is refused unless the stream failed.

## Asynchronous work

PostgreSQL is the source of truth. Redis carries session caching, distributed
rate limiting, and the worker startup lock only.

Email is queued in the same transaction as the state change. The producer
resolves the recipient, writes the `email_notifications` row, and fires
`pg_notify('livdot_email')` inside its own transaction. The worker holds a
`LISTEN` connection and drains the queue on wake, and a ticker drains it again as
a fallback. The email idempotency key makes a redelivery a no-op, and delivery
retries with backoff.

Time-based work has no trigger, so it runs as a sweep. Expired reservations are
claimed with `FOR UPDATE SKIP LOCKED` and their slots returned to the event.

The worker takes a Redis lock at startup, so a one-time boot section never runs
twice. Migrations are a deploy step, not boot work.

## Failure handling

- Claim loops use `FOR UPDATE SKIP LOCKED` and a lease timestamp, so another
  worker reclaims a crashed batch instead of stalling.
- Provider calls reuse their idempotency key on retry.
- A missing recipient address never blocks a ticket or a refund; the money action
  proceeds and the email is skipped.
- We treat `pg_notify` as a hint. A missed notification costs latency, not
  correctness, because the row is the queue.

## Configuration

Koanf loads `.env`, then environment variables with the `LIVDOT_` prefix, mapping
a grouped key to a struct field (`LIVDOT_DATABASE_HOST` to `database.host`).
Permanent external configuration is validated at startup, and the development
payment secret is rejected in production. Fixed values such as the stream name,
rate-limit quotas, and template keys are code constants, not environment
variables.

## Contract

The OpenAPI document is assembled in `internal/openapi` from per-domain
`contract.go` files. Request schemas come from the same Go structs the handlers
bind, so the contract cannot drift from acceptance behavior. Response schemas are
written by hand because a response is an open output contract. `cmd/openapi`
writes `openapi.json` deterministically, and `task check` fails when the
committed file is stale. The document is served at `/api/openapi.json` and
rendered with Scalar at `/api/docs`. Scalar is only the viewer.
