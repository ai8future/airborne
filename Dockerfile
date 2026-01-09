# Airborne Dockerfile
# Multi-stage build for minimal production image

# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o airborne ./cmd/airborne

# Production stage
FROM alpine:3.21

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/airborne .

# Copy configs (can be overridden via volume mount)
COPY configs/ /app/configs/

# Create non-root user and data directory
RUN adduser -D -H -s /sbin/nologin airborne && \
    mkdir -p /app/data && \
    chown airborne:airborne /app/data

USER airborne

# Expose gRPC port
EXPOSE 50051

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD /app/airborne --health-check

# Run the server
ENTRYPOINT ["/app/airborne"]
