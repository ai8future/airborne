# Airborne Dockerfile
# Multi-stage build for minimal production image

# Build stage
FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder

WORKDIR /build

# Copy the host-authenticated, checksum-verified module boundary first
COPY go.mod go.sum ./
COPY vendor/ ./vendor/

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor \
    -ldflags "-X main.version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o airborne ./cmd/airborne

# Production stage
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

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
