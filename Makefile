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
BINARY := $(BIN_DIR)/airborne

.PHONY: all build build-linux build-darwin build-all clean test lint fmt proto deps help run

# Default target
all: proto build

# Build the binary
build:
	@echo "Building airborne..."
	@mkdir -p $(BIN_DIR)
	@rm -f $(BINARY)
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
	@echo "Copying pricing_db into build context..."
	@rm -rf pricing_db
	@cp -r ../pricing_db pricing_db
	docker build -t airborne:$(VERSION) .
	@rm -rf pricing_db
	@echo "Cleaned up pricing_db from build context"

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
