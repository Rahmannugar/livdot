# LIV DOT Service

LIV DOT is a backend service for paid live events. Hosts create events,
production crews operate streams, viewers purchase access, and the platform
coordinates refunds and host payouts when an event finishes or fails. The
assessment requirements are retained in `assessment.md` and is answered
`Livdot Assessment Answer.docx`.

## Technology

- Go 1.26 and Gin
- Koanf
- PostgreSQL with PGX
- Redis
- Docker Compose
- Task

## Local development

Copy the configuration template and provide the required local values:

```bash
cp .env.example .env
```

Start the local stack:

```bash
task compose-up
```

The API listens on `http://localhost:8080`.

- Liveness: `GET /health/live`
- Readiness: `GET /health/ready`

Stop the stack:

```bash
task compose-down
```

## Validation

```bash
task check
```
