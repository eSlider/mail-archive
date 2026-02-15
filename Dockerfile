# syntax=docker/dockerfile:1.4
# Multi-stage build for the Mail Archive Go application.
# Stage 1: Build the Go binary
FROM golang:1.24-bookworm AS builder

# Install build dependencies (CGO, git for module fetching).
RUN apt-get update && apt-get install -y --no-install-recommends \
    git gcc g++ libc-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy go mod files first for better caching.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source and build.
# VERSION injected at build time for tagged releases (e.g. 1.0.1).
ARG VERSION=dev
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux \
    go build -p $(nproc) \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -trimpath \
    -o /mails \
    ./cmd/mails

# Stage 2: Runtime image
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tini pst-utils && rm -rf /var/lib/apt/lists/*

COPY --from=builder /mails /usr/local/bin/mails
COPY --from=builder /build/web/static /app/web/static

# Create data directory.
RUN mkdir -p /app/users && chown 1000:1000 /app/users

USER 1000:1000
WORKDIR /app

ENV LISTEN_ADDR=":8090" \
    DATA_DIR="/app/users" \
    STATIC_DIR="/app/web/static" \
    BASE_URL="http://localhost:8090"

EXPOSE 8090

ENTRYPOINT ["tini", "--"]
CMD ["mails", "serve"]
