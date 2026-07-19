# Airborne Makefile

# Build variables
VERSION := $(shell cat VERSION)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags="-w -s -X main.version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)"

POSTGRES_E2E_IMAGE ?= postgres:16@sha256:be01cf82fc7dbba824acf0a82e150b4b360f3ff93c6631d7844af431e841a95c
REDIS_E2E_IMAGE ?= redis:7.4.5-alpine@sha256:bb186d083732f669da90be8b0f975a37812b15e913465bb14d845db72a4e3e08
E2E_IMAGE ?= airborne:e2e-$(GIT_COMMIT)
E2E_CLI = $(BIN_DIR)/airborne-cli
E2E_PROBE = $(BIN_DIR)/airborne-e2e-probe
E2E_FREEZER = $(BIN_DIR)/airborne-freeze

# Go settings
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod
GOFMT := gofmt

# Directories
BIN_DIR := bin
CMD_DIR := cmd/airborne
BINARY := $(BIN_DIR)/airborne

.PHONY: all build build-linux build-darwin build-all clean preflight test test-fast test-integration test-coverage e2e e2e-tools verify verify-source verify-clean lint fmt proto deps docker-build help run
.DEFAULT_GOAL := build

# Default target
all: proto build

# Build the binary
preflight:
	@echo "Vendoring exact remote pins: chassis-go/v11 v11.3.24; chassis-go-addons v1.2.10; pricing_db v0.0.0-20260703044902-275688ca5718"
	GOWORK=off $(GOMOD) verify
	GOWORK=off $(GOMOD) vendor

build: preflight
	@rm -f $(BINARY)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY) ./$(CMD_DIR)
	@echo "Built $(BINARY)"

# Build for linux/amd64
build-linux:
	@echo "Building airborne for linux/amd64..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY)-linux-amd64 ./$(CMD_DIR)

# Build for darwin/arm64
build-darwin:
	@echo "Building airborne for darwin/arm64..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BINARY)-darwin-arm64 ./$(CMD_DIR)

# Build all platforms and install launcher
build-all: build-linux build-darwin
	cp scripts/launcher.sh $(BINARY)
	chmod +x $(BINARY)

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	@./scripts/generate-proto.sh

# Run the server
run: build
	@echo "Starting airborne server..."
	@$(BIN_DIR)/airborne

# Run the complete Go test suite.
test: test-fast

# Fast unit and contract tests that do not require a Docker daemon.
test-fast: preflight
	@echo "Running fast Go verification..."
	./scripts/test-private-module-cache-prime.sh
	./e2e/tests/test-resolve-docker-host.sh
	./e2e/tests/test-frozen-config-permissions.sh
	./scripts/test-verification-clean.sh
	$(GOTEST) -short -race ./...
	cd markdown_svc/clients/go && $(GOTEST) -short -race ./...

# Required Docker-backed database integration checks; skips are converted to failures.
test-integration: preflight
	@echo "Running required database integration tests..."
	@set -eu; \
		docker_host="$$(./scripts/resolve-docker-host.sh --check)"; \
		DOCKER_HOST="$$docker_host" AIRBORNE_REQUIRE_INTEGRATION=1 $(GOTEST) -count=1 -race ./internal/db

# Atomic all-package coverage with the repository's enforced floor.
test-coverage: preflight
	@echo "Running enforced Go coverage..."
	./scripts/test-go-coverage.sh

# Build the exact current production image, then exercise it against the isolated stack.
e2e: preflight docker-build e2e-tools
	@echo "Running deterministic production-image E2E..."
	POSTGRES_E2E_IMAGE='$(POSTGRES_E2E_IMAGE)' REDIS_E2E_IMAGE='$(REDIS_E2E_IMAGE)' AIRBORNE_E2E_IMAGE='$(E2E_IMAGE)' AIRBORNE_E2E_CLI='$(abspath $(E2E_CLI))' AIRBORNE_E2E_PROBE='$(abspath $(E2E_PROBE))' AIRBORNE_E2E_FREEZER='$(abspath $(E2E_FREEZER))' ./e2e/run.sh

# Build the exact current CLI used by the black-box E2E runner.
e2e-tools:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(E2E_CLI) ./cmd/airborne-cli
	CGO_ENABLED=0 $(GOBUILD) -o $(E2E_PROBE) ./e2e/cmd/probe
	CGO_ENABLED=0 $(GOBUILD) -o $(E2E_FREEZER) ./cmd/airborne-freeze
	@$(E2E_CLI) --help | grep -q '^  health'

# Fail-closed release verification across Go, dashboard, Docker integration, and cleanup.
verify: verify-source test-fast test-integration test-coverage
	@echo "Verifying dashboard test, coverage, lint, build, and browser gates..."
	@if [ ! -d dashboard/node_modules ]; then cd dashboard && npm ci; fi
	./scripts/verify-dashboard.sh
	$(GOCMD) vet ./...
	$(MAKE) e2e
	$(MAKE) clean
	@rm -rf dashboard/playwright-report dashboard/test-results dashboard/coverage e2e/artifacts
	@docker image rm -f $(E2E_IMAGE) >/dev/null 2>&1 || true
	$(MAKE) verify-clean

verify-source:
	@unformatted="$$(git ls-files -z -- '*.go' | xargs -0 gofmt -l)"; \
		if [ -n "$$unformatted" ]; then \
			echo "Go files require formatting:" >&2; \
			printf '%s\n' "$$unformatted" >&2; \
			exit 1; \
		fi
	git diff --check
	git diff --cached --check

verify-clean:
	./scripts/assert-verification-clean.sh


# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Lint code
lint:
	@echo "Linting code..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)
	@rm -f coverage.out coverage.html markdown_svc/clients/go/coverage.out $(E2E_CLI) $(E2E_PROBE) $(E2E_FREEZER)

# Install buf (protobuf tooling)
install-buf:
	@echo "Installing buf..."
	@if ! command -v buf >/dev/null 2>&1; then \
		go install github.com/bufbuild/buf/cmd/buf@latest; \
	else \
		echo "buf already installed"; \
	fi

# Validate protobuf files
proto-lint:
	@echo "Linting protobuf files..."
	buf lint

# Docker build
docker-build: preflight
	@echo "Building Docker image..."
	docker build \
			--build-arg VERSION="$(VERSION)" \
			--build-arg GIT_COMMIT="$(GIT_COMMIT)" \
			--build-arg BUILD_TIME="$(BUILD_TIME)" \
			-t airborne:$(VERSION) \
			-t airborne:latest \
			-t $(E2E_IMAGE) .

# Help
help:
	@echo "Airborne Makefile targets:"
	@echo ""
	@echo "  all            - Generate protos and build binary (default)"
	@echo "  build          - Build the binary"
	@echo "  build-linux    - Build for linux/amd64"
	@echo "  build-darwin   - Build for darwin/arm64"
	@echo "  build-all      - Build all platforms with launcher"
	@echo "  proto          - Generate protobuf code"
	@echo "  run            - Build and run the server"
	@echo "  test           - Run fast Go tests (compatibility alias)"
	@echo "  test-fast      - Run fast Go and resolver tests"
	@echo "  test-integration - Run required Docker-backed DB tests"
	@echo "  test-coverage  - Run enforced Go coverage gate"
	@echo "  e2e            - Build exact production image and E2E tools, then run deterministic E2E"
	@echo "  verify         - Run all release gates and clean generated artifacts"
	@echo "  fmt            - Format Go code"
	@echo "  lint           - Lint Go code (requires golangci-lint)"
	@echo "  deps           - Download and tidy dependencies"
	@echo "  clean          - Remove build artifacts"
	@echo "  install-buf    - Install buf protobuf tooling"
	@echo "  proto-lint     - Lint protobuf files"
	@echo "  docker-build   - Build Docker image"
	@echo "  help           - Show this help"
