package db

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewThread(t *testing.T) {
	userID := "test-user"
	thread := NewThread(userID)

	if thread == nil {
		t.Fatal("NewThread() returned nil")
	}
	if thread.UserID != userID {
		t.Errorf("UserID = %q, want %q", thread.UserID, userID)
	}
	if thread.Status != ThreadStatusActive {
		t.Errorf("Status = %q, want %q", thread.Status, ThreadStatusActive)
	}
	if thread.MessageCount != 0 {
		t.Errorf("MessageCount = %d, want 0", thread.MessageCount)
	}
	if thread.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if thread.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if !thread.CreatedAt.Equal(thread.UpdatedAt) {
		t.Errorf("CreatedAt (%v) != UpdatedAt (%v)", thread.CreatedAt, thread.UpdatedAt)
	}
}

func TestNewMessage(t *testing.T) {
	threadID := uuid.New()
	role := RoleUser
	content := "Hello, world!"

	msg := NewMessage(threadID, role, content)

	if msg == nil {
		t.Fatal("NewMessage() returned nil")
	}
	if msg.ThreadID != threadID {
		t.Errorf("ThreadID = %v, want %v", msg.ThreadID, threadID)
	}
	if msg.Role != role {
		t.Errorf("Role = %q, want %q", msg.Role, role)
	}
	if msg.Content != content {
		t.Errorf("Content = %q, want %q", msg.Content, content)
	}
	if msg.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if time.Since(msg.CreatedAt) > time.Second {
		t.Errorf("CreatedAt too old: %v", msg.CreatedAt)
	}
}

func TestMessage_SetAssistantMetrics(t *testing.T) {
	msg := NewMessage(uuid.New(), RoleAssistant, "Response")

	provider := "gemini"
	model := "gemini-pro"
	inputTokens := 100
	outputTokens := 50
	processingTimeMs := 500
	costUSD := 0.001
	responseID := "resp-123"

	msg.SetAssistantMetrics(provider, model, inputTokens, outputTokens, processingTimeMs, costUSD, responseID)

	if *msg.Provider != provider {
		t.Errorf("Provider = %q, want %q", *msg.Provider, provider)
	}
	if *msg.Model != model {
		t.Errorf("Model = %q, want %q", *msg.Model, model)
	}
	if *msg.InputTokens != inputTokens {
		t.Errorf("InputTokens = %d, want %d", *msg.InputTokens, inputTokens)
	}
	if *msg.OutputTokens != outputTokens {
		t.Errorf("OutputTokens = %d, want %d", *msg.OutputTokens, outputTokens)
	}
	if *msg.TotalTokens != inputTokens+outputTokens {
		t.Errorf("TotalTokens = %d, want %d", *msg.TotalTokens, inputTokens+outputTokens)
	}
	if *msg.CostUSD != costUSD {
		t.Errorf("CostUSD = %f, want %f", *msg.CostUSD, costUSD)
	}
	if *msg.ProcessingTimeMs != processingTimeMs {
		t.Errorf("ProcessingTimeMs = %d, want %d", *msg.ProcessingTimeMs, processingTimeMs)
	}
	if *msg.ResponseID != responseID {
		t.Errorf("ResponseID = %q, want %q", *msg.ResponseID, responseID)
	}
}

func TestMessage_SetAssistantMetrics_EmptyResponseID(t *testing.T) {
	msg := NewMessage(uuid.New(), RoleAssistant, "Response")
	msg.SetAssistantMetrics("gemini", "gemini-pro", 100, 50, 500, 0.001, "")

	if msg.ResponseID != nil {
		t.Error("ResponseID should be nil for empty responseID")
	}
}

func TestMessage_TruncateContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		maxLen  int
		want    string
	}{
		{"short content", "Hello", 10, "Hello"},
		{"exact length", "HelloWorld", 10, "HelloWorld"},
		{"needs truncation", "Hello World!", 5, "Hello..."},
		{"empty content", "", 10, ""},
		{"single char max", "Hello", 1, "H..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{Content: tt.content}
			got := msg.TruncateContent(tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateContent(%d) = %q, want %q", tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestParseCitations(t *testing.T) {
	tests := []struct {
		name    string
		json    *string
		wantLen int
		wantErr bool
	}{
		{"nil json", nil, 0, false},
		{"empty string", strPtr(""), 0, false},
		{"empty array", strPtr("[]"), 0, false},
		{"valid citations", strPtr(`[{"type":"url","url":"https://example.com","title":"Example"}]`), 1, false},
		{"invalid json", strPtr("{invalid}"), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCitations(tt.json)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCitations() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("ParseCitations() returned %d citations, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestCitationsToJSON(t *testing.T) {
	tests := []struct {
		name      string
		citations []Citation
		wantNil   bool
	}{
		{"nil citations", nil, true},
		{"empty citations", []Citation{}, true},
		{"single citation", []Citation{{Type: "url", URL: "https://example.com"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CitationsToJSON(tt.citations)
			if err != nil {
				t.Errorf("CitationsToJSON() error = %v", err)
				return
			}
			if (got == nil) != tt.wantNil {
				t.Errorf("CitationsToJSON() = %v, wantNil %v", got, tt.wantNil)
			}
			if got != nil {
				// Verify it's valid JSON
				var parsed []Citation
				if err := json.Unmarshal([]byte(*got), &parsed); err != nil {
					t.Errorf("CitationsToJSON() produced invalid JSON: %v", err)
				}
			}
		})
	}
}

func TestCitationsRoundTrip(t *testing.T) {
	original := []Citation{
		{Type: "url", URL: "https://example.com", Title: "Example"},
		{Type: "file", FileID: "file-123", Filename: "test.pdf"},
	}

	jsonStr, err := CitationsToJSON(original)
	if err != nil {
		t.Fatalf("CitationsToJSON() error = %v", err)
	}

	parsed, err := ParseCitations(jsonStr)
	if err != nil {
		t.Fatalf("ParseCitations() error = %v", err)
	}

	if len(parsed) != len(original) {
		t.Fatalf("round trip produced %d citations, want %d", len(parsed), len(original))
	}

	for i := range original {
		if parsed[i].Type != original[i].Type {
			t.Errorf("citation[%d].Type = %q, want %q", i, parsed[i].Type, original[i].Type)
		}
		if parsed[i].URL != original[i].URL {
			t.Errorf("citation[%d].URL = %q, want %q", i, parsed[i].URL, original[i].URL)
		}
	}
}

func strPtr(s string) *string { return &s }
