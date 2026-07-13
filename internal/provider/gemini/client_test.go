package gemini

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/retry"
)

func TestBuildContents(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}

	contents := buildContents("  Next  ", history, nil)
	if len(contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(contents))
	}

	if contents[0].Role != "user" {
		t.Fatalf("expected role user, got %q", contents[0].Role)
	}
	if contents[1].Role != "model" {
		t.Fatalf("expected role model, got %q", contents[1].Role)
	}
	if contents[2].Role != "user" {
		t.Fatalf("expected role user for input, got %q", contents[2].Role)
	}
	if contents[2].Parts[0].Text != "Next" {
		t.Fatalf("expected trimmed input, got %q", contents[2].Parts[0].Text)
	}
}

func TestBuildContents_EmptyHistory(t *testing.T) {
	contents := buildContents("Hello", nil, nil)
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	if contents[0].Role != "user" {
		t.Fatalf("expected role user, got %q", contents[0].Role)
	}
}

func TestExtractText(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: &genai.Content{Parts: []*genai.Part{{Text: "Hello "}, {Text: "world"}}}},
			{Content: nil}, // nil content should be skipped
		},
	}

	got := extractText(resp)
	if got != "Hello world" {
		t.Fatalf("extractText() = %q, want %q", got, "Hello world")
	}
}

func TestExtractText_Nil(t *testing.T) {
	if extractText(nil) != "" {
		t.Fatal("extractText(nil) should be empty")
	}

	if extractText(&genai.GenerateContentResponse{}) != "" {
		t.Fatal("extractText(empty) should be empty")
	}

	if extractText(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{}}) != "" {
		t.Fatal("extractText(no candidates) should be empty")
	}
}

func TestExtractUsage(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 20,
			TotalTokenCount:      30,
		},
	}

	usage := extractUsage(resp)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.InputTokens != 10 {
		t.Fatalf("InputTokens = %d, want 10", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Fatalf("OutputTokens = %d, want 20", usage.OutputTokens)
	}
	if usage.TotalTokens != 30 {
		t.Fatalf("TotalTokens = %d, want 30", usage.TotalTokens)
	}
}

func TestExtractUsage_AllFields(t *testing.T) {
	// Test that all 5 Gemini token types are captured for accurate pricing:
	// - PromptTokenCount (standard input)
	// - CandidatesTokenCount (output)
	// - CachedContentTokenCount (cached input - 10% of input rate)
	// - ThoughtsTokenCount (thinking - charged at OUTPUT rate)
	// - ToolUsePromptTokenCount (tool use input - added to input)
	resp := &genai.GenerateContentResponse{
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:        1000,
			CandidatesTokenCount:    500,
			TotalTokenCount:         1750,
			CachedContentTokenCount: 200,
			ThoughtsTokenCount:      50,
			ToolUsePromptTokenCount: 100,
		},
	}

	usage := extractUsage(resp)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}

	// Verify basic fields
	if usage.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", usage.InputTokens)
	}
	if usage.OutputTokens != 500 {
		t.Errorf("OutputTokens = %d, want 500", usage.OutputTokens)
	}
	if usage.TotalTokens != 1750 {
		t.Errorf("TotalTokens = %d, want 1750", usage.TotalTokens)
	}

	// Verify Gemini-specific fields
	if usage.CachedTokens != 200 {
		t.Errorf("CachedTokens = %d, want 200", usage.CachedTokens)
	}
	if usage.ThinkingTokens != 50 {
		t.Errorf("ThinkingTokens = %d, want 50", usage.ThinkingTokens)
	}
	if usage.ToolUseTokens != 100 {
		t.Errorf("ToolUseTokens = %d, want 100", usage.ToolUseTokens)
	}
}

func TestExtractUsage_ZeroGeminiFields(t *testing.T) {
	// Ensure Gemini fields default to zero when not present in response
	resp := &genai.GenerateContentResponse{
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     100,
			CandidatesTokenCount: 50,
			TotalTokenCount:      150,
			// Gemini-specific fields not set (should default to 0)
		},
	}

	usage := extractUsage(resp)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}

	if usage.CachedTokens != 0 {
		t.Errorf("CachedTokens = %d, want 0", usage.CachedTokens)
	}
	if usage.ThinkingTokens != 0 {
		t.Errorf("ThinkingTokens = %d, want 0", usage.ThinkingTokens)
	}
	if usage.ToolUseTokens != 0 {
		t.Errorf("ToolUseTokens = %d, want 0", usage.ToolUseTokens)
	}
}

func TestExtractUsage_Nil(t *testing.T) {
	// extractUsage returns zero-value usage (not nil) to avoid nil pointer errors
	usage := extractUsage(nil)
	if usage == nil {
		t.Fatal("expected non-nil usage for nil response")
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0 {
		t.Fatal("expected zero values for nil response")
	}

	usage = extractUsage(&genai.GenerateContentResponse{})
	if usage == nil {
		t.Fatal("expected non-nil usage when metadata missing")
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0 {
		t.Fatal("expected zero values when metadata missing")
	}
}

func TestExtractCitations(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				GroundingMetadata: &genai.GroundingMetadata{
					GroundingChunks: []*genai.GroundingChunk{
						{Web: &genai.GroundingChunkWeb{URI: "https://example.com", Title: "Example"}},
						{Web: nil}, // nil web should be skipped
					},
				},
			},
		},
	}

	citations := extractCitations(resp, nil)
	if len(citations) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(citations))
	}
	if citations[0].Type != provider.CitationTypeURL {
		t.Fatalf("expected URL citation type")
	}
	if citations[0].URL != "https://example.com" {
		t.Fatalf("URL = %q, want https://example.com", citations[0].URL)
	}
	if citations[0].Title != "Example" {
		t.Fatalf("Title = %q, want Example", citations[0].Title)
	}
	if citations[0].Provider != "gemini" {
		t.Fatalf("Provider = %q, want gemini", citations[0].Provider)
	}
}

func TestExtractCitations_NoMetadata(t *testing.T) {
	if len(extractCitations(nil, nil)) != 0 {
		t.Fatal("expected empty citations for nil response")
	}

	if len(extractCitations(&genai.GenerateContentResponse{}, nil)) != 0 {
		t.Fatal("expected empty citations for empty response")
	}

	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{GroundingMetadata: nil}},
	}
	if len(extractCitations(resp, nil)) != 0 {
		t.Fatal("expected empty citations for nil metadata")
	}
}

func TestBuildSafetySettings(t *testing.T) {
	tests := []struct {
		threshold string
		expected  genai.HarmBlockThreshold
	}{
		{"BLOCK_NONE", genai.HarmBlockThresholdBlockNone},
		{"LOW_AND_ABOVE", genai.HarmBlockThresholdBlockLowAndAbove},
		{"MEDIUM_AND_ABOVE", genai.HarmBlockThresholdBlockMediumAndAbove},
		{"ONLY_HIGH", genai.HarmBlockThresholdBlockOnlyHigh},
		{"unknown", genai.HarmBlockThresholdBlockMediumAndAbove},
		{"", genai.HarmBlockThresholdBlockMediumAndAbove},
	}

	for _, tt := range tests {
		t.Run(tt.threshold, func(t *testing.T) {
			settings := buildSafetySettings(tt.threshold)
			if len(settings) != 4 {
				t.Fatalf("expected 4 settings, got %d", len(settings))
			}
			for _, setting := range settings {
				if setting.Threshold != tt.expected {
					t.Fatalf("Threshold = %v, want %v", setting.Threshold, tt.expected)
				}
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
		{"nil", nil, false},
		{"429 rate limit", errors.New("429 too many requests"), true},
		{"500 server error", errors.New("500 internal server error"), true},
		{"503 unavailable", errors.New("503 service unavailable"), true},
		{"resource exhausted", errors.New("resource exhausted"), true},
		{"overloaded", errors.New("service overloaded"), true},
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
	if got := client.Name(); got != "gemini" {
		t.Errorf("Name() = %q, want %q", got, "gemini")
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
	if client.SupportsNativeContinuity() {
		t.Error("SupportsNativeContinuity() should be false")
	}
	// SupportsStreaming returns true - Gemini supports streaming via GenerateContentStream
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
	if err.Error() != "Gemini API key is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}
func TestGeminiConversionBoundaries(t *testing.T) {
	if parseThinkingLevel("high") == parseThinkingLevel("nonsense") {
		t.Fatal("thinking levels")
	}
	s := convertToSchema(map[string]interface{}{"type": "string", "enum": []interface{}{"a"}})
	if s == nil || len(s.Enum) != 1 {
		t.Fatal("schema")
	}
	d := buildFunctionDeclaration(provider.Tool{Name: "f", ParametersSchema: `{"type":"object"}`})
	if d.Name != "f" {
		t.Fatal("function")
	}
	if len(extractFunctionCalls(nil)) != 0 || len(extractCodeExecutionResults(nil)) != 0 {
		t.Fatal("nil")
	}
}
func TestGeminiPureResponseConversions(t *testing.T) {
	if positiveInt32OrDefault(nil, 7) != 7 || positiveInt32OrDefault(intPtr(0), 7) != 7 || positiveInt32OrDefault(intPtr(2), 7) != 2 {
		t.Fatal("positive defaults")
	}
	if schema := structuredOutputSchema(); schema == nil || schema.Properties["reply"] == nil || len(schema.Required) != 2 {
		t.Fatal("structured schema")
	}
	for _, reason := range []genai.FinishReason{genai.FinishReasonSafety, genai.FinishReasonRecitation, genai.FinishReasonBlocklist, genai.FinishReasonProhibitedContent, genai.FinishReasonSPII} {
		if getBlockReason(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: reason}}}) == "" {
			t.Fatalf("missing reason for %v", reason)
		}
	}

	parts := []*genai.Part{
		{Text: `{"reply":"done","intent":"request","requires_user_action":true,"entities":[{"name":"Ada","type":"person"}],"topics":["code"],"scheduling_intent":{"detected":true,"datetime_mentioned":"tomorrow"}}`},
		{FunctionCall: &genai.FunctionCall{ID: "call-1", Name: "lookup", Args: map[string]any{"q": "gemini"}}},
		{ExecutableCode: &genai.ExecutableCode{Code: "print(1)", Language: genai.LanguagePython}},
		{CodeExecutionResult: &genai.CodeExecutionResult{Outcome: genai.OutcomeOK, Output: "1\n"}},
	}
	response := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: parts}}}}
	text, metadata := extractStructuredResponse(response)
	if text != "done" || metadata == nil || metadata.Intent != "request" || len(metadata.Entities) != 1 || metadata.Scheduling == nil {
		t.Fatalf("structured conversion = %#v %#v", text, metadata)
	}
	if calls := extractFunctionCalls(response); len(calls) != 1 || calls[0].Name != "lookup" || calls[0].Arguments != `{"q":"gemini"}` {
		t.Fatalf("function calls = %#v", calls)
	}
	if executions := extractCodeExecutionResults(response); len(executions) != 1 || executions[0].ExitCode != 0 || executions[0].Stdout != "1\n" {
		t.Fatalf("executions = %#v", executions)
	}
	bad := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "not-json"}}}}}}
	if raw, metadata := extractStructuredResponse(bad); raw != "not-json" || metadata != nil {
		t.Fatalf("malformed structured response = %q %#v", raw, metadata)
	}
}

func intPtr(v int) *int { return &v }

func TestGenerateReplyHTTPProtocol(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"temperature"`) || !strings.Contains(string(body), `"functionDeclarations"`) {
			t.Errorf("request body missing config: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"fixture reply"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`)
	}))
	defer srv.Close()
	temp := 0.25
	result, err := NewClient().GenerateReply(context.Background(), provider.GenerateParams{
		UserInput: "hello", EnableCodeExecution: true, EnableWebSearch: true,
		Tools:  []provider.Tool{{Name: "lookup", Description: "lookup", ParametersSchema: `{"type":"object"}`}},
		Config: provider.ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.5-pro", Temperature: &temp, ExtraOptions: map[string]string{"thinking_level": "LOW", "safety_threshold": "BLOCK_NONE"}},
	})
	if err != nil {
		t.Fatalf("GenerateReply() error = %v", err)
	}
	if result.Text != "fixture reply" || result.Usage.TotalTokens != 5 || result.Model != "gemini-2.5-pro" || requests != 1 {
		t.Fatalf("result = %#v requests=%d", result, requests)
	}
}

func TestGenerateReplyHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad fixture", http.StatusBadRequest) }))
	defer srv.Close()
	_, err := NewClient().GenerateReply(context.Background(), provider.GenerateParams{UserInput: "hello", Config: provider.ProviderConfig{APIKey: "key", BaseURL: srv.URL}})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("error = %v", err)
	}
}
