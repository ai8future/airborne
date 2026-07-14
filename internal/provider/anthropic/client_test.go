package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestBuildMessagesTruncatesOldHistory(t *testing.T) {
	long := strings.Repeat("x", maxHistoryChars)
	messages := buildMessages("latest", []provider.Message{{Role: "user", Content: long}, {Role: "assistant", Content: "recent"}})
	if len(messages) != 3 || messages[0].Role != anthropic.MessageParamRoleUser || messages[1].Role != anthropic.MessageParamRoleAssistant || messages[2].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("messages = %#v", messages)
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

func TestExtractTextUnknownUnionIgnored(t *testing.T) {
	msg := &anthropic.Message{Content: []anthropic.ContentBlockUnion{{Type: "text", Text: " hello "}}}
	if got := extractText(msg); got != "" {
		t.Fatalf("extractText() = %q", got)
	}
}

func TestExtractContentThinkingAndText(t *testing.T) {
	resp := &anthropic.Message{Content: []anthropic.ContentBlockUnion{
		{Type: "thinking", Thinking: "reasoning"},
		{Type: "text", Text: " first "},
		{Type: "text", Text: "second"},
	}}
	text, thinking := extractContent(resp, true)
	if text != "first \nsecond" || thinking != "reasoning" {
		t.Fatalf("extractContent() = %q, %q", text, thinking)
	}
	_, thinking = extractContent(resp, false)
	if thinking != "" {
		t.Fatalf("thinking = %q", thinking)
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

func TestGenerateReplyOptionalRequestFields(t *testing.T) {
	temperature, topP, maxTokens := 0.4, 0.8, 123
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"temperature":0.4`, `"top_p":0.8`, `"max_tokens":123`, `"system":[{"text":"system"`} {
			if !strings.Contains(string(body), want) {
				t.Errorf("request %s missing %s", body, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	_, err := NewClient(WithDebugLogging(true)).GenerateReply(context.Background(), provider.GenerateParams{
		UserInput: "next", Instructions: "system", ConversationHistory: []provider.Message{{Role: "user", Content: "before"}},
		Config: provider.ProviderConfig{APIKey: "key", BaseURL: srv.URL, Temperature: &temperature, TopP: &topP, MaxOutputTokens: &maxTokens},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGenerateReplyRejectsInvalidBaseURL(t *testing.T) {
	params := provider.GenerateParams{Config: provider.ProviderConfig{APIKey: "key", BaseURL: "ftp://invalid"}}
	if _, err := NewClient().GenerateReply(context.Background(), params); err == nil {
		t.Fatal("expected invalid base URL error")
	}
	if _, err := NewClient().GenerateReplyStream(context.Background(), params); err == nil {
		t.Fatal("expected invalid stream base URL error")
	}
}

func TestGenerateReplyHTTPProtocolErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"client-error", `{"error":{"type":"invalid_request_error","message":"bad"}}`, http.StatusBadRequest},
		{"malformed-success", `not-json`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := NewClient().GenerateReply(context.Background(), provider.GenerateParams{Config: provider.ProviderConfig{APIKey: "key", BaseURL: srv.URL}})
			if err == nil {
				t.Fatal("expected protocol error")
			}
		})
	}
}

func TestGenerateReplyStream_SSEFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()
	ch, err := NewClient().GenerateReplyStream(context.Background(), provider.GenerateParams{UserInput: "hi", Config: provider.ProviderConfig{APIKey: "test-key", BaseURL: srv.URL}})
	if err != nil {
		t.Fatalf("GenerateReplyStream: %v", err)
	}
	var chunks []provider.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 || chunks[0].Text != "hello" || chunks[1].Type != provider.ChunkTypeComplete || chunks[1].Usage.TotalTokens != 5 {
		t.Fatalf("chunks = %#v", chunks)
	}
}
