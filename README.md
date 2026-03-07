# Airborne

A multi-provider AI gateway that exposes a unified gRPC API for routing LLM requests across 20+ providers. Handles multi-tenancy, authentication, rate limiting, RAG, image generation, conversation persistence, and automatic failover — so consumers talk to one API regardless of the underlying model or vendor.

## Supported Providers

| Tier | Providers |
|------|-----------|
| **Core** | OpenAI, Gemini, Anthropic |
| **Tier 1** | DeepSeek, Grok/xAI, Mistral, Perplexity |
| **Tier 2** | Cohere, Bedrock*, Watsonx*, Databricks* |
| **Tier 3** | Together AI, Fireworks AI, OpenRouter, DeepInfra, Hyperbolic |
| **Tier 4** | Nebius, Cerebras, Upstage, HuggingFace*, MiniMax* |

*Providers marked with \* are defined in the proto schema but not yet implemented.*

Core providers use native SDKs. Most others use a shared OpenAI-compatible client layer, making it trivial to add new providers that expose an OpenAI-style API.

## Architecture

```
                     ┌──────────────────────────────────────┐
                     │          gRPC Server (:50612)         │
                     │                                      │
  Clients ──gRPC──►  │  Recovery → Tracing → Metrics →      │
                     │  Logging → TenantInterceptor → Auth  │
                     │                                      │
                     │  ┌─────────────┐ ┌───────────────┐   │
                     │  │AirborneService│ │ AdminService  │   │
                     │  │  GenerateReply│ │ Health/Ready  │   │
                     │  │  Stream      │ │ Version       │   │
                     │  │  SelectProvider│└───────────────┘   │
                     │  └──────┬──────┘ ┌───────────────┐   │
                     │         │        │ FileService    │   │
                     │         ▼        │ (RAG stores)   │   │
                     │  ┌─────────────┐ └───────────────┘   │
                     │  │Provider Router│                    │
                     │  └──────┬──────┘                     │
                     └─────────┼────────────────────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          ▼                    ▼                     ▼
   ┌────────────┐    ┌──────────────┐     ┌──────────────┐
   │  OpenAI    │    │   Gemini     │     │  Anthropic   │
   │  (native)  │    │  (native)    │     │  (native)    │
   └────────────┘    └──────────────┘     └──────────────┘
          ▼
   ┌────────────────────────────────────────────────────┐
   │      OpenAI-Compatible Layer (compat.Client)       │
   │  DeepSeek, Grok, Mistral, Perplexity, Together,   │
   │  Fireworks, OpenRouter, DeepInfra, Cohere,         │
   │  Hyperbolic, Nebius, Cerebras, Upstage             │
   └────────────────────────────────────────────────────┘
```

### Supporting Services

| Service | Purpose |
|---------|---------|
| **PostgreSQL** | Conversation threads, messages, activity feed (tenant-isolated tables) |
| **Redis** | API key storage, bcrypt verification, rate limiting (Lua scripts) |
| **Ollama** | Embedding generation for self-hosted RAG |
| **Qdrant** | Vector search for RAG document retrieval |
| **Docbox** | PDF/document text extraction (Pandoc-backed) |
| **Doppler** | Secrets management for tenant API keys |
| **markdown_svc** | Renders AI response markdown to HTML |

## gRPC API

### AirborneService

| RPC | Type | Description |
|-----|------|-------------|
| `GenerateReply` | Unary | Send a prompt, get a complete response |
| `GenerateReplyStream` | Server-stream | Send a prompt, receive response tokens as they arrive |
| `SelectProvider` | Unary | Determine which provider will handle a request based on content triggers, continuity, and user tier |

Requests support: provider/model override, system prompts, conversation history, file search (RAG), web search, code execution, tool calling, structured output (JSON schema), inline images, temperature/top-p/max-tokens tuning.

### AdminService

| RPC | Description |
|-----|-------------|
| `Health` | Liveness check (no auth required) |
| `Ready` | Dependency readiness (Redis, Qdrant, Ollama) |
| `Version` | Build version, git commit, build time, Go version |

### FileService (requires RAG)

| RPC | Description |
|-----|-------------|
| `CreateFileStore` | Create a vector store (OpenAI, Gemini, or Qdrant) |
| `UploadFile` | Client-streaming file upload to a store |
| `DeleteFileStore` | Remove a store |
| `GetFileStore` / `ListFileStores` | Retrieve store metadata |

### HTTP Admin Server (optional, default port 8473)

Endpoints: `/admin/activity`, `/admin/debug/{id}`, `/admin/thread/{id}`, `/admin/health`, `/admin/healthz`, `/admin/version`, `/admin/test`, `/admin/chat`, `/admin/upload`

## Multi-Tenancy

Each tenant gets isolated configuration:

- **Provider API keys and models** with per-provider temperature, system prompts, and failover order
- **Rate limits** (requests/min, requests/day, tokens/min)
- **Image generation** settings (provider, triggers, models)
- **Database isolation** via tenant-prefixed tables (e.g. `ai8_airborne_threads`)

Tenant configs load from YAML/JSON files, Doppler API, or a frozen config snapshot.

## Authentication

Two modes controlled by `AIRBORNE_AUTH_MODE`:

**Static** (`static`) — A single bearer token (`AIRBORNE_ADMIN_TOKEN`) compared via constant-time comparison. Suitable for development and single-consumer deployments.

**Redis** (`redis`) — API keys in the format `airborne_sk_{keyID}_{secret}`. Keys are stored in Redis with bcrypt-hashed secrets, expiry dates, per-client permissions (`chat`, `chat:stream`, `files`, `admin`), and rate limit enforcement via atomic Lua scripts.

## Getting Started

### Prerequisites

- Go 1.25+
- At least one provider API key (e.g. `OPENAI_API_KEY`)
- [buf](https://buf.build) (for proto generation, optional)

### Build

```bash
make build          # builds bin/airborne
make proto          # regenerates protobuf code
make test           # runs tests with race detection
make test-coverage  # generates HTML coverage report
```

### Run Locally

```bash
# Minimal setup with static auth
export AIRBORNE_ADMIN_TOKEN="your-token"
export OPENAI_API_KEY="sk-..."
make run
```

The gRPC server starts on port **50612** by default.

### Docker

```bash
# Set env vars in your shell or .env file
make docker-build
docker compose up
```

### Configuration

Primary config file: `configs/airborne.yaml`

Key environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `AIRBORNE_GRPC_PORT` | `50612` | gRPC listen port |
| `AIRBORNE_HOST` | `0.0.0.0` | Bind address |
| `AIRBORNE_AUTH_MODE` | `static` | `static` or `redis` |
| `AIRBORNE_ADMIN_TOKEN` | — | Bearer token for static auth |
| `AIRBORNE_STARTUP_MODE` | `production` | `production` or `development` |
| `AIRBORNE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `AIRBORNE_TLS_ENABLED` | `false` | Enable TLS on gRPC server |
| `DATABASE_ENABLED` | `false` | Enable PostgreSQL persistence |
| `DATABASE_URL` | — | PostgreSQL connection string |
| `REDIS_ADDR` | — | Redis address for auth/rate limiting |
| `RAG_ENABLED` | `false` | Enable RAG (requires Ollama + Qdrant) |
| `ADMIN_ENABLED` | `false` | Enable HTTP admin server |
| `ADMIN_PORT` | `8473` | HTTP admin port |

Provider keys: `OPENAI_API_KEY`, `GEMINI_API_KEY`, `ANTHROPIC_API_KEY`, `DEEPSEEK_API_KEY`, `GROK_API_KEY`, `MISTRAL_API_KEY`, etc.

### Frozen Config

For production deployments that shouldn't fetch secrets at runtime:

```bash
# Resolve all secrets and write a static config snapshot
./airborne-freeze

# Start with frozen config
AIRBORNE_USE_FROZEN=true ./bin/airborne
```

## CLI Tool

`airborne-cli` provides admin and debugging commands:

```bash
airborne-cli health          # check server health
airborne-cli activity        # recent activity feed
airborne-cli test            # send a test generation request
airborne-cli debug <msg-id>  # inspect full request/response for a message
airborne-cli watch           # live activity monitoring
```

## Observability

- **Tracing**: OpenTelemetry with W3C trace context propagation across gRPC and HTTP
- **Metrics**: `rpc.server.duration` histograms via OTel
- **Logging**: Structured JSON logs with request IDs, trace IDs, and tenant context
- **Cost tracking**: Per-request cost calculation using embedded model pricing data

## Security

- gRPC interceptor chain: panic recovery, tracing, metrics, logging, tenant resolution, auth
- HTTP admin: CORS, request timeouts (30s), body size limits (2MB/100MB), JSON security validation (rejects dangerous keys, excessive nesting)
- API key secrets stored as bcrypt hashes
- Rate limiting via atomic Redis Lua scripts
- SSRF protection on custom provider base URLs (requires admin permission)
- TLS support for gRPC transport
- Non-root Docker container

## Project Structure

```
cmd/
  airborne/           Server entrypoint
  airborne-cli/       Admin CLI
  airborne-freeze/    Config freeze tool
api/proto/            Protobuf definitions
gen/go/               Generated gRPC/proto code
internal/
  server/             gRPC server construction
  service/            Service implementations (chat, admin, files)
  provider/           Provider interface + 16 implementations
  auth/               Authentication, API keys, rate limiting
  tenant/             Multi-tenant config loading
  config/             Global configuration
  db/                 PostgreSQL models and repository
  rag/                RAG pipeline (chunker, embedder, extractor, vectorstore)
  imagegen/           Image generation (Gemini, DALL-E)
  admin/              HTTP admin server
  pricing/            LLM cost calculation
  validation/         Input validation
  retry/              Retry/backoff utilities
configs/              YAML config and frozen snapshots
migrations/           PostgreSQL schema migrations
dashboard/            Next.js admin dashboard
deployments/          Docker and systemd templates
pkg/client/           Public Go client library
```

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25 |
| API | gRPC + Protocol Buffers |
| Dashboard | Next.js 16 / React 19 / TypeScript |
| Database | PostgreSQL (via pgx) |
| Cache/Auth Store | Redis |
| Tracing/Metrics | OpenTelemetry (OTLP export) |
| Secrets | Doppler |
| Proto Tooling | buf |
| Shared Library | chassis-go v5 |
