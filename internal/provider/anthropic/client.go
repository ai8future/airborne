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

	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/validation"
)

const (
	maxAttempts    = 3
	requestTimeout = 3 * time.Minute
	backoffBase    = 250 * time.Millisecond
	defaultModel   = "claude-sonnet-4-20250514"
)

// Client implements the provider.Provider interface using Anthropic's Messages API.
type Client struct{}

// NewClient creates a new Anthropic provider client.
func NewClient() *Client {
	return &Client{}
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
	// Ensure request has a timeout
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}

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

	// Create client
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
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

	// Execute with retry
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		slog.Info("anthropic request",
			"attempt", attempt,
			"model", model,
			"request_id", params.RequestID,
		)

		reqCtx, reqCancel := context.WithTimeout(ctx, requestTimeout)
		resp, err := client.Messages.New(reqCtx, reqParams)
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

		// Extract text from response
		text := extractText(resp)
		if text == "" {
			lastErr = errors.New("anthropic returned empty response")
			continue
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
			Text:  text,
			Usage: usage,
			Model: model,
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

	// Helper to clean up cancel on error returns
	cleanup := func() {
		if cancel != nil {
			cancel()
		}
	}

	cfg := params.Config

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

	// Create client
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
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
			Type:  provider.ChunkTypeComplete,
			Model: model,
			Usage: usage,
		}
	}()

	return ch, nil
}

// buildMessages builds conversation messages from history and current input.
func buildMessages(userInput string, history []provider.Message) []anthropic.MessageParam {
	var messages []anthropic.MessageParam

	// Add conversation history
	for _, msg := range history {
		if msg.Role == "assistant" {
			messages = append(messages, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(msg.Content),
			))
		} else {
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(msg.Content),
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
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "529") ||
		strings.Contains(errStr, "overloaded") ||
		strings.Contains(errStr, "rate limit") {
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
