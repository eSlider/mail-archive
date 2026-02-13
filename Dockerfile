# Multi-stage build for the Mail Archive Go application.
# Stage 1: Build the Go binary
FROM golang:1.24-bookworm AS builder

WORKDIR /build

# Install C dependencies for CGO (DuckDB, SQLite).
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc g++ libc-dev && rm -rf /var/lib/apt/lists/*

# Cache Go module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
# VERSION injected at build time for tagged releases (e.g. 1.0.1).
ARG VERSION=dev
COPY . .
RUN CGO_ENABLED=1 go build -ldflags "-X main.version=${VERSION}" -o /mails ./cmd/mails

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
