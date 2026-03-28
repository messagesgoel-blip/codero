# Build stage: compile the Go binary with CGO enabled (required by go-sqlite3).
FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=1 GOOS=linux \
    go build -buildvcs=false -ldflags="-s -w -X main.version=${VERSION}" \
    -o /codero ./cmd/codero

# ---- Runtime stage ----
FROM debian:bookworm-slim

ARG APP_USER=codero

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /codero /usr/local/bin/codero
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# Runtime data directories (overridden by volume mounts in production).
RUN set -eux; \
    APP_HOME="/data/runtime/${APP_USER}"; \
    addgroup --system "${APP_USER}"; \
    adduser --system --ingroup "${APP_USER}" --home "${APP_HOME}" "${APP_USER}"; \
    mkdir -p /data/db /data/logs /data/pids /data/tmp /data/snapshots; \
    chown -R "${APP_USER}:${APP_USER}" "${APP_HOME}" /data

VOLUME ["/data"]

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

USER ${APP_USER}

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["codero", "daemon"]
