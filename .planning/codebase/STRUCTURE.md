# Codebase Structure

**Analysis Date:** 2026-01-14

## Directory Layout

```
airborne/
├── api/
│   └── proto/
│       └── airborne/v1/      # gRPC service definitions
├── cmd/
│   └── airborne/             # Binary entry point
├── configs/                  # Configuration files
├── deployments/
│   ├── docker/              # Docker deployment configs
│   └── systemd/             # Systemd service files
├── docs/
│   └── plans/               # Development plans
├── gen/
│   └── go/
│       └── airborne/v1/     # Generated gRPC code
├── internal/
│   ├── auth/                # Authentication & rate limiting
│   ├── config/              # Configuration loading
│   ├── errors/              # Error handling utilities
│   ├── metrics/             # Metrics (placeholder)
│   ├── provider/            # LLM provider abstraction
│   │   ├── openai/         # OpenAI implementation
│   │   ├── gemini/         # Google Gemini implementation
│   │   └── anthropic/      # Anthropic Claude implementation
│   ├── rag/                 # RAG subsystem
│   │   ├── chunker/        # Document chunking
│   │   ├── embedder/       # Embedding generation
│   │   ├── extractor/      # Document extraction
│   │   ├── vectorstore/    # Vector database
│   │   └── testutil/       # Test mocks
│   ├── redis/               # Redis client wrapper
│   ├── server/              # gRPC server setup
│   ├── service/             # Business logic services
│   ├── tenant/              # Multi-tenancy management
│   └── validation/          # Input validation
├── pkg/
│   └── client/
│       └── go/              # Go client SDK (placeholder)
├── scripts/                 # Build scripts
├── Dockerfile               # Container build
├── docker-compose.yml       # Local development stack
├── Makefile                 # Build automation
├── go.mod                   # Go module definition
├── go.sum                   # Dependency checksums
├── VERSION                  # Current version (0.6.9)
└── CHANGELOG.md             # Release history
```

## Directory Purposes

**api/proto/airborne/v1/**
- Purpose: gRPC service and message definitions
- Contains: `airborne.proto`, `files.proto`, `admin.proto`, `common.proto`
- Key files: Service definitions for AIBox, File, Admin services
- Subdirectories: None

**cmd/airborne/**
- Purpose: Application entry point
- Contains: `main.go` - CLI parsing, server startup, signal handling
- Key files: Single entry point binary
- Subdirectories: None

**gen/go/airborne/v1/**
- Purpose: Generated gRPC Go code
- Contains: `*.pb.go` (messages), `*_grpc.pb.go` (service stubs)
- Key files: `airborne.pb.go`, `airborne_grpc.pb.go`
- Subdirectories: None (generated)

**internal/auth/**
- Purpose: Authentication, authorization, rate limiting
- Contains: Interceptors, key store, rate limiter, tenant context
- Key files: `interceptor.go`, `static.go`, `keys.go`, `ratelimit.go`
- Subdirectories: None

**internal/config/**
- Purpose: Configuration loading and validation
- Contains: Config struct, YAML parsing, environment overrides
- Key files: `config.go`, `startup_mode.go`
- Subdirectories: None

**internal/provider/**
- Purpose: LLM provider abstraction layer
- Contains: Provider interface and implementations
- Key files: `provider.go` (interface)
- Subdirectories: `openai/`, `gemini/`, `anthropic/` (implementations)

**internal/rag/**
- Purpose: Retrieval-Augmented Generation pipeline
- Contains: Service orchestration and component interfaces
- Key files: `service.go` (main RAG service)
- Subdirectories: `chunker/`, `embedder/`, `extractor/`, `vectorstore/`, `testutil/`

**internal/service/**
- Purpose: gRPC service implementations
- Contains: Business logic for all services
- Key files: `chat.go`, `files.go`, `admin.go`
- Subdirectories: None

**internal/tenant/**
- Purpose: Multi-tenant configuration management
- Contains: Tenant config loading, secrets, environment parsing
- Key files: `manager.go`, `config.go`, `loader.go`, `secrets.go`
- Subdirectories: None

## Key File Locations

**Entry Points:**
- `cmd/airborne/main.go` - Application entry, CLI flags, server startup

**Configuration:**
- `configs/airborne.yaml` - Default configuration
- `internal/config/config.go` - Config struct and loading
- `internal/tenant/config.go` - Per-tenant configuration

**Core Logic:**
- `internal/service/chat.go` - GenerateReply/Stream implementation
- `internal/service/files.go` - File upload and store management
- `internal/provider/provider.go` - LLM provider interface
- `internal/rag/service.go` - RAG orchestration

**Testing:**
- `internal/*/_test.go` - Co-located unit tests
- `internal/rag/testutil/` - Mock implementations

**Documentation:**
- `README.md` - Project overview (if present)
- `CHANGELOG.md` - Release history
- `docs/plans/` - Development planning documents

## Naming Conventions

**Files:**
- snake_case.go for Go source files
- *_test.go for test files (Go standard)
- *.proto for protocol buffer definitions
- *.pb.go for generated protobuf code
- *_grpc.pb.go for generated gRPC stubs

**Directories:**
- lowercase for all directories
- Singular names for packages (auth, config, service)
- Plural for collections within (providers would be under provider/)

**Special Patterns:**
- client.go in provider subdirs for implementation
- service.go for main orchestration logic
- manager.go for state management

## Where to Add New Code

**New LLM Provider:**
- Primary code: `internal/provider/{provider-name}/client.go`
- Interface: Implement `provider.Provider` from `internal/provider/provider.go`
- Tests: `internal/provider/{provider-name}/client_test.go`

**New gRPC Service:**
- Definition: `api/proto/airborne/v1/{service}.proto`
- Implementation: `internal/service/{service}.go`
- Registration: `internal/server/grpc.go`
- Tests: `internal/service/{service}_test.go`

**New RAG Component:**
- Interface: `internal/rag/{component}/{component}.go`
- Implementation: Same directory
- Tests: Same directory with `_test.go` suffix

**Configuration Options:**
- Struct fields: `internal/config/config.go`
- Environment parsing: Same file, `applyEnvOverrides()` method
- YAML example: `configs/airborne.yaml`

**Utilities:**
- Validation: `internal/validation/`
- Error handling: `internal/errors/`
- Redis operations: `internal/redis/`

## Special Directories

**gen/**
- Purpose: Auto-generated code from protobuf
- Source: Generated by `buf generate` via `scripts/generate-proto.sh`
- Committed: Yes (for builds without protoc)
- Note: Do not edit manually

**internal/**
- Purpose: Private packages (Go convention)
- Contains: All application logic
- Note: Cannot be imported by external packages

**deployments/**
- Purpose: Deployment configuration
- Contains: Docker and systemd files
- Note: Not used during development

---

*Structure analysis: 2026-01-14*
*Update when directory structure changes*
