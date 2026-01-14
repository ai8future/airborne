// Package anthropic provides the Anthropic Claude LLM provider implementation.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ai8future/airborne/internal/httpcapture"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/validation"
)

const (
	maxAttempts     = 3
	requestTimeout  = 3 * time.Minute
	thinkingTimeout = 15 * time.Minute // Extended timeout for thinking operations
	backoffBase     = 250 * time.Millisecond
	defaultModel    = "claude-sonnet-4-20250514"
	// maxHistoryChars limits conversation history to prevent context overflow
	maxHistoryChars = 50000
)

// Client implements the provider.Provider interface using Anthropic's Messages API.
type Client struct {
	debug bool
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose Anthropic payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {
		c.debug = enabled
	}
}

// NewClient creates a new Anthropic provider client.
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
	return "anthropic"
}

// SupportsFileSearch returns false as Anthropic doesn't have native RAG.
func (c *Client) SupportsFileSearch() bool {
	return false
}

// SupportsWebSearch returns false as Anthropic doesn't have native web search.
func (c *Client) SupportsWebSearch() bool {
	return false
}

// SupportsNativeContinuity returns false as Anthropic requires full conversation history.
func (c *Client) SupportsNativeContinuity() bool {
	return false
}

// SupportsStreaming returns true as Anthropic supports streaming.
func (c *Client) SupportsStreaming() bool {
	return true
}

// GenerateReply implements provider.Provider using Anthropic's Messages API.
func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		return provider.GenerateResult{}, errors.New("Anthropic API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Check if thinking is enabled
	thinkingEnabled := cfg.ExtraOptions["thinking_enabled"] == "true"
	includeThoughts := cfg.ExtraOptions["include_thoughts"] == "true"
	var thinkingBudget int
	if budgetStr := cfg.ExtraOptions["thinking_budget"]; budgetStr != "" {
		fmt.Sscanf(budgetStr, "%d", &thinkingBudget)
	}

	// Choose timeout based on thinking mode
	timeout := requestTimeout
	if thinkingEnabled {
		timeout = thinkingTimeout
	}

	// Ensure request has a timeout
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Create capturing transport for debug JSON
	capture := httpcapture.New()

	// Create client
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithHTTPClient(capture.Client()),
	}
	if cfg.BaseURL != "" {
		// SECURITY: Validate base URL to prevent SSRF attacks
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return provider.GenerateResult{}, fmt.Errorf("invalid base URL: %w", err)
		}
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	client := anthropic.NewClient(opts...)

	// Build messages from history and current input
	messages := buildMessages(params.UserInput, params.ConversationHistory)

	// Build request parameters
	maxTokens := int64(4096)
	if cfg.MaxOutputTokens != nil {
		maxTokens = int64(*cfg.MaxOutputTokens)
	}

	reqParams := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		Messages:  messages,
	}

	// Set system prompt
	if params.Instructions != "" {
		reqParams.System = []anthropic.TextBlockParam{
			{Text: params.Instructions},
		}
	}

	// Apply optional parameters
	if cfg.Temperature != nil {
		reqParams.Temperature = anthropic.Float(*cfg.Temperature)
	}
	if cfg.TopP != nil {
		reqParams.TopP = anthropic.Float(*cfg.TopP)
	}

	// Add extended thinking if enabled
	if thinkingEnabled {
		budget := thinkingBudget
		if budget < 1024 {
			budget = 1024
		}
		reqParams.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(budget))
	}

	if c.debug {
		slog.Debug("anthropic request",
			"model", model,
			"thinking_enabled", thinkingEnabled,
			"thinking_budget", thinkingBudget,
			"request_id", params.RequestID,
		)
	}

	// Execute with retry
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		slog.Info("anthropic request",
			"attempt", attempt,
			"model", model,
			"thinking_enabled", thinkingEnabled,
			"request_id", params.RequestID,
		)

		var resp *anthropic.Message
		var err error

		reqCtx, reqCancel := context.WithTimeout(ctx, timeout)

		// Use streaming for thinking operations (required by Anthropic for long operations)
		if thinkingEnabled {
			stream := client.Messages.NewStreaming(reqCtx, reqParams)
			accumulated := anthropic.Message{}
			for stream.Next() {
				event := stream.Current()
				if accErr := accumulated.Accumulate(event); accErr != nil {
					err = fmt.Errorf("stream accumulation error: %w", accErr)
					break
				}
			}
			if stream.Err() != nil {
				err = stream.Err()
			} else if err == nil {
				resp = &accumulated
			}
		} else {
			resp, err = client.Messages.New(reqCtx, reqParams)
		}
		reqCancel()

		if err != nil {
			// Check if parent context is still valid
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				lastErr = fmt.Errorf("anthropic request timeout: %w", err)
				slog.Warn("anthropic timeout, retrying", "attempt", attempt)
				if attempt < maxAttempts {
					sleepWithBackoff(ctx, attempt)
					continue
				}
				return provider.GenerateResult{}, lastErr
			}

			lastErr = fmt.Errorf("anthropic error: %w", err)
			if !isRetryableError(err) {
				return provider.GenerateResult{}, lastErr
			}

			slog.Warn("anthropic retryable error", "attempt", attempt, "error", err)
			if attempt < maxAttempts {
				sleepWithBackoff(ctx, attempt)
				continue
			}
			return provider.GenerateResult{}, lastErr
		}

		// Extract text and thinking from response
		text, thinkingText := extractContent(resp, includeThoughts)
		if text == "" {
			lastErr = errors.New("anthropic returned empty response")
			if attempt < maxAttempts {
				sleepWithBackoff(ctx, attempt)
			}
			continue
		}

		// Optionally prepend thinking to response
		finalText := text
		if includeThoughts && thinkingText != "" {
			finalText = fmt.Sprintf("<details><summary>Claude's Thinking</summary>\n\n%s\n\n</details>\n\n%s", thinkingText, text)
		}

		usage := &provider.Usage{
			InputTokens:  int64(resp.Usage.InputTokens),
			OutputTokens: int64(resp.Usage.OutputTokens),
			TotalTokens:  int64(resp.Usage.InputTokens + resp.Usage.OutputTokens),
		}

		slog.Info("anthropic request completed",
			"model", model,
			"tokens_in", usage.InputTokens,
			"tokens_out", usage.OutputTokens,
		)

		return provider.GenerateResult{
			Text:         finalText,
			ResponseID:   resp.ID,
			Usage:        usage,
			Model:        model,
			RequestJSON:  capture.RequestBody,
			ResponseJSON: capture.ResponseBody,
		}, nil
	}

	return provider.GenerateResult{}, lastErr
}

// GenerateReplyStream implements streaming responses.
func (c *Client) GenerateReplyStream(ctx context.Context, params provider.GenerateParams) (<-chan provider.StreamChunk, error) {
	cfg := params.Config

	// Check if thinking is enabled - use extended timeout
	thinkingEnabled := cfg.ExtraOptions["thinking_enabled"] == "true"
	timeout := requestTimeout
	if thinkingEnabled {
		timeout = thinkingTimeout
	}

	// Ensure request has a timeout
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}

	// Helper to clean up cancel on error returns
	cleanup := func() {
		if cancel != nil {
			cancel()
		}
	}

	if strings.TrimSpace(cfg.APIKey) == "" {
		cleanup()
		return nil, errors.New("Anthropic API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Create capturing transport for debug JSON
	capture := httpcapture.New()

	// Create client
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithHTTPClient(capture.Client()),
	}
	if cfg.BaseURL != "" {
		// SECURITY: Validate base URL to prevent SSRF attacks
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			cleanup()
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	client := anthropic.NewClient(opts...)

	// Build messages
	messages := buildMessages(params.UserInput, params.ConversationHistory)

	maxTokens := int64(4096)
	if cfg.MaxOutputTokens != nil {
		maxTokens = int64(*cfg.MaxOutputTokens)
	}

	reqParams := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		Messages:  messages,
	}

	if params.Instructions != "" {
		reqParams.System = []anthropic.TextBlockParam{
			{Text: params.Instructions},
		}
	}

	if cfg.Temperature != nil {
		reqParams.Temperature = anthropic.Float(*cfg.Temperature)
	}
	if cfg.TopP != nil {
		reqParams.TopP = anthropic.Float(*cfg.TopP)
	}

	// Add extended thinking if enabled
	if thinkingEnabled {
		var budget int
		if budgetStr := cfg.ExtraOptions["thinking_budget"]; budgetStr != "" {
			fmt.Sscanf(budgetStr, "%d", &budget)
		}
		if budget < 1024 {
			budget = 1024
		}
		reqParams.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(budget))
	}

	ch := make(chan provider.StreamChunk, 100)

	go func() {
		defer close(ch)
		if cancel != nil {
			defer cancel()
		}

		stream := client.Messages.NewStreaming(ctx, reqParams)
		message := anthropic.Message{}

		for stream.Next() {
			event := stream.Current()
			_ = message.Accumulate(event)

			switch eventVariant := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				switch deltaVariant := eventVariant.Delta.AsAny().(type) {
				case anthropic.TextDelta:
					ch <- provider.StreamChunk{
						Type: provider.ChunkTypeText,
						Text: deltaVariant.Text,
					}
				}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- provider.StreamChunk{
				Type:      provider.ChunkTypeError,
				Error:     err,
				Retryable: isRetryableError(err),
			}
			return
		}

		usage := &provider.Usage{
			InputTokens:  int64(message.Usage.InputTokens),
			OutputTokens: int64(message.Usage.OutputTokens),
			TotalTokens:  int64(message.Usage.InputTokens + message.Usage.OutputTokens),
		}

		ch <- provider.StreamChunk{
			Type:       provider.ChunkTypeComplete,
			ResponseID: message.ID,
			Model:      model,
			Usage:      usage,
		}
	}()

	return ch, nil
}

// buildMessages builds conversation messages from history and current input.
func buildMessages(userInput string, history []provider.Message) []anthropic.MessageParam {
	var messages []anthropic.MessageParam

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

		if msg.Role == "assistant" {
			messages = append(messages, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(trimmed),
			))
		} else {
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(trimmed),
			))
		}
	}

	// Add current user input
	messages = append(messages, anthropic.NewUserMessage(
		anthropic.NewTextBlock(strings.TrimSpace(userInput)),
	))

	// Ensure messages start with user (Claude requirement)
	if len(messages) > 0 && messages[0].Role != anthropic.MessageParamRoleUser {
		messages = append([]anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("[continuing conversation]")),
		}, messages...)
	}

	return messages
}

// extractContent extracts text and thinking from the response content blocks.
func extractContent(resp *anthropic.Message, includeThinking bool) (text, thinking string) {
	var textParts []string
	var thinkingParts []string

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "thinking":
			if includeThinking {
				thinkingParts = append(thinkingParts, block.Thinking)
			}
		}
	}

	return strings.TrimSpace(strings.Join(textParts, "\n")), strings.Join(thinkingParts, "\n")
}

// extractText extracts text from the response.
func extractText(resp *anthropic.Message) string {
	if resp == nil {
		return ""
	}
	var text strings.Builder
	for _, block := range resp.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			text.WriteString(v.Text)
		}
	}
	return strings.TrimSpace(text.String())
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
	authErrors := []string{"401", "403", "invalid_api_key", "authentication", "permission_denied"}
	for _, authErr := range authErrors {
		if strings.Contains(errLower, authErr) {
			return false
		}
	}

	// Don't retry invalid request errors
	invalidErrors := []string{"400", "invalid_request", "malformed"}
	for _, invErr := range invalidErrors {
		if strings.Contains(errLower, invErr) {
			return false
		}
	}

	// Retry rate limit and server errors
	if strings.Contains(errStr, "429") || strings.Contains(errLower, "overloaded") ||
		strings.Contains(errLower, "rate") {
		return true
	}
	if strings.Contains(errStr, "500") || strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") || strings.Contains(errStr, "504") ||
		strings.Contains(errStr, "529") {
		return true
	}

	// Retry network errors
	networkErrors := []string{"connection", "timeout", "eof"}
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
