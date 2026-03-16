# Build stage: compile the Go binary with CGO enabled (required by go-sqlite3).
FROM golang:1.22-bullseye AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=1 GOOS=linux \
    go build -ldflags="-s -w -X main.version=${VERSION}" \
    -o /codero ./cmd/codero

# ---- Runtime stage ----
FROM debian:bullseye-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /codero /usr/local/bin/codero

# Runtime data directories (overridden by volume mounts in production).
RUN mkdir -p /data/db /data/logs /data/pids /data/tmp /data/snapshots

VOLUME ["/data"]

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["codero", "daemon"]
