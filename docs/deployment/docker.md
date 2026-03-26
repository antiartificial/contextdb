---
title: Docker
parent: Deployment
nav_order: 2
---

# Docker

## Standalone (embedded mode)

```bash
# Build
docker build -t contextdb:dev .

# Run with persistent storage
docker run --rm \
  -p 7700:7700 \
  -p 7701:7701 \
  -p 7702:7702 \
  -v contextdb-data:/data \
  -e CONTEXTDB_DATA_DIR=/data \
  contextdb:dev
```

The Docker image is built from `scratch` -- no shell, no OS, just the static binary and TLS certificates. Image size is typically under 20MB.

## With Postgres (Docker Compose)

```bash
docker compose up --build
```

The included `docker-compose.yml` starts contextdb with a Postgres + pgvector backend:

```yaml
version: "3.9"

services:
  contextdb:
    build: .
    ports:
      - "7700:7700"   # gRPC
      - "7701:7701"   # REST
    volumes:
      - contextdb-data:/data
    environment:
      - CONTEXTDB_DATA_DIR=/data
      - CONTEXTDB_LOG_LEVEL=debug
      - CONTEXTDB_DSN=postgres://contextdb:contextdb@postgres:5432/contextdb?sslmode=disable
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: pgvector/pgvector:pg16
    ports:
      - "5432:5432"
    environment:
      POSTGRES_DB: contextdb
      POSTGRES_USER: contextdb
      POSTGRES_PASSWORD: contextdb
    volumes:
      - pg-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U contextdb"]
      interval: 5s
      timeout: 3s
      retries: 5

volumes:
  contextdb-data:
  pg-data:
```

## Dockerfile

The multi-stage Dockerfile produces a minimal image:

```dockerfile
# Stage 1: build
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache ca-certificates git make
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w -extldflags=-static" \
    -o /out/contextdb ./cmd/contextdb

# Stage 2: scratch runtime
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/contextdb /contextdb
VOLUME ["/data"]
EXPOSE 7700 7701
ENTRYPOINT ["/contextdb"]
```

## CI/CD

The GitHub Actions workflow builds, tests, and pushes to `ghcr.io` on every push to `main`:

```bash
# Pull the latest image
docker pull ghcr.io/antiartificial/contextdb:latest

# Run it
docker run --rm -p 7701:7701 ghcr.io/antiartificial/contextdb:latest
```

Tags:
- `latest` -- latest main branch build
- `sha-<commit>` -- specific commit
- `main` -- branch tag
