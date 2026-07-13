# Airborne Makefile

# Build variables
VERSION := $(shell cat VERSION)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags="-w -s -X main.version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)"

PRICING_DB_REF ?= b7cf0ec4e2f5ccae0ee5bb7545a137777e4b2c24
CHASSIS_GO_REF ?= 8601951558c28bb23081af0d5207af7567f607b8
CHASSIS_GO_ADDONS_REF ?= 9bdb354cb37cd4935609444bbec532f5db25e48e

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

.PHONY: all build build-linux build-darwin build-all clean test lint fmt proto deps help run
.DEFAULT_GOAL := build

# Default target
all: proto build

# Build the binary
build:
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

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

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
	@rm -f coverage.out coverage.html

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
docker-build:
	@echo "Building Docker image..."
	@echo "Staging pinned local replace targets into build context..."
	@set -e; \
		rm -rf pricing_db chassis_suite; \
		cleanup() { rm -rf pricing_db chassis_suite; }; \
		trap cleanup EXIT; \
		stage_dep() { \
			repo="$$1"; ref="$$2"; dest="$$3"; \
			git -C "$$repo" cat-file -e "$$ref^{commit}"; \
			mkdir -p "$$dest"; \
			git -C "$$repo" archive --format=tar "$$ref" | tar -x -C "$$dest"; \
		}; \
		stage_dep ../pricing_db "$(PRICING_DB_REF)" pricing_db; \
		stage_dep ../../chassis_suite/chassis-go "$(CHASSIS_GO_REF)" chassis_suite/chassis-go; \
		stage_dep ../../chassis_suite/chassis-go-addons "$(CHASSIS_GO_ADDONS_REF)" chassis_suite/chassis-go-addons; \
		docker build \
			--build-arg VERSION="$(VERSION)" \
			--build-arg GIT_COMMIT="$(GIT_COMMIT)" \
			--build-arg BUILD_TIME="$(BUILD_TIME)" \
			-t airborne:$(VERSION) \
			-t airborne:latest .
	@echo "Cleaned up pinned local replace targets from build context"

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
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  fmt            - Format Go code"
	@echo "  lint           - Lint Go code (requires golangci-lint)"
	@echo "  deps           - Download and tidy dependencies"
	@echo "  clean          - Remove build artifacts"
	@echo "  install-buf    - Install buf protobuf tooling"
	@echo "  proto-lint     - Lint protobuf files"
	@echo "  docker-build   - Build Docker image"
	@echo "  help           - Show this help"
