// Package compat provides OpenAI-compatible provider implementations.
// Many AI providers use OpenAI-compatible APIs, allowing code reuse.
package compat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/ai8future/airborne/internal/httpcapture"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/validation"
)

const (
	maxAttempts    = 3
	requestTimeout = 3 * time.Minute
	backoffBase    = 250 * time.Millisecond
)

// ProviderConfig contains configuration for an OpenAI-compatible provider.
type ProviderConfig struct {
	// Name is the provider identifier (e.g., "deepseek", "grok")
	Name string

	// DefaultBaseURL is the provider's API endpoint
	DefaultBaseURL string

	// DefaultModel is the default model to use
	DefaultModel string

	// SupportsFileSearch indicates if the provider supports file search
	SupportsFileSearch bool

	// SupportsWebSearch indicates if the provider supports web search
	SupportsWebSearch bool

	// SupportsStreaming indicates if the provider supports streaming
	SupportsStreaming bool

	// APIKeyEnvVar is the environment variable name for the API key (for docs)
	APIKeyEnvVar string
}

// Client implements provider.Provider for OpenAI-compatible APIs.
type Client struct {
	config ProviderConfig
	debug  bool
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {
		c.debug = enabled
	}
}

// NewClient creates a new OpenAI-compatible provider client.
func NewClient(config ProviderConfig, opts ...ClientOption) *Client {
	c := &Client{config: config}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// Name returns the provider identifier.
func (c *Client) Name() string {
	return c.config.Name
}

// SupportsFileSearch returns whether the provider supports file search.
func (c *Client) SupportsFileSearch() bool {
	return c.config.SupportsFileSearch
}

// SupportsWebSearch returns whether the provider supports web search.
func (c *Client) SupportsWebSearch() bool {
	return c.config.SupportsWebSearch
}

// SupportsNativeContinuity returns false - most don't support this.
func (c *Client) SupportsNativeContinuity() bool {
	return false
}

// SupportsStreaming returns whether the provider supports streaming.
func (c *Client) SupportsStreaming() bool {
	return c.config.SupportsStreaming
}

// GenerateReply implements provider.Provider using OpenAI-compatible Chat Completions API.
func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
	// Ensure request has a timeout
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}

	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		return provider.GenerateResult{}, fmt.Errorf("%s API key is required", c.config.Name)
	}

	model := cfg.Model
	if model == "" {
		model = c.config.DefaultModel
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Determine base URL
	baseURL := c.config.DefaultBaseURL
	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return provider.GenerateResult{}, fmt.Errorf("invalid base URL: %w", err)
		}
		baseURL = cfg.BaseURL
	}

	// Create capturing transport for debug JSON
	capture := httpcapture.New()

	// Create client
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(capture.Client()),
	}

	client := openai.NewClient(opts...)

	// Build messages
	messages := buildMessages(params.Instructions, params.UserInput, params.ConversationHistory)

	// Build request
	reqParams := openai.ChatCompletionNewParams{
		Model:    model,
		Messages: messages,
	}

	// Apply optional parameters
	if cfg.Temperature != nil {
		reqParams.Temperature = openai.Float(*cfg.Temperature)
	}
	if cfg.TopP != nil {
		reqParams.TopP = openai.Float(*cfg.TopP)
	}
	if cfg.MaxOutputTokens != nil {
		reqParams.MaxTokens = openai.Int(int64(*cfg.MaxOutputTokens))
	}

	if c.debug {
		slog.Debug(fmt.Sprintf("%s request", c.config.Name),
			"model", model,
			"base_url", baseURL,
			"request_id", params.RequestID,
		)
	}

	// Execute with retry
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		slog.Info(fmt.Sprintf("%s request", c.config.Name),
			"attempt", attempt,
			"model", model,
			"request_id", params.RequestID,
		)

		reqCtx, reqCancel := context.WithTimeout(ctx, requestTimeout)
		resp, err := client.Chat.Completions.New(reqCtx, reqParams)
		reqCancel()

		if err != nil {
			// Check if parent context is still valid
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				lastErr = fmt.Errorf("%s request timeout: %w", c.config.Name, err)
				slog.Warn(fmt.Sprintf("%s timeout, retrying", c.config.Name), "attempt", attempt)
				if attempt < maxAttempts {
					sleepWithBackoff(ctx, attempt)
					continue
				}
				return provider.GenerateResult{}, lastErr
			}

			lastErr = fmt.Errorf("%s error: %w", c.config.Name, err)
			if !isRetryableError(err) {
				return provider.GenerateResult{}, lastErr
			}

			slog.Warn(fmt.Sprintf("%s retryable error", c.config.Name), "attempt", attempt, "error", err)
			if attempt < maxAttempts {
				sleepWithBackoff(ctx, attempt)
				continue
			}
			return provider.GenerateResult{}, lastErr
		}

		// Extract text
		text := extractText(resp)
		if text == "" {
			lastErr = fmt.Errorf("%s returned empty response", c.config.Name)
			if attempt < maxAttempts {
				sleepWithBackoff(ctx, attempt)
			}
			continue
		}

		usage := extractUsage(resp)

		slog.Info(fmt.Sprintf("%s request completed", c.config.Name),
			"model", model,
			"tokens_in", usage.InputTokens,
			"tokens_out", usage.OutputTokens,
		)

		return provider.GenerateResult{
			Text:         text,
			Usage:        usage,
			Model:        resp.Model,
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

	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("%s API key is required", c.config.Name)
	}

	model := cfg.Model
	if model == "" {
		model = c.config.DefaultModel
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Determine base URL
	baseURL := c.config.DefaultBaseURL
	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			if cancel != nil {
				cancel()
			}
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
		baseURL = cfg.BaseURL
	}

	// Create client
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(baseURL),
	}
	client := openai.NewClient(opts...)

	// Build messages
	messages := buildMessages(params.Instructions, params.UserInput, params.ConversationHistory)

	// Build request
	reqParams := openai.ChatCompletionNewParams{
		Model:    model,
		Messages: messages,
	}

	if cfg.Temperature != nil {
		reqParams.Temperature = openai.Float(*cfg.Temperature)
	}
	if cfg.TopP != nil {
		reqParams.TopP = openai.Float(*cfg.TopP)
	}
	if cfg.MaxOutputTokens != nil {
		reqParams.MaxTokens = openai.Int(int64(*cfg.MaxOutputTokens))
	}

	ch := make(chan provider.StreamChunk, 100)

	go func() {
		defer close(ch)
		if cancel != nil {
			defer cancel()
		}

		stream := client.Chat.Completions.NewStreaming(ctx, reqParams)
		var fullText strings.Builder
		var usage *provider.Usage

		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				text := chunk.Choices[0].Delta.Content
				fullText.WriteString(text)
				ch <- provider.StreamChunk{
					Type: provider.ChunkTypeText,
					Text: text,
				}
			}

			// Capture usage if available
			if chunk.Usage.TotalTokens > 0 {
				usage = &provider.Usage{
					InputTokens:  int64(chunk.Usage.PromptTokens),
					OutputTokens: int64(chunk.Usage.CompletionTokens),
					TotalTokens:  int64(chunk.Usage.TotalTokens),
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

		ch <- provider.StreamChunk{
			Type:  provider.ChunkTypeComplete,
			Model: model,
			Usage: usage,
		}
	}()

	return ch, nil
}

// buildMessages constructs the message array for chat completion.
func buildMessages(instructions, userInput string, history []provider.Message) []openai.ChatCompletionMessageParamUnion {
	var messages []openai.ChatCompletionMessageParamUnion

	// Add system instruction
	if instructions != "" {
		messages = append(messages, openai.SystemMessage(instructions))
	}

	// Add conversation history
	for _, msg := range history {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if msg.Role == "assistant" {
			messages = append(messages, openai.AssistantMessage(content))
		} else {
			messages = append(messages, openai.UserMessage(content))
		}
	}

	// Add current user input
	messages = append(messages, openai.UserMessage(strings.TrimSpace(userInput)))

	return messages
}

// extractText extracts text from the response.
func extractText(resp *openai.ChatCompletion) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content)
}

// extractUsage extracts token usage from the response.
func extractUsage(resp *openai.ChatCompletion) *provider.Usage {
	if resp == nil {
		return &provider.Usage{}
	}
	return &provider.Usage{
		InputTokens:  int64(resp.Usage.PromptTokens),
		OutputTokens: int64(resp.Usage.CompletionTokens),
		TotalTokens:  int64(resp.Usage.TotalTokens),
	}
}

// isRetryableError checks if an error should trigger a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	errStr := err.Error()
	errLower := strings.ToLower(errStr)

	// Don't retry auth errors
	authErrors := []string{"401", "403", "invalid_api_key", "authentication", "unauthorized"}
	for _, authErr := range authErrors {
		if strings.Contains(errLower, authErr) {
			return false
		}
	}

	// Don't retry invalid request errors
	invalidErrors := []string{"400", "invalid_request", "malformed", "validation"}
	for _, invErr := range invalidErrors {
		if strings.Contains(errLower, invErr) {
			return false
		}
	}

	// Retry rate limit and server errors
	if strings.Contains(errStr, "429") || strings.Contains(errLower, "rate") {
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
