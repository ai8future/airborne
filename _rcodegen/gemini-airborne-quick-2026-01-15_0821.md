Date Created: 2026-01-15 08:21
Date Updated: 2026-01-15

# REMAINING ITEMS

## REFACTOR SUGGESTIONS (Future Consideration)

### 1. Consolidate Provider Clients
**Description:**
Many provider packages (mistral, deepseek, fireworks, etc.) are identical thin wrappers around `internal/provider/compat`.
**Recommendation:**
Instead of maintaining separate folders for each OpenAI-compatible provider, create a generic `openai_compat` provider factory that takes a configuration object. This would reduce code duplication and maintenance overhead. New providers could be added via configuration rather than new code files.

### 2. Centralize Error Handling
**Description:**
Retry logic (`isRetryableError`) is duplicated or implemented similarly across providers.
**Recommendation:**
Move `isRetryableError` and backoff logic to `internal/provider/common` or similar to ensure consistent behavior across all providers.

---

*All actionable audit and fix items have been moved to the implementation plan at `docs/plans/2026-01-15-audit-fixes.md`.*
