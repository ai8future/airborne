# Code Refactor Report
Date Created: 2026-01-08 00:20:47 +0100
Date Updated: 2026-01-08

## Scope
- Reviewed Go services under `cmd/`, `internal/`, and generated gRPC interfaces for usage context.
- Focused on duplication, code quality, and maintainability (no code changes made).
- Skipped `_studies/` and `_proposals/` per agent instructions.

## High-Impact Refactor Opportunities
1) Consolidate provider configuration types and mapping logic.
- Evidence: `internal/config/config.go`, `internal/tenant/config.go`, `internal/provider/provider.go`, `internal/service/chat.go`.
- Issue: Three separate ProviderConfig structs (plus PB config) require manual mapping/merging in `buildProviderConfig`, which is easy to drift.
- Refactor direction: Define a single internal provider config type (or a shared adapter with explicit conversion helpers) and reuse it across config/tenant/request paths. This reduces duplicate fields and simplifies defaults/validation.

2) Centralize retry/backoff logic across providers.
- Evidence: `internal/provider/openai/client.go`, `internal/provider/anthropic/client.go`, `internal/provider/gemini/client.go`.
- Issue: Each provider has its own maxAttempts/backoff/isRetryableError/sleepWithBackoff logic; policy drift is likely.
- Refactor direction: Move retry policies to a shared helper (e.g., `internal/provider/retry`) and standardize on a single backoff strategy and error classification contract.

3) Reduce config system duplication between `internal/config` and `internal/tenant`.
- Evidence: `internal/config/config.go`, `internal/tenant/env.go`, `internal/tenant/manager.go`.
- Issue: Two configuration loaders parse overlapping environment variables and defaults. This increases maintenance cost and risks inconsistent behavior.
- Refactor direction: Unify env parsing in one place and pass resolved values into tenant manager; consider moving tenant config directory into `config.Config` and removing the parallel `EnvConfig` if not needed.

## Maintainability Improvements (Medium Priority)
4) Expand tenant ID extraction to avoid per-RPC special cases.
- Evidence: `internal/auth/tenant_interceptor.go`.
- Issue: `extractTenantID` only handles `GenerateReplyRequest` and `SelectProviderRequest`; other RPCs (e.g., FileService calls) cannot provide tenant IDs in multitenant mode.
- Refactor direction: Standardize tenant ID transport via metadata or add a shared interface/utility for any request carrying `tenant_id`, then use it across services.

5) Improve determinism around tenant/provider defaults.
- Evidence: `internal/tenant/config.go` (DefaultProvider loops map), `internal/service/chat.go` (fallback selection).
- Issue: When failover is not enabled, map iteration order decides provider selection (non-deterministic), and fallback order is hard-coded in chat service rather than tenant config.
- Refactor direction: Always prefer a deterministic order (e.g., tenant failover order or a sorted provider list) and reuse tenant config for fallback order to avoid surprises.

## Smaller Refactors / Hygiene
6) Reduce repeated environment parsing and validation patterns.
- Evidence: `internal/tenant/env.go`, `internal/config/config.go`.
- Issue: Duplicate env parsing and validation logic makes it easy to forget updating one path.
- Refactor direction: Extract shared helpers for parsing ints/bools and validating TLS settings.

7) Improve interface boundaries for external dependencies.
- Evidence: `internal/redis/client.go`, `internal/rag/vectorstore/qdrant.go`, `internal/rag/extractor/docbox.go`.
- Issue: Concrete clients are constructed inline; tests may require external services or heavy mocking.
- Refactor direction: Define small interfaces for Redis, vector store, and extractor clients in their packages and inject them into services for easier testing and maintainability.

## Suggested Sequencing
- Phase 1 (medium): centralize retry/backoff logic
- Phase 2 (larger): consolidate config types + loaders; standardize tenant ID transport; refactor provider config mapping

## Notes
- Several items were fixed in v0.5.6:
  - #2 (Extract shared request preparation) - FIXED: Added `prepareRequest()` helper
  - #4 (Streaming false positive) - FIXED: OpenAI/Gemini now return `SupportsStreaming() = false`
  - #6 (Tenant reload path bug) - FIXED: Manager now stores effective configDir
  - #8 (Tenant vs client ID semantics) - NOT A BUG: Intentional fallback behavior
  - #9 (Dead code) - FIXED: Removed unused selectProvider, debug flags, ProviderKeys
  - #10 (RAG payload keys) - FIXED: Added constants for payload field names
