# services/openclaw-adapter/Dockerfile
# Multi-stage build for the OpenClaw query adapter.

FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -buildvcs=false -ldflags="-s -w" \
    -o /openclaw-adapter ./services/openclaw-adapter

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/*

RUN addgroup --system openclaw && \
    adduser --system --ingroup openclaw openclaw && \
    mkdir -p /data/logs && chown openclaw:openclaw /data/logs

COPY --from=builder /openclaw-adapter /usr/local/bin/openclaw-adapter

EXPOSE 8112

HEALTHCHECK --interval=10s --timeout=5s --retries=3 \
    CMD wget -qO- http://localhost:8112/health || exit 1

USER openclaw
ENTRYPOINT ["/usr/local/bin/openclaw-adapter"]
