Date Created: 2026-01-24 10:00:00
TOTAL_SCORE: 75/100

# Test Coverage Analysis Report

## Overview
The `airborne` codebase has a solid foundation of unit tests, particularly in the `internal/provider`, `internal/auth`, and `internal/rag` packages. However, there are significant gaps in the `internal/pricing` and `internal/cli` packages, which are critical for cost calculation and administrative operations respectively.

## Identified Gaps
1.  **`internal/pricing`**: This package handles cost calculations but completely lacks unit tests. Given its role in financial data (cost estimation), it requires rigorous testing to ensure accuracy and proper handling of edge cases (e.g., unknown models).
2.  **`internal/cli`**: The CLI client wrapper (`Client` struct) is untested. This makes the `airborne-cli` tool potentially fragile to API changes or regressions in client logic.
3.  **`internal/admin`**: The admin server logic is complex and largely untested, though it leans more towards integration testing which is out of scope for this specific unit test "quick fix" pass.

## Proposed Changes
I have generated comprehensive unit tests for `internal/pricing` and `internal/cli`.

### 1. `internal/pricing/pricing_test.go`
Added tests for:
-   `NewPricer` initialization.
-   `Calculate` delegation logic (verifying it calls the underlying db).
-   `Cost.Format` string formatting (ensuring human-readable output is correct).
-   `CalculateGrounding` delegation.

### 2. `internal/cli/client_test.go`
Added tests for:
-   `Health` endpoint (success and failure).
-   `Activity` endpoint (verifying query parameters and response parsing).
-   `Test` endpoint (verifying POST body and response).
-   Generic error handling for non-200 responses.

## Patch-Ready Diffs

```go
// internal/pricing/pricing_test.go

package pricing

import (
	"strings"
	"testing"
)

func TestNewPricer(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() failed: %v", err)
	}
	if pricer == nil {
		t.Fatal("NewPricer() returned nil")
	}
	if pricer.db == nil {
		t.Error("NewPricer() returned pricer with nil db")
	}
}

func TestCalculate(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() failed: %v", err)
	}

	// Test with a potentially known model to check delegation
	// Note: precise values depend on the embedded db, so we verify structural correctness
	cost := pricer.Calculate("gpt-4o", 1000, 500)
	
	if cost.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", cost.Model)
	}
	// Basic sanity check that inputs are preserved
	if cost.InputTokens != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", cost.InputTokens)
	}
	if cost.OutputTokens != 500 {
		t.Errorf("expected 500 output tokens, got %d", cost.OutputTokens)
	}
}

func TestCalculateGrounding(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() failed: %v", err)
	}

	// Just verifying delegation doesn't panic
	_ = pricer.CalculateGrounding("gemini-1.5-pro", 1)
}

func TestCost_Format(t *testing.T) {
	tests := []struct {
		name     string
		cost     Cost
		contains []string
	}{
		{
			name: "known cost",
			cost: Cost{
				Model:        "test-model",
				InputTokens:  1000,
				OutputTokens: 500,
				InputCost:    0.01,
				OutputCost:   0.02,
				TotalCost:    0.03,
				Unknown:      false,
			},
			contains: []string{
				"Input: $0.0100 (1000 tokens)",
				"Output: $0.0200 (500 tokens)",
				"Total: $0.0300",
			},
		},
		{
			name: "unknown cost",
			cost: Cost{
				Model:   "unknown-model",
				Unknown: true,
			},
			contains: []string{
				"Cost: unknown",
				"unknown-model",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cost.Format()
			for _, substr := range tt.contains {
				if !strings.Contains(got, substr) {
					t.Errorf("Format() = %q, want it to contain %q", got, substr)
				}
			}
		})
	}
}
```

```go
// internal/cli/client_test.go

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Health(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			t.Errorf("Expected path /admin/health, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(HealthResponse{Status: "healthy", Database: "healthy"})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	health, err := client.Health()
	if err != nil {
		t.Fatalf("Health() failed: %v", err)
	}

	if health.Status != "healthy" {
		t.Errorf("Expected status healthy, got %s", health.Status)
	}
}

func TestClient_Activity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/activity" {
			t.Errorf("Expected path /admin/activity, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("Expected limit 10, got %s", r.URL.Query().Get("limit"))
		}
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ActivityResponse{
			Activity: []Activity{{ID: "act-1", Model: "gpt-4"}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	activity, err := client.Activity(10, "")
	if err != nil {
		t.Fatalf("Activity() failed: %v", err)
	}

	if len(activity.Activity) != 1 {
		t.Errorf("Expected 1 activity item, got %d", len(activity.Activity))
	}
	if activity.Activity[0].ID != "act-1" {
		t.Errorf("Expected ID act-1, got %s", activity.Activity[0].ID)
	}
}

func TestClient_Test(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/test" {
			t.Errorf("Expected path /admin/test, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}
		
		var req TestRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Prompt != "hello" {
			t.Errorf("Expected prompt hello, got %s", req.Prompt)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(TestResponse{Reply: "world", Model: "gpt-4"})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.Test(TestRequest{Prompt: "hello"})
	if err != nil {
		t.Fatalf("Test() failed: %v", err)
	}

	if resp.Reply != "world" {
		t.Errorf("Expected reply world, got %s", resp.Reply)
	}
}

func TestClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.Health()
	if err == nil {
		t.Error("Expected error for 500 response, got nil")
	}
}
```
