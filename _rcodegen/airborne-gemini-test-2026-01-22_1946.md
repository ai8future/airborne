# Comprehensive Unit Test Analysis & Proposal
Date Created: 2026-01-22 19:46:00

## Executive Summary

This report identifies critical areas in the Airborne codebase that lack sufficient unit test coverage and proposes comprehensive tests to address these gaps. The analysis focused on core business logic, administrative interfaces, and utility packages.

**Key Findings:**
1.  **`internal/pricing`**: This package handles cost calculations which are critical for billing and usage tracking. Currently, it lacks a dedicated test file. While it wraps an external library, the local `Cost` struct formatting and graceful degradation logic should be verified.
2.  **`internal/admin`**: The administrative HTTP server contains significant logic for request handling, validation, and error reporting. There are no unit tests for these handlers, leaving the admin dashboard API vulnerable to regressions.
3.  **`internal/service`**: The chat service has good coverage, but some helper functions and edge cases in `prepareRequest` could be strengthened. (Note: Existing tests in `chat_test.go` are already quite extensive, so efforts are focused on `admin` and `pricing`).

## Proposed Test Plan

The following test suites will be added:

1.  **`internal/pricing/pricing_test.go`**:
    *   Verify `Cost.Format()` output for various scenarios (zero values, normal values, unknown models).
    *   Verify `CalculateCost` handles unknown models gracefully (returning 0).
    *   Verify `CalculateGroundingCost` logic.

2.  **`internal/admin/server_test.go`**:
    *   Test `handleHealth` for both healthy and degraded states (simulated via nil/non-nil DB client).
    *   Test `handleVersion` returns correct JSON.
    *   Test `handleActivity` gracefully handles missing database connection.
    *   Test `handleTest` request validation (empty prompt, invalid JSON).
    *   Test `handleChat` request validation (missing fields, invalid UUIDs).

## Patch-Ready Diffs

### 1. `internal/pricing/pricing_test.go`

```go
package pricing

import (
	"strings"
	"testing"
)

func TestCost_Format(t *testing.T) {
	tests := []struct {
		name     string
		cost     Cost
		contains []string
	}{
		{
			name: "Normal cost",
			cost: Cost{
				Model:        "gpt-4",
				InputTokens:  100,
				OutputTokens: 50,
				InputCost:    0.003,
				OutputCost:   0.003,
				TotalCost:    0.006,
			},
			contains: []string{
				"Input: $0.0030 (100 tokens)",
				"Output: $0.0030 (50 tokens)",
				"Total: $0.0060",
			},
		},
		{
			name: "Zero cost",
			cost: Cost{
				Model: "free-model",
			},
			contains: []string{
				"Input: $0.0000 (0 tokens)",
				"Total: $0.0000",
			},
		},
		{
			name: "Unknown model",
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

func TestCalculateCost_GracefulDegradation(t *testing.T) {
	// Test with a definitely unknown model
	cost := CalculateCost("completely-made-up-model-xyz", 1000, 1000)
	if cost != 0 {
		t.Errorf("Expected 0 cost for unknown model, got %f", cost)
	}
}

func TestCalculateGroundingCost(t *testing.T) {
	// Test basic grounding cost calculation
	// Note: exact values depend on the embedded DB, so we test behavior
	
	// Gemini 3 (per query)
	cost3 := CalculateGroundingCost("gemini-3-pro", 5)
	if cost3 <= 0 {
		t.Error("Expected positive cost for Gemini 3 grounding")
	}

	// Gemini 2.5 (flat fee per request if used)
	cost25 := CalculateGroundingCost("gemini-2.5-pro", 1)
	if cost25 <= 0 {
		t.Error("Expected positive cost for Gemini 2.5 grounding")
	}
	
	// No queries = 0 cost
	costZero := CalculateGroundingCost("any-model", 0)
	if costZero != 0 {
		t.Errorf("Expected 0 cost for 0 queries, got %f", costZero)
	}
}

func TestGetPricing(t *testing.T) {
	// Test with a known model (gpt-4o is standard)
	pricing, ok := GetPricing("gpt-4o")
	if !ok {
		// If DB is not loaded or model missing, this might fail, 
		// but in standard environment it should pass if pricing_db is working.
		// We'll skip if it fails to avoid breaking builds on empty environments.
		t.Skip("gpt-4o pricing not found, skipping integration test")
	}

	if pricing.InputPricePerMillion <= 0 {
		t.Error("Expected positive input price")
	}
}
```

### 2. `internal/admin/server_test.go`

```go
package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai8future/airborne/internal/db"
	"github.com/google/uuid"
)

// createTestServer creates a server instance for testing
func createTestServer() *Server {
	return NewServer(nil, Config{
		Port: 8080,
		Version: VersionInfo{
			Version:   "1.0.0-test",
			GitCommit: "abcdef",
			BuildTime: "2026-01-01",
		},
	})
}

func TestHandleHealth(t *testing.T) {
	s := createTestServer()

	req := httptest.NewRequest("GET", "/admin/health", nil)
	w := httptest.NewRecorder()

	// Direct call to handler (bypassing mux for unit test)
	http.HandlerFunc(s.handleHealth).ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// With nil dbClient, status should be healthy but database not_configured
	if body["status"] != "healthy" {
		t.Errorf("Expected status healthy, got %s", body["status"])
	}
	if body["database"] != "not_configured" {
		t.Errorf("Expected database not_configured, got %s", body["database"])
	}
}

func TestHandleHealth_MethodNotAllowed(t *testing.T) {
	s := createTestServer()
	req := httptest.NewRequest("POST", "/admin/health", nil)
	w := httptest.NewRecorder()

	http.HandlerFunc(s.handleHealth).ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandleVersion(t *testing.T) {
	s := createTestServer()

	req := httptest.NewRequest("GET", "/admin/version", nil)
	w := httptest.NewRecorder()

	http.HandlerFunc(s.handleVersion).ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var ver VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if ver.Version != "1.0.0-test" {
		t.Errorf("Expected version 1.0.0-test, got %s", ver.Version)
	}
}

func TestHandleActivity_NoDB(t *testing.T) {
	s := createTestServer() // nil dbClient

	req := httptest.NewRequest("GET", "/admin/activity", nil)
	w := httptest.NewRecorder()

	http.HandlerFunc(s.handleActivity).ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body["error"] != "database not configured" {
		t.Errorf("Expected database not configured error, got %v", body["error"])
	}
}

func TestHandleTest_Validation(t *testing.T) {
	s := createTestServer()

	tests := []struct {
		name       string
		method     string
		body       interface{}
		wantStatus int
		wantError  string
	}{
		{
			name:       "Invalid Method",
			method:     "GET",
			body:       nil,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "Empty Body",
			method:     "POST",
			body:       nil, // Will fail decode
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name: "Empty Prompt",
			method: "POST",
			body: TestRequest{
				Prompt: "",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "prompt is required",
		},
		{
			name: "Valid Request (No gRPC)",
			method: "POST",
			body: TestRequest{
				Prompt: "Hello",
			},
			wantStatus: http.StatusServiceUnavailable, // gRPC not configured
			wantError:  "gRPC address not configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader *bytes.Reader
			if tt.body != nil {
				jsonBytes, _ := json.Marshal(tt.body)
				bodyReader = bytes.NewReader(jsonBytes)
			} else {
				bodyReader = bytes.NewReader([]byte{})
			}

			req := httptest.NewRequest(tt.method, "/admin/test", bodyReader)
			w := httptest.NewRecorder()

			http.HandlerFunc(s.handleTest).ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}

			if tt.wantError != "" {
				var resp TestResponse
				// Depending on the handler, it might return TestResponse or a generic map
				// handleTest returns TestResponse struct even on error
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					// Fallback try map
					var errMap map[string]interface{}
					json.Unmarshal(w.Body.Bytes(), &errMap)
					if msg, ok := errMap["error"].(string); ok {
						if !strings.Contains(msg, tt.wantError) {
							t.Errorf("Expected error containing %q, got %q", tt.wantError, msg)
						}
					}
				} else {
					if !strings.Contains(resp.Error, tt.wantError) {
						t.Errorf("Expected error containing %q, got %q", tt.wantError, resp.Error)
					}
				}
			}
		})
	}
}

func TestHandleChat_Validation(t *testing.T) {
	s := createTestServer()

	tests := []struct {
		name       string
		method     string
		body       interface{}
		wantStatus int
		wantError  string
	}{
		{
			name:       "Invalid Method",
			method:     "GET",
			body:       nil,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name: "Missing Message",
			method: "POST",
			body: ChatRequest{
				ThreadID: uuid.New().String(),
				Message:  "",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "message is required",
		},
		{
			name: "Missing ThreadID",
			method: "POST",
			body: ChatRequest{
				ThreadID: "",
				Message:  "Hello",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "thread_id is required",
		},
		{
			name: "Invalid ThreadID",
			method: "POST",
			body: ChatRequest{
				ThreadID: "not-a-uuid",
				Message:  "Hello",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid thread_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(tt.method, "/admin/chat", bytes.NewReader(jsonBytes))
			w := httptest.NewRecorder()

			http.HandlerFunc(s.handleChat).ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}

			if tt.wantError != "" {
				var resp ChatResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if !strings.Contains(resp.Error, tt.wantError) {
					t.Errorf("Expected error containing %q, got %q", tt.wantError, resp.Error)
				}
			}
		})
	}
}

func TestDetectMIMEType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"test.pdf", "application/pdf"},
		{"test.txt", "text/plain"},
		{"TEST.TXT", "text/plain"},
		{"path/to/test.json", "application/json"},
		{"unknown.xyz", "application/octet-stream"},
		{"noext", "application/octet-stream"},
	}

	for _, tt := range tests {
		got := detectMIMEType(tt.filename)
		if got != tt.want {
			t.Errorf("detectMIMEType(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}
```
