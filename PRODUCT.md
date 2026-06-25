# Airborne -- Product Overview

## What Airborne Is

Airborne is a **multi-provider AI gateway** -- a centralized backend service that sits between consuming applications and LLM (Large Language Model) providers. It presents a single, unified gRPC API so that any application (email service, chat dashboard, internal tool, etc.) can send prompts and receive AI-generated responses without knowing or caring which LLM provider is fulfilling the request behind the scenes.

It is one service within the `ai_suite` family of internal backends, and it is the canonical inference entry point for those products: sibling services (notably the **Solstice** email-processing pipeline) do not call provider SDKs themselves -- they call Airborne, and in some cases share Airborne's PostgreSQL schema (see "Solstice Integration" below).

The core value proposition: **one API to rule all LLMs, with enterprise-grade multi-tenancy, authentication, cost tracking, and failover built in.**

### Live providers vs. scaffolded providers (read this before editing provider code)

Airborne ships native, fully-wired integrations for **three core providers: OpenAI, Google Gemini, and Anthropic.** These are the only providers the running `ChatService` will route to -- its provider-resolution switch returns `unknown provider` for anything else, and tenant requests fail over only among these three (default chains: OpenAI->Gemini, Gemini->OpenAI, Anthropic->OpenAI).

The repository **also contains 13 additional provider packages** under `internal/provider/` (DeepSeek, Grok/xAI, Mistral, Perplexity, Cohere, Together AI, Fireworks AI, OpenRouter, DeepInfra, Hyperbolic, Nebius, Cerebras, Upstage), most built on a shared OpenAI-compatible client layer (`internal/provider/compat`). These packages implement the `provider.Provider` interface and are covered by tests, but they are **not yet imported or instantiated by `ChatService`** -- they are scaffolding for a future expansion of the live routing set, plus additional providers defined only in the proto enum (Bedrock, Watsonx, Databricks, HuggingFace, MiniMax). Treat "20+ providers" as the proto/aspirational surface; treat "3 providers" as today's production routing reality. When adding a compat provider to the live path, the work is wiring it into `ChatService` (construction + the name switch in `getProviderForName`/`getFallbackProvider`), not writing the client from scratch.

---

## Why Airborne Exists -- Business Goals

### 1. Provider Abstraction and Vendor Independence

The AI landscape is fragmented across OpenAI, Google Gemini, Anthropic, DeepSeek, Mistral, and many others. Each provider has its own SDK, authentication scheme, streaming protocol, and data format. Airborne eliminates this complexity for consuming applications. A chat application does not need to integrate with each vendor's API -- it integrates with Airborne once, and Airborne handles the rest. (Today the live routing set is the three core providers; the architecture and the additional provider packages exist so the set can grow without any consumer change.)

This gives the business:
- **Negotiating leverage** -- Easily switch providers or redistribute traffic without rewriting client applications.
- **Risk mitigation** -- If one provider has an outage, Airborne can failover to another automatically.
- **Speed to adopt new models** -- Adding a new provider (e.g., when a startup launches a compelling model) requires implementing a single interface, not refactoring every consuming application.

### 2. Multi-Tenancy and Tenant Isolation

Airborne is designed from the ground up for multi-tenant operation. Multiple distinct products/brands (e.g., "ai8" and "email4ai") share the same Airborne deployment but are completely isolated:

- **Per-tenant provider configuration**: Each tenant gets its own set of AI provider API keys, default models, temperature settings, system prompts, and model overrides. One tenant might default to Gemini while another defaults to OpenAI.
- **Per-tenant rate limits**: Each tenant has independently configurable requests-per-minute (RPM), requests-per-day (RPD), and tokens-per-minute (TPM) limits.
- **Database-level isolation**: Each tenant has its own set of PostgreSQL tables (e.g., `ai8_airborne_threads`, `email4ai_airborne_threads`). This is table-level isolation, not row-level filtering, which provides stronger data separation guarantees.
- **Per-tenant image generation settings**: Each tenant can configure whether image generation is enabled, which provider (Gemini or DALL-E) to use, and custom trigger phrases.
- **Per-tenant failover chains**: Each tenant can define its own failover order (e.g., "try Gemini first, then OpenAI, then Anthropic").

This architecture supports a **platform business model** where the operator can onboard multiple independent customers or internal products onto a single Airborne deployment, each with fully customized AI behavior.

### 3. Cost Tracking and Financial Visibility

Every AI inference request has a real dollar cost. Airborne provides granular cost tracking at every level:

- **Per-request cost**: Every API call calculates and persists the USD cost based on model-specific pricing (input tokens, output tokens, cached tokens, thinking tokens).
- **Grounding/web search cost**: Google Search grounding queries are tracked separately because they have a different billing model (per-query pricing that varies by volume tier).
- **Per-thread cost**: The total accumulated cost of an entire conversation thread is tracked, enabling cost analysis per user interaction.
- **Per-tenant cost**: Cross-tenant views aggregate costs for administrative reporting.
- **Gemini-specific cost granularity**: For Gemini models, the system tracks cached token costs (at a 90% discount), thinking token costs (charged at output rate), tool use token costs, and grounding costs -- each as separate line items.

This enables the business to:
- Bill tenants accurately for AI usage.
- Set budgets and enforce spending limits via rate limiting.
- Identify cost optimization opportunities (e.g., switching from an expensive model to a cheaper one for certain request types).
- Monitor for runaway costs in real time.

### 4. Centralized Authentication and Access Control

Airborne manages authentication centrally so consuming applications do not need to implement their own AI credential management:

- **Static token mode**: A simple bearer token for development and single-consumer setups.
- **Redis-backed API key system**: Production-grade API keys in the format `airborne_sk_{keyID}_{secret}` with bcrypt-hashed secrets, expiry dates, and per-key permissions.
- **Granular permissions**: Each API key can be scoped to specific capabilities: `chat` (unary requests), `chat:stream` (streaming), `files` (RAG document management), `admin` (operational endpoints). This allows, for example, a frontend app to have chat-only access while an admin tool has full access.
- **Rate limiting enforcement**: Rate limits are enforced atomically using Redis Lua scripts, preventing race conditions. Limits are checked per-request and tokens-per-minute are tracked per-response.

### 5. Retrieval-Augmented Generation (RAG)

Airborne provides a built-in RAG pipeline so tenants can give the AI access to their own documents:

- **Multi-backend file stores**: Documents can be stored in OpenAI Vector Stores, Gemini FileSearchStores, or a self-hosted Qdrant vector database -- abstracted behind a unified FileService API.
- **Self-hosted pipeline**: For the Qdrant path, Airborne handles the full RAG pipeline: text extraction (via Docbox/Pandoc), chunking (with configurable overlap), embedding generation (via Ollama), vector storage, and similarity search at query time.
- **Automatic context injection**: When a user's request has file search enabled, Airborne retrieves relevant document chunks and injects them into the system prompt as structured XML context, with injection-prevention instructions.
- **Tenant-isolated collections**: Each tenant's documents are stored in separate Qdrant collections, preventing cross-tenant data leakage.

This allows the business to offer "chat with your documents" functionality as a platform feature to any tenant without each tenant needing to build their own RAG infrastructure.

### 6. Image Generation

Airborne integrates AI image generation as a first-class feature:

- **Provider support**: Images can be generated via Google Gemini or OpenAI DALL-E, configurable per-tenant.
- **Trigger-based activation**: Image generation is activated by slash commands (`/image`) or configurable trigger phrases (e.g., `@image`, "generate image").
- **Inline in conversations**: Generated images are returned as part of the normal response flow, with metadata (dimensions, MIME type, alt text).
- **Fallback behavior**: If image generation fails, the system can optionally fall back to a text-only response instead of erroring.

### 7. Conversation Persistence and Activity Tracking

Airborne persists every conversation turn to PostgreSQL, creating a complete audit trail:

- **Thread model**: Conversations are organized into threads, each containing a sequence of messages (user, assistant, system).
- **Full metric capture**: Every assistant response records the provider used, model, input/output tokens, cost in USD, processing time in milliseconds, grounding queries, and web search grounding costs.
- **Debug data retention**: The raw JSON request sent to the provider and the raw JSON response received back are stored alongside each message, enabling deep debugging and compliance auditing.
- **Rendered HTML**: If the markdown rendering service is available, the HTML-rendered version of each AI response is stored alongside the raw text.
- **Activity feed**: A real-time activity feed aggregates recent AI interactions across all tenants, showing who asked what, which provider answered, how long it took, and how much it cost.
- **Failed request tracking**: Even failed provider requests are persisted with the error message, enabling failure pattern analysis.

### 8. Automatic Failover

When a primary provider fails (e.g., OpenAI returns an error), Airborne can automatically retry the request with a fallback provider:

- **Configurable failover order**: Each tenant defines their provider failover sequence.
- **Transparent to consumers**: The response includes `failed_over: true` and the original error, but the consumer gets a successful response without needing retry logic.
- **Default fallback chains**: If no explicit fallback is specified, the system uses sensible defaults (OpenAI fails over to Gemini, Gemini fails over to OpenAI, Anthropic fails over to OpenAI).

### 9. Streaming Support

Airborne supports both synchronous (unary) and streaming response modes:

- **Server-side streaming**: Responses are streamed token-by-token as they are generated, enabling real-time "typing" UX in chat applications.
- **Rich stream events**: The stream carries not just text deltas but also citation updates, tool call updates, code execution results, usage metrics, and a completion event with final metadata.
- **Provider-transparent**: Whether the underlying provider streams natively or not, Airborne normalizes the stream format.

### 10. Structured Output and Entity Extraction

Airborne can request structured metadata alongside AI responses:

- **Intent classification**: Categorizes user messages (question, request, task delegation, feedback, complaint, follow-up, attachment analysis).
- **Entity extraction**: Identifies named entities (people, organizations, locations, products, technologies) mentioned in the conversation.
- **Topic tagging**: Generates 2-4 keyword tags per response.
- **Scheduling detection**: Identifies when the user mentions dates/times for potential calendar integration.

This enables downstream applications to make routing decisions, build search indexes, or trigger automations based on AI-extracted metadata.

### 11. Tool Calling and Code Execution

Airborne supports agentic workflows:

- **Function calling / tool use**: Consumers can define tools with JSON Schema parameter definitions. The AI can request tool invocations, and the consumer provides results in a follow-up request.
- **Code execution**: When enabled, providers that support it (e.g., Gemini) can execute code and return stdout/stderr along with generated files.
- **Multi-turn tool loops**: The `requires_tool_output` flag on responses signals when the consumer must provide tool results before the conversation can continue.

### 12. Operational Observability

Airborne provides comprehensive observability:

- **OpenTelemetry integration**: Distributed tracing with W3C trace context propagation across gRPC and HTTP boundaries, plus `rpc.server.duration` metric histograms.
- **Structured JSON logging**: Every log line includes request IDs, trace IDs, and tenant context for correlation.
- **Admin dashboard**: A Next.js web dashboard provides a visual interface for activity monitoring, thread inspection, debug data viewing, and test message submission.
- **CLI tool**: The `airborne-cli` utility provides command-line health checking, activity feed monitoring, test request submission, and message debugging.
- **Event bus**: An optional Kafka-based event bus publishes `inference.completed` events for downstream analytics systems.

### 13. Slash Command Processing

Airborne has a built-in command parser that intercepts user input before it reaches the AI:

- **/image**: Triggers image generation with the remaining text as the prompt.
- **/ignore**: Strips marked content from the prompt before sending it to the AI (useful for email signatures or boilerplate that should not influence the AI response).
- **Extensibility**: The command parser is designed to support additional commands.

### 14. Security Hardening

Security is treated as a first-class concern:

- **SSRF protection**: Custom base URLs in provider configs require admin permission, and all URLs are validated against SSRF attack patterns.
- **Path traversal prevention**: Secret file paths (`FILE=` references) are restricted to whitelisted directories (`/etc/airborne/secrets`, `/run/secrets`, `/var/run/secrets`) with symlink resolution.
- **Input validation**: Request payloads are size-limited (100KB user input, 50KB instructions, 100 history messages, 50 metadata entries). Request IDs are validated against injection patterns.
- **JSON security**: The admin HTTP server validates incoming JSON for dangerous keys and excessive nesting.
- **Error sanitization**: Provider errors are sanitized before being returned to clients, preventing information leakage (e.g., API keys in error messages).
- **Non-root Docker**: The production container runs as a non-root user.
- **bcrypt key hashing**: API key secrets are stored only as bcrypt hashes -- never in plaintext.
- **Atomic rate limiting**: Redis Lua scripts ensure rate limit checks and increments happen atomically, preventing race-condition bypass.

### 15. Configuration and Deployment Flexibility

Airborne supports multiple configuration and deployment strategies:

- **File-based config**: Tenant configurations are loaded from JSON or YAML files in the `configs/` directory.
- **Doppler integration**: In production, secrets and tenant configs can be loaded from the Doppler secrets management platform.
- **Frozen config**: The `airborne-freeze` tool resolves all secrets at build time and writes a static snapshot, enabling deployments that do not fetch secrets at runtime (e.g., air-gapped environments).
- **Hot reload**: Tenant configurations can be reloaded at runtime without restarting the server, with diff reporting showing which tenants were added, removed, or unchanged.
- **Development mode**: A startup mode flag allows running without optional dependencies (Redis, database) for local development.
- **Multi-platform builds**: The Makefile supports building for Linux/amd64 and Darwin/arm64 with a platform-detection launcher script.

---

## Business Logic Summary

The core business logic flow for a typical request:

1. **Client sends gRPC request** with a prompt, tenant ID, preferred provider, and optional parameters (system prompt, conversation history, file search, web search, tools, etc.).
2. **Tenant interceptor** resolves the tenant configuration, validating the tenant exists and injecting its config into the request context.
3. **Authentication interceptor** validates the API key (static or Redis-backed) and checks permissions.
4. **Rate limiter** checks if the client is within their RPM, RPD, and TPM limits.
5. **Command parser** processes slash commands (`/image`, `/ignore`).
6. **Provider selection** determines which AI provider to use based on: explicit request preference, tenant default, trigger phrase matching, or conversation continuity.
7. **Config builder** merges tenant-level provider settings with request-level overrides to produce the final provider configuration (API key, model, temperature, etc.).
8. **RAG retrieval** (if enabled) queries the vector store for relevant document chunks and injects them into the system prompt.
9. **Provider call** sends the request to the selected AI provider's SDK.
10. **Failover** (if enabled and primary fails) retries with a fallback provider.
11. **Cost calculation** computes the USD cost of the request using embedded model pricing data.
12. **Token recording** updates the rate limiter with actual token usage.
13. **Conversation persistence** asynchronously writes the user message, assistant response, debug data, and cost metrics to the tenant's PostgreSQL tables.
14. **Event publishing** (if Kafka is configured) emits an `inference.completed` event.
15. **Response** is returned to the client with the AI text, usage metrics, citations, tool calls, images, structured metadata, and failover information.

---

## Who Uses Airborne

Airborne is designed to serve as infrastructure for multiple products within an organization:

- **Chat applications** that need conversational AI with document knowledge (RAG).
- **Email AI services** (e.g., "email4ai") that generate AI-powered email responses.
- **Admin/ops dashboards** that need to monitor AI usage, costs, and performance across all products.
- **Internal tools** that need AI capabilities (summarization, classification, entity extraction) without each building their own provider integrations.

Each of these consuming applications talks to a single Airborne API and benefits from centralized authentication, cost tracking, provider management, and operational observability.

The two real tenants in shipped config are **`ai8`** and **`email4ai`** (plus a `zztest` tenant used for tests/CI). Database isolation is implemented as **table-level** prefixing -- e.g. `ai8_airborne_threads`, `email4ai_airborne_threads` -- not row-level filtering, which is the stronger separation guarantee the multi-tenancy section describes.

---

## Solstice Integration -- Shared Schema, Not Shared Logic

Airborne's PostgreSQL schema is extended (migrations `007`, `008`, `009`) to support **Solstice**, a sibling email-processing service in the suite. This is a deliberate, load-bearing design choice that is easy to misread, so it is documented here explicitly:

- **Migration 007 (thread extensions)** adds Solstice-specific columns to the per-tenant `*_airborne_threads` tables: `solstice_thread_id`, `original_sender`, `seen_recipients`, `has_replied`, `done`, `revision` (optimistic-locking counter), `conversation_history` (cross-provider history JSONB), `process_hash`, `solstice_version`, and store/continuity IDs (`file_search_store_id`, `vector_store_id`, `response_id`). This lets Solstice reuse Airborne's thread storage as the single source of truth for a conversation rather than maintaining a parallel store.
- **Migration 008 (jobs tables)** creates per-tenant `*_airborne_jobs` tables for crash-proof email intake: a job row is persisted (with an R2 object prefix and a per-job AES encryption key) before Postmark gets a 200 OK, so a crashed worker can resume. Deleting the row deletes the key, crypto-shredding the R2 payload.
- **Migration 009 (archives tables)** creates per-tenant `*_airborne_archives` tables that store the **raw inbound email** (headers, envelope, bodies, attachment paths) for retry/reprocessing, with a short TTL, separate from the permanent AI-conversation record in threads/messages.

**Why this matters for code changes:** the `jobs` and `archives` tables -- and most Solstice thread columns -- are **owned and written by Solstice, not by Airborne's Go code.** Airborne hosts the schema (the migrations live here) but its services do not read or write `*_airborne_jobs` / `*_airborne_archives`. If you are editing Airborne's repository layer and see these tables, do not assume Airborne populates them; changing their shape is a cross-service contract change with Solstice, not a local refactor.

---

## How to Think About Code Changes

Hard constraints and "what belongs here vs. a sibling repo":

- **Airborne owns inference, not application workflows.** Provider routing, prompt assembly, RAG retrieval, cost calculation, conversation/metric persistence, auth, and rate limiting belong here. Email parsing, webhook intake, job durability/recovery, and business-specific orchestration belong in **Solstice** (or the relevant consumer), even though some of their schema is hosted here. Resist pulling email/workflow logic into Airborne.
- **Adding a provider to the live path is a wiring task, not a from-scratch task.** The client likely already exists under `internal/provider/<name>` (often via `compat`). The actual change is constructing it in `ChatService` and adding its name to the resolution switches and failover defaults. Do not add a provider to the README/PRODUCT "live" set until it is routed by `ChatService`.
- **The `provider.Provider` interface is the contract.** Every provider must answer `Name`, `GenerateReply`, `GenerateReplyStream`, and the four capability predicates (`SupportsFileSearch`, `SupportsWebSearch`, `SupportsNativeContinuity`, `SupportsStreaming`). Routing and RAG behavior branch on these predicates -- e.g. RAG context is injected manually for providers without native file search, and full history is re-sent for providers without native continuity.
- **Security invariants are not optional.** Custom provider `base_url` overrides require the `admin` permission and pass SSRF validation; `FILE=` secret references are restricted to the whitelist in `internal/tenant/secrets.go` (`/etc/airborne/secrets`, `/run/secrets`, `/var/run/secrets`) with symlink resolution; request payloads are bounded by `internal/validation/limits.go` (100KB user input, 50KB instructions, 100 history messages, 50 metadata entries, 1KB/10KB per metadata key/value); rate-limit checks must stay atomic (Redis Lua). Weakening any of these is a security regression, not a convenience tweak.
- **Persistence and event publishing are best-effort and additive.** Conversation writes happen after the response path and the Kafka event bus (`ai8.ai.airborne.inference.completed`, via chassis-go `kafkakit`) is tolerated-on-failure. New side effects on the request path should preserve this property: never let an analytics/persistence failure break a successful inference response.
- **Generated gRPC code is not hand-edited.** Change `api/proto/**`, then regenerate into `gen/go/**` with `make proto` (buf). Editing generated files directly will be overwritten.
- **Shared infrastructure comes from chassis.** Lifecycle, Kafka, Redis, Postgres, SSRF checks, and version stamping come from `chassis-go/v11` and the `chassis-go-addons` kits; prefer those over re-implementing transport/lifecycle concerns locally.

---

## Deployment Model and Scale

- **Process shape:** a single Go binary (`cmd/airborne`) hosting a gRPC server (default port **50612**) with an interceptor chain (recovery -> tracing -> metrics -> logging -> tenant resolution -> auth), plus an **optional** HTTP admin server (default port **8473**) for the dashboard/debug endpoints. Companion binaries: `cmd/airborne-cli` (admin/debug CLI) and `cmd/airborne-freeze` (resolve-secrets-to-static-snapshot tool).
- **Dependencies are graduated, not all-required.** Provider keys are the only hard requirement. PostgreSQL (persistence), Redis (Redis-mode auth + rate limiting), Ollama + Qdrant (self-hosted RAG), Docbox (document extraction), markdown_svc (HTML rendering), Doppler (secrets), and Kafka (event bus) are each independently enableable and degrade gracefully when off. A development startup mode runs without the optional dependencies.
- **Stateless service tier.** All durable state lives in PostgreSQL, Redis, and the vector stores, so the gRPC tier itself is horizontally scalable behind a load balancer; per-tenant rate limits are enforced centrally in Redis so limits hold across replicas.
- **Packaging:** multi-platform builds (Linux/amd64, Darwin/arm64) via the Makefile (`build-linux` / `build-darwin`); a non-root Docker image (`Dockerfile`) and a root-level `docker-compose.yml` that brings up the service plus its dependencies. (The `deployments/` directory is currently effectively empty -- the README's mention of systemd templates there is aspirational.)

---

## Current State / Status

- **Version:** `1.9.5` (see `VERSION` / `CHANGELOG.md`). Go toolchain 1.26; built on `chassis-go/v11`.
- **Built and in use:** the three-provider live routing path (OpenAI, Gemini, Anthropic), unary + streaming generation, provider selection, failover, static and Redis-backed auth with scoped permissions and atomic rate limiting, per-request/per-thread/per-tenant cost tracking, conversation persistence with debug capture and optional HTML rendering, the activity feed, RAG across OpenAI/Gemini/Qdrant backends, image generation (Gemini, DALL-E), slash-command processing (`/image`, `/ignore`), structured-output metadata extraction, the OpenTelemetry observability stack, the Kafka `inference.completed` event, the Next.js admin dashboard, and the CLI.
- **Scaffolded / not yet live:** the 13 OpenAI-compatible provider packages (present, interface-compliant, tested, but not wired into `ChatService`), and the proto-only providers (Bedrock, Watsonx, Databricks, HuggingFace, MiniMax) which have no client implementation yet. The Solstice `jobs`/`archives` schema is present and owned by Solstice; Airborne hosts but does not exercise it.
- **Note for documentation editors:** the repository `README.md` references a `pkg/client/` public Go client library and `chassis-go v10`; as of this version there is **no `pkg/` directory** and the dependency is `chassis-go/v11`. Trust the code and `go.mod` over those README lines.
