# Changelog

All notable changes to this project will be documented in this file.

## [1.2.15] - 2026-01-19

### Changed
- **Dashboard Layout**: Removed TestPanel, added search input to Conversations header
  - Removed "Test AI Connection" box from dashboard
  - Added search text input to the right of "Conversations" title

Agent: Claude:Opus 4.5

## [1.2.14] - 2026-01-19

### Added
- **Conversation Panel**: Added chat-style conversation display to dashboard
  - New `ConversationPanel` component with thread list sidebar and message display
  - Glass-morphism styling matching chatapp design (glass-bubble-user, glass-bubble-ai)
  - Thread grouping from activity data with cost totals
  - API route `/api/threads/[threadId]` for fetching thread conversations
  - Lifted activity state to page level for sharing between ActivityPanel and ConversationPanel

Agent: Claude:Opus 4.5

## [1.2.13] - 2026-01-18

### Fixed
- **Debug Data Query**: Fix JSONB coalesce error for citations column
  - Changed `COALESCE(m.citations, '')` to `COALESCE(m.citations::text, '')`
  - JSONB columns must be cast to text before coalescing with empty string

Agent: Claude:Opus 4.5

## [1.2.12] - 2026-01-18

### Added
- **Conversation Viewer**: Chat-style conversation display in dashboard
  - New `/admin/thread/{thread_id}` API endpoint
  - `GetThreadConversation` repository method returns all messages with rendered HTML
  - `ConversationMessage` and `ThreadConversation` models for conversation view
  - Enables viewing full conversation threads from Live Activity feed

Agent: Claude:Opus 4.5

## [1.2.11] - 2026-01-18

### Added
- **Rendered HTML Storage**: Store markdown_svc output in database
  - New `rendered_html TEXT` column in `airborne_messages` (TOAST-compressed by PostgreSQL)
  - Migration: `003_rendered_html.sql`
  - Both streaming and non-streaming responses now persist rendered HTML
  - `DebugData` struct includes `rendered_html` for admin dashboard

Agent: Claude:Opus 4.5

## [1.2.10] - 2026-01-18

### Added
- **Streaming Response Capture**: Raw request/response JSON now captured for streaming calls
  - Added `RequestJSON` and `ResponseJSON` fields to `StreamChunk`
  - Gemini streaming captures request body via httpcapture
  - Response captured as synthetic JSON with text, usage, and metadata at stream end
  - Streaming conversations now persisted to database with debug info
  - Enables full debug inspection for both streaming and non-streaming calls

Agent: Claude:Opus 4.5

## [1.2.9] - 2026-01-18

### Changed
- **Gemini Provider Defaults**: Updated to production-ready settings:
  - Default model: `gemini-3-pro-preview` (was `gemini-2.5-flash`)
  - Default temperature: `1.0` (creative but coherent)
  - Default max output tokens: `32000` (full response length)
  - Thinking level: `HIGH` for Pro models (unchanged, existing behavior)
  - TopP: No default (API handles it; only set if explicitly configured)

Agent: Claude:Opus 4.5

## [1.2.8] - 2026-01-18

### Changed
- **Dashboard Activity Panel**: Simplified to Solstice-style side-by-side viewer
  - Click any row to open full request/response inspector
  - Removed separate eyeball icon column
  - Removed redundant ContentModal (quick view)
  - DebugModal shows side-by-side: Request (left) | Response (right)
  - Supports Parsed view and raw JSON view toggle

Agent: Claude:Opus 4.5

## [1.2.7] - 2026-01-18

### Added
- **Test AI Connection Panel** for dashboard:
  - New `POST /admin/test` endpoint sends test messages to AI providers via gRPC
  - Dashboard API route `/api/test` proxies requests to admin server
  - `TestPanel.tsx` component with provider selector (Gemini/OpenAI/Anthropic)
  - Displays response text, model, tokens, and processing time
  - Gemini is default provider for testing

- **Admin Server Enhancements**:
  - Admin server now has gRPC client to call back to main service
  - Lazy initialization of gRPC connection
  - Auth token passed via gRPC metadata

Agent: Claude:Opus 4.5

## [1.2.6] - 2026-01-18

### Added
- **Dashboard Containerization** for K8s deployment:
  - `dashboard/Dockerfile`: Multi-stage build for Next.js standalone output
  - Configure standalone output in `next.config.ts` for minimal container size
  - Dashboard runs as sidecar container in K8s pod
  - Connects to Airborne admin API on localhost:8473

Agent: Claude:Opus 4.5

## [1.2.5] - 2026-01-18

### Changed
- **Enable raw JSON capture for debug inspector**:
  - All providers (OpenAI, Anthropic, Gemini, OpenAI-compat) now capture raw HTTP
    request/response bodies for every request
  - Captured JSON is stored in `raw_request_json` and `raw_response_json` columns
  - Debug inspector now shows full API payloads in JSON view
  - System prompt is also captured and stored

- **Files modified**:
  - `internal/provider/openai/client.go`: Always enable httpcapture transport
  - `internal/provider/anthropic/client.go`: Always enable httpcapture transport
  - `internal/provider/gemini/client.go`: Always enable httpcapture transport
  - `internal/provider/compat/openai_compat.go`: Always enable httpcapture transport
  - `internal/service/chat.go`: Pass debug info to PersistConversationTurnWithDebug()

Agent: Claude:Opus 4.5

## [1.2.4] - 2026-01-18

### Added
- **Request/Response Debug Inspector** for activity dashboard:
  - New `GET /admin/debug/{message_id}` endpoint returns full request/response data
  - Side-by-side "Parsed" view showing system prompt, user input, and AI response
  - "JSON" view with raw HTTP request/response bodies (dark terminal theme)
  - Inspect button (eye icon) on each activity row opens the debug modal

- **Database Debug Columns** (`migrations/002_debug_columns.sql`):
  - `system_prompt` column on airborne_messages
  - `raw_request_json` and `raw_response_json` JSONB columns
  - Index for messages with debug data

- **Dashboard Components**:
  - `DebugModal.tsx`: Full request/response inspector with Parsed/JSON toggle
  - `api/debug/[id]/route.ts`: API proxy for debug endpoint

- **Backend Support**:
  - `DebugData` struct in `internal/db/models.go`
  - `GetDebugData()` repository method with user input correlation
  - `PersistConversationTurnWithDebug()` for storing debug data

Agent: Claude:Opus 4.5

## [1.2.3] - 2026-01-18

### Changed
- **Tenant configs now loaded from Doppler**: When `DOPPLER_TOKEN` is set,
  tenant configurations are loaded entirely from Doppler instead of local
  JSON files. This removes the need for `configs/*.json` tenant files.

  **Doppler structure:**
  - `code_airborne/prod/BRAND_TENANTS` - comma-separated list of brand projects
  - `brand_email4ai/prod/AIRBORNE_TENANT_CONFIG` - full tenant config as JSON blob

  **Migration:** Tenant config file (`email4ai.json`) removed. Configs now live
  in Doppler with secrets inline (no more `ENV=` resolution needed since
  Doppler handles secret management).

- SIGHUP reload now fetches fresh configs from Doppler (clears cache first)
- Falls back to file-based loading if `DOPPLER_TOKEN` is not set
- Uses same retry logic as Solstice (15 attempts with exponential backoff)

### Removed
- `configs/email4ai.json` - migrated to Doppler `brand_email4ai/AIRBORNE_TENANT_CONFIG`

Agent: Claude:Opus 4.5

## [1.2.2] - 2026-01-18

### Changed
- **Pricing from JSON files**: Replaced hardcoded pricing map with file-based loading
  - Loads from `configs/*_pricing.json` (same format as Solstice)
  - Doppler `ai_providers` project is now single source of truth
  - Added 22 provider pricing files with 175+ models
  - Lazy initialization with graceful fallback
  - Maintains backwards-compatible `CalculateCost()` API

Agent: Claude:Opus 4.5

## [1.2.1] - 2026-01-18

### Changed
- Randomized default ports to avoid collisions:
  - GRPC port: 50051 → 50612
  - HTTP admin port: 50052 → 8473
- Updated docker-compose.yml port mapping to match

Agent: Claude:Opus 4.5

## [1.2.0] - 2026-01-18

### Added
- **StructuredMetadata Feature** (Gemini-only):
  - Extracts intent, entities, topics, and scheduling signals from LLM responses
  - Request parameter `enable_structured_output` enables JSON mode with entity extraction
  - 7 intent types: question, request, task_delegation, feedback, complaint, follow_up, attachment_analysis
  - 21 entity types across 5 categories (Core, Business, Technology, Operations, Content)
  - Scheduling intent detection with datetime mention extraction

- **Proto Changes** (`api/proto/airborne/v1/`):
  - `common.proto`: Added `StructuredMetadata`, `StructuredEntity`, `SchedulingIntent` messages
  - `airborne.proto`: Added `enable_structured_output` field to `GenerateReplyRequest`
  - Added `structured_metadata` field to `GenerateReplyResponse` and `StreamComplete`

- **Provider Types** (`internal/provider/provider.go`):
  - `StructuredMetadata`, `StructuredEntity`, `SchedulingIntent` Go types
  - `EnableStructuredOutput` field in `GenerateParams`
  - `StructuredMetadata` field in `GenerateResult`

- **Gemini Implementation** (`internal/provider/gemini/client.go`):
  - Updated `structuredOutputSchema()` with full 21 entity types and scheduling_intent
  - `extractStructuredResponse()` parses JSON and returns both text and metadata
  - Switched trigger from `cfg.ExtraOptions["structured_output"]` to `params.EnableStructuredOutput`

- **Service Integration** (`internal/service/chat.go`):
  - Wires `enable_structured_output` from request to provider params
  - `convertStructuredMetadata()` helper for proto conversion
  - Includes `StructuredMetadata` in response via `buildResponse()`

Agent: Claude:Opus 4.5

## [1.1.15] - 2026-01-18

### Added
- **PostgreSQL Message Persistence** (`internal/db/`):
  - `postgres.go`: Connection pool management with pgx/v5 driver
  - `models.go`: Thread, Message, and ActivityEntry data models
  - `repository.go`: CRUD operations for threads/messages, activity feed queries
  - Async persistence via goroutines to avoid blocking gRPC responses

- **Database Schema** (`migrations/001_initial_schema.sql`):
  - `threads` table: Conversation threads with tenant, user, title, token/cost tracking
  - `messages` table: Individual messages with role, content, provider, model, tokens, cost
  - Automatic thread cost aggregation via triggers
  - Performance indexes for tenant/user/time queries
  - `activity_feed` view for efficient dashboard queries

- **Pricing Module** (`internal/pricing/pricing.go`):
  - Cost calculation for all supported LLM models
  - Support for Anthropic (Claude), OpenAI (GPT-4, GPT-3.5), Google (Gemini) models
  - Per-token pricing based on input/output token counts

- **Admin HTTP Server** (`internal/admin/server.go`):
  - REST endpoint: `GET /admin/activity` - Returns recent activity with filtering
  - REST endpoint: `GET /admin/health` - Health check
  - Configurable via `admin.enabled` and `admin.port` in config

- **Next.js Dashboard** (`dashboard/`):
  - Next.js 16 + React 19 + Tailwind CSS v4 stack
  - Live Activity Feed with 3-second polling
  - Pause/Resume/Clear controls
  - Provider color coding (Anthropic amber, Gemini cyan, OpenAI emerald)
  - Content modal for viewing full message details
  - Displays: tenant, tokens (in/out/total), cost, thread cost, model, duration

- **Configuration Extensions** (`internal/config/config.go`):
  - `DatabaseConfig`: PostgreSQL connection settings (url, max_connections, log_queries)
  - `AdminConfig`: HTTP admin server settings (enabled, port)

Agent: Claude:Opus 4.5

## [1.1.14] - 2026-01-17

### Added
- **imagegen Package Tests** (`internal/imagegen/client_test.go`):
  - Tests for `DetectImageRequest`: trigger detection, case insensitivity, empty prompts, disabled config
  - Tests for `truncateForAlt`: truncation logic and edge cases
  - Tests for `Config` methods: `IsEnabled()`, `GetProvider()`, `GetModel()` with nil handling

- **retry Package Tests** (`internal/retry/retry_test.go`):
  - Comprehensive tests for `IsRetryable`: 35+ cases covering auth errors, rate limits, server errors, network issues
  - Tests for `SleepWithBackoff`: context cancellation and exponential delay verification
  - Tests for default constants: `MaxAttempts`, `RequestTimeout`, `BackoffBase`

Agent: Claude:Opus 4.5

## [1.1.13] - 2026-01-17

### Fixed
- **Mutex Protection for tenantStream** (`internal/auth/tenant_interceptor.go`):
  - Added `sync.Mutex` to `tenantStream` struct to protect `tenantSet` and `tenantCfg` fields
  - `Context()` method now acquires mutex before reading `tenantCfg`
  - `RecvMsg()` method now uses mutex when reading/writing shared state
  - Prevents potential data races when multiple goroutines call these methods concurrently
  - Defensive programming improvement (low priority - server-side streaming typically has single RecvMsg caller)

Agent: Claude:Opus 4.5

## [1.1.12] - 2026-01-17

### Added
- **Anthropic ThinkingDelta Streaming Support** (`internal/provider/anthropic/client.go`):
  - Added `ThinkingDelta` case handler in `GenerateReplyStream` method
  - When using Claude models with extended thinking enabled (Claude 3.7+), thinking content is now streamed to users in real-time
  - Previously, ThinkingDelta events were silently ignored, causing a "dead" period before text appeared
  - Users now see the model's reasoning process as it happens

Agent: Claude:Opus 4.5

## [1.1.11] - 2026-01-17

### Fixed
- **tenant_not_found Test** (`internal/auth/tenant_interceptor_test.go`):
  - Fixed test case that expected NotFound error when tenant doesn't exist
  - Test now provides metadata context with `x-tenant-id` header so the stream interceptor extracts tenant from metadata (matching production behavior)
  - Previously the test used `context.Background()` with no metadata, causing fallback to single-tenant mode instead of triggering the expected NotFound error

Agent: Claude:Opus 4.5

## [1.1.10] - 2026-01-17

### Added
- **Image Generation in Streaming Path** (`internal/service/chat.go`):
  - Added text accumulation during streaming to enable image generation triggers
  - `GenerateReplyStream` now calls `processImageGeneration` on stream completion
  - Generated images are included in the `StreamComplete.images` field
  - Matches existing unary `GenerateReply` behavior for streaming responses

Agent: Claude:Opus 4.5

## [1.1.9] - 2026-01-17

### Changed
- **Consolidated Retry Logic** (`internal/retry/retryable.go`):
  - Extracted duplicate `isRetryableError` functions from 4 provider files into shared `retry.IsRetryable`
  - Removed ~200 lines of duplicate code across OpenAI, Gemini, Anthropic, and compat providers
  - Unified retry detection patterns for auth errors (401, 403), invalid requests (400, 422), and retryable conditions (429, 5xx, network errors)

- **Debug Logging for Compat Streaming** (`internal/provider/compat/openai_compat.go`):
  - Added debug logging to streaming path matching non-streaming behavior
  - Logs model, base_url, and request_id when debug mode enabled

Agent: Claude:Opus 4.5

## [1.1.8] - 2026-01-17

### Changed
- **Gemini Defaults to Match Solstice**:
  - Default model changed from `gemini-2.5-flash` to `gemini-3-pro-preview` (Gemini 3 Pro with long thinking)
  - Pro models now default to `thinking_level: HIGH` (matching Solstice behavior)
  - Updated default in `configs/airborne.yaml`, `internal/config/config.go`, and `configs/email4ai.json`
  - Thinking config applied to both streaming and non-streaming Generate paths

Agent: Claude:Opus 4.5

## [1.1.7] - 2026-01-17

### Added
- **AI Image Generation Support** (`internal/imagegen/`):
  - New package for generating images via Gemini (default) and OpenAI DALL-E providers
  - Config-driven trigger phrases from tenant config (e.g., `@image`, `generate image`)
  - Supports tenant-level API key configuration via existing provider config
  - Automatic PNG to JPEG conversion for smaller response sizes
  - Images included in gRPC `GenerateReplyResponse.images` field

- **GeneratedImage Proto Message** (`api/proto/airborne/v1/airborne.proto`):
  - New `GeneratedImage` message with data, mime_type, prompt, alt_text, width, height, content_id
  - Added `images` field to `GenerateReplyResponse` and `StreamComplete`

- **Tenant Image Generation Config** (`internal/tenant/config.go`):
  - New `ImageGenerationConfig` struct with enabled, provider, model, trigger_phrases, fallback_on_error, max_images

Agent: Claude:Opus 4.5

## [1.1.6] - 2026-01-17

### Added
- **OpenAI Compatibility Layer Tests** (`internal/provider/compat/openai_compat_test.go`):
  - Comprehensive test coverage for the compat package used by 12+ providers
  - Tests for `buildMessages`, `extractText`, `extractUsage`, `isRetryableError`
  - Validation tests for missing API key and invalid base URL scenarios
  - 40+ test cases covering retry logic classification

- **Provider Capability Tests** (`internal/provider/providers_test.go`):
  - Table-driven tests verifying all 13 OpenAI-compatible providers
  - Validates Name(), SupportsFileSearch(), SupportsWebSearch(), SupportsStreaming(), SupportsNativeContinuity()
  - Compile-time interface compliance verification

Agent: Claude:Opus 4.5

## [1.1.5] - 2026-01-17

### Security
- **SSRF DNS Rebinding Defense**: Added DNS resolution check in URL validation to prevent hostnames that resolve to private/metadata IPs from bypassing SSRF protection
- **Metadata DoS Prevention**: Added key/value size limits (1KB keys, 10KB values) to `ValidateMetadata` to prevent memory exhaustion attacks
- **SecretHash Redaction**: Redacted `SecretHash` from `ListKeys` response to prevent offline cracking attempts
- **Client ID Log Spoofing Prevention**: Use authenticated client ID from context instead of request field to prevent log pollution

### Fixed
- **RAG Nil Guard**: Added `ensureRAGEnabled` helper to prevent nil panic in FileService when RAG is disabled
- **RAG Embedding Count Validation**: Validate that embedding count matches chunk count in ingestion to prevent index panics
- **Auth Header Normalization**: Added case-insensitive Bearer prefix matching and whitespace trimming to prevent auth failures

### Changed
- **BREAKING**: Environment variable prefix changed from `AIBOX_` to `AIRBORNE_` for consistency (e.g., `AIBOX_ADMIN_TOKEN` → `AIRBORNE_ADMIN_TOKEN`)
- **API Key Parsing**: Replaced magic numbers with named constants for improved maintainability

Agent: Claude:Opus 4.5

## [1.1.4] - 2026-01-17

### Added
- **Shared Retry Package** (`internal/retry/`):
  - Extracted common retry constants (`MaxAttempts`, `RequestTimeout`, `BackoffBase`) and `SleepWithBackoff` function
  - Reduces code duplication across provider implementations

### Fixed
- **Gemini Streaming Config Parity**: Added missing safety settings, thinking configuration, and structured output configuration to streaming API (was only in non-streaming)
- **Compat Provider Debug Logging**: Fixed no-op `WithDebugLogging` in all 13 OpenAI-compatible providers (cerebras, cohere, deepinfra, deepseek, fireworks, grok, hyperbolic, mistral, nebius, openrouter, perplexity, together, upstage) - now properly wires through to underlying compat client
- **Failover Error Sanitization**: Sanitized error messages in failover responses to prevent leaking sensitive provider details to clients

### Changed
- **Code Quality Improvements** (`internal/service/chat.go`):
  - Defined `ragSnippetMaxLen` constant to replace magic number 200
  - Removed redundant `mapProviderFromString` wrapper function

Agent: Claude:Opus 4.5

## [1.1.3] - 2026-01-16

### Fixed
- **Fix streaming not receiving tenant config for server-side streaming RPCs**:
  - The stream interceptor was relying on RecvMsg to extract tenant_id from the request
  - For server-side streaming (like GenerateReplyStream), the request is passed directly
    to the handler, not via RecvMsg, so tenant config was never set
  - Now extracts tenant_id from x-tenant-id gRPC metadata header for streaming
  - Falls back to single-tenant mode if not in metadata
  - This fixes "Gemini API key is required" errors during streaming

Agent: Claude:Opus 4.5

## [1.1.2] - 2026-01-16

### Fixed
- **Update Gemini default model from gemini-2.0-flash to gemini-2.5-flash**:
  - The gemini-2.0-flash model is no longer available in the Gemini API
  - Updated default model in `internal/provider/gemini/client.go` (both streaming and non-streaming)
  - Updated default model in `internal/config/config.go`
  - Updated config files: `configs/airborne.yaml`, `configs/email4ai.json`

Agent: Claude:Opus 4.5

## [1.1.1] - 2026-01-16

### Changed
- **Optimize HTTP Capture in Providers** (`internal/provider/compat/openai_compat.go`, `internal/provider/gemini/client.go`):
  - HTTP capture transport now only created when debug mode is enabled
  - Reduces overhead for non-debug requests

### Added
- **Unit Tests** for httpcapture, redis, tenant/env, mistral provider

### Removed
- Cleaned up old `_codex/` report files

Agent: Claude:Opus 4.5

## [1.1.0] - 2026-01-16

### Added
- **Static Auth Unit Tests** (`internal/auth/static_test.go`):
  - Comprehensive tests for `StaticAuthenticator` (token extraction, auth flow, interceptors)
  - Tests cover: token extraction precedence (Authorization vs x-api-key), successful auth with ClientKey injection, missing metadata, invalid token, missing token, skip method, and full interceptor flow
  - Improves coverage of critical security path

### Documentation
- Updated `_rcodegen/codex-airborne-test-2026-01-16_1254.md` with implementation status
- Updated `_rcodegen/gemini-airborne-test-2026-01-16_1410.md` with implementation status

Agent: Claude:Opus 4.5

## [1.0.9] - 2026-01-16

### Security
- **Escape Filename in RAG Context XML** (`internal/service/chat.go`):
  - `formatRAGContext` didn't escape filenames in the XML `source` attribute
  - Filenames containing `"`, `<`, `>`, or `&` could break XML structure
  - Now uses `html.EscapeString()` to properly escape filenames in XML attributes

Agent: Claude:Opus 4.5

## [1.0.8] - 2026-01-16

### Security
- **Mitigate RAG Prompt Injection** (`internal/service/chat.go`):
  - RAG context was injected directly into prompts without structural protection
  - Malicious documents could contain prompt injection attacks like "Ignore previous instructions..."
  - Now wraps RAG context in `<document_context>` and `<chunk>` XML tags
  - Adds explicit instruction that content within tags is reference material, not instructions
  - Updated tests to verify new XML-wrapped format

Agent: Claude:Opus 4.5

## [1.0.7] - 2026-01-16

### Security
- **Add Docbox SSRF Validation** (`internal/rag/extractor/docbox.go`):
  - Docbox extractor accepted arbitrary BaseURL without validation
  - If config was manipulated, attacker could point Docbox at internal services (SSRF)
  - Added validation using `ValidateProviderURL` to enforce SSRF protections
  - Invalid URLs (non-HTTPS to non-localhost) now fall back to safe localhost default
  - Logs warning when URL is rejected for audit trail
  - Added test for SSRF validation behavior

Agent: Claude:Opus 4.5

## [1.0.6] - 2026-01-16

### Security
- **Fix Weak Random Entropy** (`internal/auth/keys.go`):
  - `generateRandomString` generated N bytes, hex-encoded to 2N chars, then truncated to N
  - This effectively halved entropy: requesting 32 characters only got 128 bits instead of 256 bits
  - Fixed by calculating exact byte count needed: `(length + 1) / 2` bytes for `length` hex characters
  - Now provides full entropy for generated random strings (API keys, secrets, etc.)

Agent: Claude:Opus 4.5

## [1.0.5] - 2026-01-16

### Fixed
- **Fix Missing Error Details in Logs** (`internal/errors/sanitize.go`):
  - When sanitizing unknown errors for clients, the actual error was not logged
  - Server-side logs only showed "provider error occurred (details redacted for security)"
  - This made debugging "provider temporarily unavailable" errors impossible
  - Now includes full error details in server-side logs for proper debugging
  - Client still receives sanitized "provider temporarily unavailable" message

Agent: Claude:Opus 4.5

## [1.0.4] - 2026-01-16

### Fixed
- **Fix Stream Resource Leaks** (`internal/provider/openai/client.go`, `internal/provider/anthropic/client.go`):
  - OpenAI and Anthropic streaming goroutines did not call `Close()` on the stream object
  - This caused potential connection leaks over time as HTTP connections were not properly released
  - Added `defer stream.Close()` immediately after stream creation in both providers
  - Ensures proper cleanup of streaming connections regardless of how the goroutine exits

Agent: Claude:Opus 4.5

## [1.0.3] - 2026-01-16

### Fixed
- **Fix Anthropic History Truncation** (`internal/provider/anthropic/client.go`):
  - The `buildMessages` function iterated oldest-to-newest and broke when limit reached
  - This caused the **most recent** messages to be discarded instead of the oldest
  - Users lost immediate context while keeping stale, less-relevant messages
  - Fixed by iterating backwards to prioritize newest messages when truncating
  - Now properly keeps the most recent conversation context

Agent: Claude:Opus 4.5

## [1.0.2] - 2026-01-16

### Security
- **Fix ExtraOptions Map Data Race** (`internal/service/chat.go`):
  - ExtraOptions map was assigned by direct reference from tenant config
  - Request overrides then mutated this shared map, causing data races
  - In concurrent multi-tenant environment, this could leak per-request options across tenants
  - Fixed by deep copying the map before allowing mutations
  - Prevents tenant data leakage and race conditions

### Fixed
- **Fix OpenAI Streaming/Non-Streaming Request Parity** (`internal/provider/openai/client.go`):
  - Streaming requests were missing several options available in non-streaming path
  - Added `service_tier` option to streaming requests
  - Added `verbosity` option to streaming requests
  - Added `prompt_cache_retention` option to streaming requests
  - Added `Background` flag to streaming requests
  - Users now get consistent behavior regardless of streaming mode

Agent: Claude:Opus 4.5

## [1.0.1] - 2026-01-15

### Security
- **Remove Tenant ID from Error Messages** (`internal/auth/tenant_interceptor.go`):
  - Changed error message from `tenant %q not found` to generic `tenant not found`
  - Prevents tenant enumeration attacks by not echoing back the requested tenant ID
  - Attackers can no longer probe for valid tenant IDs by analyzing error responses
  - Changed from `status.Errorf` to `status.Error` (no formatting needed)

Agent: Claude:Opus 4.5

## [0.6.25] - 2026-01-15

### Security
- **Add Rate Limiting for File Upload Operations** (`internal/service/files.go`, `internal/server/grpc.go`):
  - Added `rateLimiter` field to `FileService` struct
  - Updated `NewFileService` to accept `*auth.RateLimiter` parameter
  - Added rate limit check in `UploadFile` after permission check
  - Returns `codes.ResourceExhausted` with "file upload rate limit exceeded" on limit breach
  - Prevents resource exhaustion through unlimited file upload requests
  - Uses existing `auth.RateLimiter` infrastructure shared with ChatService

Agent: Claude:Opus 4.5

## [0.6.24] - 2026-01-15

### Fixed
- **Fix Race Condition in Token Rate Limiter** (`internal/auth/ratelimit.go`):
  - Added `tokenRecordScript` Lua script for atomic token recording with TTL
  - `RecordTokens` now uses Lua script instead of separate INCRBY + conditional EXPIRE
  - Previous implementation had race condition: if two requests incremented simultaneously,
    neither might see `count == tokens` condition, leaving the key without expiry
  - New implementation checks TTL in Lua and sets EXPIRE if TTL is -1 (no expiry)
  - Follows same atomic pattern as existing `rateLimitScript` for request rate limiting

Agent: Claude:Opus 4.5

## [0.6.23] - 2026-01-15

### Documentation
- **Add Prominent Warning to Development Auth Interceptors** (`internal/server/grpc.go`):
  - Added explicit WARNING comments to `developmentAuthInterceptor` function
  - Added explicit WARNING comments to `developmentAuthStreamInterceptor` function
  - Makes it clear these functions bypass authentication entirely
  - Warns developers to never wire these into production builds
  - Recommends using build tags or explicit development mode checks if needed

Agent: Claude:Opus 4.5

## [0.6.22] - 2026-01-15

### Fixed
- **Include Key ID in JSON Unmarshal Error Messages** (`internal/auth/keys.go`):
  - Error message for key unmarshal failures now includes the key ID
  - Changed from generic "failed to unmarshal key" to "data corruption in key store for <keyID>"
  - Helps distinguish between transient Redis failures and permanent data corruption issues
  - Makes debugging easier when investigating key store problems

Agent: Claude:Opus 4.5

## [0.6.21] - 2026-01-15

### Fixed
- **Log Rate Limit Recording Errors Instead of Suppressing** (`internal/service/chat.go`):
  - `RecordTokens` errors from `GenerateReply` now logged with slog.Warn
  - `RecordTokens` errors from `GenerateReplyStream` now logged with slog.Warn
  - Previously errors were silently ignored with `_ =`, making Redis issues invisible
  - Operators can now detect when rate limiting data is not being recorded

Agent: Claude:Opus 4.5

## [0.6.20] - 2026-01-15

### Fixed
- **Log Warnings for Invalid Environment Variable Values** (`internal/config/config.go`):
  - Added slog.Warn() logging for all env var parse failures in `applyEnvOverrides()`
  - Previously invalid env vars (e.g., `AIRBORNE_GRPC_PORT="invalid"`) were silently ignored
  - Operators now receive warnings to help diagnose configuration issues
  - Affected environment variables:
    - `AIRBORNE_GRPC_PORT` - integer parsing
    - `AIRBORNE_TLS_ENABLED` - boolean parsing
    - `REDIS_DB` - integer parsing
    - `RAG_ENABLED` - boolean parsing
    - `RAG_CHUNK_SIZE` - integer parsing
    - `RAG_CHUNK_OVERLAP` - integer parsing
    - `RAG_RETRIEVAL_TOP_K` - integer parsing

Agent: Claude:Opus 4.5

## [0.6.19] - 2026-01-15

### Performance
- **Fix Unconditional HTTP Capture Performance Issue** (All provider clients):
  - **OpenAI Provider** (`internal/provider/openai/client.go`):
    - HTTP capture transport now only created when debug mode is enabled
    - Previously `httpcapture.New()` was called for every request
  - **Anthropic Provider** (`internal/provider/anthropic/client.go`):
    - Same fix applied to both `GenerateReply` and `GenerateReplyStream`
    - Streaming method no longer creates HTTP capture (not needed)
  - **Gemini Provider** (`internal/provider/gemini/client.go`):
    - HTTP capture transport conditionally created based on debug flag
  - **OpenAI-Compatible Provider** (`internal/provider/compat/openai_compat.go`):
    - HTTP capture transport conditionally created based on debug flag
  - Impact: Reduces GC pressure and latency overhead for production usage
    where debug mode is typically disabled

Agent: Claude:Opus 4.5

## [0.6.18] - 2026-01-15

### Security
- **Fix DoS via Memory Exhaustion in File Uploads** (`internal/service/files.go`):
  - Replaced `bytes.Buffer` with temporary file for streaming file uploads
  - Prevents memory exhaustion when handling concurrent large file uploads
  - Previously: 50 concurrent 100MB uploads could consume 5GB RAM, causing OOM
  - Now: File chunks written to disk-backed temp files, constant memory usage
  - Temp files automatically cleaned up after processing

Agent: Claude:Opus 4.5

## [0.6.17] - 2026-01-15

### Added
- **Phase 5: True Streaming Support** (All providers now support real-time streaming):
  - **OpenAI Provider** (`internal/provider/openai/client.go`):
    - True streaming via `Responses.NewStreaming()` API
    - Real-time text delta events (`response.output_text.delta`)
    - Streaming tool call support (`response.function_call_arguments.done`)
    - Streaming code execution events
    - Response completion with usage metrics
    - `SupportsStreaming()` now returns true
  - **Gemini Provider** (`internal/provider/gemini/client.go`):
    - True streaming via `Models.GenerateContentStream()` API
    - Real-time text streaming from response candidates
    - Streaming function call extraction
    - Streaming code execution results
    - `SupportsStreaming()` now returns true
  - **Anthropic Provider** - Already had true streaming implemented
  - **Compat Client** - Already had true streaming for OpenAI-compatible providers

Agent: Claude:Opus 4.5

## [0.6.16] - 2026-01-15

### Added
- **Phase 4: Function Calling & Code Execution** (Unified tool interface):
  - **Proto definitions** (`api/proto/airborne/v1/common.proto`):
    - `Tool` message - Function definition with name, description, JSON schema parameters
    - `ToolCall` message - Model's request to invoke a tool
    - `ToolResult` message - Output from tool execution for multi-turn
    - `CodeExecutionResult` message - Output from code interpreter/execution
    - `GeneratedFile` message - Files created during code execution
  - **Request enhancements** (`api/proto/airborne/v1/airborne.proto`):
    - `enable_code_execution` flag for code interpreter/execution
    - `tools` field for custom function definitions
    - `tool_results` field for multi-turn tool conversations
  - **Response enhancements**:
    - `tool_calls` - Tools the model wants to invoke
    - `requires_tool_output` - Signals client must provide tool results
    - `code_executions` - Results from code execution
  - **Streaming support**:
    - `ToolCallUpdate` chunk for streaming tool calls
    - `CodeExecutionUpdate` chunk for streaming code execution
  - **OpenAI provider** (`internal/provider/openai/client.go`):
    - Custom function tools via `params.Tools`
    - Code interpreter tool via `enable_code_execution`
    - Tool call extraction from function_call outputs
    - Code execution extraction from code_interpreter_call outputs
  - **Gemini provider** (`internal/provider/gemini/client.go`):
    - Custom function tools via FunctionDeclarations
    - Code execution tool via ToolCodeExecution
    - Function call extraction from response parts
    - Code execution result extraction (ExecutableCode, CodeExecutionResult)
  - **Provider interface** (`internal/provider/provider.go`):
    - `EnableCodeExecution` in GenerateParams
    - `Tools` and `ToolResults` in GenerateParams
    - `ToolCalls`, `RequiresToolOutput`, `CodeExecutions` in GenerateResult
    - `ChunkTypeToolCall` and `ChunkTypeCodeExecution` chunk types

Agent: Claude:Opus 4.5

## [0.6.15] - 2026-01-14

### Added
- **Phase 3: Additional Providers** (13 new LLM providers via OpenAI-compatible API):
  - **Reusable compat client** (`internal/provider/compat/openai_compat.go`):
    - Shared OpenAI-compatible client base for providers with standard API
    - Supports streaming, tools, system prompts, and all Generate/GenerateStream operations
    - Provider-specific configuration (base URL, model, API key env var, features)
  - **Tier 1 - High Usage Providers**:
    - `deepseek` - DeepSeek Chat API (deepseek-chat model)
    - `grok` - xAI Grok API (grok-2-latest model)
    - `mistral` - Mistral AI API (mistral-large-latest model)
    - `perplexity` - Perplexity API with web search (llama-3.1-sonar-large-128k-online)
  - **Tier 2 - Enterprise Provider**:
    - `cohere` - Cohere Command API (command-r-plus model)
  - **Tier 3 - Inference Platforms**:
    - `together` - Together AI (meta-llama/Llama-3.3-70B-Instruct-Turbo)
    - `fireworks` - Fireworks AI (accounts/fireworks/models/llama-v3p1-70b-instruct)
    - `openrouter` - OpenRouter multi-provider gateway (anthropic/claude-3.5-sonnet)
    - `deepinfra` - DeepInfra (meta-llama/Meta-Llama-3.1-70B-Instruct)
    - `hyperbolic` - Hyperbolic Labs (meta-llama/Meta-Llama-3.1-70B-Instruct)
  - **Tier 4 - Specialized Providers**:
    - `cerebras` - Cerebras fast inference (llama3.1-70b)
    - `nebius` - Nebius AI Studio (meta-llama/Meta-Llama-3.1-70B-Instruct)
    - `upstage` - Upstage Solar LLM (solar-pro)
  - **Proto updates** (`api/proto/airborne/v1/common.proto`):
    - Added 24 new provider enums organized by tier
    - Tier 1: DEEPSEEK, GROK, MISTRAL, PERPLEXITY
    - Tier 2: BEDROCK, WATSONX, DATABRICKS, COHERE
    - Tier 3: TOGETHER, FIREWORKS, OPENROUTER, DEEPINFRA, BASETEN, HYPERBOLIC
    - Tier 4: HUGGINGFACE, PREDIBASE, PARASAIL, UPSTAGE, NEBIUS, CEREBRAS, MINIMAX

Agent: Claude:Opus 4.5

## [0.6.14] - 2026-01-14

### Added
- **Phase 2: File Handling API** (Native provider file stores):
  - **OpenAI Vector Stores**:
    - `CreateVectorStore` - Create OpenAI vector stores with optional expiration
    - `UploadFileToVectorStore` - Upload files with automatic processing polling
    - `DeleteVectorStore` - Delete vector stores
    - `GetVectorStore` - Retrieve vector store info and file counts
    - `ListVectorStores` - List all vector stores for account
  - **Gemini FileSearchStore** (REST API wrapper):
    - `CreateFileSearchStore` - Create Gemini file search stores
    - `UploadFileToFileSearchStore` - Upload files with operation monitoring
    - `DeleteFileSearchStore` - Delete stores with force option
    - `GetFileSearchStore` - Retrieve store info and document counts
    - `ListFileSearchStores` - List all file search stores
  - **FileService Provider Routing**:
    - Routes `CreateFileStore`, `UploadFile`, `DeleteFileStore`, `GetFileStore`, `ListFileStores` by provider
    - `PROVIDER_OPENAI` → OpenAI Vector Stores API
    - `PROVIDER_GEMINI` → Gemini FileSearchStore REST API
    - `PROVIDER_UNSPECIFIED` → Internal Qdrant-based RAG (existing behavior)

Agent: Claude:Opus 4.5

## [0.6.13] - 2026-01-14

### Added
- **Gemini Provider Enhancements** (Phase 1 of Solstice migration):
  - Request/response JSON capture for debugging
  - Debug logging mode via `WithDebugLogging()` client option
  - Inline images support via `InlineImages` in `GenerateParams`
  - FileSearch with FileSearchStore integration
  - Thinking configuration (level, budget, include thoughts) for non-Flash models
  - Structured output (JSON mode with schema) via `ExtraOptions["structured_output"]`
  - File ID to filename mapping in system prompt
  - Block reason detection for safety filter responses
  - Conversation history truncation (50000 char limit)
  - Improved retry error detection (resource exhausted, overloaded)

- **Anthropic Provider Enhancements** (Phase 1 of Solstice migration):
  - Request/response JSON capture for debugging
  - Debug logging mode via `WithDebugLogging()` client option
  - Extended thinking support via `ExtraOptions["thinking_enabled"]`
  - Thinking budget configuration via `ExtraOptions["thinking_budget"]`
  - Include thoughts option via `ExtraOptions["include_thoughts"]`
  - Extended timeout (15 min) for thinking operations
  - Streaming accumulation for thinking responses (required by Anthropic API)
  - Conversation history truncation (50000 char limit)
  - Improved retry error detection

- **Provider Interface Updates**:
  - Added `InlineImage` type to provider package
  - Added `InlineImages` field to `GenerateParams`

Agent: Claude:Opus 4.5

## [0.6.12] - 2026-01-14

### Added
- **OpenAI Provider Enhancements** (Phase 1 of Solstice migration):
  - Request/response JSON capture for debugging (new `RequestJSON`/`ResponseJSON` fields in `GenerateResult`)
  - HTTP capture utility (`internal/httpcapture`) for intercepting API payloads
  - Verbosity setting support via `ExtraOptions["verbosity"]`
  - Prompt cache retention for gpt-5.x models via `ExtraOptions["prompt_cache_retention"]`
  - Citation marker stripping (removes `fileciteturn` markers from GPT-5 responses)
  - Debug logging mode via `WithDebugLogging()` client option
  - Improved retry error detection (broader network error matching)

Agent: Claude:Opus 4.5

## [0.6.11] - 2026-01-14

### Added
- **Solstice Migration Plan**: Created comprehensive migration plan document
  - `.planning/SOLSTICE_MIGRATION_PLAN.md` - 5-phase plan to replace Solstice's internal LLM code with Airborne
  - Phase 1: Core provider parity (OpenAI, Gemini, Anthropic enhancements)
  - Phase 2: File handling API for RAG functionality
  - Phase 3: Additional providers (21 providers from Solstice)
  - Phase 4: Advanced features (failover, streaming, structured output)
  - Phase 5: Solstice integration with gradual rollout
  - Estimated effort: 8-11 weeks

Agent: Claude:Opus 4.5

## [0.6.10] - 2026-01-14

### Added
- **Codebase Documentation**: Created `.planning/codebase/` with 7 structured analysis documents
  - STACK.md - Technology stack and dependencies (Go 1.25, gRPC, LLM SDKs)
  - ARCHITECTURE.md - Layered architecture, data flow, key abstractions
  - STRUCTURE.md - Directory layout and module organization
  - CONVENTIONS.md - Code style, naming patterns, error handling
  - TESTING.md - Test framework, patterns, coverage approach
  - INTEGRATIONS.md - External services (OpenAI, Gemini, Anthropic, Redis, RAG stack)
  - CONCERNS.md - Technical debt and areas for improvement

Agent: Claude:Opus 4.5

## [0.6.9] - 2026-01-09

### Changed
- **Project Renamed**: Renamed from `aibox` to `airborne`
  - GitHub repository renamed to `ai8future/airborne`
  - Go module path changed to `github.com/ai8future/airborne`
  - Proto package renamed from `aibox.v1` to `airborne.v1`
  - Environment variables changed from `AIBOX_*` to `AIRBORNE_*`
  - Config file renamed from `configs/aibox.yaml` to `configs/airborne.yaml`
  - Binary renamed from `aibox` to `airborne`
  - Docker service and user renamed from `aibox` to `airborne`

Agent: Claude:Opus 4.5

## [0.6.8] - 2026-01-09

### Removed
- **Admin UI**: Completely removed web-based admin interface
  - Bizops already provides this functionality
  - Removed `internal/admin/` package (auth.go, server.go, handlers.go, frontend/)
  - Removed `AdminPort` config option and `AIBOX_ADMIN_PORT` env var
  - Simplified main.go by removing HTTP server code

Agent: Claude:Opus 4.5

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
