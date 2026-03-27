# ── Stage 1: builder ─────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

# ca-certificates needed for outbound TLS (LLM API calls in later phases)
RUN apk add --no-cache ca-certificates git make

WORKDIR /src

# Cache dependency downloads separately from source changes
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a statically linked binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w -extldflags=-static" \
    -o /out/contextdb ./cmd/contextdb

# ── Stage 2: runtime ─────────────────────────────────────────────────────────
FROM scratch

# Bring in TLS root certs so the binary can make HTTPS calls
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# The binary
COPY --from=builder /out/contextdb /contextdb

# Data directory for embedded BadgerDB (mounted as a volume in production)
VOLUME ["/data"]

EXPOSE 7700 7701

ENTRYPOINT ["/contextdb"]
