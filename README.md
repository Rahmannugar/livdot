# LIV DOT Service

LIV DOT is a backend service for paid live events. Hosts create events,
production crews operate streams, viewers purchase access, and the platform
coordinates refunds and host payouts when an event finishes or fails. The
assessment brief is in `assessment.md` and the submitted design answer in
`Livdot Assessment Answer.docx`.

## Technology

- Go 1.26 and Gin
- Koanf
- PostgreSQL with PGX and SQLC
- Tern migrations
- Redis
- OpenAPI with Scalar
- Docker Compose
- Task (Taskfile.yml)

## Local development

Copy the configuration template and provide the required local values:

```bash
cp .env.example .env
```

Start the local stack without forcing an image rebuild (missing images are built
automatically):

```bash
task up
```

Build the application images before starting the stack when application code or
dependencies change:

```bash
task up-build
```

The API listens on `http://localhost:8080`.

- Liveness: `GET /health/live`
- Readiness: `GET /health/ready`
- OpenAPI document: `GET /api/openapi.json`
- API reference: `GET /api/docs`

To run the processes directly instead of the stack:

```bash
task migrate
task run-api
task run-worker
```

Internal admins are created through a command, not an endpoint:

```bash
task admin-create -- -email admin@livdot.local -password password123 -full-name Admin -role admin
```

Stop the stack:

```bash
task down
```

Follow the stack logs:

```bash
task logs
```

## Documentation

- `API.md` maps the endpoints to the OpenAPI document.
- `ARCHITECTURE.md` covers the system shape and its durable invariants.

## Validation

```bash
task check
```
