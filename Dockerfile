# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS builder

WORKDIR /src

COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    for i in 1 2 3; do \
        go mod download && exit 0; \
        sleep 5; \
    done; \
    go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN test -f ./cmd/habr-tg-bot/main.go
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/habr-tg-bot ./cmd/habr-tg-bot

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/habr-tg-bot /app/habr-tg-bot
COPY migrations /app/migrations

RUN adduser -D -H -u 10001 appuser && mkdir -p /app/data && chown -R appuser:appuser /app
USER appuser

VOLUME ["/app/data"]

ENTRYPOINT ["/app/habr-tg-bot"]
