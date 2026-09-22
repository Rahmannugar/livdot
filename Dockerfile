# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build

WORKDIR /src

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/livdot-api ./cmd/api
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/livdot-worker ./cmd/worker
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/livdot-migrate ./cmd/migrate

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

COPY --from=build /src/internal/infra/database/migrations /migrations
COPY --from=build /out/livdot-api /usr/local/bin/livdot-api
COPY --from=build /out/livdot-worker /usr/local/bin/livdot-worker
COPY --from=build /out/livdot-migrate /usr/local/bin/livdot-migrate

EXPOSE 8080

ENTRYPOINT ["livdot-api"]
