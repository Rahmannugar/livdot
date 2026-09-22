FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/livdot-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/livdot-worker ./cmd/worker

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

COPY --from=build /out/livdot-api /usr/local/bin/livdot-api
COPY --from=build /out/livdot-worker /usr/local/bin/livdot-worker

EXPOSE 8080

ENTRYPOINT ["livdot-api"]

