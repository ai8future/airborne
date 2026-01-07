package openai

import (
	"context"
	"errors"
	"testing"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"

	"github.com/cliffpyles/aibox/internal/provider"
)

func TestBuildUserPrompt_NoHistory(t *testing.T) {
	got := buildUserPrompt("  hello  ", nil)
	if got != "hello" {
		t.Fatalf("buildUserPrompt() = %q, want %q", got, "hello")
	}
}

func TestBuildUserPrompt_WithHistory(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}

	got := buildUserPrompt("  How are you?  ", history)
	want := "Previous conversation:\n\nUser: Hello\n\nAssistant: Hi there\n\n---\n\nNew message:\n\nHow are you?"
	if got != want {
		t.Fatalf("buildUserPrompt() = %q, want %q", got, want)
	}
}

func TestMapReasoningEffort(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  shared.ReasoningEffort
	}{
		{"none", "none", shared.ReasoningEffort("none")},
		{"low", "LOW", shared.ReasoningEffortLow},
		{"medium", "Medium", shared.ReasoningEffortMedium},
		{"high", "high", shared.ReasoningEffortHigh},
		{"default", "unknown", shared.ReasoningEffortHigh},
		{"empty", "", shared.ReasoningEffortHigh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapReasoningEffort(tt.input)
			if got != tt.want {
				t.Fatalf("mapReasoningEffort(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMapServiceTier(t *testing.T) {
	tests := []struct {
		input string
		want  responses.ResponseNewParamsServiceTier
	}{
		{"default", responses.ResponseNewParamsServiceTierDefault},
		{"flex", responses.ResponseNewParamsServiceTierFlex},
		{"priority", responses.ResponseNewParamsServiceTierPriority},
		{"unknown", responses.ResponseNewParamsServiceTierAuto},
		{"", responses.ResponseNewParamsServiceTierAuto},
		{"DEFAULT", responses.ResponseNewParamsServiceTierDefault},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapServiceTier(tt.input)
			if got != tt.want {
				t.Fatalf("mapServiceTier(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		want    bool
	}{
		{"nil error", nil, false},
		{"status 429", &openai.Error{StatusCode: 429}, true},
		{"status 500", &openai.Error{StatusCode: 500}, true},
		{"status 502", &openai.Error{StatusCode: 502}, true},
		{"status 503", &openai.Error{StatusCode: 503}, true},
		{"status 504", &openai.Error{StatusCode: 504}, true},
		{"status 400", &openai.Error{StatusCode: 400}, false},
		{"status 401", &openai.Error{StatusCode: 401}, false},
		{"status 403", &openai.Error{StatusCode: 403}, false},
		{"status 404", &openai.Error{StatusCode: 404}, false},
		{"status 422", &openai.Error{StatusCode: 422}, false},
		{"connection failure", errors.New("connection failed"), true},
		{"timeout", errors.New("request timeout exceeded"), true},
		{"temporary failure", errors.New("temporary network issue"), true},
		{"bad request", errors.New("bad request"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			if got != tt.want {
				t.Fatalf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestWaitForCompletion_NilResponse(t *testing.T) {
	_, err := waitForCompletion(context.Background(), openai.Client{}, nil)
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestWaitForCompletion_CompletedOrNoID(t *testing.T) {
	// Already completed
	resp := &responses.Response{ID: "resp_1", Status: responses.ResponseStatusCompleted}
	got, err := waitForCompletion(context.Background(), openai.Client{}, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != resp {
		t.Fatalf("expected same response pointer")
	}

	// No ID (immediate return)
	respNoID := &responses.Response{}
	got, err = waitForCompletion(context.Background(), openai.Client{}, respNoID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != respNoID {
		t.Fatalf("expected same response pointer for empty ID")
	}
}

func TestNewClient(t *testing.T) {
	t.Run("creates client", func(t *testing.T) {
		client := NewClient()
		if client == nil {
			t.Fatal("NewClient() returned nil")
		}
	})
}

func TestClientName(t *testing.T) {
	client := NewClient()
	if got := client.Name(); got != "openai" {
		t.Errorf("Name() = %q, want %q", got, "openai")
	}
}

func TestClientCapabilities(t *testing.T) {
	client := NewClient()

	if !client.SupportsFileSearch() {
		t.Error("SupportsFileSearch() should be true")
	}
	if !client.SupportsWebSearch() {
		t.Error("SupportsWebSearch() should be true")
	}
	if !client.SupportsNativeContinuity() {
		t.Error("SupportsNativeContinuity() should be true")
	}
	// SupportsStreaming returns false until true streaming is implemented
	if client.SupportsStreaming() {
		t.Error("SupportsStreaming() should be false (falls back to non-streaming)")
	}
}

func TestGenerateReply_MissingAPIKey(t *testing.T) {
	client := NewClient()
	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{
		Config: provider.ProviderConfig{APIKey: ""},
	})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if err.Error() != "OpenAI API key is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractCitations_EmptyResponse(t *testing.T) {
	citations := extractCitations(nil, nil)
	if len(citations) != 0 {
		t.Fatalf("expected empty citations for nil response, got %d", len(citations))
	}

	citations = extractCitations(&responses.Response{}, nil)
	if len(citations) != 0 {
		t.Fatalf("expected empty citations for empty response, got %d", len(citations))
	}
}
