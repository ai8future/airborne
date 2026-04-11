Date Created: 2026-01-18 21:43:42 +0100
TOTAL_SCORE: 84/100

# Airborne Code Quality & Maintainability Review (Quick Scan)

## Scope and Method
- Time-boxed scan of core runtime and provider code paths; no code edits.
- Focused on duplication, maintainability risks, and configuration consistency.
- Files sampled include provider clients, service layer, RAG service, and config/tenant logic.

## Overall Assessment
The codebase is generally well-structured with clear package boundaries, strong validation, and solid test coverage in key areas. The biggest maintainability drag is duplication across provider implementations (request construction, retries, logging, config parsing, and provider dispatch). Consolidating these patterns would reduce future regression risk as new providers and features are added.

## Key Opportunities (Ordered by Impact)

1) Consolidate provider configuration and HTTP client setup
- Evidence: repeated API key validation, default model selection, base URL validation, and httpcapture wiring in `internal/provider/openai/client.go`, `internal/provider/gemini/client.go`, `internal/provider/anthropic/client.go`, `internal/provider/compat/openai_compat.go`.
- Why it matters: small divergences (timeouts, base URL validation, debug toggles) are easy to introduce and hard to detect.
- Suggestion: introduce a shared helper or base type to normalize `provider.ProviderConfig`, resolve default model + override, validate base URL, and return a configured HTTP client or options bundle.

2) Deduplicate request-building between streaming and non-streaming paths
- Evidence: `GenerateReply` and `GenerateReplyStream` duplicate request construction in `internal/provider/openai/client.go` and `internal/provider/gemini/client.go` (parallel code blocks build params, optional fields, and tools).
- Risk: drift between streaming vs non-streaming capabilities (e.g., new fields added to one path only).
- Suggestion: extract a `buildRequest(...)` or `buildGenerateParams(...)` helper per provider that both paths call.

3) Normalize conversation history formatting and truncation across providers
- Evidence: `buildUserPrompt` in `internal/provider/openai/client.go` uses a custom textual history format; `buildContents` in `internal/provider/gemini/client.go` and `buildMessages` in `internal/provider/anthropic/client.go` implement separate history handling and have separate `maxHistoryChars` constants.
- Why it matters: inconsistent history handling leads to different behavior per provider and complicates debugging/reporting.
- Suggestion: a shared “history formatter” or adapter to consistently apply trimming rules, role mapping, and whitespace cleanup, with provider-specific conversion at the final step.

4) Replace stringly-typed `ExtraOptions` with typed, validated option structs
- Evidence: `provider.ProviderConfig.ExtraOptions` in `internal/provider/provider.go` is accessed via string keys in `internal/provider/openai/client.go`, `internal/provider/gemini/client.go`, and `internal/provider/anthropic/client.go`.
- Risk: silent typos, inconsistent defaults, and limited discoverability of supported options.
- Suggestion: define provider-specific option structs with explicit fields and validation; parse once (e.g., in `buildProviderConfig` in `internal/service/chat.go`).

5) Centralize provider registry and mapping logic
- Evidence: provider dispatch is spread across multiple switches in `internal/service/chat.go` and `internal/service/files.go`, plus conversions like `mapProviderToProto` in `internal/service/chat.go`.
- Risk: adding a new provider requires edits in multiple locations; easy to miss a switch.
- Suggestion: create a registry mapping provider name/enum to a struct of capabilities and handler interfaces (chat, file store, image gen), and reuse across services.

6) Unify file-store workflows across providers
- Evidence: near-duplicate create/upload/list/delete flows for OpenAI and Gemini in `internal/service/files.go`, plus similar config validation in `internal/provider/openai/filestore.go` and `internal/provider/gemini/filestore.go`.
- Suggestion: extract a file-store interface with provider-specific implementations and a shared adapter in the service layer to reduce switch-case duplication.

7) Standardize logging fields and retry behavior
- Evidence: logging in `internal/provider/openai/client.go`, `internal/provider/gemini/client.go`, `internal/provider/anthropic/client.go`, and `internal/provider/compat/openai_compat.go` varies in fields and message shape; retry loops are similar but slightly divergent.
- Suggestion: unify logging helper and retry wrapper to keep fields consistent (provider, model, request_id, attempt, latency) and simplify audits.

## Quick Wins
- Extract per-provider request builders so streaming/unary stay in lockstep (`internal/provider/openai/client.go`, `internal/provider/gemini/client.go`).
- Introduce constants for `ExtraOptions` keys to reduce typo risk (`internal/provider/provider.go`).
- Add a central provider registry to cut down on switch-case edits when adding providers (`internal/service/chat.go`, `internal/service/files.go`).

## Longer-Term Refactors
- Build a shared provider base layer that handles config normalization, timeout setup, retry, logging, and optional parameter mapping; let provider-specific code focus on request/response translation.
- Define a unified conversation-history formatter that can be adapted per provider while preserving consistent trimming and role mapping.
- Add a file-store interface and handler registry to reduce duplicated OpenAI/Gemini flows and make internal Qdrant integration consistent.

## Notable Strengths
- Clear separation of concerns between service, provider, validation, and RAG modules.
- Strong validation and permission checks before sensitive operations (`internal/service/chat.go`, `internal/service/files.go`).
- Good use of tests for provider formatting and chat request validation (`internal/provider/*_test.go`, `internal/service/chat_test.go`).

## Suggested Follow-up Questions
- Do you want provider behavior to be intentionally different per provider (prompt formatting, truncation), or is consistency the goal?
- Is there a preferred direction for shared provider infrastructure (base struct vs. helper package vs. registry)?

