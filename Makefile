# Airborne Makefile

# Build variables
VERSION ?= $(shell cat VERSION 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)"

# Go settings
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod
GOFMT := gofmt

# Directories
BIN_DIR := bin
CMD_DIR := cmd/airborne

.PHONY: all build clean test lint fmt proto deps help run

# Default target
all: proto build

# Build the binary
build:
	@echo "Building airborne..."
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/airborne ./$(CMD_DIR)
	@echo "Built $(BIN_DIR)/airborne"

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
	@rm -rf gen/go
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
	docker build -t airborne:$(VERSION) .

# Help
help:
	@echo "Airborne Makefile targets:"
	@echo ""
	@echo "  all            - Generate protos and build binary (default)"
	@echo "  build          - Build the binary"
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
