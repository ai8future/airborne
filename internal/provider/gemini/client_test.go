package gemini

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/genai"

	"github.com/ai8future/airborne/internal/provider"
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
			got := isRetryableError(tt.err)
			if got != tt.want {
				t.Fatalf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
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
	if err.Error() != "Gemini API key is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}
