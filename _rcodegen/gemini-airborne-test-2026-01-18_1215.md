Date Created: Sunday, January 18, 2026 at 12:15 PM
TOTAL_SCORE: 78/100

# Audit Report

## Summary
The `airborne` codebase demonstrates a solid testing foundation, particularly in core modules like `internal/auth`, `internal/rag`, and `internal/service`. However, significant gaps exist in business-critical areas such as `internal/pricing` (cost calculation) and `internal/admin` (observability). The provider implementations are largely wrappers but lack basic instantiation tests.

## Coverage Analysis
*   **High Coverage:** `auth`, `rag` (chunker, embedder, extractor), `config`, `service`.
*   **Missing Coverage:**
    *   `internal/pricing`: **Critical**. Cost calculations are central to the business logic but are currently untested.
    *   `internal/admin`: **High**. Operational endpoints (health, debug) need verification.
    *   `internal/provider/*`: **Medium**. Many individual provider clients (e.g., `cerebras`, `deepseek`) lack basic instantiation tests.

## Proposed Tests

### 1. Pricing Logic (`internal/pricing`)
**Rationale:** Ensures accurate cost tracking for various models and providers.

```go
// internal/pricing/pricing_test.go
package pricing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPricer_Calculate(t *testing.T) {
	// Setup temp config dir
	tmpDir := t.TempDir()
	
	pricingData := pricingFile{
		Provider: "testprovider",
		Models: map[string]ModelPricing{
			"gpt-test": {
				InputPerMillion:  10.0,
				OutputPerMillion: 30.0,
			},
			"claude-base": {
				InputPerMillion: 5.0,
				OutputPerMillion: 15.0,
			},
		},
	}
	
	data, _ := json.Marshal(pricingData)
	_ = os.WriteFile(filepath.Join(tmpDir, "test_pricing.json"), data, 0644)

	// Init pricer
	pricer, err := NewPricer(tmpDir)
	if err != nil {
		t.Fatalf("NewPricer failed: %v", err)
	}

	tests := []struct {
		name         string
		model        string
		input        int64
		output       int64
		expectedCost float64
		unknown      bool
	}{
		{
			name:         "exact match",
			model:        "gpt-test",
			input:        1_000_000,
			output:       1_000_000,
			expectedCost: 40.0, // 10 + 30
		},
		{
			name:         "prefix match",
			model:        "claude-base-v1",
			input:        1_000_000,
			output:       0,
			expectedCost: 5.0,
		},
		{
			name:    "unknown model",
			model:   "unknown-model",
			unknown: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := pricer.Calculate(tt.model, tt.input, tt.output)
			if tt.unknown {
				if !cost.Unknown {
					t.Error("expected unknown model")
				}
				return
			}

			if cost.TotalCost != tt.expectedCost {
				t.Errorf("got cost %f, want %f", cost.TotalCost, tt.expectedCost)
			}
		})
	}
}
```

### 2. Admin Server Health (`internal/admin`)
**Rationale:** Verifies the operational interface and health check logic.

```go
// internal/admin/server_test.go
package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServer_Health(t *testing.T) {
	// Create server with nil repo (should report healthy but no db)
	srv := NewServer(nil, Config{Port: 8080})

	req := httptest.NewRequest("GET", "/admin/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}

	if body["status"] != "healthy" {
		t.Errorf("expected status healthy, got %v", body["status"])
	}
}
```

### 3. Cerebras Provider Client (`internal/provider/cerebras`)
**Rationale:** Ensures the provider implementation satisfies the interface and handles options correctly.

```go
// internal/provider/cerebras/client_test.go
package cerebras

import (
	"testing"

	"github.com/ai8future/airborne/internal/provider"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	
	// Verify it implements the interface
	var _ provider.Provider = client
}

func TestNewClient_Options(t *testing.T) {
	client := NewClient(WithDebugLogging(true))
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
```
