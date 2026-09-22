# API

All routes are under `/api`. Authenticated routes require
`Authorization: Bearer <token>` from a sign-in or sign-up response. Roles are
enforced per route; `internal_admin` is created through a command, not signup.

The authoritative contract is `openapi.json`, served at `/api/openapi.json` and
rendered by Scalar at `/api/docs`. This file maps the endpoints; the OpenAPI
document defines the exact schemas.

## Authentication

| Method | Path | Role | Purpose |
| --- | --- | --- | --- |
| POST | `/api/signup/host` | public | Register a host |
| POST | `/api/signin/host` | public | Sign in a host |
| POST | `/api/signup/crew` | public | Register a crew |
| POST | `/api/signin/crew` | public | Sign in a crew |
| POST | `/api/signup/user` | public | Register a viewer |
| POST | `/api/signin/user` | public | Sign in a viewer |
| POST | `/api/signin/internal` | public | Sign in an internal admin |

Sign-up and sign-in return `{ accountId, email, token, expiresAt }`.

## Account and crews

| Method | Path | Role | Purpose |
| --- | --- | --- | --- |
| GET | `/api/account/crews` | crew | Read the caller's crew profile |
| PATCH | `/api/account/crews` | crew | Update name, list, or availability |
| GET | `/api/crews` | host | Browse crews with `name` and `availability` filters and keyset pagination |

## Events

| Method | Path | Role | Purpose |
| --- | --- | --- | --- |
| GET | `/api/events` | public | List events with `name`, `status`, `duration[gte|lte]`, `amount[gte|lte]`, `cursor`, `pageSize` |
| GET | `/api/events/{id}` | public | Event detail |
| POST | `/api/events` | host | Create an event, optionally assigning a crew |
| PATCH | `/api/events/{id}` | host | Update an event, reassign the crew, or cancel with `status=cancelled` |

## Ticketing

| Method | Path | Role | Purpose |
| --- | --- | --- | --- |
| POST | `/api/events/{id}/purchase` | user | Reserve a ticket and start a payment; body carries `idempotencyKey` |
| GET | `/api/tickets/{id}` | user | Read a ticket the caller owns |

Purchases are one per user per event. A reservation holds a slot for ten
minutes; the response exposes `ticketId` and `checkoutUrl`.

## Streaming

| Method | Path | Role | Purpose |
| --- | --- | --- | --- |
| POST | `/api/events/{id}/start` | host | Open the room and move the event live |
| POST | `/api/events/{id}/join` | user | Issue a viewer token to an active ticket holder |
| POST | `/api/events/{id}/end` | host | End the stream and accrue the host payout |
| POST | `/api/events/{id}/stream-failure` | host | Record a failure with a `reason` |

A failure in the first quarter of the scheduled duration refunds viewers
automatically; a later failure is left for admin review.

## Finance

| Method | Path | Role | Purpose |
| --- | --- | --- | --- |
| POST | `/api/events/{id}/refund` | internal_admin | Refund viewers of an event whose stream failed |
| GET | `/api/refunds` | user, internal_admin | List refunds; viewers see only their own |
| GET | `/api/refunds/{id}` | user, internal_admin | Read one refund |
| GET | `/api/payouts` | host, internal_admin | List payouts; hosts see only their own |
| GET | `/api/payouts/{id}` | host, internal_admin | Read one payout |

`GET /api/refunds?userId=` and `GET /api/payouts?hostId=` are honored only for
internal admins, so a caller cannot read another account's money records.

## Webhooks

| Method | Path | Role | Purpose |
| --- | --- | --- | --- |
| POST | `/api/webhooks/payments` | public | Receive a signed provider callback; authenticity is the signature |

## Health and documentation

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health/live` | Liveness |
| GET | `/health/ready` | Readiness, including PostgreSQL and Redis |
| GET | `/api/openapi.json` | OpenAPI document |
| GET | `/api/docs` | Scalar API reference |

## Errors

Errors use `{ "error": { "code": "...", "message": "..." } }`. Status codes:
`400` invalid request, `401` unauthenticated, `403` forbidden, `404` not found,
`409` conflict (state or idempotency), `429` rate limited. Rate-limited
responses include `X-RateLimit-Limit`, `X-RateLimit-Remaining`, and
`Retry-After`.
