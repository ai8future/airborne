Date Created: Saturday, January 24, 2026
Date Updated: 2026-01-28
TOTAL_SCORE: 92/100

# Test Coverage Analysis

The `airborne` codebase demonstrates a solid testing culture with comprehensive tests in `internal/imagegen`, `internal/config`, and `internal/provider`. Core logic is generally well-covered.

However, a few key areas are missing unit tests:
1.  **`internal/admin`**: The admin server handlers and logic (specifically `buildCompressedHistory`) are untested. This is a critical gap as it handles operational endpoints and chat history compression.

*`internal/pricing` - TESTED in v1.7.15 (pricing_test.go)*

## Proposed Improvements

I have prepared patch-ready diffs to add:
1.  `internal/admin/server_test.go`: Tests for `handleVersion`, `handleHealth`, `detectMIMEType`, and `buildCompressedHistory`.
2.  `internal/pricing/pricing_test.go`: Tests for `Cost.Format` and basic wrapper functionality.

These additions will increase confidence in the admin interface and cost reporting logic.

## Patches

### 1. `internal/admin/server_test.go`

```go
package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai8future/airborne/internal/db"
)

func TestHandleVersion(t *testing.T) {
	version := VersionInfo{
		Version:   "1.0.0",
		GitCommit: "abcdef",
		BuildTime: "2026-01-01",
	}
	server := &Server{
		version: version,
	}

	req := httptest.NewRequest("GET", "/admin/version", nil)
	w := httptest.NewRecorder()

	server.handleVersion(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var got VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	if got != version {
		t.Errorf("expected %+v, got %+v", version, got)
	}
}

func TestHandleHealth(t *testing.T) {
	// Test healthy state (no db configured = healthy but not_configured)
	server := &Server{
		dbClient: nil,
	}

	req := httptest.NewRequest("GET", "/admin/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	if got["status"] != "healthy" {
		t.Errorf("expected status healthy, got %s", got["status"])
	}
	if got["database"] != "not_configured" {
		t.Errorf("expected database not_configured, got %s", got["database"])
	}
}

func TestDetectMIMEType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"test.pdf", "application/pdf"},
		{"TEST.PDF", "application/pdf"},
		{"image.png", "image/png"},
		{"doc.txt", "text/plain"},
		{"unknown.xyz", "application/octet-stream"},
		{"noext", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := detectMIMEType(tt.filename); got != tt.want {
				t.Errorf("detectMIMEType(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestBuildCompressedHistory(t *testing.T) {
	// Helper to create a message
	makeMsg := func(role, content string) db.Message {
		respID := "resp-123"
		return db.Message{
			Role:       role,
			Content:    content,
			CreatedAt:  time.Now(),
			ResponseID: &respID,
		}
	}

	t.Run("basic compression", func(t *testing.T) {
		messages := []db.Message{
			makeMsg("user", "Hello"),
			makeMsg("assistant", "Hi there"),
			makeMsg("user", "How are you?"),
		}

		var prevID string
		got := buildCompressedHistory(messages, &prevID)

		if len(got) != 3 {
			t.Errorf("expected 3 messages, got %d", len(got))
		}
		if prevID != "resp-123" {
			t.Errorf("expected prevID resp-123, got %s", prevID)
		}
	})

	t.Run("truncates long AI responses", func(t *testing.T) {
		// Create a long response
		longContent := ""
		for i := 0; i < 1000; i++ {
			longContent += "a"
		}
		
		// Create enough messages to trigger compression (> 3 full AI responses)
		var messages []db.Message
		for i := 0; i < 5; i++ {
			messages = append(messages, makeMsg("user", "hi"))
			messages = append(messages, makeMsg("assistant", longContent))
		}

		var prevID string
		got := buildCompressedHistory(messages, &prevID)
		
		// The 4th AI response (index 7 in the result list) should be truncated
		// Logic: currentAIResponse 1, 2, 3 are kept full. 4 is truncated.
		
		if len(got) < 8 {
			t.Fatalf("expected at least 8 messages, got %d", len(got))
		}
		
		fourthAI := got[7] 
		if len(fourthAI.Content) >= 1000 {
			t.Errorf("expected 4th AI response to be truncated, got len %d", len(fourthAI.Content))
		}
	})
}
```

### 2. `internal/pricing/pricing_test.go`

```go
package pricing

import (
	"testing"
)

func TestCostFormat(t *testing.T) {
	tests := []struct {
		name string
		cost Cost
		want string
	}{
		{
			name: "valid cost",
			cost: Cost{
				Model:        "gpt-4",
				InputTokens:  10,
				OutputTokens: 20,
				InputCost:    0.01,
				OutputCost:   0.02,
				TotalCost:    0.03,
			},
			want: "Input: $0.0100 (10 tokens) | Output: $0.0200 (20 tokens) | Total: $0.0300",
		},
		{
			name: "unknown model",
			cost: Cost{
				Model:   "unknown-model",
				Unknown: true,
			},
			want: "Cost: unknown (model \"unknown-model\" not in pricing data)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cost.Format(); got != tt.want {
				t.Errorf("Format() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewPricer(t *testing.T) {
	// This integration test depends on the pricing_db being available.
	p, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer failed: %v", err)
	}

	if p == nil {
		t.Fatal("NewPricer returned nil")
	}

	// Basic check that we can list providers (ensures db is loaded)
	providers := p.ListProviders()
	if len(providers) == 0 {
		t.Error("expected at least one provider")
	}
}

func TestCalculateCost(t *testing.T) {
	// Test the package-level helper
	cost := CalculateCost("gpt-4", 100, 100)
	if cost < 0 {
		t.Errorf("expected positive cost, got %f", cost)
	}
}
```
