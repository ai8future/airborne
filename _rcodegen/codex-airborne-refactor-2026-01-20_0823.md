Date Created: 2026-01-20 08:23:04 +0100
TOTAL_SCORE: 84/100

# Airborne Refactor Review (Quick Scan)

## Scope
- Focused pass over provider clients, service layer, config, and RAG helpers.
- Looked for duplication, oversized files, and stringly-typed configuration hotspots.
- Not an exhaustive audit; code reading kept intentionally brief.

## Score Rationale (84/100)
- Solid structure, clear package boundaries, and strong use of tests and validation.
- Main deductions are for repeated provider setup logic, very large files mixing concerns, and stringly-typed config that is easy to drift.

## Strengths Noted
- Consistent SSRF protection via base URL validation in provider clients (e.g., `internal/validation/url.go`).
- Provider interface is clear and comprehensive, with good test coverage in `internal/provider/providers_test.go`.
- RAG chunking and validation logic are reasonably isolated and readable (e.g., `internal/rag/chunker/chunker.go`).

## Opportunities to Improve Maintainability & Reduce Duplication

### 1) Consolidate provider client setup + request scaffolding
Evidence:
- Repeated patterns for timeouts, API key checks, base URL validation, and debug capture across:
  - `internal/provider/openai/client.go`
  - `internal/provider/gemini/client.go`
  - `internal/provider/anthropic/client.go`
  - `internal/provider/compat/openai_compat.go`
  - `internal/provider/openai/filestore.go`
  - `internal/provider/gemini/filestore.go`

Why it matters:
- Each provider independently re-implements safety checks and defaults. This increases drift risk and makes bug fixes repetitive.

Suggested direction:
- Introduce a small shared helper (e.g., `internal/provider/providerutil`) to handle:
  - default timeout enforcement
  - required API key checks
  - base URL validation + normalization
  - optional debug/capture wiring
- Centralize common error phrasing to keep UX consistent.

### 2) Extract a retry helper instead of inline loops
Evidence:
- Similar retry loops with `retry.MaxAttempts`, backoff, and request-scoped timeouts appear in:
  - `internal/provider/openai/client.go`
  - `internal/provider/anthropic/client.go`
  - `internal/provider/compat/openai_compat.go`

Why it matters:
- Repeated control flow increases the chance of inconsistent behavior across providers (e.g., timeout handling or retryable error parsing).

Suggested direction:
- Add a `retry.Do(ctx, func(attempt int) error)` helper in `internal/retry` that wraps logging, timeouts, and backoff.
- Keep provider-specific logic inside the callback but reuse the orchestration.

### 3) Reduce stringly-typed config in ProviderConfig.ExtraOptions
Evidence:
- `internal/provider/provider.go` defines `ExtraOptions map[string]string`.
- Each provider interprets different keys with ad-hoc parsing:
  - OpenAI: `reasoning_effort`, `service_tier`, `verbosity`, `prompt_cache_retention` in `internal/provider/openai/client.go`.
  - Gemini: `safety_threshold`, `thinking_level`, `thinking_budget`, `include_thoughts` in `internal/provider/gemini/client.go`.
  - Anthropic: `thinking_enabled`, `thinking_budget`, `include_thoughts` in `internal/provider/anthropic/client.go`.

Why it matters:
- Typos or missing validation silently change behavior. It is also hard to discover supported options.

Suggested direction:
- Define typed option structs per provider (e.g., `OpenAIOptions`, `GeminiOptions`) and decode once.
- Provide constants or enums for option keys if staying with a map.

### 4) Break up oversized files with mixed responsibilities
Evidence:
- `internal/service/chat.go` is ~1,100 lines and includes: validation, command parsing, provider selection, RAG injection, response mapping, tool conversion, and image generation.
- Provider clients are very large:
  - `internal/provider/gemini/client.go` (~1,150 lines)
  - `internal/provider/openai/client.go` (~830 lines)

Why it matters:
- Large files inhibit navigation, make reviews slower, and amplify merge conflicts.

Suggested direction:
- Split `internal/service/chat.go` into smaller files or subpackages by concern (e.g., request prep, RAG handling, response mapping, tool conversion, image handling).
- Split provider clients into request-building, response-parsing, and streaming-specific files.

### 5) Eliminate duplicated helper blocks inside provider clients
Evidence:
- The “file ID mapping” system instruction block is duplicated in `internal/provider/gemini/client.go` (appears in both unary and streaming paths).

Why it matters:
- Duplication in a single file increases drift risk; the two paths can diverge without obvious signals.

Suggested direction:
- Extract a small helper like `buildSystemInstruction(params)` used by both paths.
- Apply the same pattern to other duplicated logic (e.g., generation config creation).

### 6) Simplify OpenAI-compat provider wrappers
Evidence:
- Many providers have identical wrapper structure (e.g., `internal/provider/deepseek/client.go`, `internal/provider/grok/client.go`, `internal/provider/together/client.go`).

Why it matters:
- Adding or updating a provider requires touching many nearly identical files, which is error-prone and time-consuming.

Suggested direction:
- Create a registry in `internal/provider/compat` with per-provider config data and a single constructor.
- Optionally generate wrappers from a small config file if explicit packages are still desired.

### 7) Unify file store client logic at the service boundary
Evidence:
- `internal/service/files.go` delegates to provider-specific file store helpers with similar error handling and validation.

Why it matters:
- Service code has to know provider details and branching logic, which makes it harder to extend.

Suggested direction:
- Define a `FileStoreClient` interface with `CreateStore`, `UploadFile`, etc., then inject provider-specific implementations. The service would become provider-agnostic.

### 8) Standardize provider name constants
Evidence:
- Provider names are strings in multiple areas (`internal/config/config.go`, service/provider selection logic, and provider clients).

Why it matters:
- Stringly identifiers drift easily, especially when adding new providers or renaming models.

Suggested direction:
- Introduce constants for provider IDs in one package and reference them consistently (e.g., `provider.IDOpenAI`).

## Quick Wins (Low Effort)
- Add helper to share `ctx` timeout logic across providers (same pattern in multiple files).
- Create a `buildSystemInstruction` helper in Gemini provider to remove the duplicated block.
- Extract shared request-building from `GenerateReply` / `GenerateReplyStream` within each provider.

## Longer-Term Refactors
- Introduce a common provider setup module (API key + base URL validation + capture + retry wrapper).
- Split `internal/service/chat.go` into smaller files to isolate RAG/tool/image concerns.
- Replace `ExtraOptions` map with typed provider option structs.

## Overall Grade
- 84/100: Strong fundamentals and solid testing, but meaningful duplication and oversized modules are starting to tax maintainability.
