Date Created: 2026-01-20 18:02:34 +0100
TOTAL_SCORE: 82/100

Scope
- Quick scan of core service, provider, configuration, RAG, auth, pricing, and file-store modules.
- Focused on code quality, duplication, and maintainability risks rather than feature changes.

Score Rationale (82/100)
- Strengths: clear provider interface, reasonable modular boundaries, and security validation around base URLs; test coverage exists across key areas.
- Deductions: repeated provider setup logic, manual env override plumbing, and model pricing collisions introduce drift and maintenance costs.

Findings (ordered by severity)
1) High - Pricing collisions from provider-agnostic model map
   - Evidence: `internal/pricing/pricing.go` merges all provider models into a single flat map keyed only by model string; later files override earlier ones. `findPricingByPrefix` can also match the wrong model when providers share prefixes.
   - Risk: incorrect cost accounting if two providers share model names or prefixes.
   - Improvement: key pricing by provider+model, or require provider context in cost calculation; add a guard for collisions when loading pricing files.

2) Medium - Provider request setup and retry logic duplicated across core providers
   - Evidence: `internal/provider/openai/client.go`, `internal/provider/gemini/client.go`, `internal/provider/anthropic/client.go`, `internal/provider/compat/openai_compat.go` each repeat the same steps (timeout setup, base URL validation, capture transport, logging, retry/backoff).
   - Risk: behavior drift (timeouts, error handling) and higher effort to fix bugs consistently.
   - Improvement: centralize shared behavior in a helper/base client (timeouts, base URL validation, retries, logging), with provider-specific request/response logic in small adapters.

3) Medium - OpenAI-compatible wrapper providers are nearly identical
   - Evidence: `internal/provider/deepseek/client.go`, `internal/provider/openrouter/client.go`, `internal/provider/perplexity/client.go`, and others all wrap `compat.NewClient` with near-identical option plumbing.
   - Risk: drift when adding new flags or capabilities, duplication of boilerplate.
   - Improvement: build a registry of compat providers (name, base URL, defaults, capabilities), and generate or construct clients from that list.

4) Medium - Manual env override parsing is large and repetitive
   - Evidence: `internal/config/config.go` `applyEnvOverrides` is long, with repeated parse-and-log patterns for booleans, ints, and strings.
   - Risk: easy to miss new config fields, inconsistent logging or validation; tests are harder to target.
   - Improvement: create helper functions for typed env reads or move to a table-driven map of env keys to setters. This reduces duplication and makes it easier to test.

5) Medium - File-store client logic is duplicated and inconsistently abstracted
   - Evidence: `internal/provider/openai/filestore.go` and `internal/provider/gemini/filestore.go` both validate base URLs, run polling loops, and perform similar error handling, but with different shapes and ad-hoc HTTP handling.
   - Risk: inconsistent behavior under failure (timeouts, retries), and more complex future changes.
   - Improvement: add a shared file-store interface with helper utilities (polling/backoff, base URL validation) to standardize behavior.

6) Low - Unary vs streaming chat path duplicates response handling
   - Evidence: `internal/service/chat.go` duplicates HTML rendering, slash command handling, and persistence logic in both `GenerateReply` and `GenerateReplyStream`.
   - Risk: future changes can diverge or miss one path.
   - Improvement: extract common completion/persistence steps into shared helpers; keep stream-specific logic minimal.

7) Low - Error sanitization uses substring matching
   - Evidence: `internal/errors/sanitize.go` relies on substring checks against error strings.
   - Risk: false positives/negatives as provider error messages evolve.
   - Improvement: prefer typed errors or provider-specific error mapping with explicit codes.

8) Low - Multiple config schemas for provider settings
   - Evidence: provider-related config structs exist in `internal/config/config.go`, `internal/tenant/config.go`, and `internal/provider/provider.go` with similar fields.
   - Risk: field drift or mismatch over time; increased maintenance cost.
   - Improvement: normalize to a shared internal struct or provide explicit mapping functions with tests to ensure parity.

Test/Validation Gaps Noted
- No focused tests for pricing collisions or prefix matching (`internal/pricing/pricing.go`).
- Env override parsing logic lacks targeted unit tests (`internal/config/config.go`).
- Wrapper providers rely on compatibility tests but not on behavior regression tests for shared settings.

Open Questions / Assumptions
- Is pricing intended to be provider-agnostic? If not, cost calculations should include provider context to avoid collisions.
- Is base URL override required for every provider? If not, consider restricting to reduce surface area and simplify validation.
- Are large file uploads expected for Gemini file search? `internal/provider/gemini/filestore.go` reads entire content into memory; streaming support might be needed if large files are common.

Suggested Priority Order
- P1: Fix pricing collisions by including provider context or collision detection.
- P2: Centralize provider request setup/retry logic; generate or registry-define compat providers.
- P3: Refactor env override parsing into reusable helpers or table-driven mappings.
