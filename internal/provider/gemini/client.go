// Package gemini provides the Google Gemini LLM provider implementation.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/ai8future/airborne/internal/httpcapture"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/validation"
)

const (
	maxAttempts    = 3
	requestTimeout = 3 * time.Minute
	backoffBase    = 250 * time.Millisecond
	// maxHistoryChars limits conversation history to prevent context overflow
	maxHistoryChars = 50000
)

// Client implements the provider.Provider interface using Google's Gemini API.
type Client struct {
	debug bool
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose Gemini payload logging.
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

// SupportsStreaming returns false because the current implementation falls back to
// non-streaming (calls GenerateReply and returns result as single chunk).
func (c *Client) SupportsStreaming() bool {
	return false
}

// GenerateReply implements provider.Provider using Google's Gemini API.
func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
	// Ensure request has a timeout
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}

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

	// Create capturing transport for debug JSON
	capture := httpcapture.New()

	// Create Gemini client
	clientConfig := &genai.ClientConfig{
		APIKey:     cfg.APIKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: capture.Client(),
	}
	if cfg.BaseURL != "" {
		// SECURITY: Validate base URL to prevent SSRF attacks
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return provider.GenerateResult{}, fmt.Errorf("invalid base URL: %w", err)
		}
		clientConfig.HTTPOptions = genai.HTTPOptions{
			BaseURL: cfg.BaseURL,
		}
	}

	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return provider.GenerateResult{}, fmt.Errorf("creating gemini client: %w", err)
	}

	// Build conversation content with inline images
	contents := buildContents(params.UserInput, params.ConversationHistory, params.InlineImages)

	// Build system instruction with file ID mappings
	systemInstruction := params.Instructions
	if len(params.FileIDToFilename) > 0 {
		var mappings []string
		for id, name := range params.FileIDToFilename {
			mappings = append(mappings, fmt.Sprintf("- %s: %s", id, name))
		}
		sort.Strings(mappings)
		systemInstruction += "\n\nThe following files are attached. When referencing them, use the original filename:\n" + strings.Join(mappings, "\n")
	}

	// Build generation config
	generateConfig := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(systemInstruction)},
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

	// Configure thinking (not supported on Flash models)
	modelLower := strings.ToLower(model)
	isFlashModel := strings.Contains(modelLower, "flash")
	if !isFlashModel {
		thinkingLevel := cfg.ExtraOptions["thinking_level"]
		thinkingBudgetStr := cfg.ExtraOptions["thinking_budget"]
		includeThoughts := cfg.ExtraOptions["include_thoughts"] == "true"

		if thinkingLevel != "" || thinkingBudgetStr != "" || includeThoughts {
			thinkingConfig := &genai.ThinkingConfig{
				IncludeThoughts: includeThoughts,
			}
			if thinkingLevel != "" {
				thinkingConfig.ThinkingLevel = parseThinkingLevel(thinkingLevel)
			}
			if thinkingBudgetStr != "" {
				var budget int
				fmt.Sscanf(thinkingBudgetStr, "%d", &budget)
				if budget > 0 {
					budget32 := int32(budget)
					thinkingConfig.ThinkingBudget = &budget32
				}
			}
			generateConfig.ThinkingConfig = thinkingConfig
		}
	}

	// Build tools - FileSearch and GoogleSearch cannot be used together
	var tools []*genai.Tool
	hasFileSearch := params.EnableFileSearch && strings.TrimSpace(params.FileStoreID) != ""
	if hasFileSearch {
		tools = append(tools, &genai.Tool{
			FileSearch: &genai.FileSearch{
				FileSearchStoreNames: []string{params.FileStoreID},
			},
		})
	}
	if params.EnableWebSearch && !hasFileSearch {
		tools = append(tools, &genai.Tool{
			GoogleSearch: &genai.GoogleSearch{},
		})
	}
	if len(tools) > 0 {
		generateConfig.Tools = tools
	}

	// Enable structured output (JSON mode) if requested
	structuredOutputEnabled := cfg.ExtraOptions["structured_output"] == "true"
	if structuredOutputEnabled {
		generateConfig.ResponseMIMEType = "application/json"
		generateConfig.ResponseJsonSchema = structuredOutputSchema()
	}

	if c.debug {
		slog.Debug("gemini request",
			"model", model,
			"file_store_id", params.FileStoreID,
			"web_search", params.EnableWebSearch && !hasFileSearch,
			"structured_output", structuredOutputEnabled,
			"request_id", params.RequestID,
		)
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

		// Extract text from response (handles both plain text and structured JSON)
		var text string
		if structuredOutputEnabled {
			text = extractStructuredText(resp)
		} else {
			text = extractText(resp)
		}

		if text == "" {
			// Check if blocked by safety filters
			if reason := getBlockReason(resp); reason != "" {
				return provider.GenerateResult{}, fmt.Errorf("gemini response blocked: %s", reason)
			}
			lastErr = errors.New("gemini returned empty response")
			if attempt < maxAttempts {
				sleepWithBackoff(ctx, attempt)
			}
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
			Text:         text,
			Usage:        usage,
			Citations:    citations,
			Model:        model,
			RequestJSON:  capture.RequestBody,
			ResponseJSON: capture.ResponseBody,
		}, nil
	}

	return provider.GenerateResult{}, lastErr
}

// GenerateReplyStream implements streaming responses.
func (c *Client) GenerateReplyStream(ctx context.Context, params provider.GenerateParams) (<-chan provider.StreamChunk, error) {
	// Ensure request has a timeout
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
	}

	// For now, fall back to non-streaming
	ch := make(chan provider.StreamChunk, 1)
	go func() {
		defer close(ch)
		if cancel != nil {
			defer cancel()
		}
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

// InlineImage represents an image to include in the prompt.
type InlineImage struct {
	URI      string
	MIMEType string
	Filename string
}

// buildContents builds conversation content from input, history, and images.
func buildContents(userInput string, history []provider.Message, inlineImages []provider.InlineImage) []*genai.Content {
	var contents []*genai.Content

	// Add conversation history with size limit
	totalChars := 0
	for _, msg := range history {
		trimmed := strings.TrimSpace(msg.Content)
		if trimmed == "" {
			continue
		}
		msgLen := len(trimmed)
		if totalChars+msgLen > maxHistoryChars {
			slog.Debug("truncating conversation history",
				"total_chars", totalChars,
				"max_chars", maxHistoryChars)
			break
		}
		totalChars += msgLen

		var role genai.Role
		if msg.Role == "assistant" {
			role = genai.RoleModel
		} else {
			role = genai.RoleUser
		}
		contents = append(contents, genai.NewContentFromText(trimmed, role))
	}

	// Build user content with text and optional images
	var parts []*genai.Part
	parts = append(parts, genai.NewPartFromText(strings.TrimSpace(userInput)))

	// Add inline images
	for _, img := range inlineImages {
		parts = append(parts, genai.NewPartFromURI(img.URI, img.MIMEType))
	}

	contents = append(contents, &genai.Content{
		Role:  genai.RoleUser,
		Parts: parts,
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

// extractStructuredText extracts the reply field from structured JSON output.
func extractStructuredText(resp *genai.GenerateContentResponse) string {
	rawJSON := extractText(resp)
	if rawJSON == "" {
		return ""
	}

	var parsed struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &parsed); err != nil {
		slog.Warn("failed to parse structured response, using raw text",
			"error", err.Error())
		return rawJSON
	}
	return parsed.Reply
}

// getBlockReason checks if the response was blocked and returns the reason.
func getBlockReason(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}

	candidate := resp.Candidates[0]
	switch candidate.FinishReason {
	case genai.FinishReasonSafety:
		return "content blocked by safety filters"
	case genai.FinishReasonRecitation:
		return "content blocked due to potential recitation"
	case genai.FinishReasonBlocklist:
		return "content contains forbidden terms"
	case genai.FinishReasonProhibitedContent:
		return "content contains prohibited content"
	case genai.FinishReasonSPII:
		return "content contains sensitive personally identifiable information"
	}
	return ""
}

// extractUsage extracts token usage from the response.
func extractUsage(resp *genai.GenerateContentResponse) *provider.Usage {
	if resp == nil || resp.UsageMetadata == nil {
		return &provider.Usage{}
	}

	usage := &provider.Usage{
		InputTokens:  int64(resp.UsageMetadata.PromptTokenCount),
		OutputTokens: int64(resp.UsageMetadata.CandidatesTokenCount),
		TotalTokens:  int64(resp.UsageMetadata.TotalTokenCount),
	}

	// Ensure TotalTokens is at least sum of input + output
	expectedTotal := usage.InputTokens + usage.OutputTokens
	if usage.TotalTokens < expectedTotal {
		usage.TotalTokens = expectedTotal
	}

	return usage
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

		// Extract from grounding chunks
		for _, chunk := range candidate.GroundingMetadata.GroundingChunks {
			if chunk.Web != nil {
				citations = append(citations, provider.Citation{
					Type:     provider.CitationTypeURL,
					Provider: "gemini",
					URL:      chunk.Web.URI,
					Title:    chunk.Web.Title,
				})
			}
			if chunk.RetrievedContext != nil {
				fileID := chunk.RetrievedContext.Title
				filename := fileID
				if fn, ok := fileIDToFilename[fileID]; ok {
					filename = fn
				}
				citations = append(citations, provider.Citation{
					Type:     provider.CitationTypeFile,
					Provider: "gemini",
					FileID:   fileID,
					Filename: filename,
					Snippet:  chunk.RetrievedContext.Text,
				})
			}
		}

		// Extract from grounding supports (with position data)
		for _, support := range candidate.GroundingMetadata.GroundingSupports {
			if support.Segment == nil || len(support.GroundingChunkIndices) == 0 {
				continue
			}
			for _, chunkIdx := range support.GroundingChunkIndices {
				if int(chunkIdx) >= len(candidate.GroundingMetadata.GroundingChunks) {
					continue
				}
				chunk := candidate.GroundingMetadata.GroundingChunks[chunkIdx]
				citation := provider.Citation{
					Provider:   "gemini",
					StartIndex: int(support.Segment.StartIndex),
					EndIndex:   int(support.Segment.EndIndex),
				}
				if chunk.Web != nil {
					citation.Type = provider.CitationTypeURL
					citation.URL = chunk.Web.URI
					citation.Title = chunk.Web.Title
				} else if chunk.RetrievedContext != nil {
					citation.Type = provider.CitationTypeFile
					fileID := chunk.RetrievedContext.Title
					citation.FileID = fileID
					citation.Filename = fileID
					if fn, ok := fileIDToFilename[fileID]; ok {
						citation.Filename = fn
					}
					citation.Snippet = chunk.RetrievedContext.Text
				}
				citations = append(citations, citation)
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
	case "LOW_AND_ABOVE", "BLOCK_LOW_AND_ABOVE":
		level = genai.HarmBlockThresholdBlockLowAndAbove
	case "MEDIUM_AND_ABOVE", "BLOCK_MEDIUM_AND_ABOVE":
		level = genai.HarmBlockThresholdBlockMediumAndAbove
	case "ONLY_HIGH", "BLOCK_ONLY_HIGH":
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

// parseThinkingLevel converts config string to genai.ThinkingLevel.
func parseThinkingLevel(s string) genai.ThinkingLevel {
	switch strings.ToUpper(s) {
	case "MINIMAL":
		return genai.ThinkingLevel("MINIMAL")
	case "LOW":
		return genai.ThinkingLevelLow
	case "MEDIUM":
		return genai.ThinkingLevel("MEDIUM")
	case "HIGH":
		return genai.ThinkingLevelHigh
	default:
		return genai.ThinkingLevelUnspecified
	}
}

// structuredOutputSchema returns the JSON schema for structured output mode.
func structuredOutputSchema() *genai.Schema {
	return &genai.Schema{
		Type: "object",
		Properties: map[string]*genai.Schema{
			"reply": {
				Type:        "string",
				Description: "The main response text",
			},
			"intent": {
				Type:        "string",
				Description: "The detected intent of the user message",
				Enum:        []string{"question", "request", "task_delegation", "feedback", "complaint", "follow_up", "attachment_analysis"},
			},
			"entities": {
				Type:        "array",
				Description: "Key entities mentioned in the response",
				Items: &genai.Schema{
					Type: "object",
					Properties: map[string]*genai.Schema{
						"name": {Type: "string", Description: "Entity name"},
						"type": {Type: "string", Description: "Entity type", Enum: []string{"person", "organization", "location", "product", "technology", "tool", "service"}},
					},
					Required: []string{"name", "type"},
				},
			},
			"topics": {
				Type:        "array",
				Description: "2-4 keywords describing the topics discussed",
				Items:       &genai.Schema{Type: "string"},
			},
			"requires_user_action": {
				Type:        "boolean",
				Description: "Whether the response requires user action",
			},
		},
		Required: []string{"reply"},
	}
}

// isRetryableError checks if an error should trigger a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Don't retry context errors
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	errStr := err.Error()
	errLower := strings.ToLower(errStr)

	// Don't retry auth errors
	authErrors := []string{"401", "403", "invalid_api_key", "permission_denied", "unauthenticated"}
	for _, authErr := range authErrors {
		if strings.Contains(errLower, authErr) {
			return false
		}
	}

	// Don't retry invalid request errors
	invalidErrors := []string{"400", "invalid_argument", "invalid_request", "malformed"}
	for _, invErr := range invalidErrors {
		if strings.Contains(errLower, invErr) {
			return false
		}
	}

	// Retry rate limit and server errors
	if strings.Contains(errStr, "429") || strings.Contains(errLower, "resource") ||
		strings.Contains(errLower, "rate") || strings.Contains(errLower, "overloaded") {
		return true
	}
	if strings.Contains(errStr, "500") || strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") || strings.Contains(errStr, "504") {
		return true
	}

	// Retry network errors
	networkErrors := []string{"connection", "timeout", "temporary", "eof"}
	for _, netErr := range networkErrors {
		if strings.Contains(errLower, netErr) {
			return true
		}
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
