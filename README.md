# Airborne

A multi-provider AI gateway that exposes a unified gRPC API for chat generation, provider selection, admin health checks, and RAG file-store operations. The runtime chat path currently routes generation requests to OpenAI, Gemini, and Anthropic; the repo also contains OpenAI-compatible provider client packages and proto enum values for planned expansion. Airborne handles multi-tenancy, authentication, rate limiting, RAG, image generation, conversation persistence, and automatic failover for the providers wired into the runtime path.

## Provider Status

| Status | Providers | Current behavior |
|--------|-----------|------------------|
| **Runtime chat providers** | OpenAI, Gemini, Anthropic | Wired into `AirborneService.GenerateReply`, streaming, tenant config, and failover. |
| **Runtime file-store providers** | Internal/Qdrant, OpenAI Vector Stores, Gemini FileSearchStore | Exposed by `FileService` only when `RAG_ENABLED=true`. |
| **Implemented client packages, not chat-routed yet** | DeepSeek, Grok/xAI, Mistral, Perplexity, Cohere, Together AI, Fireworks AI, OpenRouter, DeepInfra, Hyperbolic, Nebius, Cerebras, Upstage | Implement `provider.Provider` through the shared OpenAI-compatible client layer, but are not selected by `ChatService` today. |
| **Proto placeholders without runtime clients** | Bedrock, Watsonx, Databricks, Baseten, HuggingFace, Predibase, Parasail, MiniMax | Reserved enum values; selecting them for `GenerateReply` is unsupported until a runtime client is wired in. |

Selecting anything other than OpenAI, Gemini, or Anthropic for `GenerateReply` currently returns an unknown-provider error.

## Architecture

```
                     ┌──────────────────────────────────────┐
                     │          gRPC Server (:50612)         │
                     │                                      │
  Clients ──gRPC──►  │  Recovery → Tracing → Metrics →      │
                     │  Logging → TenantInterceptor → Auth  │
                     │                                      │
                     │  ┌───────────────┐ ┌───────────────┐ │
                     │  │AirborneService│ │ AdminService  │ │
                     │  │Generate/Stream│ │ Health/Ready  │ │
                     │  │SelectProvider │ │ Version       │ │
                     │  └───────┬───────┘ └───────────────┘ │
                     │          │       ┌─────────────────┐ │
                     │          │       │ FileService     │ │
                     │          │       │ RAG stores only │ │
                     │          │       │ when RAG is on  │ │
                     │          ▼       └─────────────────┘ │
                     │  ┌───────────────────────────────┐   │
                     │  │ChatService provider selection │   │
                     │  └──────────────┬────────────────┘   │
                     └─────────────────┼────────────────────┘
                                       │
          ┌────────────────────────────┼────────────────────────────┐
          ▼                            ▼                            ▼
   ┌────────────┐              ┌──────────────┐             ┌──────────────┐
   │  OpenAI    │              │   Gemini     │             │  Anthropic   │
   │  (native)  │              │  (native)    │             │  (native)    │
   └────────────┘              └──────────────┘             └──────────────┘

   ┌────────────────────────────────────────────────────────────────────┐
   │ Provider client packages for future routing: compat.Client-based   │
   │ DeepSeek, Grok, Mistral, Perplexity, Together, Fireworks,          │
   │ OpenRouter, DeepInfra, Cohere, Hyperbolic, Nebius, Cerebras,       │
   │ and Upstage clients.                                               │
   └────────────────────────────────────────────────────────────────────┘
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

Requests support: provider/model override for the runtime chat providers, system prompts, conversation history, file search (RAG), web search, code execution, tool calling, structured output (JSON schema), inline images, and temperature/top-p/max-tokens tuning. Advanced feature support is provider-dependent; OpenAI and Gemini carry most of the feature-specific paths.

### AdminService

| RPC | Description |
|-----|-------------|
| `Health` | Liveness check (no auth required) |
| `Ready` | Admin-gated dependency readiness; currently reports Redis when configured. Standard gRPC health and HTTP `/admin/healthz` cover database/RAG checks. |
| `Version` | Build version, git commit, build time, Go version |

### FileService (registered only when RAG is enabled)

| RPC | Description |
|-----|-------------|
| `CreateFileStore` | Create a vector store (OpenAI, Gemini, or Qdrant) |
| `UploadFile` | Client-streaming file upload to a store |
| `DeleteFileStore` | Remove a store |
| `GetFileStore` / `ListFileStores` | Retrieve store metadata |

### HTTP Admin Server (optional, default port 8473)

Endpoints: `/admin/activity`, `/admin/debug/{id}`, `/admin/thread/{id}`, `/admin/health`, `/admin/healthz`, `/admin/version`, `/admin/test`, `/admin/chat`, `/admin/upload`

Security boundary: `/admin/health`, `/admin/healthz`, and CORS preflight requests are public for
load balancers and probes. Every other HTTP admin endpoint requires the configured admin token as
`Authorization: Bearer $AIRBORNE_ADMIN_TOKEN` or `X-API-Key: $AIRBORNE_ADMIN_TOKEN`. If no token is
configured, protected admin HTTP routes fail closed. Browser CORS is restricted to explicit origins
from `ADMIN_ALLOWED_ORIGINS` (wildcard origins are rejected/ignored).

## Multi-Tenancy

Each tenant gets isolated configuration:

- **Provider API keys and models** with per-provider temperature, system prompts, and failover order
- **Rate limits** (requests/min, requests/day, tokens/min)
- **Image generation** settings (provider, triggers, models)
- **Database isolation** via PostgreSQL Row-Level Security: shared tables (`airborne_chats`, `airborne_chat_messages`, `airborne_files`, etc.) carry a `tenant_id` column, and every transaction sets the `airborne.tenant_id` GUC; RLS policies (`FORCE ROW LEVEL SECURITY`) scope all reads/writes to that tenant. Tenant existence/status is registry-backed (`airborne_tenants` table), not a hardcoded list.

Tenant configs load from YAML/JSON files, Doppler API, or a frozen config snapshot.

## Authentication

Two modes controlled by `AIRBORNE_AUTH_MODE`:

**Static** (`static`) — A single bearer token (`AIRBORNE_ADMIN_TOKEN`) compared via constant-time comparison. Suitable for development and single-consumer deployments.

**Redis** (`redis`) — API keys in the format `airborne_sk_{keyID}_{secret}`. Keys are stored in Redis with bcrypt-hashed secrets, expiry dates, per-client permissions (`chat`, `chat:stream`, `files`, `admin`), and rate limit enforcement via atomic Lua scripts.

## Getting Started

### Prerequisites

- Go 1.26.5+
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
# Uses the pinned, Makefile-built airborne:latest image.
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
| `ADMIN_ALLOWED_ORIGINS` | localhost dashboard origins | Comma-separated explicit CORS origins for HTTP admin; `*` is not allowed |
| `AIRBORNE_ADMIN_URL` | CLI/dashboard dependent | HTTP admin server URL for CLI/dashboard proxy calls |
| `DASHBOARD_ADMIN_TOKEN` | `AIRBORNE_ADMIN_TOKEN` | Token accepted by the Next.js dashboard API routes; set explicitly when dashboard auth should differ from backend admin auth |

Provider keys: `OPENAI_API_KEY`, `GEMINI_API_KEY`, `ANTHROPIC_API_KEY`, `DEEPSEEK_API_KEY`, `GROK_API_KEY`, `MISTRAL_API_KEY`, etc.

### Frozen Config

For production deployments that shouldn't fetch secrets at runtime:

```bash
# Resolve all secrets and write a static config snapshot
./airborne-freeze              # tracked convenience binary
# or, from source:
go run ./cmd/airborne-freeze

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

**CI/build note:** the Docker workflow and `make docker-build` stage pinned snapshots of every
local `replace` target (`pricing_db`, `chassis-go`, and `chassis-go-addons`) into the Docker build
context before image builds, then copy them to the absolute paths that satisfy the `go.mod`
replacements inside the builder image. Keep those context-staging refs aligned with any future
`replace` directive or dependency release.

## Admin Dashboard

The Next.js dashboard under `dashboard/` is an operator UI and admin proxy. Its API routes are
protected separately from the Go admin server:

- Accepted client credentials: `Authorization: Bearer <token>`, `X-API-Key: <token>`, or an
  `airborne_admin_token` cookie.
- Expected dashboard token: `DASHBOARD_ADMIN_TOKEN`, falling back to `AIRBORNE_ADMIN_TOKEN`.
- Backend forwarding token: `AIRBORNE_ADMIN_TOKEN`, falling back to `DASHBOARD_ADMIN_TOKEN`.
- Cookie auth on state-changing requests requires same-origin `Origin`/`Referer` headers to reduce
  CSRF exposure.

Run locally:

```bash
cd dashboard
AIRBORNE_ADMIN_URL=http://localhost:8473 \
AIRBORNE_ADMIN_TOKEN="$AIRBORNE_ADMIN_TOKEN" \
DASHBOARD_ADMIN_TOKEN="$AIRBORNE_ADMIN_TOKEN" \
npm run dev
```

If you put the dashboard behind an auth gateway, configure that gateway to inject one of the
accepted credentials into calls to `/api/*`, or set the `airborne_admin_token` cookie from a
trusted same-origin login flow. The dashboard intentionally does not expose a built-in token entry
screen.

## CLI Tool

`airborne-cli` provides admin and debugging commands. `make build` only builds the server binary,
so build or run the CLI explicitly. The CLI default URL is `http://localhost:50054`; with the
checked-in server config, pass `--url http://localhost:8473` or set `AIRBORNE_ADMIN_URL`. Except
for `health`, commands call protected admin endpoints and need `AIRBORNE_ADMIN_TOKEN` or `--token`.

```bash
go build -o bin/airborne-cli ./cmd/airborne-cli

export AIRBORNE_ADMIN_TOKEN="your-token"

bin/airborne-cli --url http://localhost:8473 health          # public health check
bin/airborne-cli --url http://localhost:8473 activity        # recent activity feed
bin/airborne-cli --url http://localhost:8473 test            # send a test generation request
bin/airborne-cli --url http://localhost:8473 debug <msg-id>  # inspect full request/response
bin/airborne-cli --url http://localhost:8473 watch           # live activity monitoring

# Or pass the token explicitly:
bin/airborne-cli --url http://localhost:8473 --token "$AIRBORNE_ADMIN_TOKEN" activity
```

## Observability

- **Tracing**: OpenTelemetry with W3C trace context propagation across gRPC and HTTP
- **Metrics**: `rpc.server.duration` histograms via OTel
- **Logging**: Structured JSON logs with request IDs, trace IDs, and tenant context
- **Cost tracking**: Per-request cost calculation using embedded model pricing data

## Security

- gRPC interceptor chain: panic recovery, tracing, metrics, logging, tenant resolution, auth
- HTTP admin: explicit bearer/API-key auth on protected routes, explicit-origin CORS, request timeouts (30s), body size limits (2MB JSON / 100MB upload), JSON security validation (rejects dangerous keys, excessive nesting)
- Dashboard API proxy: bearer/API-key/cookie auth, same-origin CSRF guard for cookie writes, no raw AI-rendered HTML injection, and safe `http`/`https` citation links only
- API key secrets stored as bcrypt hashes
- Rate limiting via atomic Redis Lua scripts
- SSRF protection on custom provider base URLs; FileService external store overrides require admin permission and reject credentials/userinfo in URLs
- Request, history, upload, provider-error, Docbox, and HTTP-capture body reads are bounded to reduce memory-exhaustion and log-leak risk
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
  service/            Service implementations (chat, admin, files, idempotency)
  provider/           Provider interface, runtime clients, and compat-based client packages
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
Dockerfile            Production container build
docker-compose.yml    Local compose wiring for Airborne and dependencies
deploy/               Chassis deploy metadata
```

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.26.5 |
| API | gRPC + Protocol Buffers |
| Dashboard | Next.js 16.3 canary / React 19 / TypeScript |
| Database | PostgreSQL (via pgx) |
| Cache/Auth Store | Redis |
| Tracing/Metrics | OpenTelemetry (OTLP export) |
| Secrets | Doppler |
| Proto Tooling | buf |
| Shared Library | chassis-go/v11 v11.3.0 + chassis-go-addons v1.2.10 |
