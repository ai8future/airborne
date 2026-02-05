Date Created: Friday, January 23, 2026 at 10:15:00 AM PST
TOTAL_SCORE: 65/100

# Test Coverage Analysis & Proposal

## Executive Summary
The `airborne` codebase exhibits mixed test coverage. Core logic in `internal/service` and `internal/auth` is well-tested. However, significant gaps exist in the data access layer (`internal/db`) and provider integrations (`internal/provider/*`), where many sub-packages lack dedicated tests. `internal/db` relies heavily on integration with a running PostgreSQL instance, making unit testing difficult without extensive mocking or refactoring.

## Scoring Breakdown
- **Service Layer (`internal/service`):** 90/100. Good coverage with `_test.go` files for most components.
- **Auth Layer (`internal/auth`):** 85/100. Interceptors and logic are tested.
- **Provider Layer (`internal/provider`):** 40/100. `compat` and `openai` are tested, but many specific providers (DeepSeek, Grok, etc.) lack configuration tests.
- **DB Layer (`internal/db`):** 10/100. No tests found. Core logic is tightly coupled to `pgx`.
- **Image Gen (`internal/imagegen`):** 50/100. `client_test.go` exists, but specific provider implementations like `gemini.go` contain untested logic.

**Overall Grade:** 65/100

## Proposed Improvements
This report proposes adding unit tests for:
1.  **`internal/db`**: Testing pure logic methods in `models.go` (Citations parsing, Struct methods).
2.  **`internal/provider/deepseek`**: Adding a configuration test to ensure the client is initialized correctly, establishing a pattern for other providers.
3.  **`internal/imagegen`**: Testing the `convertToJPEG` helper function in `gemini.go`.

## 1. Internal DB Models Tests
`internal/db/models.go` contains several logic-heavy methods (`ParseCitations`, `CitationsToJSON`, `NewThread`, etc.) that can be tested without a database connection.

```go
// internal/db/models_test.go

package db

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCitationsJSON(t *testing.T) {
	// Test CitationsToJSON
	citations := []Citation{
		{
			Type:  "url",
			URL:   "https://example.com",
			Title: "Example",
		},
	}

	jsonStr, err := CitationsToJSON(citations)
	if err != nil {
		t.Fatalf("CitationsToJSON failed: %v", err)
	}
	if jsonStr == nil {
		t.Fatal("expected non-nil json string")
	}

	// Test ParseCitations
	parsed, err := ParseCitations(jsonStr)
	if err != nil {
		t.Fatalf("ParseCitations failed: %v", err)
	}

	if len(parsed) != 1 {
		t.Errorf("expected 1 citation, got %d", len(parsed))
	}
	if parsed[0].URL != "https://example.com" {
		t.Errorf("expected URL https://example.com, got %s", parsed[0].URL)
	}

	// Test nil/empty handling
	nilJSON, err := CitationsToJSON(nil)
	if err != nil {
		t.Error(err)
	}
	if nilJSON != nil {
		t.Error("expected nil for nil citations")
	}

	emptyParsed, err := ParseCitations(nil)
	if err != nil {
		t.Error(err)
	}
	if emptyParsed != nil {
		t.Error("expected nil for nil string")
	}
}

func TestNewThread(t *testing.T) {
	userID := "user123"
	thread := NewThread(userID)

	if thread.UserID != userID {
		t.Errorf("expected userID %s, got %s", userID, thread.UserID)
	}
	if thread.Status != ThreadStatusActive {
		t.Errorf("expected status %s, got %s", ThreadStatusActive, thread.Status)
	}
	if thread.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if thread.MessageCount != 0 {
		t.Errorf("expected message count 0, got %d", thread.MessageCount)
	}
}

func TestMessage_SetAssistantMetrics(t *testing.T) {
	msg := NewMessage(uuid.New(), RoleAssistant, "content")
	
	provider := "openai"
	model := "gpt-4"
	input := 10
	output := 20
	timeMs := 100
	cost := 0.05
	respID := "resp-123"

	msg.SetAssistantMetrics(provider, model, input, output, timeMs, cost, respID)

	if *msg.Provider != provider {
		t.Errorf("expected provider %s, got %v", provider, msg.Provider)
	}
	if *msg.TotalTokens != 30 {
		t.Errorf("expected total tokens 30, got %v", msg.TotalTokens)
	}
	if *msg.CostUSD != cost {
		t.Errorf("expected cost %f, got %v", cost, msg.CostUSD)
	}
	if *msg.ResponseID != respID {
		t.Errorf("expected response ID %s, got %v", respID, msg.ResponseID)
	}
}

func TestMessage_TruncateContent(t *testing.T) {
	msg := &Message{Content: "hello world"}
	
	if got := msg.TruncateContent(5); got != "hello..." {
		t.Errorf("expected 'hello...', got '%s'", got)
	}
	
	if got := msg.TruncateContent(20); got != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", got)
	}
}
```

## 2. DeepSeek Provider Tests
Ensures the DeepSeek client is correctly configured, acting as a template for other providers.

```go
// internal/provider/deepseek/client_test.go

package deepseek

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.Name() != "deepseek" {
		t.Errorf("expected name 'deepseek', got '%s'", client.Name())
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	client := NewClient(WithDebugLogging(true))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	
	// Note: We can't easily check the debug flag on the embedded client without reflection 
	// or exposing it, but we verify the option application doesn't panic.
}
```

## 3. Image Gen Helper Tests
Testing the image conversion logic in `internal/imagegen/gemini.go`.

```go
// internal/imagegen/gemini_test.go

package imagegen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestConvertToJPEG(t *testing.T) {
	// Create a simple test image (PNG)
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			img.Set(x, y, color.White)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to create test image: %v", err)
	}

	// Test conversion
	jpegData, width, height := convertToJPEG(buf.Bytes())

	if width != 10 || height != 10 {
		t.Errorf("expected dimensions 10x10, got %dx%d", width, height)
	}

	if len(jpegData) == 0 {
		t.Error("returned empty jpeg data")
	}
	
	// Verify it is a valid JPEG
	_, format, err := image.DecodeConfig(bytes.NewReader(jpegData))
	if err != nil {
		t.Errorf("failed to decode result: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("expected jpeg format, got %s", format)
	}
}

func TestConvertToJPEG_InvalidData(t *testing.T) {
	invalidData := []byte("not an image")
	
	// Should return original data and 0 dims on failure
	out, w, h := convertToJPEG(invalidData)
	
	if w != 0 || h != 0 {
		t.Errorf("expected 0x0 dims, got %dx%d", w, h)
	}
	
	if string(out) != string(invalidData) {
		t.Error("expected original data to be returned")
	}
}
```
