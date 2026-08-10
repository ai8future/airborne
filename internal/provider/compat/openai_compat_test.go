package compat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go"

	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/retry"
)

func TestNewClient(t *testing.T) {
	config := ProviderConfig{
		Name:               "test-provider",
		DefaultBaseURL:     "https://api.test.com/v1",
		DefaultModel:       "test-model",
		SupportsFileSearch: false,
		SupportsWebSearch:  true,
		SupportsStreaming:  true,
		APIKeyEnvVar:       "TEST_API_KEY",
	}

	client := NewClient(config)

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.config.Name != "test-provider" {
		t.Errorf("Name = %q, want %q", client.config.Name, "test-provider")
	}
	if client.debug {
		t.Error("debug should be false by default")
	}
}

func TestNewClientWithDebugLogging(t *testing.T) {
	config := ProviderConfig{Name: "test"}

	client := NewClient(config, WithDebugLogging(true))
	if !client.debug {
		t.Error("expected debug to be true")
	}

	client2 := NewClient(config, WithDebugLogging(false))
	if client2.debug {
		t.Error("expected debug to be false")
	}

	// Test nil option is handled
	client3 := NewClient(config, nil)
	if client3 == nil {
		t.Error("NewClient with nil option should not return nil")
	}
}

func TestClientName(t *testing.T) {
	config := ProviderConfig{Name: "test-provider"}
	client := NewClient(config)

	if got := client.Name(); got != "test-provider" {
		t.Errorf("Name() = %q, want %q", got, "test-provider")
	}
}

func TestClientCapabilities(t *testing.T) {
	tests := []struct {
		name           string
		config         ProviderConfig
		wantFileSearch bool
		wantWebSearch  bool
		wantContinuity bool
		wantStreaming  bool
	}{
		{
			name: "all capabilities enabled",
			config: ProviderConfig{
				SupportsFileSearch: true,
				SupportsWebSearch:  true,
				SupportsStreaming:  true,
			},
			wantFileSearch: true,
			wantWebSearch:  true,
			wantContinuity: false, // Always false for compat clients
			wantStreaming:  true,
		},
		{
			name: "all capabilities disabled",
			config: ProviderConfig{
				SupportsFileSearch: false,
				SupportsWebSearch:  false,
				SupportsStreaming:  false,
			},
			wantFileSearch: false,
			wantWebSearch:  false,
			wantContinuity: false,
			wantStreaming:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)

			if got := client.SupportsFileSearch(); got != tt.wantFileSearch {
				t.Errorf("SupportsFileSearch() = %v, want %v", got, tt.wantFileSearch)
			}
			if got := client.SupportsWebSearch(); got != tt.wantWebSearch {
				t.Errorf("SupportsWebSearch() = %v, want %v", got, tt.wantWebSearch)
			}
			if got := client.SupportsNativeContinuity(); got != tt.wantContinuity {
				t.Errorf("SupportsNativeContinuity() = %v, want %v", got, tt.wantContinuity)
			}
			if got := client.SupportsStreaming(); got != tt.wantStreaming {
				t.Errorf("SupportsStreaming() = %v, want %v", got, tt.wantStreaming)
			}
		})
	}
}

func TestBuildMessages(t *testing.T) {
	tests := []struct {
		name         string
		instructions string
		userInput    string
		history      []provider.Message
		wantLen      int
	}{
		{
			name:         "no instructions no history",
			instructions: "",
			userInput:    "hello",
			history:      nil,
			wantLen:      1, // Just user message
		},
		{
			name:         "with instructions",
			instructions: "You are helpful",
			userInput:    "hello",
			history:      nil,
			wantLen:      2, // System + user
		},
		{
			name:         "with history",
			instructions: "You are helpful",
			userInput:    "hello",
			history: []provider.Message{
				{Role: "user", Content: "Hi"},
				{Role: "assistant", Content: "Hello!"},
			},
			wantLen: 4, // System + 2 history + user
		},
		{
			name:         "empty history content skipped",
			instructions: "",
			userInput:    "hello",
			history: []provider.Message{
				{Role: "user", Content: ""},
				{Role: "assistant", Content: "   "},
			},
			wantLen: 1, // Empty messages skipped, only user input
		},
		{
			name:         "whitespace trimmed",
			instructions: "",
			userInput:    "  hello  ",
			history: []provider.Message{
				{Role: "user", Content: "  hi  "},
			},
			wantLen: 2, // history + user (both trimmed but non-empty)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := buildMessages(tt.instructions, tt.userInput, tt.history)
			if got := len(messages); got != tt.wantLen {
				t.Errorf("buildMessages() returned %d messages, want %d", got, tt.wantLen)
			}
		})
	}
}

func TestExtractText(t *testing.T) {
	tests := []struct {
		name string
		resp *openai.ChatCompletion
		want string
	}{
		{
			name: "nil response",
			resp: nil,
			want: "",
		},
		{
			name: "empty choices",
			resp: &openai.ChatCompletion{},
			want: "",
		},
		{
			name: "valid response",
			resp: &openai.ChatCompletion{
				Choices: []openai.ChatCompletionChoice{
					{Message: openai.ChatCompletionMessage{Content: "Hello"}},
				},
			},
			want: "Hello",
		},
		{
			name: "whitespace trimmed",
			resp: &openai.ChatCompletion{
				Choices: []openai.ChatCompletionChoice{
					{Message: openai.ChatCompletionMessage{Content: "  Hello world  "}},
				},
			},
			want: "Hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractText(tt.resp); got != tt.want {
				t.Errorf("extractText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractUsage(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		usage := extractUsage(nil)
		if usage == nil {
			t.Fatal("expected non-nil usage")
		}
		if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0 {
			t.Errorf("expected zero usage, got %+v", usage)
		}
	})

	t.Run("valid response", func(t *testing.T) {
		resp := &openai.ChatCompletion{
			Usage: openai.CompletionUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
		}
		usage := extractUsage(resp)
		if usage.InputTokens != 100 {
			t.Errorf("InputTokens = %d, want %d", usage.InputTokens, 100)
		}
		if usage.OutputTokens != 50 {
			t.Errorf("OutputTokens = %d, want %d", usage.OutputTokens, 50)
		}
		if usage.TotalTokens != 150 {
			t.Errorf("TotalTokens = %d, want %d", usage.TotalTokens, 150)
		}
	})
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Non-retryable cases
		{"nil error", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"auth error 401", errors.New("401 unauthorized"), false},
		{"auth error 403", errors.New("403 forbidden"), false},
		{"invalid api key", errors.New("invalid_api_key error"), false},
		{"authentication failed", errors.New("authentication failed"), false},
		{"bad request 400", errors.New("400 bad request"), false},
		{"invalid request", errors.New("invalid_request error"), false},
		{"malformed request", errors.New("malformed json"), false},
		{"validation error", errors.New("validation failed"), false},

		// Retryable cases
		{"rate limit 429", errors.New("status code 429"), true},
		{"rate limit text", errors.New("rate limit exceeded"), true},
		{"server error 500", errors.New("500 internal server error"), true},
		{"server error 502", errors.New("502 bad gateway"), true},
		{"server error 503", errors.New("503 service unavailable"), true},
		{"server error 504", errors.New("504 gateway timeout"), true},
		{"connection error", errors.New("connection refused"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"timeout error", errors.New("request timeout"), true},
		{"temporary failure", errors.New("temporary network issue"), true},
		{"eof error", errors.New("unexpected EOF"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retry.IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("retry.IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestGenerateReply_MissingAPIKey(t *testing.T) {
	client := NewClient(ProviderConfig{
		Name:           "test",
		DefaultBaseURL: "https://api.test.com",
		DefaultModel:   "test-model",
	})

	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{
		Config: provider.ProviderConfig{APIKey: ""},
	})

	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if got := err.Error(); got != "test API key is required" {
		t.Errorf("error = %q, want %q", got, "test API key is required")
	}
}

func TestGenerateReply_WhitespaceAPIKey(t *testing.T) {
	client := NewClient(ProviderConfig{
		Name:           "test",
		DefaultBaseURL: "https://api.test.com",
		DefaultModel:   "test-model",
	})

	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{
		Config: provider.ProviderConfig{APIKey: "   "},
	})

	if err == nil {
		t.Fatal("expected error for whitespace-only API key")
	}
}

func TestGenerateReply_InvalidBaseURL(t *testing.T) {
	client := NewClient(ProviderConfig{
		Name:           "test",
		DefaultBaseURL: "https://api.test.com",
		DefaultModel:   "test-model",
	})

	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{
		Config: provider.ProviderConfig{
			APIKey:  "test-key",
			BaseURL: "javascript:alert(1)",
		},
	})

	if err == nil {
		t.Fatal("expected error for invalid base URL")
	}
}

func TestGenerateReplyStream_MissingAPIKey(t *testing.T) {
	client := NewClient(ProviderConfig{
		Name:           "test",
		DefaultBaseURL: "https://api.test.com",
		DefaultModel:   "test-model",
	})

	ch, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{
		Config: provider.ProviderConfig{APIKey: ""},
	})

	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if ch != nil {
		t.Error("expected nil channel on error")
	}
}

func TestGenerateReplyStream_InvalidBaseURL(t *testing.T) {
	client := NewClient(ProviderConfig{
		Name:           "test",
		DefaultBaseURL: "https://api.test.com",
		DefaultModel:   "test-model",
	})

	ch, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{
		Config: provider.ProviderConfig{
			APIKey:  "test-key",
			BaseURL: "ftp://invalid",
		},
	})

	if err == nil {
		t.Fatal("expected error for invalid base URL")
	}
	if ch != nil {
		t.Error("expected nil channel on error")
	}
}

func TestCompatGenerateReplyAndStreamFixtures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"compat-model","choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
	}))
	defer srv.Close()
	client := NewClient(ProviderConfig{Name: "compat", DefaultBaseURL: srv.URL, DefaultModel: "compat-model", SupportsStreaming: true})
	params := provider.GenerateParams{UserInput: "hi", Config: provider.ProviderConfig{APIKey: "test-key"}}
	resp, err := client.GenerateReply(context.Background(), params)
	if err != nil || resp.Text != "hello" || resp.Usage.TotalTokens != 5 {
		t.Fatalf("reply = %#v, %v", resp, err)
	}
	ch, err := client.GenerateReplyStream(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	var chunks []provider.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}
	if len(chunks) != 1 || chunks[0].Type != provider.ChunkTypeComplete {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestCompatGenerateReplyOptionalFields(t *testing.T) {
	temperature, topP, maxTokens := 0.2, 0.9, 64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"temperature":0.2`, `"top_p":0.9`, `"max_tokens":64`} {
			if !strings.Contains(string(body), want) {
				t.Errorf("request %s missing %s", body, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"compat-model","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	client := NewClient(ProviderConfig{Name: "compat", DefaultBaseURL: srv.URL, DefaultModel: "compat-model"}, WithDebugLogging(true))
	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{UserInput: "hi", Config: provider.ProviderConfig{APIKey: "key", Temperature: &temperature, TopP: &topP, MaxOutputTokens: &maxTokens}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCompatStreamOptionalFields(t *testing.T) {
	temperature, topP, maxTokens := 0.3, 0.7, 32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		for _, want := range []string{`"temperature":0.3`, `"top_p":0.7`, `"max_tokens":32`} {
			if !strings.Contains(string(body), want) {
				t.Errorf("request missing %s: %s", want, body)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	client := NewClient(ProviderConfig{Name: "compat", DefaultBaseURL: srv.URL, DefaultModel: "model", SupportsStreaming: true})
	ch, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{Config: provider.ProviderConfig{APIKey: "key", Temperature: &temperature, TopP: &topP, MaxOutputTokens: &maxTokens}})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
}
