Date Created: 2026-02-15T23:15:00Z
TOTAL_SCORE: 62/100

# Airborne Unit Test Coverage Report

**Agent**: Claude:Opus 4.6 (Claude Code)
**Project**: airborne (Multi-Provider AI Gateway)
**Version**: 1.8.3
**Go Version**: 1.25.5

---

## Executive Summary

Airborne has **47 test files** covering **~30 of ~46 non-generated Go source files** (excluding `gen/`, `cmd/`, and interface-only files). The existing tests are generally well-written with good use of table-driven patterns and mocks, but significant gaps exist in the CLI, admin HTTP server, database repository, retry utilities, image generation config, and provider model selection. The test infrastructure (TestMain fixtures, mock utilities in `rag/testutil`) is solid.

### Score Breakdown

| Category | Points | Max | Notes |
|----------|--------|-----|-------|
| Core service tests (chat, files, admin) | 14 | 15 | chat_test excellent; admin_test good; files_test solid |
| Provider tests | 10 | 15 | 4/25+ providers tested; compat layer covers others partially |
| Auth tests | 9 | 10 | Comprehensive; ratelimit has skipped tests |
| Config tests | 8 | 10 | Good env/YAML coverage; envutil tested |
| RAG tests | 9 | 10 | Excellent mock infrastructure; good validation |
| Database tests | 3 | 10 | Only models_test; no repository or postgres tests |
| Retry/resilience tests | 4 | 5 | IsRetryable comprehensive; backoff/context untested |
| Validation tests | 5 | 5 | URL SSRF and limits both well-tested |
| CLI tests | 0 | 5 | **Completely untested** (output.go, client.go, commands.go) |
| Admin HTTP server tests | 0 | 5 | **Completely untested** (1325-line server.go) |
| Miscellaneous | 0 | 10 | No imagegen/config, provider/model, auth/errors tests |
| **Total** | **62** | **100** | |

---

## Detailed Gap Analysis

### 1. CRITICAL: `internal/cli/output.go` — No Tests

Pure formatting functions with no external dependencies. **Highest ROI for new tests** — entirely unit-testable with zero mocking.

**Functions needing tests:**
- `FormatTokens(int) string` — threshold at 1000, K suffix formatting
- `FormatCost(float64) string` — zero cost special case, decimal formatting
- `FormatDuration(int) string` — threshold at 1000ms, seconds formatting
- `FormatTimestamp(string) string` — RFC3339 parsing, fallback format, invalid input
- `TruncateString(string, int) string` — newline replacement, boundary conditions, ellipsis

### 2. CRITICAL: `internal/provider/model.go` — No Tests

Simple priority-based model selection with only 18 lines. Trivial to test but important correctness guarantee.

**Functions needing tests:**
- `SelectModel(configModel, defaultModel, overrideModel string) string` — override priority, whitespace trimming, empty string handling

### 3. HIGH: `internal/retry/backoff.go` — No Tests

Exponential backoff calculation. Existing `retry_test.go` tests `IsRetryable` but not `SleepWithBackoff`.

**Functions needing tests:**
- `SleepWithBackoff(ctx, attempt)` — delay calculation, context cancellation

### 4. HIGH: `internal/retry/context.go` — No Tests

Context timeout utility. Simple but safety-critical.

**Functions needing tests:**
- `EnsureTimeout(ctx, timeout)` — existing deadline preserved, new deadline applied, noop cancel safety

### 5. HIGH: `internal/imagegen/config.go` — No Tests

Configuration getters with nil-receiver safety and defaults.

**Functions needing tests:**
- `(*Config).IsEnabled()` — nil receiver, enabled/disabled states
- `(*Config).GetProvider()` — nil receiver, empty string, custom provider
- `(*Config).GetModel()` — nil receiver, empty vs set model

### 6. MODERATE: `internal/db/repository.go` — No Tests (beyond models)

The 813-line repository has no unit tests. While full SQL testing requires a database, the table name generation and tenant validation are unit-testable.

**Functions needing tests:**
- `NewTenantRepository()` — valid/invalid tenant IDs, error wrapping
- `threadsTable()`, `messagesTable()`, `filesTable()`, etc. — legacy vs tenant-prefixed names
- `TenantID()` — returns correct tenant ID

### 7. MODERATE: `internal/auth/errors.go` — No Tests

Error sentinel values. While simple, asserting their existence and message content prevents accidental changes.

### 8. LOW: `internal/admin/server.go` — No Tests

The 1325-line admin server has complex HTTP handlers but testing requires significant mocking of `db.Client`, `tenant.Manager`, `redis.Client`, and gRPC clients. Lower ROI due to integration nature, but `detectMIMEType()` and `buildCompressedHistory()` could be unit tested.

---

## Existing Test Quality Notes

**Strengths:**
- `internal/service/chat_test.go` — 40+ test functions, excellent mock setup, thorough edge cases
- `internal/retry/retry_test.go` — 66-case table-driven `TestIsRetryable`
- `internal/rag/` — Comprehensive mock infrastructure in `testutil/mocks.go`
- `internal/config/config_test.go` — 22 tests covering env vars, YAML, TLS validation
- Table-driven tests used consistently across auth, provider, and validation packages

**Weaknesses:**
- `internal/auth/ratelimit_test.go` — Multiple tests skipped (require Docker/Redis)
- `internal/errors/sanitize_test.go` — Only 1 test function for the entire package
- No concurrent/race condition tests anywhere (despite `go test -race` in Makefile)
- No fuzzing tests anywhere

---

## Proposed Unit Tests — Patch-Ready Diffs

### Test 1: `internal/cli/output_test.go` (NEW FILE)

```diff
--- /dev/null
+++ b/internal/cli/output_test.go
@@ -0,0 +1,156 @@
+package cli
+
+import (
+	"testing"
+)
+
+func TestFormatTokens(t *testing.T) {
+	tests := []struct {
+		name   string
+		tokens int
+		want   string
+	}{
+		{"zero", 0, "0"},
+		{"small", 42, "42"},
+		{"boundary_below", 999, "999"},
+		{"boundary_exact", 1000, "1.0K"},
+		{"large", 1500, "1.5K"},
+		{"very_large", 128000, "128.0K"},
+		{"millions", 1000000, "1000.0K"},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := FormatTokens(tt.tokens)
+			if got != tt.want {
+				t.Errorf("FormatTokens(%d) = %q, want %q", tt.tokens, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestFormatCost(t *testing.T) {
+	tests := []struct {
+		name string
+		cost float64
+		want string
+	}{
+		{"zero", 0, "$0.000"},
+		{"small", 0.001, "$0.001"},
+		{"normal", 0.05, "$0.050"},
+		{"dollar", 1.234, "$1.234"},
+		{"negative", -0.5, "$-0.500"},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := FormatCost(tt.cost)
+			if got != tt.want {
+				t.Errorf("FormatCost(%f) = %q, want %q", tt.cost, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestFormatDuration(t *testing.T) {
+	tests := []struct {
+		name string
+		ms   int
+		want string
+	}{
+		{"zero", 0, "0ms"},
+		{"small", 50, "50ms"},
+		{"boundary_below", 999, "999ms"},
+		{"boundary_exact", 1000, "1.0s"},
+		{"large", 2500, "2.5s"},
+		{"very_large", 60000, "60.0s"},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := FormatDuration(tt.ms)
+			if got != tt.want {
+				t.Errorf("FormatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestFormatTimestamp(t *testing.T) {
+	tests := []struct {
+		name  string
+		input string
+	}{
+		{"rfc3339", "2026-01-15T10:30:00Z"},
+		{"rfc3339_offset", "2026-01-15T10:30:00+05:00"},
+		{"alt_format", "2026-01-15T10:30:00Z"},
+		{"invalid_returns_original", "not-a-timestamp"},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := FormatTimestamp(tt.input)
+			if got == "" {
+				t.Errorf("FormatTimestamp(%q) returned empty string", tt.input)
+			}
+			// Invalid timestamps should return the original string
+			if tt.name == "invalid_returns_original" && got != tt.input {
+				t.Errorf("FormatTimestamp(%q) = %q, want original string returned", tt.input, got)
+			}
+			// Valid timestamps should NOT return the original string (they're reformatted)
+			if tt.name == "rfc3339" && got == tt.input {
+				t.Errorf("FormatTimestamp(%q) was not reformatted", tt.input)
+			}
+		})
+	}
+}
+
+func TestTruncateString(t *testing.T) {
+	tests := []struct {
+		name   string
+		input  string
+		maxLen int
+		want   string
+	}{
+		{"empty", "", 10, ""},
+		{"short_enough", "hello", 10, "hello"},
+		{"exact_length", "hello", 5, "hello"},
+		{"truncated", "hello world", 8, "hello..."},
+		{"newlines_replaced", "hello\nworld", 20, "hello world"},
+		{"newlines_then_truncate", "hello\nworld\nfoo", 10, "hello w..."},
+		{"min_truncation", "abcdef", 4, "a..."},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := TruncateString(tt.input, tt.maxLen)
+			if got != tt.want {
+				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
+			}
+		})
+	}
+}
```

### Test 2: `internal/provider/model_test.go` (NEW FILE)

```diff
--- /dev/null
+++ b/internal/provider/model_test.go
@@ -0,0 +1,53 @@
+package provider
+
+import "testing"
+
+func TestSelectModel(t *testing.T) {
+	tests := []struct {
+		name          string
+		configModel   string
+		defaultModel  string
+		overrideModel string
+		want          string
+	}{
+		{
+			name:         "default_when_all_empty",
+			defaultModel: "gpt-4o",
+			want:         "gpt-4o",
+		},
+		{
+			name:         "config_overrides_default",
+			configModel:  "gpt-4o-mini",
+			defaultModel: "gpt-4o",
+			want:         "gpt-4o-mini",
+		},
+		{
+			name:          "override_wins_over_config",
+			configModel:   "gpt-4o-mini",
+			defaultModel:  "gpt-4o",
+			overrideModel: "gemini-2.5-flash",
+			want:          "gemini-2.5-flash",
+		},
+		{
+			name:          "override_whitespace_ignored",
+			configModel:   "gpt-4o-mini",
+			defaultModel:  "gpt-4o",
+			overrideModel: "   ",
+			want:          "gpt-4o-mini",
+		},
+		{
+			name:          "override_with_leading_trailing_spaces",
+			defaultModel:  "gpt-4o",
+			overrideModel: "  gemini-2.5-flash  ",
+			want:          "gemini-2.5-flash",
+		},
+		{
+			name:         "empty_config_falls_to_default",
+			configModel:  "",
+			defaultModel: "gpt-4o",
+			want:         "gpt-4o",
+		},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := SelectModel(tt.configModel, tt.defaultModel, tt.overrideModel)
+			if got != tt.want {
+				t.Errorf("SelectModel(%q, %q, %q) = %q, want %q",
+					tt.configModel, tt.defaultModel, tt.overrideModel, got, tt.want)
+			}
+		})
+	}
+}
```

### Test 3: `internal/retry/backoff_test.go` (NEW FILE)

```diff
--- /dev/null
+++ b/internal/retry/backoff_test.go
@@ -0,0 +1,48 @@
+package retry
+
+import (
+	"context"
+	"testing"
+	"time"
+)
+
+func TestSleepWithBackoff(t *testing.T) {
+	// Verify exponential growth: attempt 1 = 250ms, attempt 2 = 500ms, attempt 3 = 1s
+	tests := []struct {
+		name    string
+		attempt int
+		wantMin time.Duration
+		wantMax time.Duration
+	}{
+		{"attempt_1", 1, 200 * time.Millisecond, 400 * time.Millisecond},
+		{"attempt_2", 2, 400 * time.Millisecond, 700 * time.Millisecond},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			start := time.Now()
+			SleepWithBackoff(context.Background(), tt.attempt)
+			elapsed := time.Since(start)
+
+			if elapsed < tt.wantMin {
+				t.Errorf("SleepWithBackoff(ctx, %d) slept %v, want at least %v",
+					tt.attempt, elapsed, tt.wantMin)
+			}
+			if elapsed > tt.wantMax {
+				t.Errorf("SleepWithBackoff(ctx, %d) slept %v, want at most %v",
+					tt.attempt, elapsed, tt.wantMax)
+			}
+		})
+	}
+}
+
+func TestSleepWithBackoff_ContextCancellation(t *testing.T) {
+	ctx, cancel := context.WithCancel(context.Background())
+	cancel() // Cancel immediately
+
+	start := time.Now()
+	SleepWithBackoff(ctx, 3) // Would sleep 1s without cancellation
+	elapsed := time.Since(start)
+
+	// Should return almost immediately due to cancelled context
+	if elapsed > 100*time.Millisecond {
+		t.Errorf("SleepWithBackoff with cancelled context took %v, expected near-instant", elapsed)
+	}
+}
```

### Test 4: `internal/retry/context_test.go` (NEW FILE)

```diff
--- /dev/null
+++ b/internal/retry/context_test.go
@@ -0,0 +1,42 @@
+package retry
+
+import (
+	"context"
+	"testing"
+	"time"
+)
+
+func TestEnsureTimeout_NoExistingDeadline(t *testing.T) {
+	ctx := context.Background()
+	timeout := 5 * time.Second
+
+	newCtx, cancel := EnsureTimeout(ctx, timeout)
+	defer cancel()
+
+	deadline, ok := newCtx.Deadline()
+	if !ok {
+		t.Fatal("expected context to have a deadline")
+	}
+
+	// Deadline should be approximately now + timeout
+	remaining := time.Until(deadline)
+	if remaining < 4*time.Second || remaining > 6*time.Second {
+		t.Errorf("expected deadline ~5s from now, got %v", remaining)
+	}
+}
+
+func TestEnsureTimeout_ExistingDeadline(t *testing.T) {
+	existingTimeout := 10 * time.Second
+	ctx, existingCancel := context.WithTimeout(context.Background(), existingTimeout)
+	defer existingCancel()
+
+	originalDeadline, _ := ctx.Deadline()
+
+	newCtx, cancel := EnsureTimeout(ctx, 1*time.Second)
+	defer cancel()
+
+	// Should preserve the original deadline, not apply the new shorter one
+	newDeadline, ok := newCtx.Deadline()
+	if !ok {
+		t.Fatal("expected context to have a deadline")
+	}
+	if !newDeadline.Equal(originalDeadline) {
+		t.Errorf("deadline changed: original=%v, new=%v", originalDeadline, newDeadline)
+	}
+}
```

### Test 5: `internal/imagegen/config_test.go` — Add Tests for Config Methods

The existing `internal/imagegen/client_test.go` tests the generation client but not the `Config` type. This should be a new file:

```diff
--- /dev/null
+++ b/internal/imagegen/config_methods_test.go
@@ -0,0 +1,89 @@
+package imagegen
+
+import "testing"
+
+func TestConfig_IsEnabled(t *testing.T) {
+	tests := []struct {
+		name   string
+		config *Config
+		want   bool
+	}{
+		{"nil_config", nil, false},
+		{"disabled", &Config{Enabled: false}, false},
+		{"enabled", &Config{Enabled: true}, true},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.config.IsEnabled(); got != tt.want {
+				t.Errorf("Config.IsEnabled() = %v, want %v", got, tt.want)
+			}
+		})
+	}
+}
+
+func TestConfig_GetProvider(t *testing.T) {
+	tests := []struct {
+		name   string
+		config *Config
+		want   string
+	}{
+		{"nil_config_defaults_gemini", nil, "gemini"},
+		{"empty_provider_defaults_gemini", &Config{}, "gemini"},
+		{"custom_provider", &Config{Provider: "openai"}, "openai"},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.config.GetProvider(); got != tt.want {
+				t.Errorf("Config.GetProvider() = %q, want %q", got, tt.want)
+			}
+		})
+	}
+}
+
+func TestConfig_GetModel(t *testing.T) {
+	tests := []struct {
+		name   string
+		config *Config
+		want   string
+	}{
+		{"nil_config_returns_empty", nil, ""},
+		{"empty_model", &Config{}, ""},
+		{"custom_model", &Config{Model: "dall-e-3"}, "dall-e-3"},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.config.GetModel(); got != tt.want {
+				t.Errorf("Config.GetModel() = %q, want %q", got, tt.want)
+			}
+		})
+	}
+}
```

### Test 6: `internal/db/repository_test.go` (NEW FILE)

Tests for tenant validation and table name generation (no database needed):

```diff
--- /dev/null
+++ b/internal/db/repository_test.go
@@ -0,0 +1,98 @@
+package db
+
+import (
+	"errors"
+	"testing"
+)
+
+func TestNewTenantRepository_ValidTenants(t *testing.T) {
+	validTenants := []string{"ai8", "email4ai", "zztest"}
+	for _, tenantID := range validTenants {
+		t.Run(tenantID, func(t *testing.T) {
+			repo, err := NewTenantRepository(nil, tenantID)
+			if err != nil {
+				t.Fatalf("NewTenantRepository(nil, %q) error: %v", tenantID, err)
+			}
+			if repo.TenantID() != tenantID {
+				t.Errorf("TenantID() = %q, want %q", repo.TenantID(), tenantID)
+			}
+		})
+	}
+}
+
+func TestNewTenantRepository_InvalidTenants(t *testing.T) {
+	invalidTenants := []string{"", "unknown", "AI8", "admin", "root"}
+	for _, tenantID := range invalidTenants {
+		t.Run(tenantID, func(t *testing.T) {
+			_, err := NewTenantRepository(nil, tenantID)
+			if err == nil {
+				t.Fatalf("NewTenantRepository(nil, %q) expected error, got nil", tenantID)
+			}
+			if !errors.Is(err, ErrInvalidTenant) {
+				t.Errorf("expected ErrInvalidTenant, got: %v", err)
+			}
+		})
+	}
+}
+
+func TestRepository_TableNames_WithTenant(t *testing.T) {
+	repo, err := NewTenantRepository(nil, "ai8")
+	if err != nil {
+		t.Fatal(err)
+	}
+
+	tests := []struct {
+		name string
+		fn   func() string
+		want string
+	}{
+		{"threads", repo.threadsTable, "ai8_airborne_threads"},
+		{"messages", repo.messagesTable, "ai8_airborne_messages"},
+		{"files", repo.filesTable, "ai8_airborne_files"},
+		{"file_uploads", repo.fileUploadsTable, "ai8_airborne_file_provider_uploads"},
+		{"vector_stores", repo.vectorStoresTable, "ai8_airborne_thread_vector_stores"},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.fn(); got != tt.want {
+				t.Errorf("%s() = %q, want %q", tt.name, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestRepository_TableNames_Legacy(t *testing.T) {
+	repo := NewRepository(nil) // Legacy (no tenant prefix)
+
+	tests := []struct {
+		name string
+		fn   func() string
+		want string
+	}{
+		{"threads", repo.threadsTable, "airborne_threads"},
+		{"messages", repo.messagesTable, "airborne_messages"},
+		{"files", repo.filesTable, "airborne_files"},
+		{"file_uploads", repo.fileUploadsTable, "airborne_file_provider_uploads"},
+		{"vector_stores", repo.vectorStoresTable, "airborne_thread_vector_stores"},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.fn(); got != tt.want {
+				t.Errorf("%s() = %q, want %q", tt.name, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestRepository_TableNames_DifferentTenants(t *testing.T) {
+	for _, tenantID := range []string{"ai8", "email4ai", "zztest"} {
+		t.Run(tenantID, func(t *testing.T) {
+			repo, err := NewTenantRepository(nil, tenantID)
+			if err != nil {
+				t.Fatal(err)
+			}
+			wantPrefix := tenantID + "_airborne_"
+			got := repo.threadsTable()
+			if got != wantPrefix+"threads" {
+				t.Errorf("threadsTable() = %q, want %q", got, wantPrefix+"threads")
+			}
+		})
+	}
+}
```

### Test 7: `internal/provider/provider_result_test.go` (NEW FILE)

Test the `GenerateResult.HasImages()` method:

```diff
--- /dev/null
+++ b/internal/provider/provider_result_test.go
@@ -0,0 +1,28 @@
+package provider
+
+import "testing"
+
+func TestGenerateResult_HasImages(t *testing.T) {
+	tests := []struct {
+		name   string
+		result GenerateResult
+		want   bool
+	}{
+		{
+			name:   "no_images",
+			result: GenerateResult{},
+			want:   false,
+		},
+		{
+			name:   "empty_slice",
+			result: GenerateResult{Images: []GeneratedImage{}},
+			want:   false,
+		},
+		{
+			name:   "has_images",
+			result: GenerateResult{Images: []GeneratedImage{{Data: []byte{0xFF}}}},
+			want:   true,
+		},
+	}
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.result.HasImages(); got != tt.want {
+				t.Errorf("HasImages() = %v, want %v", got, tt.want)
+			}
+		})
+	}
+}
```

### Test 8: `internal/retry/retryable_test.go` — Additional Edge Cases

The existing `retry_test.go` covers `IsRetryable` well but misses some edge patterns. Add to existing file:

```diff
--- a/internal/retry/retry_test.go
+++ b/internal/retry/retry_test.go
@@ -end of test cases, before closing brace of TestIsRetryable
+		// Edge cases - mixed signals (auth pattern in retryable context)
+		{"error_with_500_and_401", errors.New("got 500 after 401 redirect"), false},
+		{"wrapped_context_error", fmt.Errorf("provider failed: %w", context.Canceled), false},
+
+		// Case sensitivity
+		{"uppercase_TIMEOUT", errors.New("TIMEOUT occurred"), true},
+		{"mixed_case_Connection", errors.New("Connection reset"), true},
+
+		// Gemini-specific 499
+		{"499_client_closed", errors.New("499 client closed request"), true},
+
+		// TLS errors
+		{"tls_handshake_failure", errors.New("tls handshake timeout"), true},
+		{"no_such_host", errors.New("dial tcp: lookup foo.com: no such host"), true},
+
+		// Unknown error - not retryable by default
+		{"completely_unknown", errors.New("something weird happened"), false},
```

Note: This requires adding `"fmt"` to imports if not already present.

---

## Coverage Gaps NOT Addressed (Lower Priority)

These files have no tests but are lower priority due to integration complexity:

| File | Lines | Reason for Lower Priority |
|------|-------|---------------------------|
| `internal/admin/server.go` | 1325 | Requires extensive mocking of DB, Redis, gRPC, tenant manager |
| `internal/cli/client.go` | 214 | HTTP client wrapper; would need httptest server |
| `internal/cli/commands.go` | 273 | Cobra command setup; integration-level testing |
| `internal/db/postgres.go` | 175 | Requires actual PostgreSQL connection |
| `internal/tenant/doppler.go` | 252 | External API integration; httptest could help |
| `internal/provider/gemini/filestore.go` | 645 | Complex API interactions with file upload protocols |
| `internal/provider/openai/filestore.go` | 324 | External API with polling |
| 12 compat provider clients | ~50 each | Thin wrappers over `compat.Client` (already tested) |

---

## Recommendations

1. **Immediate wins (Tests 1-5, 7)**: Add the pure-function tests above. These require zero infrastructure, run fast, and cover real logic.

2. **High-value structural tests (Test 6)**: The repository table name tests catch tenant isolation bugs without needing a database.

3. **Expand retry tests (Test 8)**: Add edge cases to the already strong retry test suite.

4. **Future consideration**: Add `httptest.Server`-based tests for `internal/admin/server.go` handlers — this is the biggest untested surface area but requires the most setup.

5. **Consider fuzzing**: The `commands/parser.go` and `validation/url.go` packages handle untrusted input and would benefit from Go native fuzzing (`testing.F`).

---

## Test Infrastructure Notes

- TestMain fixtures use `chassis.RequireMajor(5)` — new test files in packages with TestMain must respect this
- Mock infrastructure in `internal/rag/testutil/mocks.go` is well-designed and reusable
- Makefile target `make test` runs `go test -v -race ./...` — all proposed tests are race-safe
- No external test dependencies needed for the proposed tests above
