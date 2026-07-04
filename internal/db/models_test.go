package db

import (
	"encoding/json"
	"testing"
)

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

// --- New Chat/ChatMessage model tests (Task 4) ---

func TestTextContent_RoundTrip(t *testing.T) {
	raw := TextContent("hi")
	if raw == nil {
		t.Fatal("TextContent() returned nil json.RawMessage; content is NOT NULL in the schema")
	}

	var decoded struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("TextContent() produced invalid JSON: %v (raw=%s)", err, raw)
	}
	if decoded.Text != "hi" {
		t.Errorf("decoded.Text = %q, want %q", decoded.Text, "hi")
	}
}

func TestTextContent_EscapesSpecialCharacters(t *testing.T) {
	input := "line1\nline2 \"quoted\" \\ backslash"
	raw := TextContent(input)

	var decoded struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("TextContent() produced invalid JSON: %v (raw=%s)", err, raw)
	}
	if decoded.Text != input {
		t.Errorf("decoded.Text = %q, want %q", decoded.Text, input)
	}
}

func TestTextContent_NeverNil(t *testing.T) {
	// Even the empty string must round-trip to a valid, non-nil JSON object,
	// since airborne_chat_messages.content is NOT NULL.
	raw := TextContent("")
	if raw == nil {
		t.Fatal("TextContent(\"\") returned nil")
	}
	if string(raw) != `{"text":""}` {
		t.Errorf("TextContent(\"\") = %s, want %s", raw, `{"text":""}`)
	}
}

func TestChatMessage_NilParentIDMarshalsAsRoot(t *testing.T) {
	msg := ChatMessage{
		ID:       "msg-1",
		TenantID: "ai8",
		ChatID:   "chat-1",
		ParentID: nil, // root message: no parent
		UserID:   "user-1",
		Role:     RoleUser,
		Content:  TextContent("hello"),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal(ChatMessage) error = %v", err)
	}

	var asMap map[string]interface{}
	if err := json.Unmarshal(data, &asMap); err != nil {
		t.Fatalf("json.Unmarshal into map error = %v", err)
	}

	// ChatMessage.ParentID is tagged `omitempty`: a nil ParentID (root message)
	// is explicitly OMITTED from the marshaled JSON rather than emitted as `null`.
	if _, present := asMap["parent_id"]; present {
		t.Errorf("expected \"parent_id\" to be omitted for a root message (nil ParentID), got present in %s", data)
	}
}

func TestChatMessage_NonNilParentIDIsPresent(t *testing.T) {
	parent := "parent-msg-1"
	msg := ChatMessage{
		ID:       "msg-2",
		TenantID: "ai8",
		ChatID:   "chat-1",
		ParentID: &parent,
		UserID:   "user-1",
		Role:     RoleAssistant,
		Content:  TextContent("reply"),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal(ChatMessage) error = %v", err)
	}

	var asMap map[string]interface{}
	if err := json.Unmarshal(data, &asMap); err != nil {
		t.Fatalf("json.Unmarshal into map error = %v", err)
	}

	got, present := asMap["parent_id"]
	if !present {
		t.Fatalf("expected \"parent_id\" to be present when ParentID is non-nil, got %s", data)
	}
	if got != parent {
		t.Errorf("parent_id = %v, want %q", got, parent)
	}
}

func TestNormalizeJSONB(t *testing.T) {
	tests := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{"nil defaults to empty object", nil, "{}"},
		{"empty slice defaults to empty object", json.RawMessage{}, "{}"},
		{"non-empty passes through unchanged", json.RawMessage(`{"temperature":0.7}`), `{"temperature":0.7}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeJSONB(tt.in)
			if got == nil {
				t.Fatal("NormalizeJSONB() returned nil; params/meta are NOT NULL")
			}
			if string(got) != tt.want {
				t.Errorf("NormalizeJSONB(%s) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}
