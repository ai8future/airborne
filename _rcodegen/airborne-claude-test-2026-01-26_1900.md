Date Created: 2026-01-26 19:00:00 UTC
TOTAL_SCORE: 68/100

# Airborne Test Coverage Analysis Report

**Audit Agent:** Claude:Opus 4.5
**Date:** 2026-01-26
**Version Analyzed:** 1.7.12

---

## Executive Summary

Airborne is a Go-based gRPC AI proxy service supporting multiple LLM providers (OpenAI, Gemini, Anthropic, Mistral, and 12+ others). The codebase demonstrates solid test coverage in critical areas (validation, auth, service layer) but has significant gaps in database operations, pricing calculations, and secondary provider implementations.

**Overall Score: 68/100**

| Category | Score | Weight | Weighted |
|----------|-------|--------|----------|
| Core Business Logic Tests | 75/100 | 30% | 22.5 |
| Provider Implementation Tests | 45/100 | 25% | 11.25 |
| Database Layer Tests | 0/100 | 20% | 0 |
| Utility/Helper Tests | 85/100 | 15% | 12.75 |
| Test Quality & Patterns | 85/100 | 10% | 8.5 |
| **TOTAL** | | | **68/100** |

---

## Current Test Coverage Summary

### Existing Test Files (40 total)
```
internal/
├── auth/           (5 tests) - Good coverage
├── commands/       (1 test)  - Adequate
├── config/         (3 tests) - Good coverage
├── errors/         (1 test)  - Adequate
├── httpcapture/    (1 test)  - Adequate
├── imagegen/       (1 test)  - Partial (missing generate tests)
├── markdownsvc/    (1 test)  - Adequate
├── provider/       (8 tests) - Partial (only 5 of 17 providers)
├── rag/            (6 tests) - Good coverage with mocks
├── redis/          (1 test)  - Adequate
├── retry/          (1 test)  - Partial (missing backoff tests)
├── server/         (1 test)  - Adequate
├── service/        (4 tests) - Excellent coverage
├── tenant/         (5 tests) - Good coverage
└── validation/     (2 tests) - Excellent (93.9% coverage)
```

### Critical Gaps Identified

| Package | Files | Lines | Tests | Priority |
|---------|-------|-------|-------|----------|
| `db/` | 3 | 813 | 0 | **CRITICAL** |
| `pricing/` | 1 | 169 | 0 | **HIGH** |
| `provider/openrouter/` | 1 | 64 | 0 | MEDIUM |
| `provider/deepseek/` | 1 | 64 | 0 | MEDIUM |
| `provider/grok/` | 1 | 63 | 0 | MEDIUM |
| `provider/fireworks/` | 1 | 63 | 0 | MEDIUM |
| `provider/together/` | 1 | ~64 | 0 | MEDIUM |
| `provider/cohere/` | 1 | ~64 | 0 | MEDIUM |
| `provider/hyperbolic/` | 1 | ~64 | 0 | LOW |
| `provider/cerebras/` | 1 | ~64 | 0 | LOW |
| `provider/deepinfra/` | 1 | ~64 | 0 | LOW |
| `provider/upstage/` | 1 | ~64 | 0 | LOW |
| `provider/nebius/` | 1 | ~64 | 0 | LOW |
| `provider/perplexity/` | 1 | ~64 | 0 | LOW |
| `imagegen/gemini.go` | 1 | 173 | 0 | MEDIUM |
| `imagegen/openai.go` | 1 | ~100 | 0 | MEDIUM |
| `retry/backoff.go` | 1 | 17 | 0 | LOW |

---

## Proposed Unit Tests

### 1. Pricing Package Tests (HIGH PRIORITY)

The pricing package wraps `pricing_db` for cost calculations. Tests should verify the wrapper functions work correctly.

**File:** `internal/pricing/pricing_test.go`

```diff
--- /dev/null
+++ b/internal/pricing/pricing_test.go
@@ -0,0 +1,185 @@
+package pricing
+
+import (
+	"testing"
+)
+
+func TestNewPricer(t *testing.T) {
+	// configDir is ignored - pricing_db uses go:embed
+	pricer, err := NewPricer("")
+	if err != nil {
+		t.Fatalf("NewPricer() error = %v", err)
+	}
+	if pricer == nil {
+		t.Fatal("NewPricer() returned nil")
+	}
+	if pricer.db == nil {
+		t.Fatal("NewPricer() db field is nil")
+	}
+}
+
+func TestPricer_Calculate(t *testing.T) {
+	pricer, err := NewPricer("")
+	if err != nil {
+		t.Fatalf("NewPricer() error = %v", err)
+	}
+
+	tests := []struct {
+		name         string
+		model        string
+		inputTokens  int64
+		outputTokens int64
+		wantUnknown  bool
+		wantPositive bool // expect non-zero cost for known models
+	}{
+		{
+			name:         "known model gpt-4",
+			model:        "gpt-4",
+			inputTokens:  1000,
+			outputTokens: 500,
+			wantUnknown:  false,
+			wantPositive: true,
+		},
+		{
+			name:         "known model claude-3-opus",
+			model:        "claude-3-opus-20240229",
+			inputTokens:  1000,
+			outputTokens: 500,
+			wantUnknown:  false,
+			wantPositive: true,
+		},
+		{
+			name:         "known model gemini-1.5-pro",
+			model:        "gemini-1.5-pro",
+			inputTokens:  1000,
+			outputTokens: 500,
+			wantUnknown:  false,
+			wantPositive: true,
+		},
+		{
+			name:         "unknown model",
+			model:        "nonexistent-model-xyz",
+			inputTokens:  1000,
+			outputTokens: 500,
+			wantUnknown:  true,
+			wantPositive: false,
+		},
+		{
+			name:         "zero tokens",
+			model:        "gpt-4",
+			inputTokens:  0,
+			outputTokens: 0,
+			wantUnknown:  false,
+			wantPositive: false,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			cost := pricer.Calculate(tt.model, tt.inputTokens, tt.outputTokens)
+
+			if cost.Unknown != tt.wantUnknown {
+				t.Errorf("Calculate() Unknown = %v, want %v", cost.Unknown, tt.wantUnknown)
+			}
+			if cost.Model != tt.model {
+				t.Errorf("Calculate() Model = %q, want %q", cost.Model, tt.model)
+			}
+			if cost.InputTokens != tt.inputTokens {
+				t.Errorf("Calculate() InputTokens = %d, want %d", cost.InputTokens, tt.inputTokens)
+			}
+			if cost.OutputTokens != tt.outputTokens {
+				t.Errorf("Calculate() OutputTokens = %d, want %d", cost.OutputTokens, tt.outputTokens)
+			}
+			if tt.wantPositive && cost.TotalCost <= 0 {
+				t.Errorf("Calculate() TotalCost = %f, want positive", cost.TotalCost)
+			}
+			// Verify TotalCost = InputCost + OutputCost
+			expectedTotal := cost.InputCost + cost.OutputCost
+			if cost.TotalCost != expectedTotal {
+				t.Errorf("Calculate() TotalCost = %f, want %f (InputCost + OutputCost)", cost.TotalCost, expectedTotal)
+			}
+		})
+	}
+}
+
+func TestCost_Format(t *testing.T) {
+	tests := []struct {
+		name string
+		cost Cost
+		want string
+	}{
+		{
+			name: "unknown model",
+			cost: Cost{Model: "unknown-model", Unknown: true},
+			want: `Cost: unknown (model "unknown-model" not in pricing data)`,
+		},
+		{
+			name: "known model with costs",
+			cost: Cost{
+				Model:        "gpt-4",
+				InputTokens:  1000,
+				OutputTokens: 500,
+				InputCost:    0.03,
+				OutputCost:   0.06,
+				TotalCost:    0.09,
+				Unknown:      false,
+			},
+			want: "Input: $0.0300 (1000 tokens) | Output: $0.0600 (500 tokens) | Total: $0.0900",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.cost.Format(); got != tt.want {
+				t.Errorf("Format() = %q, want %q", got, tt.want)
+			}
+		})
+	}
+}
+
+func TestCalculateCost_PackageLevel(t *testing.T) {
+	// Test the package-level convenience function
+	cost := CalculateCost("gpt-4", 1000, 500)
+
+	// Should return a value (0 for unknown, positive for known)
+	// Just verify it doesn't panic and returns reasonable value
+	if cost < 0 {
+		t.Errorf("CalculateCost() = %f, want non-negative", cost)
+	}
+}
+
+func TestGetPricing(t *testing.T) {
+	// Test known model
+	pricing, found := GetPricing("gpt-4")
+	if !found {
+		t.Skip("gpt-4 not in pricing data")
+	}
+	if pricing.InputPricePerMillion <= 0 {
+		t.Errorf("GetPricing() InputPricePerMillion = %f, want positive", pricing.InputPricePerMillion)
+	}
+
+	// Test unknown model
+	_, found = GetPricing("nonexistent-model-xyz")
+	if found {
+		t.Error("GetPricing() found = true for unknown model, want false")
+	}
+}
+
+func TestListProviders(t *testing.T) {
+	providers := ListProviders()
+	if len(providers) == 0 {
+		t.Error("ListProviders() returned empty list")
+	}
+}
+
+func TestModelCount(t *testing.T) {
+	count := ModelCount()
+	if count <= 0 {
+		t.Errorf("ModelCount() = %d, want positive", count)
+	}
+}
+
+func TestProviderCount(t *testing.T) {
+	count := ProviderCount()
+	if count <= 0 {
+		t.Errorf("ProviderCount() = %d, want positive", count)
+	}
+}
```

---

### 2. OpenRouter Provider Tests (MEDIUM PRIORITY)

Tests for the OpenRouter compat-based provider.

**File:** `internal/provider/openrouter/client_test.go`

```diff
--- /dev/null
+++ b/internal/provider/openrouter/client_test.go
@@ -0,0 +1,108 @@
+package openrouter
+
+import (
+	"testing"
+
+	"github.com/ai8future/airborne/internal/provider"
+)
+
+func TestNewClient(t *testing.T) {
+	client := NewClient()
+	if client == nil {
+		t.Fatal("NewClient() returned nil")
+	}
+	if client.Client == nil {
+		t.Fatal("NewClient() Client field is nil")
+	}
+}
+
+func TestNewClientWithDebugLogging(t *testing.T) {
+	// Test with debug enabled
+	client := NewClient(WithDebugLogging(true))
+	if client == nil {
+		t.Fatal("NewClient(WithDebugLogging(true)) returned nil")
+	}
+
+	// Test with debug disabled
+	client2 := NewClient(WithDebugLogging(false))
+	if client2 == nil {
+		t.Fatal("NewClient(WithDebugLogging(false)) returned nil")
+	}
+
+	// Test with nil option
+	client3 := NewClient(nil)
+	if client3 == nil {
+		t.Fatal("NewClient(nil) returned nil")
+	}
+}
+
+func TestClient_Name(t *testing.T) {
+	client := NewClient()
+	if got := client.Name(); got != "openrouter" {
+		t.Errorf("Name() = %q, want %q", got, "openrouter")
+	}
+}
+
+func TestClient_Capabilities(t *testing.T) {
+	client := NewClient()
+
+	// OpenRouter does NOT support file search
+	if client.SupportsFileSearch() {
+		t.Error("SupportsFileSearch() = true, want false")
+	}
+
+	// OpenRouter does NOT support web search
+	if client.SupportsWebSearch() {
+		t.Error("SupportsWebSearch() = true, want false")
+	}
+
+	// OpenRouter supports streaming
+	if !client.SupportsStreaming() {
+		t.Error("SupportsStreaming() = false, want true")
+	}
+
+	// Compat clients don't support native continuity
+	if client.SupportsNativeContinuity() {
+		t.Error("SupportsNativeContinuity() = true, want false")
+	}
+}
+
+func TestClient_ImplementsProvider(t *testing.T) {
+	// Verify compile-time interface compliance
+	var _ provider.Provider = (*Client)(nil)
+
+	// Also verify at runtime
+	client := NewClient()
+	var p provider.Provider = client
+	if p == nil {
+		t.Fatal("Client does not implement provider.Provider")
+	}
+}
+
+func TestWithDebugLogging(t *testing.T) {
+	tests := []struct {
+		name    string
+		enabled bool
+	}{
+		{"enabled", true},
+		{"disabled", false},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			opt := WithDebugLogging(tt.enabled)
+			if opt == nil {
+				t.Fatal("WithDebugLogging() returned nil")
+			}
+
+			// Apply the option and verify it doesn't panic
+			opts := &clientOptions{}
+			opt(opts)
+			if opts.debug != tt.enabled {
+				t.Errorf("debug = %v, want %v", opts.debug, tt.enabled)
+			}
+		})
+	}
+}
```

---

### 3. DeepSeek Provider Tests (MEDIUM PRIORITY)

**File:** `internal/provider/deepseek/client_test.go`

```diff
--- /dev/null
+++ b/internal/provider/deepseek/client_test.go
@@ -0,0 +1,108 @@
+package deepseek
+
+import (
+	"testing"
+
+	"github.com/ai8future/airborne/internal/provider"
+)
+
+func TestNewClient(t *testing.T) {
+	client := NewClient()
+	if client == nil {
+		t.Fatal("NewClient() returned nil")
+	}
+	if client.Client == nil {
+		t.Fatal("NewClient() Client field is nil")
+	}
+}
+
+func TestNewClientWithDebugLogging(t *testing.T) {
+	// Test with debug enabled
+	client := NewClient(WithDebugLogging(true))
+	if client == nil {
+		t.Fatal("NewClient(WithDebugLogging(true)) returned nil")
+	}
+
+	// Test with debug disabled
+	client2 := NewClient(WithDebugLogging(false))
+	if client2 == nil {
+		t.Fatal("NewClient(WithDebugLogging(false)) returned nil")
+	}
+
+	// Test with nil option
+	client3 := NewClient(nil)
+	if client3 == nil {
+		t.Fatal("NewClient(nil) returned nil")
+	}
+}
+
+func TestClient_Name(t *testing.T) {
+	client := NewClient()
+	if got := client.Name(); got != "deepseek" {
+		t.Errorf("Name() = %q, want %q", got, "deepseek")
+	}
+}
+
+func TestClient_Capabilities(t *testing.T) {
+	client := NewClient()
+
+	// DeepSeek does NOT support file search
+	if client.SupportsFileSearch() {
+		t.Error("SupportsFileSearch() = true, want false")
+	}
+
+	// DeepSeek does NOT support web search
+	if client.SupportsWebSearch() {
+		t.Error("SupportsWebSearch() = true, want false")
+	}
+
+	// DeepSeek supports streaming
+	if !client.SupportsStreaming() {
+		t.Error("SupportsStreaming() = false, want true")
+	}
+
+	// Compat clients don't support native continuity
+	if client.SupportsNativeContinuity() {
+		t.Error("SupportsNativeContinuity() = true, want false")
+	}
+}
+
+func TestClient_ImplementsProvider(t *testing.T) {
+	// Verify compile-time interface compliance
+	var _ provider.Provider = (*Client)(nil)
+
+	// Also verify at runtime
+	client := NewClient()
+	var p provider.Provider = client
+	if p == nil {
+		t.Fatal("Client does not implement provider.Provider")
+	}
+}
+
+func TestWithDebugLogging(t *testing.T) {
+	tests := []struct {
+		name    string
+		enabled bool
+	}{
+		{"enabled", true},
+		{"disabled", false},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			opt := WithDebugLogging(tt.enabled)
+			if opt == nil {
+				t.Fatal("WithDebugLogging() returned nil")
+			}
+
+			// Apply the option and verify it doesn't panic
+			opts := &clientOptions{}
+			opt(opts)
+			if opts.debug != tt.enabled {
+				t.Errorf("debug = %v, want %v", opts.debug, tt.enabled)
+			}
+		})
+	}
+}
```

---

### 4. Grok Provider Tests (MEDIUM PRIORITY)

**File:** `internal/provider/grok/client_test.go`

```diff
--- /dev/null
+++ b/internal/provider/grok/client_test.go
@@ -0,0 +1,108 @@
+package grok
+
+import (
+	"testing"
+
+	"github.com/ai8future/airborne/internal/provider"
+)
+
+func TestNewClient(t *testing.T) {
+	client := NewClient()
+	if client == nil {
+		t.Fatal("NewClient() returned nil")
+	}
+	if client.Client == nil {
+		t.Fatal("NewClient() Client field is nil")
+	}
+}
+
+func TestNewClientWithDebugLogging(t *testing.T) {
+	// Test with debug enabled
+	client := NewClient(WithDebugLogging(true))
+	if client == nil {
+		t.Fatal("NewClient(WithDebugLogging(true)) returned nil")
+	}
+
+	// Test with debug disabled
+	client2 := NewClient(WithDebugLogging(false))
+	if client2 == nil {
+		t.Fatal("NewClient(WithDebugLogging(false)) returned nil")
+	}
+
+	// Test with nil option
+	client3 := NewClient(nil)
+	if client3 == nil {
+		t.Fatal("NewClient(nil) returned nil")
+	}
+}
+
+func TestClient_Name(t *testing.T) {
+	client := NewClient()
+	if got := client.Name(); got != "grok" {
+		t.Errorf("Name() = %q, want %q", got, "grok")
+	}
+}
+
+func TestClient_Capabilities(t *testing.T) {
+	client := NewClient()
+
+	// Grok does NOT support file search
+	if client.SupportsFileSearch() {
+		t.Error("SupportsFileSearch() = true, want false")
+	}
+
+	// Grok does NOT support web search
+	if client.SupportsWebSearch() {
+		t.Error("SupportsWebSearch() = true, want false")
+	}
+
+	// Grok supports streaming
+	if !client.SupportsStreaming() {
+		t.Error("SupportsStreaming() = false, want true")
+	}
+
+	// Compat clients don't support native continuity
+	if client.SupportsNativeContinuity() {
+		t.Error("SupportsNativeContinuity() = true, want false")
+	}
+}
+
+func TestClient_ImplementsProvider(t *testing.T) {
+	// Verify compile-time interface compliance
+	var _ provider.Provider = (*Client)(nil)
+
+	// Also verify at runtime
+	client := NewClient()
+	var p provider.Provider = client
+	if p == nil {
+		t.Fatal("Client does not implement provider.Provider")
+	}
+}
+
+func TestWithDebugLogging(t *testing.T) {
+	tests := []struct {
+		name    string
+		enabled bool
+	}{
+		{"enabled", true},
+		{"disabled", false},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			opt := WithDebugLogging(tt.enabled)
+			if opt == nil {
+				t.Fatal("WithDebugLogging() returned nil")
+			}
+
+			// Apply the option and verify it doesn't panic
+			opts := &clientOptions{}
+			opt(opts)
+			if opts.debug != tt.enabled {
+				t.Errorf("debug = %v, want %v", opts.debug, tt.enabled)
+			}
+		})
+	}
+}
```

---

### 5. Fireworks Provider Tests (MEDIUM PRIORITY)

**File:** `internal/provider/fireworks/client_test.go`

```diff
--- /dev/null
+++ b/internal/provider/fireworks/client_test.go
@@ -0,0 +1,108 @@
+package fireworks
+
+import (
+	"testing"
+
+	"github.com/ai8future/airborne/internal/provider"
+)
+
+func TestNewClient(t *testing.T) {
+	client := NewClient()
+	if client == nil {
+		t.Fatal("NewClient() returned nil")
+	}
+	if client.Client == nil {
+		t.Fatal("NewClient() Client field is nil")
+	}
+}
+
+func TestNewClientWithDebugLogging(t *testing.T) {
+	// Test with debug enabled
+	client := NewClient(WithDebugLogging(true))
+	if client == nil {
+		t.Fatal("NewClient(WithDebugLogging(true)) returned nil")
+	}
+
+	// Test with debug disabled
+	client2 := NewClient(WithDebugLogging(false))
+	if client2 == nil {
+		t.Fatal("NewClient(WithDebugLogging(false)) returned nil")
+	}
+
+	// Test with nil option
+	client3 := NewClient(nil)
+	if client3 == nil {
+		t.Fatal("NewClient(nil) returned nil")
+	}
+}
+
+func TestClient_Name(t *testing.T) {
+	client := NewClient()
+	if got := client.Name(); got != "fireworks" {
+		t.Errorf("Name() = %q, want %q", got, "fireworks")
+	}
+}
+
+func TestClient_Capabilities(t *testing.T) {
+	client := NewClient()
+
+	// Fireworks does NOT support file search
+	if client.SupportsFileSearch() {
+		t.Error("SupportsFileSearch() = true, want false")
+	}
+
+	// Fireworks does NOT support web search
+	if client.SupportsWebSearch() {
+		t.Error("SupportsWebSearch() = true, want false")
+	}
+
+	// Fireworks supports streaming
+	if !client.SupportsStreaming() {
+		t.Error("SupportsStreaming() = false, want true")
+	}
+
+	// Compat clients don't support native continuity
+	if client.SupportsNativeContinuity() {
+		t.Error("SupportsNativeContinuity() = true, want false")
+	}
+}
+
+func TestClient_ImplementsProvider(t *testing.T) {
+	// Verify compile-time interface compliance
+	var _ provider.Provider = (*Client)(nil)
+
+	// Also verify at runtime
+	client := NewClient()
+	var p provider.Provider = client
+	if p == nil {
+		t.Fatal("Client does not implement provider.Provider")
+	}
+}
+
+func TestWithDebugLogging(t *testing.T) {
+	tests := []struct {
+		name    string
+		enabled bool
+	}{
+		{"enabled", true},
+		{"disabled", false},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			opt := WithDebugLogging(tt.enabled)
+			if opt == nil {
+				t.Fatal("WithDebugLogging() returned nil")
+			}
+
+			// Apply the option and verify it doesn't panic
+			opts := &clientOptions{}
+			opt(opts)
+			if opts.debug != tt.enabled {
+				t.Errorf("debug = %v, want %v", opts.debug, tt.enabled)
+			}
+		})
+	}
+}
```

---

### 6. Retry Backoff Tests (LOW PRIORITY)

**File:** `internal/retry/backoff_test.go`

```diff
--- /dev/null
+++ b/internal/retry/backoff_test.go
@@ -0,0 +1,78 @@
+package retry
+
+import (
+	"context"
+	"testing"
+	"time"
+)
+
+func TestSleepWithBackoff_Delays(t *testing.T) {
+	tests := []struct {
+		name           string
+		attempt        int
+		expectedDelay  time.Duration
+		tolerance      time.Duration
+	}{
+		{
+			name:          "attempt 1",
+			attempt:       1,
+			expectedDelay: BackoffBase * 1, // 250ms * 2^0 = 250ms
+			tolerance:     50 * time.Millisecond,
+		},
+		{
+			name:          "attempt 2",
+			attempt:       2,
+			expectedDelay: BackoffBase * 2, // 250ms * 2^1 = 500ms
+			tolerance:     50 * time.Millisecond,
+		},
+		{
+			name:          "attempt 3",
+			attempt:       3,
+			expectedDelay: BackoffBase * 4, // 250ms * 2^2 = 1000ms
+			tolerance:     50 * time.Millisecond,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			ctx := context.Background()
+			start := time.Now()
+			SleepWithBackoff(ctx, tt.attempt)
+			elapsed := time.Since(start)
+
+			minDelay := tt.expectedDelay - tt.tolerance
+			maxDelay := tt.expectedDelay + tt.tolerance
+
+			if elapsed < minDelay || elapsed > maxDelay {
+				t.Errorf("SleepWithBackoff(attempt=%d) elapsed = %v, want between %v and %v",
+					tt.attempt, elapsed, minDelay, maxDelay)
+			}
+		})
+	}
+}
+
+func TestSleepWithBackoff_CanceledContext(t *testing.T) {
+	ctx, cancel := context.WithCancel(context.Background())
+	cancel() // Cancel immediately
+
+	start := time.Now()
+	SleepWithBackoff(ctx, 3) // Would normally sleep 1 second
+	elapsed := time.Since(start)
+
+	// Should return almost immediately
+	if elapsed > 100*time.Millisecond {
+		t.Errorf("SleepWithBackoff with canceled context took %v, expected immediate return", elapsed)
+	}
+}
+
+func TestSleepWithBackoff_DeadlineExceeded(t *testing.T) {
+	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
+	defer cancel()
+
+	start := time.Now()
+	SleepWithBackoff(ctx, 3) // Would normally sleep 1 second
+	elapsed := time.Since(start)
+
+	// Should return after timeout (50ms), not after full backoff (1s)
+	if elapsed > 200*time.Millisecond {
+		t.Errorf("SleepWithBackoff with short deadline took %v, expected ~50ms", elapsed)
+	}
+}
```

---

### 7. Database Repository Tests (CRITICAL - Requires Test DB)

The database tests require a test PostgreSQL instance or mocking. Here's a pattern using interfaces.

**File:** `internal/db/repository_test.go`

```diff
--- /dev/null
+++ b/internal/db/repository_test.go
@@ -0,0 +1,156 @@
+package db
+
+import (
+	"testing"
+)
+
+func TestValidTenantIDs(t *testing.T) {
+	// Verify expected tenants are valid
+	expectedValid := []string{"ai8", "email4ai", "zztest"}
+	for _, id := range expectedValid {
+		if !ValidTenantIDs[id] {
+			t.Errorf("ValidTenantIDs[%q] = false, want true", id)
+		}
+	}
+
+	// Verify unexpected tenants are invalid
+	expectedInvalid := []string{"", "invalid", "test", "ai8_", "_ai8"}
+	for _, id := range expectedInvalid {
+		if ValidTenantIDs[id] {
+			t.Errorf("ValidTenantIDs[%q] = true, want false", id)
+		}
+	}
+}
+
+func TestNewTenantRepository_ValidTenants(t *testing.T) {
+	// Mock client for testing (only needs to not panic)
+	client := &Client{}
+
+	validTenants := []string{"ai8", "email4ai", "zztest"}
+	for _, tenantID := range validTenants {
+		t.Run(tenantID, func(t *testing.T) {
+			repo, err := NewTenantRepository(client, tenantID)
+			if err != nil {
+				t.Fatalf("NewTenantRepository(%q) error = %v", tenantID, err)
+			}
+			if repo == nil {
+				t.Fatal("NewTenantRepository returned nil")
+			}
+			if repo.TenantID() != tenantID {
+				t.Errorf("TenantID() = %q, want %q", repo.TenantID(), tenantID)
+			}
+		})
+	}
+}
+
+func TestNewTenantRepository_InvalidTenants(t *testing.T) {
+	client := &Client{}
+
+	invalidTenants := []string{"", "invalid", "test", "admin"}
+	for _, tenantID := range invalidTenants {
+		t.Run(tenantID, func(t *testing.T) {
+			repo, err := NewTenantRepository(client, tenantID)
+			if err == nil {
+				t.Fatalf("NewTenantRepository(%q) expected error, got nil", tenantID)
+			}
+			if repo != nil {
+				t.Error("NewTenantRepository returned non-nil repo with error")
+			}
+		})
+	}
+}
+
+func TestRepository_TableNames(t *testing.T) {
+	client := &Client{}
+
+	tests := []struct {
+		tenantID       string
+		wantThreads    string
+		wantMessages   string
+		wantFiles      string
+		wantUploads    string
+		wantVectorStores string
+	}{
+		{
+			tenantID:       "ai8",
+			wantThreads:    "ai8_airborne_threads",
+			wantMessages:   "ai8_airborne_messages",
+			wantFiles:      "ai8_airborne_files",
+			wantUploads:    "ai8_airborne_file_provider_uploads",
+			wantVectorStores: "ai8_airborne_thread_vector_stores",
+		},
+		{
+			tenantID:       "email4ai",
+			wantThreads:    "email4ai_airborne_threads",
+			wantMessages:   "email4ai_airborne_messages",
+			wantFiles:      "email4ai_airborne_files",
+			wantUploads:    "email4ai_airborne_file_provider_uploads",
+			wantVectorStores: "email4ai_airborne_thread_vector_stores",
+		},
+		{
+			tenantID:       "zztest",
+			wantThreads:    "zztest_airborne_threads",
+			wantMessages:   "zztest_airborne_messages",
+			wantFiles:      "zztest_airborne_files",
+			wantUploads:    "zztest_airborne_file_provider_uploads",
+			wantVectorStores: "zztest_airborne_thread_vector_stores",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.tenantID, func(t *testing.T) {
+			repo, err := NewTenantRepository(client, tt.tenantID)
+			if err != nil {
+				t.Fatalf("NewTenantRepository error = %v", err)
+			}
+
+			if got := repo.threadsTable(); got != tt.wantThreads {
+				t.Errorf("threadsTable() = %q, want %q", got, tt.wantThreads)
+			}
+			if got := repo.messagesTable(); got != tt.wantMessages {
+				t.Errorf("messagesTable() = %q, want %q", got, tt.wantMessages)
+			}
+			if got := repo.filesTable(); got != tt.wantFiles {
+				t.Errorf("filesTable() = %q, want %q", got, tt.wantFiles)
+			}
+			if got := repo.fileUploadsTable(); got != tt.wantUploads {
+				t.Errorf("fileUploadsTable() = %q, want %q", got, tt.wantUploads)
+			}
+			if got := repo.vectorStoresTable(); got != tt.wantVectorStores {
+				t.Errorf("vectorStoresTable() = %q, want %q", got, tt.wantVectorStores)
+			}
+		})
+	}
+}
+
+func TestNewRepository_Legacy(t *testing.T) {
+	client := &Client{}
+	repo := NewRepository(client)
+
+	if repo == nil {
+		t.Fatal("NewRepository returned nil")
+	}
+
+	// Legacy repository should have empty prefix
+	if repo.tablePrefix != "" {
+		t.Errorf("tablePrefix = %q, want empty", repo.tablePrefix)
+	}
+
+	// Legacy table names
+	if got := repo.threadsTable(); got != "airborne_threads" {
+		t.Errorf("threadsTable() = %q, want %q", got, "airborne_threads")
+	}
+	if got := repo.messagesTable(); got != "airborne_messages" {
+		t.Errorf("messagesTable() = %q, want %q", got, "airborne_messages")
+	}
+}
```

---

### 8. ImageGen Generate Tests (MEDIUM PRIORITY)

Tests for image generation (mocked HTTP).

**File:** `internal/imagegen/gemini_test.go`

```diff
--- /dev/null
+++ b/internal/imagegen/gemini_test.go
@@ -0,0 +1,96 @@
+package imagegen
+
+import (
+	"bytes"
+	"image"
+	"image/color"
+	"image/png"
+	"testing"
+)
+
+func TestConvertToJPEG_ValidPNG(t *testing.T) {
+	// Create a simple test PNG image
+	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
+	for y := 0; y < 100; y++ {
+		for x := 0; x < 100; x++ {
+			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
+		}
+	}
+
+	var pngBuf bytes.Buffer
+	if err := png.Encode(&pngBuf, img); err != nil {
+		t.Fatalf("Failed to create test PNG: %v", err)
+	}
+
+	jpegData, width, height := convertToJPEG(pngBuf.Bytes())
+
+	if len(jpegData) == 0 {
+		t.Error("convertToJPEG returned empty data")
+	}
+	if width != 100 {
+		t.Errorf("width = %d, want 100", width)
+	}
+	if height != 100 {
+		t.Errorf("height = %d, want 100", height)
+	}
+
+	// Verify it's valid JPEG by checking magic bytes
+	if len(jpegData) < 2 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
+		t.Error("output does not appear to be JPEG format")
+	}
+}
+
+func TestConvertToJPEG_InvalidData(t *testing.T) {
+	invalidData := []byte("not an image")
+	result, width, height := convertToJPEG(invalidData)
+
+	// Should return original data on decode failure
+	if !bytes.Equal(result, invalidData) {
+		t.Error("expected original data returned for invalid input")
+	}
+	if width != 0 || height != 0 {
+		t.Errorf("expected 0x0 dimensions for invalid input, got %dx%d", width, height)
+	}
+}
+
+func TestClient_Generate_NilRequest(t *testing.T) {
+	client := NewClient()
+
+	_, err := client.Generate(nil, nil)
+	if err == nil {
+		t.Fatal("expected error for nil request")
+	}
+}
+
+func TestClient_Generate_NilConfig(t *testing.T) {
+	client := NewClient()
+
+	req := &ImageRequest{
+		Prompt: "test",
+		Config: nil,
+	}
+
+	_, err := client.Generate(nil, req)
+	if err == nil {
+		t.Fatal("expected error for nil config")
+	}
+}
+
+func TestClient_Generate_UnsupportedProvider(t *testing.T) {
+	client := NewClient()
+
+	req := &ImageRequest{
+		Prompt: "test",
+		Config: &Config{
+			Enabled:  true,
+			Provider: "unsupported-provider",
+		},
+	}
+
+	_, err := client.Generate(nil, req)
+	if err == nil {
+		t.Fatal("expected error for unsupported provider")
+	}
+}
```

---

## Scoring Breakdown

### Core Business Logic (75/100)
- **Service Layer:** Excellent test coverage with mocks, table-driven tests
- **Auth Layer:** Good coverage of interceptors, rate limiting, static auth
- **Tenant Management:** Well tested with env, secrets, config, loader
- **Missing:** No tests for actual gRPC service end-to-end

### Provider Implementations (45/100)
- **Tested (5):** OpenAI, Gemini, Anthropic, Mistral, OpenAI-compat
- **Untested (12):** OpenRouter, DeepSeek, Grok, Fireworks, Together, Cohere, Hyperbolic, Cerebras, DeepInfra, Upstage, Nebius, Perplexity
- The compat layer is well-tested, but individual providers lack verification

### Database Layer (0/100)
- **CRITICAL GAP:** Zero tests for database operations
- 813 lines of untested code handling threads, messages, activity feeds
- Risk: Data persistence bugs, SQL injection (though parameterized), tenant isolation

### Utility/Helper Tests (85/100)
- **Validation:** Excellent (93.9% coverage)
- **Config:** Good coverage
- **Retry:** Partial (missing backoff tests)
- **Errors:** Adequate sanitization tests

### Test Quality & Patterns (85/100)
- Consistent use of table-driven tests
- Good mock implementations
- Clear test naming conventions
- Uses native Go testing (no external frameworks)
- Good test utilities in `rag/testutil`

---

## Recommendations (Prioritized)

### Immediate (P0)
1. **Add database repository tests** - Create integration tests or use pgx mock for critical path testing
2. **Add pricing tests** - Ensure cost calculations are correct (billing critical)

### Short-term (P1)
3. **Add tests for top 4 untested providers** - OpenRouter, DeepSeek, Grok, Fireworks
4. **Add imagegen generate tests** - Mock HTTP responses for Gemini/OpenAI image generation
5. **Add retry backoff tests** - Verify exponential backoff behavior

### Medium-term (P2)
6. **Complete remaining provider tests** - Together, Cohere, Hyperbolic, etc.
7. **Add integration tests** - End-to-end gRPC service tests
8. **Frontend testing setup** - Add Jest/Vitest for React dashboard

---

## Test Infrastructure Notes

### Running Tests
```bash
make test           # Run all tests with race detection
make test-coverage  # Generate coverage report
```

### Test Patterns in Codebase
- Table-driven tests with `t.Run()`
- Custom mocks (no external mock frameworks)
- Context helpers for auth testing
- Test utilities in `internal/rag/testutil/mocks.go`

### Adding New Tests
1. Create `*_test.go` in same package
2. Use table-driven tests for multiple cases
3. Follow existing patterns (see `validation/limits_test.go` for exemplary tests)
4. Use descriptive test names: `Test<Function>_<Scenario>`
