package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/retry"
)

func TestBuildMessages_NormalHistory(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}

	messages := buildMessages("  Next  ", history)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	if messages[0].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("expected first message role user, got %s", messages[0].Role)
	}
	if messages[1].Role != anthropic.MessageParamRoleAssistant {
		t.Fatalf("expected second message role assistant, got %s", messages[1].Role)
	}
	if messages[2].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("expected third message role user, got %s", messages[2].Role)
	}
}

func TestBuildMessages_PrependsUserWhenAssistantFirst(t *testing.T) {
	history := []provider.Message{
		{Role: "assistant", Content: "Hi"},
	}

	messages := buildMessages("  How are you?  ", history)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (placeholder + history + input), got %d", len(messages))
	}

	if messages[0].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("expected first message role user, got %s", messages[0].Role)
	}

	if messages[1].Role != anthropic.MessageParamRoleAssistant {
		t.Fatalf("expected second message role assistant, got %s", messages[1].Role)
	}

	if messages[2].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("expected final message role user, got %s", messages[2].Role)
	}
}

func TestBuildMessages_EmptyHistory(t *testing.T) {
	messages := buildMessages("Hello", nil)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("expected user role, got %s", messages[0].Role)
	}
}

func TestExtractText_Nil(t *testing.T) {
	if got := extractText(nil); got != "" {
		t.Fatalf("extractText(nil) = %q, want empty", got)
	}
}

func TestExtractText_EmptyContent(t *testing.T) {
	msg := &anthropic.Message{Content: []anthropic.ContentBlockUnion{}}
	if got := extractText(msg); got != "" {
		t.Fatalf("extractText(empty) = %q, want empty", got)
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"429 rate limit", errors.New("429 too many requests"), true},
		{"500 server error", errors.New("500 internal server error"), true},
		{"502 bad gateway", errors.New("502 bad gateway"), true},
		{"503 unavailable", errors.New("503 service unavailable"), true},
		{"529 overloaded", errors.New("529 overloaded"), true},
		{"overloaded message", errors.New("service is overloaded"), true},
		{"rate limit message", errors.New("rate limit exceeded"), true},
		{"bad request", errors.New("bad request"), false},
		{"invalid json", errors.New("invalid json"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retry.IsRetryable(tt.err)
			if got != tt.want {
				t.Fatalf("retry.IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
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
	if got := client.Name(); got != "anthropic" {
		t.Errorf("Name() = %q, want %q", got, "anthropic")
	}
}

func TestClientCapabilities(t *testing.T) {
	client := NewClient()

	if client.SupportsFileSearch() {
		t.Error("SupportsFileSearch() should be false")
	}
	if client.SupportsWebSearch() {
		t.Error("SupportsWebSearch() should be false")
	}
	if client.SupportsNativeContinuity() {
		t.Error("SupportsNativeContinuity() should be false")
	}
	if !client.SupportsStreaming() {
		t.Error("SupportsStreaming() should be true")
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
	if err.Error() != "Anthropic API key is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateReplyStream_MissingAPIKey(t *testing.T) {
	client := NewClient()
	_, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{
		Config: provider.ProviderConfig{APIKey: ""},
	})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if err.Error() != "Anthropic API key is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateReply_HTTPSuccessFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing API key header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer srv.Close()
	client := NewClient()
	resp, err := client.GenerateReply(context.Background(), provider.GenerateParams{UserInput: "hi", Instructions: "be brief", Config: provider.ProviderConfig{APIKey: "test-key", BaseURL: srv.URL}})
	if err != nil {
		t.Fatalf("GenerateReply: %v", err)
	}
	if resp.Text != "hello" || resp.ResponseID != "msg_1" || resp.Usage.TotalTokens != 5 {
		t.Fatalf("response = %#v", resp)
	}
}
