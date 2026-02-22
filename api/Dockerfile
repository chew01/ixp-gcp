# -------- Build stage --------
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install CA certs (needed later if Atomix / HTTPS)
RUN apk add --no-cache ca-certificates

# Copy go mod files first (better caching)
COPY api/go.mod api/go.sum ./
COPY shared ../shared
RUN go mod download

# Copy source
COPY api/ .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o api-gateway

# -------- Runtime stage --------
FROM gcr.io/distroless/base-debian12

WORKDIR /app

# Copy binary and certs
COPY --from=builder /app/api-gateway /api-gateway
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/api-gateway"]
