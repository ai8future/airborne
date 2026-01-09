# Changelog

All notable changes to this project will be documented in this file.

## [0.6.7] - 2026-01-09

### Fixed
- Docker container permission error when creating admin password
  - Container now creates `/app/data` directory with proper ownership
  - Code checks for `/app/data` before falling back to home directory
  - Added `AIBOX_DATA_DIR` environment variable for custom data path

Agent: Claude:Opus 4.5

## [0.6.6] - 2026-01-09

### Changed
- Admin UI: Removed robot emoji from login page, sidebar, and favicon
- AI Providers menu icon changed from robot to brain emoji

## [0.6.5] - 2026-01-09

### Added
- **Static Auth Mode**: New authentication mode that uses a static admin token
  - No Redis dependency required for authentication
  - Enables fully stateless deployment (3 servers behind LB)
  - Set `AIBOX_AUTH_MODE=static` and `AIBOX_ADMIN_TOKEN` to enable
  - Defaults to static mode for simpler deployment
- `internal/auth/static.go`: StaticAuthenticator with constant-time token comparison
- `AuthConfig.AuthMode` config field: Choose between "static" (default) or "redis"

### Changed
- `docker-compose.yml`: Simplified to single aibox service (no Redis/Qdrant/Ollama)
- Redis is now optional - only required when `AIBOX_AUTH_MODE=redis`
- Rate limiting disabled in static auth mode (rateLimiter is nil)
- AdminService Ready endpoint only includes Redis in dependencies when configured

### Removed
- Redis, Qdrant, and Ollama services from default docker-compose.yml
- Dependency volumes for external services

Agent: Claude:Opus 4.5

## [0.6.4] - 2026-01-09

### Added
- **Admin UI**: Web-based administration interface for managing aibox
  - React frontend (Vite + Tailwind) embedded in Go binary
  - Password-based authentication with bcrypt hashing
  - Session management with configurable TTL (24h default)
  - Dashboard with server status, quick stats, provider status
  - API Key management (list, create, revoke)
  - Tenant configuration viewer
  - Usage statistics with charts
  - AI Provider status pages (OpenAI, Anthropic, Gemini)
- `internal/admin/` package: HTTP server, auth, handlers
- `internal/admin/frontend/`: React admin UI
- `AdminPort` config option: Set port for admin HTTP server (0 to disable)
- `AIBOX_ADMIN_PORT` environment variable
- `KeyStore.ListKeys()`: List all API keys
- `KeyStore.CreateKey()`: Create API key with auto-generated client ID
- `redis.Client.Scan()`: Iterate over keys matching a pattern
- `ClientKey.LastUsed` field: Track last usage time

### Changed
- `server.NewGRPCServer()` now returns `ServerComponents` for use by admin server

Agent: Claude:Opus 4.5

## [0.6.3] - 2026-01-08

### Security
- **SSRF Prevention**: Added comprehensive URL validation for provider base URLs
  - Block dangerous protocols (file://, gopher://, javascript:, data://)
  - Block private/internal IP ranges (10.x, 172.16.x, 192.168.x)
  - Block cloud metadata endpoints (169.254.169.254)
  - Enforce HTTPS for external URLs (HTTP only for localhost)
  - Validation at both service layer and provider client layer
- **Development Mode Hardening**: Removed PermissionAdmin from development auth interceptors
  - Prevents accidental admin access if dev mode enabled in production
  - Added security warnings when development mode is active
- **Secret Path Validation**: Added symlink resolution to prevent path traversal
  - Uses filepath.EvalSymlinks() to resolve symlinks before validation
  - Prevents attacks via symlinks inside allowed directories
- **Error Logging**: Redact provider error details before logging
  - Prevents sensitive information leakage to external logging systems

### Fixed
- **Rate Limiter**: Handle unexpected Redis result types safely
  - Add type coercion for string results from Lua script
  - Log warnings for malformed values instead of silent failure
  - Treat unparseable values as 0 to avoid blocking requests
- **RAG Service**: Handle nil payloads in Qdrant results
  - Add nil checks to getString/getInt helpers
  - Fix chunk positions to match trimmed text after whitespace removal
- **File Upload**: Add 5-minute timeout to upload streams
  - Prevents malicious clients from holding server resources
  - Returns DeadlineExceeded on timeout
- **Provider Clients**: Enforce 3-minute request timeout on all API calls
  - Applied to GenerateReply and GenerateReplyStream in all providers
- **FileService**: Use proper gRPC status codes
  - Return codes.NotFound for missing stores
  - Return codes.Unimplemented for ListFileStores

### Added
- `internal/validation/url.go`: URL validation utilities for SSRF prevention
- Miniredis dependency for Redis unit testing

Agent: Claude:Opus 4.5

## [0.6.2] - 2026-01-08

### Added
- **Chat service helper unit tests**: Added comprehensive test coverage for `internal/service/chat.go`
  - Tests for `hasCustomBaseURL()`: checks for custom base_url in provider configs
  - Tests for `formatRAGContext()`: formats RAG chunks into instruction text
  - Tests for `ragChunksToCitations()`: converts RAG chunks to citation objects with truncation
  - Tests for `prepareRequest()`: validates inputs, selects provider, builds params
    - Input validation: empty/whitespace user input, oversized inputs, invalid request IDs
    - Provider selection: explicit provider, default provider, tenant failover order
    - Security: custom base_url requires admin permission
    - RAG integration: context injection for non-OpenAI, skipped for OpenAI (native support)
  - Tests for `buildProviderConfig()`: merges tenant config with request overrides
  - Tests for `selectProviderWithTenant()`: tenant-aware provider selection
  - Tests for `getFallbackProvider()`: default and specified fallback providers
  - Tests for `convertHistory()` and `mapProviderToProto()` helpers
  - Agent: Claude:Opus 4.5

## [0.6.1] - 2026-01-08

### Added
- **Admin service unit tests**: Added comprehensive test coverage for `internal/service/admin.go`
  - Tests for `Health()` endpoint: returns proper status, no auth required, uptime tracking
  - Tests for `Ready()` endpoint: admin permission required, dependency status reporting, Redis status handling
  - Tests for `Version()` endpoint: admin permission required, all config fields returned correctly
  - Tests for permission handling: admin permission grants access, non-admin permissions denied
  - Tests for edge cases: empty config, nil requests, context cancellation, error types
  - Agent: Claude:Opus 4.5

## [0.6.0] - 2026-01-08

### Added
- **Tenant interceptor unit tests**: Added comprehensive test coverage for `internal/auth/tenant_interceptor.go`
  - Tests for tenant ID extraction from GenerateReplyRequest and SelectProviderRequest
  - Tests for skip methods (Health, Ready, Version, FileService methods)
  - Tests for unary and stream interceptors
  - Tests for single-tenant mode (default tenant) vs multi-tenant mode
  - Tests for tenant resolution errors (missing tenant_id, tenant not found)
  - Tests for tenant ID normalization (lowercase, whitespace trimmed)
  - Tests for TenantFromContext and tenantStream context handling
  - Agent: Claude:Opus 4.5

## [0.5.14] - 2026-01-08

### Added
- **Config loader unit tests**: Added comprehensive test coverage for `internal/config/config.go`
  - Tests for default values when no config file provided
  - Tests for environment variable overrides (TLS, Redis DB, log format, RAG, etc.)
  - Tests for config file read error handling (missing file uses defaults, read errors fail)
  - Tests for YAML parsing errors and validation failures
  - Tests for env var expansion in string fields
  - Agent: Claude:Opus 4.5

## [0.5.13] - 2026-01-08

### Fixed
- **Add unique FileID to prevent RAG point collisions**: File uploads now generate a unique FileID for each upload
  - Previously, point IDs were generated from `filename_storeID_chunkIndex` which caused collisions when uploading multiple files with the same filename to the same store
  - Now generates a cryptographically random FileID (e.g., `file_<32hex>`) per upload
  - FileID is stored in vector point payloads and used to construct unique point IDs
  - Maintains backward compatibility: if FileID is not provided, falls back to old naming scheme
  - Agent: Claude:Opus 4.5

## [0.5.12] - 2026-01-08

### Fixed
- **Normalize tenant_id to lowercase on config load**: Tenant IDs are now normalized (lowercased and trimmed) immediately after loading from config files
  - Previously, mixed-case tenant IDs in config files would fail lookup since resolution uses lowercase
  - Ensures consistent tenant ID matching regardless of case in config files
  - Agent: Claude:Opus 4.5

## [0.5.11] - 2026-01-08

### Fixed
- **SelectProvider permission check**: Added `auth.RequirePermission(ctx, auth.PermissionChat)` to `SelectProvider` RPC
  - Previously any authenticated client could call this endpoint regardless of permissions
  - Now requires chat permission like `GenerateReply`
  - Agent: Claude:Opus 4.5

## [0.5.10] - 2026-01-08

### Fixed
- **Skip tenant interception for FileService RPCs**: Added FileService methods to skipMethods map in tenant interceptor
  - FileService RPCs (CreateFileStore, UploadFile, DeleteFileStore, GetFileStore, ListFileStores) don't include tenant_id in request body
  - These methods already use auth.TenantIDFromContext for tenant scoping
  - Agent: Claude:Opus 4.5

## [0.5.9] - 2026-01-08

### Fixed
- **Rate limiter negative token check**: Added validation to ignore non-positive token counts in `RecordTokens`
  - Prevents gaming the rate limiter by passing negative values to decrement counters
  - Agent: Claude:Opus 4.5

## [0.5.8] - 2026-01-08

### Fixed
- **Apply logging config in main.go**: Logger now uses `cfg.Logging` values (level and format) from configuration
  - Previously logger was hardcoded to JSON/Info level, ignoring loaded config
  - Moved config loading before logger setup so config is available
  - Added `configureLogger()` function to parse level (debug/info/warn/error) and format (text/json)
  - Agent: Claude:Opus 4.5

## [0.5.7] - 2026-01-08

### Fixed
- **Config TLS/logging env overrides**: Added environment variable support for TLS and logging configuration
  - `AIBOX_TLS_ENABLED`, `AIBOX_TLS_CERT_FILE`, `AIBOX_TLS_KEY_FILE` for TLS settings
  - `REDIS_DB` for Redis database selection
  - `AIBOX_LOG_FORMAT` for log format configuration
  - Agent: Claude:Opus 4.5

- **Config file read error handling**: Changed to fail on config read errors (permissions, corruption) while allowing missing files
  - Previously silently ignored all read errors including permission denied
  - Now properly returns error if file exists but cannot be read
  - Agent: Claude:Opus 4.5

## [0.5.6] - 2026-01-08

### Changed
- **Refactored chat service**: Extracted shared request preparation pipeline into `prepareRequest()` helper
  - Reduced ~70 lines of code duplication between `GenerateReply` and `GenerateReplyStream`
  - Single point of truth for validation, provider selection, RAG retrieval, and params building
  - Agent: Claude:Opus 4.5

### Fixed
- **Streaming capability honesty**: Changed `SupportsStreaming()` to return `false` for OpenAI and Gemini providers
  - These providers currently fall back to non-streaming (call GenerateReply and send result as single chunk)
  - Anthropic correctly returns `true` as it has real streaming implementation
  - Agent: Claude:Opus 4.5

- **Tenant config reload path bug**: Fixed `Reload()` to use the effective config directory set during `Load()`
  - Previously `Reload()` would ignore any `configDir` override passed to `Load()`
  - Added `configDir` field to Manager to track the effective directory
  - Agent: Claude:Opus 4.5

### Removed
- **Dead code cleanup**:
  - Removed unused `selectProvider()` function from chat service (superseded by `selectProviderWithTenant()`)
  - Removed unused `debug` field and `WithDebugLogging()` option from all three provider clients
  - Removed unused `ProviderKeys` field from `ClientKey` struct
  - Agent: Claude:Opus 4.5

### Improved
- **RAG payload keys**: Added constants for RAG payload field names (`payloadTenantID`, `payloadText`, etc.)
  - Reduces typo risk and makes schema evolution easier
  - Agent: Claude:Opus 4.5

## [0.5.5] - 2026-01-07

### Added
- **Provider client unit tests**: Comprehensive test coverage for all LLM provider clients
  - OpenAI: buildUserPrompt, mapReasoningEffort, mapServiceTier, isRetryableError, waitForCompletion, extractCitations
  - Anthropic: buildMessages, extractText, isRetryableError, client capabilities
  - Gemini: buildContents, extractText, extractUsage, extractCitations, buildSafetySettings, isRetryableError
  - Agent: Claude:Opus 4.5

- **Tenant module unit tests**: Full coverage for multi-tenancy configuration
  - TenantConfig: GetProvider, DefaultProvider with failover ordering
  - Secrets: loadSecret (ENV/VAR/inline), resolveSecrets, path validation
  - Loader: validateTenantConfig, loadTenants (JSON/YAML), duplicate detection
  - Manager: TenantCodes, TenantCount, Tenant, DefaultTenant, Reload
  - Agent: Claude:Opus 4.5

### Changed
- **Updated audit reports**: Marked provider and tenant test proposals as completed

## [0.5.4] - 2026-01-07

### Changed
- **Cleaned up audit reports**: Removed fixed issues from audit/fix reports and deleted fully-resolved reports
  - Deleted: code-audit-2026-01-07.md (all issues fixed)
  - Deleted: small_code_audit_report_2026-01-07-16.md (all issues fixed)
  - Updated: code-security-audit-2026-01-07.md (8 remaining low/medium issues)
  - Updated: code_audit_report_2026-01-07-15.md (4 remaining low/medium issues)
  - Updated: code_fix_report_2026-01-07-16.md (1 remaining issue)
  - Updated: small_code_fix_report_2026-01-07-16.md (3 remaining issues)
  - Kept unchanged: test and refactor reports (contain future suggestions)
  - Agent: Claude:Opus 4.5

## [0.5.3] - 2026-01-07

### Fixed
- **RAG_ENABLED env can now disable RAG**: Changed env override to use `strconv.ParseBool()` instead of only checking for truthy values
  - Previously `RAG_ENABLED=false` or `RAG_ENABLED=0` had no effect (only `true`/`1` were recognized)
  - Now supports all standard boolean strings: `true`, `false`, `1`, `0`, `t`, `f`, `T`, `F`, `TRUE`, `FALSE`, etc.
  - Agent: Claude:Opus 4.5

## [0.5.2] - 2026-01-07

### Fixed
- **Dev mode auth without Redis**: Injected dev client when Redis is unavailable in non-production mode
  - Added `developmentAuthInterceptor()` for unary requests
  - Added `developmentAuthStreamInterceptor()` for streaming requests
  - Dev client gets all permissions: Admin, Chat, ChatStream, Files
  - Prevents "not authenticated" errors when developing without Redis
  - Agent: Claude:Opus 4.5

## [0.5.1] - 2026-01-07

### Fixed
- **Prevent chunker panic on small text**: Added guard to check if chunks slice is non-empty before accessing
  - Fixed potential panic in `ChunkText` when accessing `chunks[len(chunks)-1]` with empty slice
  - Edge case occurred when first chunk didn't meet MinChunkSize requirement and no chunk was appended
  - Added `TestChunk_SmallTextNoPanic` test to verify the fix
  - Agent: Claude:Opus 4.5

## [0.5.0] - 2026-01-07

### Fixed
- **Use configured RAG TopK instead of hardcoded 5**: Changed `retrieveRAGContext()` to pass `TopK: 0` to the RAG service
  - The RAG service's `Retrieve()` method handles `TopK <= 0` by using the configured `RetrievalTopK` from `ServiceOptions`
  - This allows TopK to be configured at the service level rather than hardcoded in the chat service
  - Agent: Claude:Opus 4.5

## [0.4.15] - 2026-01-07

### Security
- **Validate tenant/store IDs before Qdrant operations**: Added input validation to prevent path manipulation attacks
  - Added `validateCollectionParts()` function with regex-based validation
  - Enforces alphanumeric characters plus underscore/hyphen only
  - Blocks path traversal attacks (e.g., `../admin`) and special characters
  - Maximum length of 128 characters for tenant_id and store_id
  - Validation added to: Ingest, Retrieve, CreateStore, DeleteStore, StoreInfo
  - Added comprehensive unit tests for validation logic
  - Agent: Claude:Opus 4.5

## [0.4.14] - 2026-01-07

### Security
- **SSRF prevention for base_url override**: Restricted custom base_url in provider configs to admin users only
  - Added `hasCustomBaseURL()` helper function to detect custom base_url in requests
  - `GenerateReply` and `GenerateReplyStream` now require `PermissionAdmin` when custom base_url is specified
  - Prevents non-admin clients from redirecting provider requests to arbitrary endpoints
  - Agent: Claude:Opus 4.5

## [0.4.13] - 2026-01-07

### Fixed
- **Container health checks**: Fixed Docker health checks that were failing due to HTTP curl against gRPC port
  - Added `--health-check` flag to aibox binary for proper gRPC health checking
  - Health check connects to AdminService/Health endpoint via gRPC
  - Supports TLS configuration when enabled
  - Updated Dockerfile HEALTHCHECK to use native gRPC health check instead of curl
  - Agent: Claude:Opus 4.5

## [0.4.12] - 2026-01-07

### Security
- **Fixed TPM rate limiting bypass**: Fixed two bypass vectors in token-per-minute (TPM) rate limiting
  - `RecordTokens` now applies default TPM limit when client-specific limit is 0 (was early-returning without checking defaults)
  - Streaming responses (`GenerateReplyStream`) now record token usage on completion (was not recording at all)
  - Added comprehensive unit tests for rate limiter default TPM behavior

## [0.4.11] - 2026-01-07

### Changed
- **Refactored tenant ID resolution**: Consolidated tenant ID extraction into shared `auth.TenantIDFromContext()` helper
  - Moved from duplicated implementations in files.go and chat.go to shared auth package
  - Consistent fallback chain: tenant config -> client ID -> "default"
  - Added unit test for TenantIDFromContext

## [0.4.10] - 2026-01-07

### Added
- **FileService tests**: Added test coverage for size limits and auth requirements
  - Tests for metadata size validation (exceeds 100MB limit)
  - Tests for streaming size enforcement during upload
  - Tests for file exactly at limit (boundary case)
  - Tests for auth requirement on all FileService endpoints

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
