# ===== STAGE 1: Build =====
FROM golang:1.25-alpine AS builder

# Install git and bash for go modules
RUN apk add --no-cache git bash

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum first for caching
COPY auction/go.mod auction/go.sum ./
COPY shared ./shared
COPY auction/vendor ./vendor

# Copy the auction source code
COPY auction/ .

# Build the Go binary (static)
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -o auction-runner

# ===== STAGE 2: Minimal runtime =====
FROM alpine:3.22

# Create a non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/auction-runner .

# Set permissions
RUN chown appuser:appgroup auction-runner

USER appuser

# Expose port (if needed for metrics, optional)
EXPOSE 8080

# Run the binary
ENTRYPOINT ["./auction-runner"]
