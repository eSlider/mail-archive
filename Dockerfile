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
COPY . .
RUN CGO_ENABLED=1 go build -o /mails ./cmd/mails

# Stage 2: Runtime image
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tini && rm -rf /var/lib/apt/lists/*

COPY --from=builder /mails /usr/local/bin/mails
COPY --from=builder /build/web/static /app/web/static

# Create data directory.
RUN mkdir -p /app/users && chown 1000:1000 /app/users

USER 1000:1000
WORKDIR /app

ENV LISTEN_ADDR=":8080" \
    DATA_DIR="/app/users" \
    STATIC_DIR="/app/web/static" \
    BASE_URL="http://localhost:8080"

EXPOSE 8080

ENTRYPOINT ["tini", "--"]
CMD ["mails", "serve"]
