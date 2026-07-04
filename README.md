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
- **Database isolation** via PostgreSQL Row-Level Security: shared tables (`airborne_chats`, `chat_message`, etc.) carry a `tenant_id` column, and every transaction sets the `airborne.tenant_id` GUC; RLS policies (`FORCE ROW LEVEL SECURITY`) scope all reads/writes to that tenant. Tenant existence/status is registry-backed (`airborne_tenants` table), not a hardcoded list.

Tenant configs load from YAML/JSON files, Doppler API, or a frozen config snapshot.

## Authentication

Two modes controlled by `AIRBORNE_AUTH_MODE`:

**Static** (`static`) — A single bearer token (`AIRBORNE_ADMIN_TOKEN`) compared via constant-time comparison. Suitable for development and single-consumer deployments.

**Redis** (`redis`) — API keys in the format `airborne_sk_{keyID}_{secret}`. Keys are stored in Redis with bcrypt-hashed secrets, expiry dates, per-client permissions (`chat`, `chat:stream`, `files`, `admin`), and rate limit enforcement via atomic Lua scripts.

## Getting Started

### Prerequisites

- Go 1.26+
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

### Database Setup — Required Non-Superuser App Role

**This is the highest-severity operational risk in the deployment: if the app connects as a
superuser (or as the table owner), Row-Level Security is silently bypassed.** PostgreSQL never
enforces RLS for the table owner or for roles with `BYPASSRLS`/`SUPERUSER`, even when
`FORCE ROW LEVEL SECURITY` is set — the policies simply do nothing and every tenant can read and
write every other tenant's rows with no error.

Migrations (`migrations/001_baseline.sql`) must run as the owner/admin role (e.g. the default
`postgres` superuser or a dedicated migration role), but the **application's `DATABASE_URL` must
authenticate as a separate, restricted role** that is neither a superuser nor the table owner:

```sql
CREATE ROLE airborne_app LOGIN PASSWORD '...' NOSUPERUSER NOBYPASSRLS;
GRANT USAGE ON SCHEMA public TO airborne_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO airborne_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO airborne_app;
```

Run this once per database, then point the app's `DATABASE_URL` at `airborne_app` (the migration
tooling/admin connection can keep using the owner/superuser role — only the app's runtime
connection needs to be restricted).

**Post-deploy verification (run this against the app's actual `DATABASE_URL`, not the admin
connection):**

```sql
SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user;
```

Both `rolsuper` and `rolbypassrls` **must be `false`**. If either is `true`, tenant isolation is
not being enforced and this must be fixed before serving traffic.

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

### Testing

`make test` runs `go test -v -race ./...`, which includes the tenant-isolation (RLS) suite in
`internal/db` — but that suite requires Docker (it spins up a real Postgres via testcontainers)
and silently **skips** (not fails) when Docker is unavailable, which is exactly the case in CI
today (see below). Until CI is repaired, run it locally with Docker before shipping any schema or
RLS change and confirm it actually ran (not skipped):

```bash
go test -mod=mod -count=1 ./internal/db/
```

This is currently the **only** verification that tenant data is actually isolated by
Row-Level Security — treat a failure here as a release blocker.

**CI status:** GitHub Actions currently cannot build this repo — the `chassis-go` local `replace`
directives in `go.mod` point at sibling paths outside the checkout, so `go mod download` fails and
the `docker-build` workflow is red on every run. Two paths to a green build: vendor the
dependency tree (`go mod vendor` + build with `-mod=vendor`), or check out chassis-go (and its
addons) as sibling repos in the workflow using org-scoped tokens.

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
| Language | Go 1.26 |
| API | gRPC + Protocol Buffers |
| Dashboard | Next.js 16 / React 19 / TypeScript |
| Database | PostgreSQL (via pgx) |
| Cache/Auth Store | Redis |
| Tracing/Metrics | OpenTelemetry (OTLP export) |
| Secrets | Doppler |
| Proto Tooling | buf |
| Shared Library | chassis-go v10 |
