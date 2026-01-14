# Architecture

**Analysis Date:** 2026-01-14

## Pattern Overview

**Overall:** Layered gRPC Microservice with Multi-Provider LLM Gateway

**Key Characteristics:**
- Unified LLM access via gRPC (OpenAI, Gemini, Anthropic)
- Optional RAG (Retrieval-Augmented Generation) capabilities
- Multi-tenant with per-tenant configuration
- Stateless service (state in Redis/external services)

## Layers

**Transport Layer:**
- Purpose: gRPC service endpoints and protocol handling
- Contains: Service definitions, interceptor chains, TLS configuration
- Location: `api/proto/`, `gen/go/`, `internal/server/grpc.go`
- Depends on: Service layer
- Used by: External gRPC clients

**Service Layer:**
- Purpose: Business logic orchestration
- Contains: ChatService, FileService, AdminService implementations
- Location: `internal/service/*.go`
- Depends on: Provider abstraction, RAG service, Auth
- Used by: Transport layer

**Provider Layer:**
- Purpose: Unified interface to LLM providers
- Contains: Provider interface, OpenAI/Gemini/Anthropic clients
- Location: `internal/provider/*.go`, `internal/provider/*/client.go`
- Depends on: External LLM SDKs, validation
- Used by: Service layer

**RAG Layer:**
- Purpose: Retrieval-augmented generation pipeline
- Contains: Document ingestion, embedding, vector search
- Location: `internal/rag/*.go`, `internal/rag/*/`
- Depends on: Ollama, Qdrant, Docbox services
- Used by: Service layer (optional)

**Auth Layer:**
- Purpose: Authentication, authorization, rate limiting
- Contains: Interceptors, key store, rate limiter
- Location: `internal/auth/*.go`
- Depends on: Redis (optional)
- Used by: Transport layer interceptors

**Infrastructure Layer:**
- Purpose: Shared utilities and configuration
- Contains: Config loading, Redis client, error handling, validation
- Location: `internal/config/`, `internal/redis/`, `internal/errors/`, `internal/validation/`
- Depends on: External services
- Used by: All layers

## Data Flow

**Chat Generation Flow (GenerateReply RPC):**

1. gRPC request received at transport layer
2. Recovery interceptor catches panics → `internal/server/grpc.go`
3. Logging interceptor logs request metadata
4. Tenant interceptor extracts tenant context → `internal/auth/tenant_interceptor.go`
5. Auth interceptor validates API key/token → `internal/auth/interceptor.go`
6. ChatService.GenerateReply() called → `internal/service/chat.go`
7. Request validation (sizes, URLs, metadata) → `internal/validation/`
8. Provider selection (tenant config or default) → `internal/service/chat.go:selectProviderWithTenant()`
9. RAG retrieval if enabled → `internal/rag/service.go:Retrieve()`
10. Provider.GenerateReply() calls external LLM → `internal/provider/*/client.go`
11. Response assembled with usage metrics, citations
12. gRPC response returned

**File Upload Flow (UploadFile Stream RPC):**

1. Client stream received at FileService → `internal/service/files.go`
2. Permission check (PermissionFile required)
3. Stream chunks assembled into file content
4. RAG Service processes file → `internal/rag/service.go:Ingest()`
5. Extractor extracts text → `internal/rag/extractor/docbox.go`
6. Chunker splits into overlapping segments → `internal/rag/chunker/chunker.go`
7. Embedder generates embeddings → `internal/rag/embedder/ollama.go`
8. VectorStore saves to Qdrant → `internal/rag/vectorstore/qdrant.go`
9. Response with file ID and store info returned

**State Management:**
- Stateless service design
- Configuration from YAML files and environment
- Rate limit state in Redis (optional)
- Vector embeddings in Qdrant (for RAG)

## Key Abstractions

**Provider Interface:**
- Purpose: Unified API for all LLM providers
- Location: `internal/provider/provider.go`
- Pattern: Strategy pattern
- Examples: OpenAI client, Gemini client, Anthropic client
- Methods: GenerateReply, GenerateReplyStream, SupportsFileSearch, SupportsWebSearch

**Service Pattern:**
- Purpose: gRPC service implementations
- Location: `internal/service/*.go`
- Pattern: Facade over domain logic
- Examples: ChatService, FileService, AdminService
- Embeds: pb.Unimplemented*Server for gRPC compatibility

**Interceptor Chain:**
- Purpose: Cross-cutting concerns (auth, logging, recovery)
- Location: `internal/server/grpc.go`, `internal/auth/*.go`
- Pattern: Chain of responsibility
- Order: Recovery → Logging → Tenant → Auth

**RAG Pipeline:**
- Purpose: Document ingestion and retrieval
- Location: `internal/rag/`
- Pattern: Pipeline with pluggable components
- Components: Extractor → Chunker → Embedder → VectorStore

**Tenant Manager:**
- Purpose: Multi-tenant configuration management
- Location: `internal/tenant/manager.go`
- Pattern: Registry with thread-safe access
- Features: Per-tenant API keys, models, providers

## Entry Points

**CLI Entry:**
- Location: `cmd/airborne/main.go`
- Triggers: Binary execution
- Responsibilities: Parse flags, load config, start gRPC server, handle signals
- Health check mode: `--health-check` flag for liveness probes

**gRPC Services:**
- AIBoxService: `GenerateReply`, `GenerateReplyStream`, `SelectProvider`
- FileService: `CreateFileStore`, `UploadFile`, `DeleteFileStore`, `GetFileStore`, `ListFileStores`
- AdminService: `Health`, `Ready`, `Version`

## Error Handling

**Strategy:** Throw errors up, sanitize at boundary, log internally

**Patterns:**
- Wrapped errors with `fmt.Errorf("context: %w", err)` throughout
- Error sanitization for clients → `internal/errors/sanitize.go`
- gRPC status codes for structured errors
- Recovery interceptor catches panics at transport layer

**Error Types:**
- Validation errors returned immediately (fail fast)
- Provider errors wrapped with context
- Auth errors as gRPC Unauthenticated/PermissionDenied codes
- Internal errors logged server-side, generic message to client

## Cross-Cutting Concerns

**Logging:**
- log/slog for structured logging
- Request ID propagation via context
- Levels: debug, info, warn, error
- Format: JSON or text (configurable)

**Validation:**
- Request size limits → `internal/validation/limits.go`
- URL validation (SSRF prevention) → `internal/validation/url.go`
- Metadata validation at service boundary

**Authentication:**
- Static token mode (default) → `internal/auth/static.go`
- Redis-backed API keys → `internal/auth/keys.go`
- Permission system: Chat, File, Admin

**Rate Limiting:**
- Three tiers: Requests/Min, Requests/Day, Tokens/Min
- Per-client tracking via Redis
- Configurable limits per auth mode

---

*Architecture analysis: 2026-01-14*
*Update when major patterns change*
