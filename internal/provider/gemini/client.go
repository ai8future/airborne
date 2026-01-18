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

	"google.golang.org/genai"

	"github.com/ai8future/airborne/internal/httpcapture"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/retry"
	"github.com/ai8future/airborne/internal/validation"
)

const (
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

// SupportsStreaming returns true as Gemini supports streaming responses.
func (c *Client) SupportsStreaming() bool {
	return true
}

// GenerateReply implements provider.Provider using Google's Gemini API.
func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
	// Ensure request has a timeout
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, retry.RequestTimeout)
		defer cancel()
	}

	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		return provider.GenerateResult{}, errors.New("Gemini API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = "gemini-3-pro-preview"
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Create capturing transport for debug JSON (always enabled for admin dashboard)
	capture := httpcapture.New()
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

	// Apply generation parameters with sensible defaults
	// Temperature: default 1.0 for Pro models (creative but coherent)
	if cfg.Temperature != nil {
		temp := float32(*cfg.Temperature)
		generateConfig.Temperature = &temp
	} else {
		defaultTemp := float32(1.0)
		generateConfig.Temperature = &defaultTemp
	}
	// TopP: only set if explicitly configured (not a default, API handles it)
	if cfg.TopP != nil {
		topP := float32(*cfg.TopP)
		generateConfig.TopP = &topP
	}
	// MaxOutputTokens: default 32000 for full response length
	if cfg.MaxOutputTokens != nil {
		generateConfig.MaxOutputTokens = int32(*cfg.MaxOutputTokens)
	} else {
		generateConfig.MaxOutputTokens = 32000
	}

	// Configure safety settings
	if threshold := cfg.ExtraOptions["safety_threshold"]; threshold != "" {
		generateConfig.SafetySettings = buildSafetySettings(threshold)
	}

	// Configure thinking (not supported on Flash models)
	// For Pro models (non-Flash), default to HIGH thinking level like Solstice
	modelLower := strings.ToLower(model)
	isFlashModel := strings.Contains(modelLower, "flash")
	isProModel := strings.Contains(modelLower, "pro")
	if !isFlashModel {
		thinkingLevel := cfg.ExtraOptions["thinking_level"]
		thinkingBudgetStr := cfg.ExtraOptions["thinking_budget"]
		includeThoughts := cfg.ExtraOptions["include_thoughts"] == "true"

		// Default to HIGH thinking level for Pro models (matching Solstice behavior)
		if thinkingLevel == "" && isProModel {
			thinkingLevel = "HIGH"
		}

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
	if params.EnableCodeExecution {
		tools = append(tools, &genai.Tool{
			CodeExecution: &genai.ToolCodeExecution{},
		})
	}
	// Add custom function tools
	if len(params.Tools) > 0 {
		functionDecls := make([]*genai.FunctionDeclaration, 0, len(params.Tools))
		for _, tool := range params.Tools {
			functionDecls = append(functionDecls, buildFunctionDeclaration(tool))
		}
		tools = append(tools, &genai.Tool{
			FunctionDeclarations: functionDecls,
		})
	}
	if len(tools) > 0 {
		generateConfig.Tools = tools
	}

	// Enable structured output (JSON mode) if requested via params
	structuredOutputEnabled := params.EnableStructuredOutput
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
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		slog.Info("gemini request",
			"attempt", attempt,
			"model", model,
			"request_id", params.RequestID,
		)

		reqCtx, reqCancel := context.WithTimeout(ctx, retry.RequestTimeout)
		resp, err := client.Models.GenerateContent(reqCtx, model, contents, generateConfig)
		reqCancel()

		if err != nil {
			// Check if parent context is still valid
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				lastErr = fmt.Errorf("gemini request timeout: %w", err)
				slog.Warn("gemini timeout, retrying", "attempt", attempt)
				if attempt < retry.MaxAttempts {
					retry.SleepWithBackoff(ctx, attempt)
					continue
				}
				return provider.GenerateResult{}, lastErr
			}

			lastErr = fmt.Errorf("gemini error: %w", err)
			if !retry.IsRetryable(err) {
				return provider.GenerateResult{}, lastErr
			}

			slog.Warn("gemini retryable error", "attempt", attempt, "error", err)
			if attempt < retry.MaxAttempts {
				retry.SleepWithBackoff(ctx, attempt)
				continue
			}
			return provider.GenerateResult{}, lastErr
		}

		// Extract text from response (handles both plain text and structured JSON)
		var text string
		var structuredMetadata *provider.StructuredMetadata
		if structuredOutputEnabled {
			text, structuredMetadata = extractStructuredResponse(resp)
		} else {
			text = extractText(resp)
		}

		if text == "" {
			// Check if blocked by safety filters
			if reason := getBlockReason(resp); reason != "" {
				return provider.GenerateResult{}, fmt.Errorf("gemini response blocked: %s", reason)
			}
			lastErr = errors.New("gemini returned empty response")
			if attempt < retry.MaxAttempts {
				retry.SleepWithBackoff(ctx, attempt)
			}
			continue
		}

		citations := extractCitations(resp, params.FileIDToFilename)
		usage := extractUsage(resp)
		toolCalls := extractFunctionCalls(resp)
		codeExecutions := extractCodeExecutionResults(resp)

		slog.Info("gemini request completed",
			"model", model,
			"tokens_in", usage.InputTokens,
			"tokens_out", usage.OutputTokens,
			"tool_calls", len(toolCalls),
			"code_executions", len(codeExecutions),
		)

		var reqJSON, respJSON []byte
		if capture != nil {
			reqJSON = capture.RequestBody
			respJSON = capture.ResponseBody
		}

		return provider.GenerateResult{
			Text:               text,
			Usage:              usage,
			Citations:          citations,
			Model:              model,
			ToolCalls:          toolCalls,
			RequiresToolOutput: len(toolCalls) > 0,
			CodeExecutions:     codeExecutions,
			StructuredMetadata: structuredMetadata,
			RequestJSON:        reqJSON,
			ResponseJSON:       respJSON,
		}, nil
	}

	return provider.GenerateResult{}, lastErr
}

// GenerateReplyStream implements streaming responses using Gemini's streaming API.
func (c *Client) GenerateReplyStream(ctx context.Context, params provider.GenerateParams) (<-chan provider.StreamChunk, error) {
	// Ensure request has a timeout
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, retry.RequestTimeout)
	}

	// Helper to clean up cancel on error returns
	cleanup := func() {
		if cancel != nil {
			cancel()
		}
	}

	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		cleanup()
		return nil, errors.New("Gemini API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = "gemini-3-pro-preview"
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Create capturing transport for debug JSON
	capture := httpcapture.New()
	clientConfig := &genai.ClientConfig{
		APIKey:     cfg.APIKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: capture.Client(),
	}
	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			cleanup()
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
		clientConfig.HTTPOptions = genai.HTTPOptions{
			BaseURL: cfg.BaseURL,
		}
	}

	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("creating gemini client: %w", err)
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

	// Apply generation parameters with sensible defaults
	// Temperature: default 1.0 for Pro models (creative but coherent)
	if cfg.Temperature != nil {
		temp := float32(*cfg.Temperature)
		generateConfig.Temperature = &temp
	} else {
		defaultTemp := float32(1.0)
		generateConfig.Temperature = &defaultTemp
	}
	// TopP: only set if explicitly configured (not a default, API handles it)
	if cfg.TopP != nil {
		topP := float32(*cfg.TopP)
		generateConfig.TopP = &topP
	}
	// MaxOutputTokens: default 32000 for full response length
	if cfg.MaxOutputTokens != nil {
		generateConfig.MaxOutputTokens = int32(*cfg.MaxOutputTokens)
	} else {
		generateConfig.MaxOutputTokens = 32000
	}

	// Configure safety settings
	if threshold := cfg.ExtraOptions["safety_threshold"]; threshold != "" {
		generateConfig.SafetySettings = buildSafetySettings(threshold)
	}

	// Configure thinking (not supported on Flash models)
	// For Pro models (non-Flash), default to HIGH thinking level like Solstice
	modelLower := strings.ToLower(model)
	isFlashModel := strings.Contains(modelLower, "flash")
	isProModel := strings.Contains(modelLower, "pro")
	if !isFlashModel {
		thinkingLevel := cfg.ExtraOptions["thinking_level"]
		thinkingBudgetStr := cfg.ExtraOptions["thinking_budget"]
		includeThoughts := cfg.ExtraOptions["include_thoughts"] == "true"

		// Default to HIGH thinking level for Pro models (matching Solstice behavior)
		if thinkingLevel == "" && isProModel {
			thinkingLevel = "HIGH"
		}

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

	// Enable structured output (JSON mode) if requested via params
	structuredOutputEnabled := params.EnableStructuredOutput
	if structuredOutputEnabled {
		generateConfig.ResponseMIMEType = "application/json"
		generateConfig.ResponseJsonSchema = structuredOutputSchema()
	}

	// Build tools
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
	if params.EnableCodeExecution {
		tools = append(tools, &genai.Tool{
			CodeExecution: &genai.ToolCodeExecution{},
		})
	}
	if len(params.Tools) > 0 {
		functionDecls := make([]*genai.FunctionDeclaration, 0, len(params.Tools))
		for _, tool := range params.Tools {
			functionDecls = append(functionDecls, buildFunctionDeclaration(tool))
		}
		tools = append(tools, &genai.Tool{
			FunctionDeclarations: functionDecls,
		})
	}
	if len(tools) > 0 {
		generateConfig.Tools = tools
	}

	ch := make(chan provider.StreamChunk, 100)

	go func() {
		defer close(ch)
		if cancel != nil {
			defer cancel()
		}

		var totalText strings.Builder
		var toolCalls []provider.ToolCall
		var codeExecutions []provider.CodeExecutionResult
		var lastUsage *provider.Usage

		// Use GenerateContentStream for streaming
		for resp, err := range client.Models.GenerateContentStream(ctx, model, contents, generateConfig) {
			if err != nil {
				ch <- provider.StreamChunk{
					Type:      provider.ChunkTypeError,
					Error:     err,
					Retryable: retry.IsRetryable(err),
				}
				return
			}

			// Extract text from response candidates
			for _, candidate := range resp.Candidates {
				if candidate.Content == nil {
					continue
				}
				for _, part := range candidate.Content.Parts {
					// Handle text parts
					if part.Text != "" {
						ch <- provider.StreamChunk{
							Type: provider.ChunkTypeText,
							Text: part.Text,
						}
						totalText.WriteString(part.Text)
					}

					// Handle function calls
					if part.FunctionCall != nil {
						argsJSON, _ := json.Marshal(part.FunctionCall.Args)
						toolCall := provider.ToolCall{
							ID:        part.FunctionCall.ID,
							Name:      part.FunctionCall.Name,
							Arguments: string(argsJSON),
						}
						toolCalls = append(toolCalls, toolCall)
						ch <- provider.StreamChunk{
							Type:     provider.ChunkTypeToolCall,
							ToolCall: &toolCall,
						}
					}

					// Handle code execution
					if part.ExecutableCode != nil {
						exec := provider.CodeExecutionResult{
							Code:     part.ExecutableCode.Code,
							Language: string(part.ExecutableCode.Language),
						}
						codeExecutions = append(codeExecutions, exec)
					}
					if part.CodeExecutionResult != nil && len(codeExecutions) > 0 {
						last := &codeExecutions[len(codeExecutions)-1]
						last.Stdout = part.CodeExecutionResult.Output
						if part.CodeExecutionResult.Outcome == genai.OutcomeOK {
							last.ExitCode = 0
						} else {
							last.ExitCode = 1
						}
						ch <- provider.StreamChunk{
							Type:          provider.ChunkTypeCodeExecution,
							CodeExecution: last,
						}
					}
				}
			}

			// Track usage from each response
			if resp.UsageMetadata != nil {
				lastUsage = &provider.Usage{
					InputTokens:  int64(resp.UsageMetadata.PromptTokenCount),
					OutputTokens: int64(resp.UsageMetadata.CandidatesTokenCount),
					TotalTokens:  int64(resp.UsageMetadata.TotalTokenCount),
				}
			}
		}

		// Build synthetic response JSON for debugging (since SSE can't be captured the same way)
		var respJSON []byte
		if lastUsage != nil {
			syntheticResp := map[string]any{
				"text":            totalText.String(),
				"model":           model,
				"input_tokens":    lastUsage.InputTokens,
				"output_tokens":   lastUsage.OutputTokens,
				"total_tokens":    lastUsage.TotalTokens,
				"tool_calls":      len(toolCalls),
				"code_executions": len(codeExecutions),
			}
			respJSON, _ = json.Marshal(syntheticResp)
		}

		// Send completion chunk with captured debug JSON
		ch <- provider.StreamChunk{
			Type:               provider.ChunkTypeComplete,
			Model:              model,
			Usage:              lastUsage,
			ToolCalls:          toolCalls,
			RequiresToolOutput: len(toolCalls) > 0,
			CodeExecutions:     codeExecutions,
			RequestJSON:        capture.RequestBody,
			ResponseJSON:       respJSON,
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

// extractStructuredResponse extracts text and metadata from structured JSON output.
func extractStructuredResponse(resp *genai.GenerateContentResponse) (string, *provider.StructuredMetadata) {
	rawJSON := extractText(resp)
	if rawJSON == "" {
		return "", nil
	}

	var parsed struct {
		Reply              string `json:"reply"`
		Intent             string `json:"intent"`
		RequiresUserAction bool   `json:"requires_user_action"`
		Entities           []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"entities"`
		Topics           []string `json:"topics"`
		SchedulingIntent *struct {
			Detected          bool   `json:"detected"`
			DatetimeMentioned string `json:"datetime_mentioned"`
		} `json:"scheduling_intent"`
	}

	if err := json.Unmarshal([]byte(rawJSON), &parsed); err != nil {
		slog.Warn("failed to parse structured response, falling back to raw text", "error", err)
		return rawJSON, nil
	}

	// Convert to provider types
	metadata := &provider.StructuredMetadata{
		Intent:             parsed.Intent,
		RequiresUserAction: parsed.RequiresUserAction,
		Topics:             parsed.Topics,
	}

	for _, e := range parsed.Entities {
		metadata.Entities = append(metadata.Entities, provider.StructuredEntity{
			Name: e.Name,
			Type: e.Type,
		})
	}

	if parsed.SchedulingIntent != nil {
		metadata.Scheduling = &provider.SchedulingIntent{
			Detected:          parsed.SchedulingIntent.Detected,
			DatetimeMentioned: parsed.SchedulingIntent.DatetimeMentioned,
		}
	}

	return parsed.Reply, metadata
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
// This extracts intent, entities, topics, and scheduling signals alongside the response.
func structuredOutputSchema() *genai.Schema {
	return &genai.Schema{
		Type: "object",
		Properties: map[string]*genai.Schema{
			"reply": {
				Type:        "string",
				Description: "The conversational response in Markdown format",
			},
			"intent": {
				Type:        "string",
				Description: "Primary intent classification",
				Enum: []string{
					"question", "request", "task_delegation",
					"feedback", "complaint", "follow_up", "attachment_analysis",
				},
			},
			"requires_user_action": {
				Type:        "boolean",
				Description: "True if response asks a clarifying question",
			},
			"entities": {
				Type:        "array",
				Description: "Named entities extracted from the text",
				Items: &genai.Schema{
					Type: "object",
					Properties: map[string]*genai.Schema{
						"name": {Type: "string", Description: "Entity name as it appears in text"},
						"type": {
							Type:        "string",
							Description: "Entity type",
							Enum: []string{
								// Core (9)
								"person", "organization", "location", "product",
								"project", "document", "event", "money", "date",
								// Business (3)
								"investor", "advisor", "metric",
								// Technology (3)
								"technology", "tool", "service",
								// Operations (3)
								"methodology", "credential", "timeframe",
								// Content (3)
								"feature", "url", "email_address",
							},
						},
					},
					Required: []string{"name", "type"},
				},
			},
			"topics": {
				Type:        "array",
				Description: "2-4 keyword tags",
				Items:       &genai.Schema{Type: "string"},
			},
			"scheduling_intent": {
				Type:        "object",
				Description: "Calendar/meeting signals",
				Properties: map[string]*genai.Schema{
					"detected":           {Type: "boolean", Description: "True if scheduling intent was detected"},
					"datetime_mentioned": {Type: "string", Description: "Raw text like 'next Tuesday at 2pm'"},
				},
			},
		},
		Required: []string{"reply", "intent"},
	}
}

// buildFunctionDeclaration converts a provider.Tool to a Gemini FunctionDeclaration.
func buildFunctionDeclaration(tool provider.Tool) *genai.FunctionDeclaration {
	decl := &genai.FunctionDeclaration{
		Name:        tool.Name,
		Description: tool.Description,
	}

	// Parse the JSON schema string into a genai.Schema
	if tool.ParametersSchema != "" {
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(tool.ParametersSchema), &schemaMap); err == nil {
			decl.Parameters = convertToSchema(schemaMap)
		} else {
			slog.Warn("invalid tool parameters schema", "tool", tool.Name, "error", err)
		}
	}

	return decl
}

// convertToSchema converts a JSON schema map to genai.Schema.
func convertToSchema(schemaMap map[string]interface{}) *genai.Schema {
	schema := &genai.Schema{}

	if t, ok := schemaMap["type"].(string); ok {
		schema.Type = genai.Type(strings.ToUpper(t))
	}
	if desc, ok := schemaMap["description"].(string); ok {
		schema.Description = desc
	}
	if props, ok := schemaMap["properties"].(map[string]interface{}); ok {
		schema.Properties = make(map[string]*genai.Schema)
		for name, prop := range props {
			if propMap, ok := prop.(map[string]interface{}); ok {
				schema.Properties[name] = convertToSchema(propMap)
			}
		}
	}
	if required, ok := schemaMap["required"].([]interface{}); ok {
		for _, r := range required {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}
	if items, ok := schemaMap["items"].(map[string]interface{}); ok {
		schema.Items = convertToSchema(items)
	}
	if enum, ok := schemaMap["enum"].([]interface{}); ok {
		for _, e := range enum {
			if s, ok := e.(string); ok {
				schema.Enum = append(schema.Enum, s)
			}
		}
	}

	return schema
}

// extractFunctionCalls extracts function calls from the response.
func extractFunctionCalls(resp *genai.GenerateContentResponse) []provider.ToolCall {
	var toolCalls []provider.ToolCall
	if resp == nil || len(resp.Candidates) == 0 {
		return toolCalls
	}

	for _, candidate := range resp.Candidates {
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				// Convert Args to JSON string
				argsJSON, err := json.Marshal(part.FunctionCall.Args)
				if err != nil {
					slog.Warn("failed to marshal function call args", "error", err)
					argsJSON = []byte("{}")
				}
				toolCalls = append(toolCalls, provider.ToolCall{
					ID:        part.FunctionCall.ID,
					Name:      part.FunctionCall.Name,
					Arguments: string(argsJSON),
				})
			}
		}
	}

	return toolCalls
}

// extractCodeExecutionResults extracts code execution results from the response.
func extractCodeExecutionResults(resp *genai.GenerateContentResponse) []provider.CodeExecutionResult {
	var executions []provider.CodeExecutionResult
	if resp == nil || len(resp.Candidates) == 0 {
		return executions
	}

	for _, candidate := range resp.Candidates {
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part.ExecutableCode != nil {
				exec := provider.CodeExecutionResult{
					Code:     part.ExecutableCode.Code,
					Language: string(part.ExecutableCode.Language),
				}
				executions = append(executions, exec)
			}
			if part.CodeExecutionResult != nil {
				// Find the matching execution and update with output
				if len(executions) > 0 {
					last := &executions[len(executions)-1]
					last.Stdout = part.CodeExecutionResult.Output
					if part.CodeExecutionResult.Outcome == genai.OutcomeOK {
						last.ExitCode = 0
					} else {
						last.ExitCode = 1
					}
				}
			}
		}
	}

	return executions
}
