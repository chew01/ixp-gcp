# ===== STAGE 1: Build =====
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum first for caching
COPY go.mod go.sum ./

COPY vendor ./vendor

# Copy the source code
COPY . .

# Build the Go binary (static)
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -o customer-agent

# ===== STAGE 2: Minimal runtime =====
FROM alpine:3.22

# Create a non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/customer-agent .

# Set permissions
RUN chown appuser:appgroup customer-agent

USER appuser

# Run the binary
ENTRYPOINT ["./customer-agent"]

