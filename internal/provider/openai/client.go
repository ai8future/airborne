// Package openai provides the OpenAI LLM provider implementation using the Responses API.
package openai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/openai/openai-go/shared/constant"

	"github.com/ai8future/airborne/internal/httpcapture"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/validation"
)

const (
	maxAttempts    = 3
	pollInitial    = 500 * time.Millisecond
	pollMax        = 5 * time.Second
	requestTimeout = 3 * time.Minute
	backoffBase    = 250 * time.Millisecond
)

// citationMarkerPattern matches OpenAI's inline file citation markers like "fileciteturn2file0"
var citationMarkerPattern = regexp.MustCompile(`filecite(?:turn\d+file\d+)+`)

// Client implements the provider.Provider interface using OpenAI's Responses API.
type Client struct {
	debug bool
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose OpenAI payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {
		c.debug = enabled
	}
}

// NewClient creates a new OpenAI provider client.
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
	return "openai"
}

// SupportsFileSearch returns true as OpenAI supports vector store file search.
func (c *Client) SupportsFileSearch() bool {
	return true
}

// SupportsWebSearch returns true as OpenAI supports web search preview.
func (c *Client) SupportsWebSearch() bool {
	return true
}

// SupportsNativeContinuity returns true as OpenAI supports previousResponseID.
func (c *Client) SupportsNativeContinuity() bool {
	return true
}

// SupportsStreaming returns false because the current implementation falls back to
// non-streaming (calls GenerateReply and returns result as single chunk).
// Set to true once true streaming is implemented.
func (c *Client) SupportsStreaming() bool {
	return false
}

// GenerateReply implements provider.Provider using OpenAI's Responses API.
func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
	// Ensure request has a timeout
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}

	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		return provider.GenerateResult{}, errors.New("OpenAI API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Create capturing transport for debug JSON
	capture := httpcapture.New()

	// Build client options
	clientOpts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithHTTPClient(capture.Client()),
	}
	if cfg.BaseURL != "" {
		// SECURITY: Validate base URL to prevent SSRF attacks
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return provider.GenerateResult{}, fmt.Errorf("invalid base URL: %w", err)
		}
		clientOpts = append(clientOpts, option.WithBaseURL(cfg.BaseURL))
	}

	client := openai.NewClient(clientOpts...)

	// Build user prompt from input and history
	userPrompt := buildUserPrompt(params.UserInput, params.ConversationHistory)

	// Build request
	req := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(model),
		Instructions: openai.String(params.Instructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
		Background: openai.Bool(true),
	}

	// Apply optional parameters
	if cfg.Temperature != nil {
		req.Temperature = openai.Float(*cfg.Temperature)
	}
	if cfg.TopP != nil {
		req.TopP = openai.Float(*cfg.TopP)
	}
	if cfg.MaxOutputTokens != nil {
		req.MaxOutputTokens = openai.Int(int64(*cfg.MaxOutputTokens))
	}

	// Apply reasoning effort
	if effort := cfg.ExtraOptions["reasoning_effort"]; effort != "" {
		req.Reasoning = shared.ReasoningParam{
			Effort: mapReasoningEffort(effort),
		}
	}

	// Apply service tier
	if tier := cfg.ExtraOptions["service_tier"]; tier != "" {
		req.ServiceTier = mapServiceTier(tier)
	}

	// Apply verbosity setting
	if verbosity := cfg.ExtraOptions["verbosity"]; verbosity != "" {
		textConfig := responses.ResponseTextConfigParam{}
		textConfig.SetExtraFields(map[string]any{
			"verbosity": strings.ToLower(verbosity),
		})
		req.Text = textConfig
	}

	// Apply prompt cache retention for gpt-5.x models
	if supportsPromptCacheRetention(model) {
		retention := cfg.ExtraOptions["prompt_cache_retention"]
		if retention == "" {
			retention = "24h"
		}
		req.SetExtraFields(map[string]any{
			"prompt_cache_retention": retention,
		})
	}

	// Build tools
	var tools []responses.ToolUnionParam
	if params.EnableFileSearch && strings.TrimSpace(params.FileStoreID) != "" {
		tools = append(tools, responses.ToolUnionParam{
			OfFileSearch: &responses.FileSearchToolParam{
				Type:           constant.FileSearch("file_search"),
				VectorStoreIDs: []string{params.FileStoreID},
			},
		})
	}
	if params.EnableWebSearch {
		tools = append(tools, responses.ToolUnionParam{
			OfWebSearchPreview: &responses.WebSearchToolParam{
				Type:              responses.WebSearchToolTypeWebSearchPreview,
				SearchContextSize: responses.WebSearchToolSearchContextSizeMedium,
			},
		})
	}
	if len(tools) > 0 {
		req.Tools = tools
	}

	// Add previous response ID for conversation continuity
	if strings.TrimSpace(params.PreviousResponseID) != "" {
		req.PreviousResponseID = openai.String(params.PreviousResponseID)
	}

	if c.debug {
		slog.Debug("openai request",
			"model", model,
			"override_model", params.OverrideModel,
			"file_store_id", params.FileStoreID,
			"request_id", params.RequestID,
		)
	}

	// Execute with retry
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		slog.Info("openai request",
			"attempt", attempt,
			"model", model,
			"request_id", params.RequestID,
		)

		reqCtx, reqCancel := context.WithTimeout(ctx, requestTimeout)
		resp, err := client.Responses.New(reqCtx, req)
		reqCancel()

		if err != nil {
			// Check if parent context is still valid
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				lastErr = fmt.Errorf("openai request timeout: %w", err)
				slog.Warn("openai timeout, retrying", "attempt", attempt)
				if attempt < maxAttempts {
					sleepWithBackoff(ctx, attempt)
					continue
				}
				return provider.GenerateResult{}, lastErr
			}

			lastErr = fmt.Errorf("openai error: %w", err)
			if !isRetryableError(err) {
				return provider.GenerateResult{}, lastErr
			}

			slog.Warn("openai retryable error", "attempt", attempt, "error", err)
			if attempt < maxAttempts {
				sleepWithBackoff(ctx, attempt)
				continue
			}
			return provider.GenerateResult{}, lastErr
		}

		// Wait for completion
		resp, err = waitForCompletion(ctx, client, resp)
		if err != nil {
			lastErr = err
			slog.Warn("openai wait error", "attempt", attempt, "error", err)
			continue
		}

		text := strings.TrimSpace(resp.OutputText())
		if text == "" {
			lastErr = errors.New("openai returned empty response")
			continue
		}

		// Strip OpenAI's inline file citation markers (e.g., "fileciteturn2file0")
		text = stripCitationMarkers(text)

		citations := extractCitations(resp, params.FileIDToFilename)

		slog.Info("openai request completed",
			"response_id", resp.ID,
			"model", model,
			"tokens_in", resp.Usage.InputTokens,
			"tokens_out", resp.Usage.OutputTokens,
		)

		return provider.GenerateResult{
			Text:       text,
			ResponseID: resp.ID,
			Usage: &provider.Usage{
				InputTokens:  resp.Usage.InputTokens,
				OutputTokens: resp.Usage.OutputTokens,
				TotalTokens:  resp.Usage.TotalTokens,
			},
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

	// For now, fall back to non-streaming and send result as single chunk
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
			Type:       provider.ChunkTypeComplete,
			ResponseID: result.ResponseID,
			Model:      result.Model,
			Usage:      result.Usage,
		}
	}()
	return ch, nil
}

// buildUserPrompt constructs the user prompt from input and history.
func buildUserPrompt(userInput string, history []provider.Message) string {
	var sb strings.Builder

	if len(history) > 0 {
		sb.WriteString("Previous conversation:\n\n")
		for _, msg := range history {
			role := "User"
			if msg.Role == "assistant" {
				role = "Assistant"
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n\n", role, msg.Content))
		}
		sb.WriteString("---\n\nNew message:\n\n")
	}

	sb.WriteString(strings.TrimSpace(userInput))
	return strings.TrimSpace(sb.String())
}

// waitForCompletion polls until the response is complete.
func waitForCompletion(ctx context.Context, client openai.Client, resp *responses.Response) (*responses.Response, error) {
	if resp == nil {
		return nil, errors.New("response is nil")
	}
	if resp.Status == responses.ResponseStatusCompleted || resp.ID == "" {
		return resp, nil
	}

	pollInterval := pollInitial
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		updated, err := client.Responses.Get(ctx, resp.ID, responses.ResponseGetParams{})
		if err != nil {
			slog.Warn("response poll error", "error", err)
			continue
		}

		switch updated.Status {
		case responses.ResponseStatusCompleted:
			return updated, nil
		case responses.ResponseStatusFailed, responses.ResponseStatusCancelled, responses.ResponseStatusIncomplete:
			var msg string
			if updated.Error.JSON.Message.Valid() {
				msg = updated.Error.Message
			}
			if msg == "" {
				msg = "no error message provided"
			}
			return nil, fmt.Errorf("response ended with status %s: %s", updated.Status, msg)
		}

		// Increase poll interval
		pollInterval = min(pollInterval*2, pollMax)
	}
}

// extractCitations extracts citations from the response.
func extractCitations(resp *responses.Response, fileIDToFilename map[string]string) []provider.Citation {
	var citations []provider.Citation
	if resp == nil {
		return citations
	}

	for _, item := range resp.Output {
		if item.Type == "message" {
			msg := item.AsMessage()
			if msg.ID == "" {
				continue
			}
			for _, content := range msg.Content {
				if content.Type == "output_text" {
					textBlock := content.AsOutputText()
					if !textBlock.JSON.Type.Valid() {
						continue
					}
					for _, ann := range textBlock.Annotations {
						switch ann.Type {
						case "url_citation":
							urlCitation := ann.AsURLCitation()
							citations = append(citations, provider.Citation{
								Type:       provider.CitationTypeURL,
								Provider:   "openai",
								URL:        urlCitation.URL,
								Title:      urlCitation.Title,
								StartIndex: int(urlCitation.StartIndex),
								EndIndex:   int(urlCitation.EndIndex),
							})
						case "file_citation":
							fileCitation := ann.AsFileCitation()
							filename := fileCitation.Filename
							if fn, ok := fileIDToFilename[fileCitation.FileID]; ok {
								filename = fn
							}
							citations = append(citations, provider.Citation{
								Type:       provider.CitationTypeFile,
								Provider:   "openai",
								FileID:     fileCitation.FileID,
								Filename:   filename,
								StartIndex: int(fileCitation.Index),
							})
						}
					}
				}
			}
		}
	}

	return citations
}

// stripCitationMarkers removes OpenAI's inline file citation markers.
// These appear as "fileciteturn2file0turn2file1" in GPT-5 File Search responses.
func stripCitationMarkers(text string) string {
	return citationMarkerPattern.ReplaceAllString(text, "")
}

// supportsPromptCacheRetention checks if the model supports extended prompt cache retention.
func supportsPromptCacheRetention(model string) bool {
	return strings.HasPrefix(model, "gpt-5.")
}

// mapReasoningEffort converts string to SDK enum.
func mapReasoningEffort(effort string) shared.ReasoningEffort {
	switch strings.ToLower(effort) {
	case "none":
		return shared.ReasoningEffort("none")
	case "low":
		return shared.ReasoningEffortLow
	case "medium":
		return shared.ReasoningEffortMedium
	case "high":
		return shared.ReasoningEffortHigh
	default:
		return shared.ReasoningEffortHigh
	}
}

// mapServiceTier converts string to SDK enum.
func mapServiceTier(tier string) responses.ResponseNewParamsServiceTier {
	switch strings.ToLower(tier) {
	case "default":
		return responses.ResponseNewParamsServiceTierDefault
	case "flex":
		return responses.ResponseNewParamsServiceTierFlex
	case "priority":
		return responses.ResponseNewParamsServiceTierPriority
	default:
		return responses.ResponseNewParamsServiceTierAuto
	}
}

// isRetryableError checks if an error should trigger a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 429, 500, 502, 503, 504:
			return true
		case 400, 401, 403, 404, 422:
			return false
		}

		// Check error types
		if apiErr.Type != "" {
			switch apiErr.Type {
			case "rate_limit_error", "server_error", "api_connection_error":
				return true
			case "invalid_request_error", "authentication_error", "permission_error", "not_found_error":
				return false
			}
		}
	}

	errStr := strings.ToLower(err.Error())

	// Network errors that are retryable
	networkErrors := []string{
		"connection",
		"timeout",
		"temporary",
		"no such host",
		"tls handshake",
		"eof",
	}
	for _, netErr := range networkErrors {
		if strings.Contains(errStr, netErr) {
			return true
		}
	}

	// Context errors are not retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
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
