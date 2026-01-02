// Package gemini provides the Google Gemini LLM provider implementation.
package gemini

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/cliffpyles/aibox/internal/provider"
)

const (
	maxAttempts    = 3
	requestTimeout = 3 * time.Minute
	backoffBase    = 250 * time.Millisecond
)

// Client implements the provider.Provider interface using Google's Gemini API.
type Client struct {
	debug bool
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {
		c.debug = enabled
	}
}

// NewClient creates a new Gemini provider client.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// Name returns the provider identifier.
func (c *Client) Name() string {
	return "gemini"
}

// SupportsFileSearch returns true as Gemini supports FileSearchStore.
func (c *Client) SupportsFileSearch() bool {
	return true
}

// SupportsWebSearch returns true as Gemini supports Google Search grounding.
func (c *Client) SupportsWebSearch() bool {
	return true
}

// SupportsNativeContinuity returns false as Gemini requires full conversation history.
func (c *Client) SupportsNativeContinuity() bool {
	return false
}

// SupportsStreaming returns true as Gemini supports streaming.
func (c *Client) SupportsStreaming() bool {
	return true
}

// GenerateReply implements provider.Provider using Google's Gemini API.
func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		return provider.GenerateResult{}, errors.New("Gemini API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Create Gemini client
	clientConfig := &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	}
	if cfg.BaseURL != "" {
		clientConfig.HTTPOptions = genai.HTTPOptions{
			BaseURL: cfg.BaseURL,
		}
	}

	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return provider.GenerateResult{}, fmt.Errorf("creating gemini client: %w", err)
	}

	// Build conversation content
	contents := buildContents(params.UserInput, params.ConversationHistory)

	// Build generation config
	generateConfig := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(params.Instructions)},
		},
	}

	// Apply optional parameters
	if cfg.Temperature != nil {
		temp := float32(*cfg.Temperature)
		generateConfig.Temperature = &temp
	}
	if cfg.TopP != nil {
		topP := float32(*cfg.TopP)
		generateConfig.TopP = &topP
	}
	if cfg.MaxOutputTokens != nil {
		generateConfig.MaxOutputTokens = int32(*cfg.MaxOutputTokens)
	}

	// Configure safety settings
	if threshold := cfg.ExtraOptions["safety_threshold"]; threshold != "" {
		generateConfig.SafetySettings = buildSafetySettings(threshold)
	}

	// Configure tools - Google Search grounding (file search requires different API)
	if params.EnableWebSearch && !params.EnableFileSearch {
		generateConfig.Tools = []*genai.Tool{{
			GoogleSearch: &genai.GoogleSearch{},
		}}
	}

	// Execute with retry
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		slog.Info("gemini request",
			"attempt", attempt,
			"model", model,
			"request_id", params.RequestID,
		)

		reqCtx, reqCancel := context.WithTimeout(ctx, requestTimeout)
		resp, err := client.Models.GenerateContent(reqCtx, model, contents, generateConfig)
		reqCancel()

		if err != nil {
			// Check if parent context is still valid
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				lastErr = fmt.Errorf("gemini request timeout: %w", err)
				slog.Warn("gemini timeout, retrying", "attempt", attempt)
				if attempt < maxAttempts {
					sleepWithBackoff(ctx, attempt)
					continue
				}
				return provider.GenerateResult{}, lastErr
			}

			lastErr = fmt.Errorf("gemini error: %w", err)
			if !isRetryableError(err) {
				return provider.GenerateResult{}, lastErr
			}

			slog.Warn("gemini retryable error", "attempt", attempt, "error", err)
			if attempt < maxAttempts {
				sleepWithBackoff(ctx, attempt)
				continue
			}
			return provider.GenerateResult{}, lastErr
		}

		// Extract text from response
		text := extractText(resp)
		if text == "" {
			lastErr = errors.New("gemini returned empty response")
			continue
		}

		citations := extractCitations(resp, params.FileIDToFilename)
		usage := extractUsage(resp)

		slog.Info("gemini request completed",
			"model", model,
			"tokens_in", usage.InputTokens,
			"tokens_out", usage.OutputTokens,
		)

		return provider.GenerateResult{
			Text:      text,
			Usage:     usage,
			Citations: citations,
			Model:     model,
		}, nil
	}

	return provider.GenerateResult{}, lastErr
}

// GenerateReplyStream implements streaming responses.
func (c *Client) GenerateReplyStream(ctx context.Context, params provider.GenerateParams) (<-chan provider.StreamChunk, error) {
	// For now, fall back to non-streaming
	ch := make(chan provider.StreamChunk, 1)
	go func() {
		defer close(ch)
		result, err := c.GenerateReply(ctx, params)
		if err != nil {
			ch <- provider.StreamChunk{
				Type:      provider.ChunkTypeError,
				Error:     err,
				Retryable: isRetryableError(err),
			}
			return
		}
		ch <- provider.StreamChunk{
			Type: provider.ChunkTypeText,
			Text: result.Text,
		}
		ch <- provider.StreamChunk{
			Type:  provider.ChunkTypeComplete,
			Model: result.Model,
			Usage: result.Usage,
		}
	}()
	return ch, nil
}

// buildContents builds conversation content from input and history.
func buildContents(userInput string, history []provider.Message) []*genai.Content {
	var contents []*genai.Content

	// Add conversation history
	for _, msg := range history {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: []*genai.Part{genai.NewPartFromText(msg.Content)},
		})
	}

	// Add current user input
	contents = append(contents, &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{genai.NewPartFromText(strings.TrimSpace(userInput))},
	})

	return contents
}

// extractText extracts text from the response.
func extractText(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}

	var text strings.Builder
	for _, candidate := range resp.Candidates {
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				text.WriteString(part.Text)
			}
		}
	}

	return strings.TrimSpace(text.String())
}

// extractUsage extracts token usage from the response.
func extractUsage(resp *genai.GenerateContentResponse) *provider.Usage {
	if resp == nil || resp.UsageMetadata == nil {
		return nil
	}

	return &provider.Usage{
		InputTokens:  int64(resp.UsageMetadata.PromptTokenCount),
		OutputTokens: int64(resp.UsageMetadata.CandidatesTokenCount),
		TotalTokens:  int64(resp.UsageMetadata.TotalTokenCount),
	}
}

// extractCitations extracts citations from grounding metadata.
func extractCitations(resp *genai.GenerateContentResponse, fileIDToFilename map[string]string) []provider.Citation {
	var citations []provider.Citation

	if resp == nil || len(resp.Candidates) == 0 {
		return citations
	}

	for _, candidate := range resp.Candidates {
		if candidate.GroundingMetadata == nil {
			continue
		}

		// Extract web search citations
		for _, chunk := range candidate.GroundingMetadata.GroundingChunks {
			if chunk.Web != nil {
				citations = append(citations, provider.Citation{
					Type:     provider.CitationTypeURL,
					Provider: "gemini",
					URL:      chunk.Web.URI,
					Title:    chunk.Web.Title,
				})
			}
		}
	}

	return citations
}

// buildSafetySettings builds safety settings from threshold string.
func buildSafetySettings(threshold string) []*genai.SafetySetting {
	var level genai.HarmBlockThreshold
	switch strings.ToUpper(threshold) {
	case "BLOCK_NONE":
		level = genai.HarmBlockThresholdBlockNone
	case "LOW_AND_ABOVE":
		level = genai.HarmBlockThresholdBlockLowAndAbove
	case "MEDIUM_AND_ABOVE":
		level = genai.HarmBlockThresholdBlockMediumAndAbove
	case "ONLY_HIGH":
		level = genai.HarmBlockThresholdBlockOnlyHigh
	default:
		level = genai.HarmBlockThresholdBlockMediumAndAbove
	}

	categories := []genai.HarmCategory{
		genai.HarmCategoryHarassment,
		genai.HarmCategoryHateSpeech,
		genai.HarmCategorySexuallyExplicit,
		genai.HarmCategoryDangerousContent,
	}

	var settings []*genai.SafetySetting
	for _, cat := range categories {
		settings = append(settings, &genai.SafetySetting{
			Category:  cat,
			Threshold: level,
		})
	}

	return settings
}

// isRetryableError checks if an error should trigger a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "resource exhausted") ||
		strings.Contains(errStr, "overloaded") {
		return true
	}
	return false
}

// sleepWithBackoff sleeps with exponential backoff.
func sleepWithBackoff(ctx context.Context, attempt int) {
	delay := backoffBase * time.Duration(1<<uint(attempt-1))
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}
