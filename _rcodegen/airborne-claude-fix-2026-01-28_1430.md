Date Created: 2026-01-28T14:30:00-08:00
Date Updated: 2026-01-28
TOTAL_SCORE: 87/100

# Airborne Codebase Audit Report

**Auditor:** Claude:Opus 4.5
**Date:** January 28, 2026
**Version Analyzed:** 1.7.12
**Codebase:** Multi-provider AI Gateway (Go/gRPC)

---

## Executive Summary

Airborne is a well-architected, production-grade AI gateway with multi-tenant support, comprehensive provider implementations, and robust error handling. The codebase demonstrates strong security practices including SSRF protection, API key security, and proper tenant isolation. Overall code quality is high with good separation of concerns and consistent patterns.

**Grade Breakdown:**
- Security: 90/100
- Code Quality: 85/100
- Error Handling: 88/100
- Architecture: 90/100
- Testing: 82/100
- Documentation: 85/100

---

## Issues Identified

### CRITICAL (0 Issues)

No critical security vulnerabilities or data loss risks identified.

---

### HIGH SEVERITY (0 Issues)

*H1 (Goroutine leak in Gemini streaming) - FIXED in v1.7.14*

---

### MEDIUM SEVERITY (1 Issue)

*M2 (Context check before persistence) - FIXED in v1.7.14*
*M3 (Non-deterministic DefaultProvider) - FIXED in v1.7.14*
*M4 (Verbose httpcapture logging) - FIXED in v1.7.14*

#### M1: Hardcoded Tenant IDs in ValidTenantIDs Map
**Location:** `internal/db/repository.go:14-19`

**Description:** Valid tenant IDs are hardcoded rather than being dynamically loaded from configuration. Adding new tenants requires code changes.

**Recommendation:** Consider loading valid tenant IDs from the tenant manager configuration or database rather than hardcoding. (Deferred - architectural change)
 	}
```

---

### LOW SEVERITY (5 Issues)

#### L1: Missing Error Check for JSON Marshal in extractFunctionCalls
**Location:** `internal/provider/gemini/client.go:1157-1161`

**Description:** JSON marshal error is logged but the function continues with empty JSON, which could mask issues.

**Current Code:**
```go
argsJSON, err := json.Marshal(part.FunctionCall.Args)
if err != nil {
    slog.Warn("failed to marshal function call args", "error", err)
    argsJSON = []byte("{}")
}
```

**Recommendation:** This is acceptable behavior for resilience, but consider adding the error to the ToolCall for debugging.

---

#### L2: fmt.Sscanf Used Without Error Checking
**Location:** `internal/provider/gemini/client.go:193-199`

**Description:** `fmt.Sscanf` return value is not checked when parsing thinking budget.

**Current Code:**
```go
if thinkingBudgetStr != "" {
    var budget int
    fmt.Sscanf(thinkingBudgetStr, "%d", &budget)
    if budget > 0 {
        budget32 := int32(budget)
        thinkingConfig.ThinkingBudget = &budget32
    }
}
```

**Patch-Ready Diff:**
```diff
--- a/internal/provider/gemini/client.go
+++ b/internal/provider/gemini/client.go
@@ -190,7 +190,10 @@ func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateParam
 			}
 			if thinkingBudgetStr != "" {
 				var budget int
-				fmt.Sscanf(thinkingBudgetStr, "%d", &budget)
+				if _, err := fmt.Sscanf(thinkingBudgetStr, "%d", &budget); err != nil {
+					slog.Warn("invalid thinking_budget value", "value", thinkingBudgetStr, "error", err)
+					budget = 0
+				}
 				if budget > 0 {
 					budget32 := int32(budget)
 					thinkingConfig.ThinkingBudget = &budget32
```

---

#### L3: Duplicate Logic for Thinking Config in GenerateReply and GenerateReplyStream
**Location:** `internal/provider/gemini/client.go:171-202` and `internal/provider/gemini/client.go:470-502`

**Description:** The thinking configuration block is duplicated between unary and streaming methods. This violates DRY principle.

**Recommendation:** Extract shared configuration building to a helper function.

**Patch-Ready Diff:**
```diff
--- a/internal/provider/gemini/client.go
+++ b/internal/provider/gemini/client.go
@@ -1207,3 +1207,31 @@ func extractCodeExecutionResults(resp *genai.GenerateContentResponse) []provider

 	return executions
 }
+
+// applyThinkingConfig configures thinking mode based on model and extra options.
+func applyThinkingConfig(generateConfig *genai.GenerateContentConfig, model string, extraOptions map[string]string) {
+	modelLower := strings.ToLower(model)
+	isFlashModel := strings.Contains(modelLower, "flash")
+	isProModel := strings.Contains(modelLower, "pro")
+
+	if isFlashModel {
+		return // Thinking not supported on Flash models
+	}
+
+	thinkingLevel := extraOptions["thinking_level"]
+	thinkingBudgetStr := extraOptions["thinking_budget"]
+	includeThoughts := extraOptions["include_thoughts"] == "true"
+
+	// Default to HIGH thinking level for Pro models
+	if thinkingLevel == "" && isProModel {
+		thinkingLevel = "HIGH"
+	}
+
+	if thinkingLevel == "" && thinkingBudgetStr == "" && !includeThoughts {
+		return
+	}
+
+	thinkingConfig := &genai.ThinkingConfig{
+		IncludeThoughts: includeThoughts,
+	}
+	// ... rest of thinking config logic
+}
```

---

#### L4: Missing import for `sort` in tenant/config.go
**Location:** `internal/tenant/config.go`

**Description:** The suggested fix for M3 requires importing the `sort` package.

---

#### L5: Magic Number for ragSnippetMaxLen
**Location:** `internal/service/chat.go:33`

**Description:** The constant 200 for snippet length is defined but could benefit from documentation explaining the rationale.

**Current Code:**
```go
const (
    // ragSnippetMaxLen is the maximum length for RAG citation snippets.
    ragSnippetMaxLen = 200
)
```

**Recommendation:** The comment is adequate but could include why 200 was chosen.

---

## Security Analysis

### Strengths

1. **SSRF Protection (Excellent)**
   - `validation/url.go` implements comprehensive URL validation
   - Blocks private IPs, cloud metadata endpoints, dangerous protocols
   - DNS resolution validation prevents hostname-based SSRF bypasses

2. **API Key Security (Excellent)**
   - API keys cannot be overridden via client requests (`service/config/builder.go:49-53`)
   - Clear security comments documenting the constraint

3. **Tenant Isolation (Good)**
   - Per-tenant database tables prevent cross-tenant data access
   - Thread-safe tenant manager with proper locking

4. **Error Sanitization (Good)**
   - `errors/sanitize.go` prevents leaking internal error details to clients
   - Pattern-based mapping to client-safe messages

5. **Rate Limiting (Good)**
   - Atomic Lua scripts for Redis-based rate limiting
   - Per-client RPM, RPD, and TPM limits

### Potential Improvements

1. Consider adding request signing for inter-service communication
2. Add audit logging for admin operations
3. Consider implementing request ID correlation across services

---

## Architecture Analysis

### Strengths

1. **Clean Provider Abstraction**
   - Well-defined `Provider` interface with clear contracts
   - Consistent implementation across OpenAI, Gemini, and Anthropic

2. **Proper Configuration Layering**
   - Environment → Config File → Frozen Config → Tenant Config → Request Override
   - Security constraints properly enforced at each layer

3. **Robust Retry Logic**
   - Centralized retry patterns in `retry` package
   - Pattern-based retryability detection

4. **Multi-Tenant Support**
   - Clean separation with tenant-specific database tables
   - Thread-safe tenant config management

### Suggestions

1. Consider extracting common streaming logic to reduce code duplication
2. The `chat.go` service file (1260 LOC) could be split into smaller focused files

---

## Testing Analysis

**Test Files Found:** 40+
**Estimated Test LOC:** ~11,300

### Coverage by Area
- Provider clients: Good coverage
- Authentication: Good coverage
- Rate limiting: Good coverage
- Repository: Moderate coverage
- RAG service: Good coverage
- Service layer: Could use more integration tests

### Missing Test Areas
1. End-to-end streaming tests with context cancellation
2. Concurrent request tests for tenant isolation
3. Config builder edge cases

---

## Code Smells

1. **Large File:** `chat.go` at 1260 lines should be split
2. **Duplicate Code:** Thinking config in Gemini client appears twice
3. **Magic Strings:** Provider names ("openai", "gemini", "anthropic") could be constants

---

## Recommendations Summary

### Immediate Actions (This Sprint)
1. Fix H1: Add context cancellation check in streaming goroutine
2. Fix M4: Change httpcapture logging from Info to Debug

### Short-Term (Next Sprint)
1. Address M2: Add context check before persistence goroutine
2. Address M3: Make DefaultProvider deterministic
3. Address L2: Add error checking to fmt.Sscanf

### Technical Debt (Backlog)
1. Address M1: Dynamic tenant ID validation
2. Extract common Gemini client logic (L3)
3. Split chat.go into smaller files
4. Add provider name constants

---

## Conclusion

Airborne is a well-designed, production-ready codebase with strong security practices. The identified issues are relatively minor and don't represent significant risks. The architecture is clean with good separation of concerns, and the code follows Go best practices consistently.

**Overall Assessment:** Ready for production with minor improvements recommended.

---

*Report generated by Claude Code Audit Agent (Claude:Opus 4.5)*
