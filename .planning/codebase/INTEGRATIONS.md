# External Integrations

**Analysis Date:** 2026-01-14

## APIs & External Services

**LLM Providers:**

- **OpenAI** - Primary LLM provider with Responses API
  - SDK/Client: github.com/openai/openai-go v1.12.0
  - Auth: API key in tenant config or `OPENAI_API_KEY`
  - Implementation: `internal/provider/openai/client.go`
  - Features: File search, web search, conversation continuity (response_id)
  - Default model: gpt-4o (configurable)
  - BaseURL override support for custom endpoints

- **Google Gemini** - Secondary LLM provider
  - SDK/Client: google.golang.org/genai v1.40.0
  - Auth: API key in tenant config or `GEMINI_API_KEY`
  - Implementation: `internal/provider/gemini/client.go`
  - Features: File search (FileSearchStore), Google Search grounding
  - Default model: gemini-2.0-flash (configurable)
  - BaseURL override support

- **Anthropic Claude** - Tertiary LLM provider
  - SDK/Client: github.com/anthropics/anthropic-sdk-go v1.19.0
  - Auth: API key in tenant config or `ANTHROPIC_API_KEY`
  - Implementation: `internal/provider/anthropic/client.go`
  - Features: Streaming support, full conversation history
  - Default model: claude-sonnet-4-20250514 (configurable)
  - BaseURL override support

## Data Storage

**Databases:**
- None (stateless service, configuration file-based)

**Caching/State:**
- **Redis** - Session management and rate limiting
  - Client: github.com/redis/go-redis/v9 v9.17.2
  - Implementation: `internal/redis/client.go`
  - Auth: `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB` env vars
  - Connection Pool: 10 connections, 2 minimum idle
  - Default: localhost:6379
  - Optional: Can use static auth mode without Redis

## RAG Services

**Embedding Service:**
- **Ollama** - Text embedding generation
  - Protocol: HTTP REST API
  - Implementation: `internal/rag/embedder/ollama.go`
  - URL: Configurable via `RAG_OLLAMA_URL` (default: http://localhost:11434)
  - Models: nomic-embed-text (768 dims), mxbai-embed-large (1024), bge-m3, bge-large-en-v1.5

**Vector Database:**
- **Qdrant** - Vector storage and similarity search
  - Protocol: HTTP REST API
  - Implementation: `internal/rag/vectorstore/qdrant.go`
  - URL: Configurable via `RAG_QDRANT_URL` (default: http://localhost:6333)
  - Distance: Cosine similarity
  - Multi-tenant: Collection-based isolation

**Document Extraction:**
- **Docbox** - Document text extraction (Pandoc-based)
  - Protocol: HTTP API
  - Implementation: `internal/rag/extractor/docbox.go`
  - URL: Configurable via `RAG_DOCBOX_URL` (default: http://localhost:41273)
  - Formats: PDF, DOCX, and other document types

## Authentication & Identity

**Auth Modes:**
- **Static Token** (default) - Simple admin token authentication
  - Implementation: `internal/auth/static.go`
  - Token: `AIRBORNE_ADMIN_TOKEN` env var
  - No Redis dependency

- **Redis-backed Auth** - API key management with permissions
  - Implementation: `internal/auth/keys.go`
  - Requires Redis connection
  - Enable via `AIRBORNE_AUTH_MODE=redis`

**Rate Limiting:**
- Implementation: `internal/auth/ratelimit.go`
- Three tiers: Requests/Min, Requests/Day, Tokens/Min
- Requires Redis when enabled

## Monitoring & Observability

**Logging:**
- log/slog (Go standard library structured logging)
- Levels: debug, info, warn, error
- Format: JSON or text (configurable)

**Error Tracking:**
- Not detected (no Sentry or similar)

**Metrics:**
- `internal/metrics/` package exists (placeholder)
- Token usage tracked per request

## CI/CD & Deployment

**Containerization:**
- Docker multi-stage build - `Dockerfile`
- Alpine 3.21 base image
- Health check via gRPC health endpoint

**Orchestration:**
- Docker Compose - `docker-compose.yml`
- Systemd service files - `deployments/systemd/`

**Build:**
- Makefile targets: build, test, proto, run, lint
- Protocol buffers: `scripts/generate-proto.sh`

## Environment Configuration

**Development:**
- Required: None (static auth mode works standalone)
- Optional: Redis, Qdrant, Ollama, Docbox for full features
- Config: `configs/airborne.yaml`

**Production:**
- Required: `AIRBORNE_ADMIN_TOKEN` (or Redis for dynamic auth)
- Required for RAG: `RAG_ENABLED=true`, Qdrant, Ollama, Docbox
- LLM API keys per tenant or via environment

**Key Environment Variables:**
- `AIRBORNE_GRPC_PORT` - gRPC listen port (default: 50051)
- `AIRBORNE_ADMIN_TOKEN` - Static auth token
- `AIRBORNE_AUTH_MODE` - "static" (default) or "redis"
- `RAG_ENABLED` - Enable RAG subsystem
- `RAG_OLLAMA_URL` - Ollama embedding service
- `RAG_QDRANT_URL` - Qdrant vector database
- `RAG_DOCBOX_URL` - Docbox extraction service

## Webhooks & Callbacks

**Incoming:**
- None (gRPC-only service)

**Outgoing:**
- LLM provider API calls (streaming and unary)
- RAG service calls (Qdrant, Ollama, Docbox)

---

*Integration audit: 2026-01-14*
*Update when adding/removing external services*
