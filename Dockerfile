# Airborne Dockerfile
# Multi-stage build for minimal production image

# Build stage
FROM golang:1.26.5-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files and local replace targets for layer caching
COPY go.mod go.sum ./
COPY markdown_svc/ ./markdown_svc/
# Local replace targets are staged into the Docker build context by Makefile/CI
# and copied to the absolute paths that resolve from /build/../../...
COPY pricing_db/ /pricing_db/
COPY chassis_suite/chassis-go/ /chassis_suite/chassis-go/
COPY chassis_suite/chassis-go-addons/ /chassis_suite/chassis-go-addons/
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}" \
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
    chown -R airborne:airborne /app/data /app/configs && \
    chmod -R u=rwX,go= /app/configs

USER airborne

# Expose gRPC port
EXPOSE 50612

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD /app/airborne --health-check

# Run the server
ENTRYPOINT ["/app/airborne"]
