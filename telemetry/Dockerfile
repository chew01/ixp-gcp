# ===== STAGE 1: Build =====
FROM golang:1.25-alpine AS builder

# Install git and bash for go modules
RUN apk add --no-cache git bash

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum first for caching
COPY telemetry/go.mod telemetry/go.sum ./
# Copy shared package used by the telemetry module
COPY shared ../shared

# Copy the source code so `go mod tidy` can evaluate imports
COPY telemetry/ .

# Ensure module files are consistent and download dependencies
RUN go mod tidy

# Build the Go binary (static)
RUN CGO_ENABLED=0 GOOS=linux go build -o telemetry-service

# ===== STAGE 2: Minimal runtime =====
FROM alpine:3.22

# Create a non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/telemetry-service .

# Set permissions
RUN chown appuser:appgroup telemetry-service

USER appuser

# Expose port (if needed for metrics, optional)
EXPOSE 8080

# Run the binary
ENTRYPOINT ["./telemetry-service"]
