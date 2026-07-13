package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"

	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/retry"
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
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"status 429", errors.New("rate limit: status 429"), true},
		{"status 500", errors.New("server error: status 500"), true},
		{"status 502", errors.New("bad gateway: status 502"), true},
		{"status 503", errors.New("service unavailable: status 503"), true},
		{"status 504", errors.New("gateway timeout: status 504"), true},
		{"status 400", errors.New("bad request: status 400"), false},
		{"status 401", errors.New("unauthorized: status 401"), false},
		{"status 403", errors.New("forbidden: status 403"), false},
		{"status 422", errors.New("unprocessable: status 422"), false},
		{"connection failure", errors.New("connection failed"), true},
		{"timeout", errors.New("request timeout exceeded"), true},
		{"temporary failure", errors.New("temporary network issue"), true},
		{"bad request text", errors.New("bad request"), false},
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
	// SupportsStreaming returns true - OpenAI supports streaming via Responses API
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

func TestStripCitationMarkers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no markers", "Hello world", "Hello world"},
		{"single marker", "Hello fileciteturn2file0 world", "Hello  world"},
		{"multiple markers", "fileciteturn1file0 Hello fileciteturn2file1 world", " Hello  world"},
		{"complex marker", "Test fileciteturn2file0turn2file1turn3file2 end", "Test  end"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCitationMarkers(tt.input)
			if got != tt.want {
				t.Fatalf("stripCitationMarkers(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSupportsPromptCacheRetention(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"gpt-5.0", true},
		{"gpt-5.1-turbo", true},
		{"gpt-4o", false},
		{"gpt-4-turbo", false},
		{"o1-preview", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := supportsPromptCacheRetention(tt.model)
			if got != tt.want {
				t.Fatalf("supportsPromptCacheRetention(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestNewClientWithDebugLogging(t *testing.T) {
	client := NewClient(WithDebugLogging(true))
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if !client.debug {
		t.Error("expected debug to be true")
	}

	client2 := NewClient(WithDebugLogging(false))
	if client2.debug {
		t.Error("expected debug to be false")
	}
}

func TestGenerateReplyInvalidBaseURL(t *testing.T) {
	_, err := NewClient().GenerateReply(context.Background(), provider.GenerateParams{Config: provider.ProviderConfig{APIKey: "key", BaseURL: "ftp://invalid"}})
	if err == nil {
		t.Fatal("invalid base URL must fail before network")
	}
	_, err = NewClient().GenerateReplyStream(context.Background(), provider.GenerateParams{Config: provider.ProviderConfig{APIKey: "key", BaseURL: "ftp://invalid"}})
	if err == nil {
		t.Fatal("invalid stream base URL must fail before network")
	}
}

func TestGenerateReplyHTTPTSuccess(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_1","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"fixture reply"}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`)
	}))
	defer s.Close()
	got, err := NewClient().GenerateReply(context.Background(), provider.GenerateParams{UserInput: "hi", Config: provider.ProviderConfig{APIKey: "test", BaseURL: s.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "fixture reply" {
		t.Fatalf("%+v", got)
	}
}
func TestGenerateReplyHTTPError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad", http.StatusBadRequest) }))
	defer s.Close()
	_, err := NewClient().GenerateReply(context.Background(), provider.GenerateParams{Config: provider.ProviderConfig{APIKey: "x", BaseURL: s.URL}})
	if err == nil {
		t.Fatal("error expected")
	}
}
func TestBuildFunctionToolSchemas(t *testing.T) {
	for _, x := range []provider.Tool{{Name: "x"}, {Name: "x", ParametersSchema: "bad"}, {Name: "x", ParametersSchema: `{"type":"object"}`, Strict: true}} {
		got := buildFunctionTool(x)
		if got.OfFunction == nil || got.OfFunction.Name != "x" {
			t.Fatal("tool")
		}
	}
}
func TestFileStoreValidationFailures(t *testing.T) {
	ctx := context.Background()
	for _, fn := range []func() error{func() error { _, e := CreateVectorStore(ctx, FileStoreConfig{}, "x"); return e }, func() error { return DeleteVectorStore(ctx, FileStoreConfig{}, "x") }, func() error { _, e := GetVectorStore(ctx, FileStoreConfig{}, "x"); return e }, func() error { _, e := ListVectorStores(ctx, FileStoreConfig{}, 1); return e }, func() error {
		_, e := UploadFileToVectorStore(ctx, FileStoreConfig{}, "s", "f", strings.NewReader("x"))
		return e
	}} {
		if fn() == nil {
			t.Fatal("expected validation error")
		}
	}
}
func TestVectorStoreHTTPLifecycle(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /vector_stores", "GET /vector_stores/vs", "DELETE /vector_stores/vs":
			io.WriteString(w, `{"id":"vs","name":"fixture","status":"completed","created_at":0,"file_counts":{"total":1}}`)
		case "GET /vector_stores":
			io.WriteString(w, `{"data":[{"id":"vs","name":"fixture","status":"completed","created_at":0,"file_counts":{"total":1}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	cfg := FileStoreConfig{APIKey: "x", BaseURL: s.URL}
	if _, e := CreateVectorStore(context.Background(), cfg, "fixture"); e != nil {
		t.Fatal(e)
	}
	if _, e := GetVectorStore(context.Background(), cfg, "vs"); e != nil {
		t.Fatal(e)
	}
	if _, e := ListVectorStores(context.Background(), cfg, 1); e != nil {
		t.Fatal(e)
	}
	if e := DeleteVectorStore(context.Background(), cfg, "vs"); e != nil {
		t.Fatal(e)
	}
}

func TestGenerateReplyStreamResponsesSSEFixture(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream\"}}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_stream\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n")
	}))
	defer s.Close()
	ch, err := NewClient().GenerateReplyStream(context.Background(), provider.GenerateParams{UserInput: "hi", Config: provider.ProviderConfig{APIKey: "test", BaseURL: s.URL}})
	if err != nil {
		t.Fatal(err)
	}
	var got []provider.StreamChunk
	for chunk := range ch {
		got = append(got, chunk)
	}
	if len(got) != 2 || got[0].Text != "hello" || got[1].Type != provider.ChunkTypeComplete || got[1].Usage.TotalTokens != 5 {
		t.Fatalf("chunks = %#v", got)
	}
}

func TestWaitForCompletionNilAndCanceled(t *testing.T) {
	if _, err := waitForCompletion(context.Background(), openai.Client{}, nil); err == nil {
		t.Fatal("nil response must fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitForCompletion(ctx, openai.Client{}, &responses.Response{ID: "resp_1", Status: responses.ResponseStatusInProgress}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait error = %v", err)
	}
}
