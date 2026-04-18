# ===== STAGE 1: Build frontend =====
FROM node:22-alpine AS frontend
ARG VITE_BASE_PATH=/
ENV VITE_BASE_PATH=$VITE_BASE_PATH
WORKDIR /app/frontend
COPY frontend/package.json frontend/bun.lock* ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# ===== STAGE 2: Build Go binary =====
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY . .
# Replace placeholder dist with the real built assets
COPY --from=frontend /app/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -o dashboard .

# ===== STAGE 3: Minimal runtime =====
FROM alpine:3.22
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app
COPY --from=builder /app/dashboard .
RUN chown appuser:appgroup dashboard
USER appuser
EXPOSE 8082
ENTRYPOINT ["./dashboard"]
