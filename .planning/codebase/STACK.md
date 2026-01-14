# Technology Stack

**Analysis Date:** 2026-01-14

## Languages

**Primary:**
- Go 1.25.5 - All application code (`go.mod`)

**Secondary:**
- Protocol Buffers 3 - API definitions (`api/proto/airborne/v1/*.proto`)
- Shell - Build scripts (`scripts/generate-proto.sh`)

## Runtime

**Environment:**
- Go 1.25 (Alpine Linux container for production) - `Dockerfile`
- No browser runtime (server-side only)

**Package Manager:**
- Go modules
- Lockfile: `go.sum` present

## Frameworks

**Core:**
- google.golang.org/grpc v1.78.0 - gRPC server framework (`go.mod`)
- google.golang.org/protobuf v1.36.10 - Protocol buffer runtime (`go.mod`)

**Testing:**
- Go standard `testing` package - Unit and integration tests
- github.com/alicebob/miniredis/v2 v2.35.0 - Redis mocking (`go.mod`)

**Build/Dev:**
- Make - Build automation (`Makefile`)
- Buf - Protocol buffer compilation (`buf.yaml`, `buf.gen.yaml`)
- Docker - Containerization (`Dockerfile`, `docker-compose.yml`)

## Key Dependencies

**Critical:**
- github.com/openai/openai-go v1.12.0 - OpenAI API client (`go.mod`)
- github.com/anthropics/anthropic-sdk-go v1.19.0 - Anthropic Claude client (`go.mod`)
- google.golang.org/genai v1.40.0 - Google Gemini API client (`go.mod`)
- github.com/redis/go-redis/v9 v9.17.2 - Redis client for auth/rate limiting (`go.mod`)

**Infrastructure:**
- gopkg.in/yaml.v3 v3.0.1 - YAML configuration parsing (`go.mod`)
- golang.org/x/crypto v0.46.0 - Cryptographic operations (`go.mod`)
- log/slog - Structured logging (Go standard library)

## Configuration

**Environment:**
- YAML configuration files - `configs/airborne.yaml`
- Environment variable overrides with `AIRBORNE_*` prefix
- Tenant-specific configurations via JSON files
- Required: `AIRBORNE_ADMIN_TOKEN` for static auth mode

**Build:**
- `Makefile` - Build targets (build, test, proto, run, lint)
- `buf.yaml` - Protocol buffer configuration
- `buf.gen.yaml` - Code generation configuration

## Platform Requirements

**Development:**
- macOS/Linux/Windows (any platform with Go 1.25+)
- Optional: Docker for local development stack
- Optional: golangci-lint for linting

**Production:**
- Alpine 3.21 Docker container - `Dockerfile`
- gRPC port: 50051 (configurable via `AIRBORNE_GRPC_PORT`)
- Optional: Redis for rate limiting and auth (static token mode available)
- Optional: Qdrant, Ollama, Docbox for RAG capabilities

---

*Stack analysis: 2026-01-14*
*Update after major dependency changes*
