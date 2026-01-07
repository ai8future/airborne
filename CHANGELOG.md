# Changelog

All notable changes to this project will be documented in this file.

## [0.4.9] - 2026-01-07

### Added
- **Auth unit tests**: Added comprehensive test coverage for auth package
  - `keys_test.go`: Tests for parseAPIKey, generateRandomString, HasPermission
  - `interceptor_test.go`: Tests for extractAPIKey, RequirePermission, ClientFromContext

## [0.4.8] - 2026-01-07

### Security
- **API key security**: Removed API key override from request body in ChatService
  - API keys must now come from server-side tenant configuration only
  - Prevents clients from bypassing tenant-configured providers

## [0.4.7] - 2026-01-07

### Security
- **AdminService authorization**: Added `auth.RequirePermission(PermissionAdmin)` check to Ready and Version endpoints
- Removed `/aibox.v1.AdminService/Version` from auth skip list - now requires authentication

## [0.4.6] - 2026-01-07

### Security
- **Path traversal protection**: Added validation for FILE= secret paths to prevent arbitrary file reads
  - Paths must be within allowed directories: `/etc/aibox/secrets`, `/run/secrets`, `/var/run/secrets`
  - Rejects paths containing `..` traversal sequences

## [0.4.5] - 2026-01-07

### Security
- **FileService authorization**: Added `auth.RequirePermission(PermissionFiles)` check to all FileService endpoints
- **FileService tenant isolation**: Fixed tenant isolation by using `tenantIDFromContext()` instead of hardcoded "default"
- **File upload size limits**: Added 100MB upload limit with validation at both metadata and chunk levels

### Changed
- FileService now properly derives tenant ID from authenticated client context
- Tests updated to use auth context with file permissions

## [0.4.4] - 2026-01-07

### Added
- Patch-ready diffs for all 15 security issues in audit report
  - Each finding now includes copy-paste ready unified diff format
  - Appendix B with instructions for applying patches via `git apply`
  - Proposed design for key rotation mechanism (issue #13)

## [0.4.3] - 2026-01-07

### Added
- Comprehensive security audit report in `_codex/security-audit-2026-01-07.md`
  - Identified 15 security issues (3 critical, 2 high, 5 medium, 5 low)
  - Critical: FileService missing auth checks, arbitrary file read via FILE= prefix, dev mode auth bypass
  - High: Hardcoded tenant ID in FileService, API keys accepted in request body
  - Full remediation priority list with timelines
  - Positive security observations documented

## [0.4.2] - 2026-01-05

### Added
- **RAG integration into GenerateReply**: Non-OpenAI providers (Gemini, Anthropic) now use self-hosted RAG for file search
  - RAG context retrieved from Qdrant and injected into system prompt for Gemini/Anthropic
  - OpenAI continues to use native `file_search` tool
  - RAG citations automatically included in response for non-OpenAI providers
- RAG support for GenerateReplyStream endpoint with citation streaming

### Changed
- ChatService now accepts optional RAG service in constructor
- Server initialization reordered to create RAG service before ChatService
- RAG retrieval fails gracefully with warning log (continues without context)

## [0.4.1] - 2026-01-05

### Added
- Comprehensive unit test suite for RAG package:
  - `chunker_test.go`: 20+ test cases for text chunking (97.9% coverage)
  - `ollama_test.go`: HTTP mocking tests for Ollama embedder (95.1% coverage)
  - `qdrant_test.go`: HTTP mocking tests for Qdrant vector store (88.7% coverage)
  - `docbox_test.go`: HTTP mocking tests for Docbox extractor (85.7% coverage)
  - `service_test.go`: RAG service orchestration tests (93.2% coverage)
  - `testutil/mocks.go`: Configurable mocks for Embedder, Store, and Extractor interfaces
- FileService gRPC unit tests with streaming upload support

### Fixed
- MockEmbedder field/method name conflict (Model -> ModelName)
- ServiceOptions default handling for ChunkOverlap

## [0.4.0] - 2026-01-05

### Added
- **Self-hosted RAG (Retrieval-Augmented Generation)**: Provider-agnostic file search using Qdrant and Ollama
- New `internal/rag` package with modular architecture:
  - `embedder`: Interface and Ollama implementation for text embeddings
  - `vectorstore`: Interface and Qdrant implementation for vector storage
  - `extractor`: Interface and Docbox/Pandoc implementation for text extraction
  - `chunker`: Text chunking with overlap and smart boundary detection
  - `service`: RAG orchestrator for ingest and retrieval operations
- Complete FileService gRPC implementation for file store management
- Docker Compose configuration with Qdrant and Ollama services
- RAG configuration section in `configs/aibox.yaml`
- Environment variable overrides for RAG settings (`RAG_ENABLED`, `RAG_OLLAMA_URL`, etc.)

### Changed
- FileService now uses self-hosted Qdrant instead of provider-specific vector stores
- Server initialization includes optional RAG service registration when enabled

### Infrastructure
- Added `docker-compose.yml` with Redis, Qdrant, and Ollama services
- Added `Dockerfile` for multi-stage Alpine build

## [0.3.0] - 2026-01-03

### Added
- **Multitenancy support**: AIBox can now serve multiple tenants with isolated configurations
- New `internal/tenant` package with TenantManager for per-tenant configuration
- Per-tenant provider API keys and settings via `configs/{tenant_id}.json`
- `tenant_id` field in `GenerateReplyRequest` and `SelectProviderRequest` proto messages
- TenantInterceptor for validating and injecting tenant config into gRPC context
- Secret resolution with `ENV=` and `FILE=` prefixes for API keys
- Hot-reload support for tenant configurations (SIGHUP)
- Tenant-scoped Redis key prefixes for data isolation
- Backwards-compatible single-tenant mode when only one tenant is configured

### Changed
- ChatService now uses tenant config for provider selection and credentials
- KeyStore supports tenant-scoped key prefixes via `NewTenantKeyStore()`
- gRPC server logs tenant count on startup

## [0.2.0] - 2026-01-02

### Security
- **BREAKING**: Server now requires Redis in production mode to prevent authentication bypass
- Add input size validation to prevent DoS attacks (100KB user input, 50KB instructions, 100 history messages)
- Fix rate limiting race condition with atomic Lua script
- Sanitize error messages to prevent information leakage
- Validate request IDs to prevent log injection attacks

### Added
- `startup_mode` configuration option (`production`/`development`)
- `AIBOX_STARTUP_MODE` environment variable override
- Input validation package with size limits
- Error sanitization package
- Request ID validation and generation

### Changed
- Rate limiter uses atomic Redis Lua scripts instead of separate commands

### Removed
- Unused `extractTextFromValue` function from Anthropic provider

## [0.1.0] - 2026-01-02

### Added
- Initial AIBox gRPC service definitions
- Core infrastructure with proto files for aibox, admin, files, and common services
- Go implementation with provider system (OpenAI, Anthropic, Gemini)
- Authentication system with API keys and rate limiting
- Redis client integration
- Configuration management
- gRPC server implementation
