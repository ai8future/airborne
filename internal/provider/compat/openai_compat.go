// Package compat provides OpenAI-compatible provider implementations.
// Many AI providers use OpenAI-compatible APIs, allowing code reuse.
package compat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/ai8future/airborne/internal/httpcapture"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/retry"
	"github.com/ai8future/airborne/internal/validation"
)

// Note: retry.MaxAttempts, retry.RequestTimeout, and backoffBase constants are defined in the retry package

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
	ctx, cancel := retry.EnsureTimeout(ctx, retry.RequestTimeout)
	defer cancel()

	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		return provider.GenerateResult{}, fmt.Errorf("%s API key is required", c.config.Name)
	}

	model := provider.SelectModel(cfg.Model, c.config.DefaultModel, params.OverrideModel)

	// Determine base URL
	baseURL := c.config.DefaultBaseURL
	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return provider.GenerateResult{}, fmt.Errorf("invalid base URL: %w", err)
		}
		baseURL = cfg.BaseURL
	}

	// Create capturing transport for debug JSON (always enabled for admin dashboard)
	capture := httpcapture.New()
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
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		slog.Info(fmt.Sprintf("%s request", c.config.Name),
			"attempt", attempt,
			"model", model,
			"request_id", params.RequestID,
		)

		reqCtx, reqCancel := context.WithTimeout(ctx, retry.RequestTimeout)
		resp, err := client.Chat.Completions.New(reqCtx, reqParams)
		reqCancel()

		if err != nil {
			// Check if parent context is still valid
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				lastErr = fmt.Errorf("%s request timeout: %w", c.config.Name, err)
				slog.Warn(fmt.Sprintf("%s timeout, retrying", c.config.Name), "attempt", attempt)
				if attempt < retry.MaxAttempts {
					retry.SleepWithBackoff(ctx, attempt)
					continue
				}
				return provider.GenerateResult{}, lastErr
			}

			lastErr = fmt.Errorf("%s error: %w", c.config.Name, err)
			if !retry.IsRetryable(err) {
				return provider.GenerateResult{}, lastErr
			}

			slog.Warn(fmt.Sprintf("%s retryable error", c.config.Name), "attempt", attempt, "error", err)
			if attempt < retry.MaxAttempts {
				retry.SleepWithBackoff(ctx, attempt)
				continue
			}
			return provider.GenerateResult{}, lastErr
		}

		// Extract text
		text := extractText(resp)
		if text == "" {
			lastErr = fmt.Errorf("%s returned empty response", c.config.Name)
			if attempt < retry.MaxAttempts {
				retry.SleepWithBackoff(ctx, attempt)
			}
			continue
		}

		usage := extractUsage(resp)

		slog.Info(fmt.Sprintf("%s request completed", c.config.Name),
			"model", model,
			"tokens_in", usage.InputTokens,
			"tokens_out", usage.OutputTokens,
		)

		var reqJSON, respJSON []byte
		if capture != nil {
			reqJSON = capture.RequestBody
			respJSON = capture.ResponseBody
		}

		return provider.GenerateResult{
			Text:         text,
			Usage:        usage,
			Model:        resp.Model,
			RequestJSON:  reqJSON,
			ResponseJSON: respJSON,
		}, nil
	}

	return provider.GenerateResult{}, lastErr
}

// GenerateReplyStream implements streaming responses.
func (c *Client) GenerateReplyStream(ctx context.Context, params provider.GenerateParams) (<-chan provider.StreamChunk, error) {
	// Ensure request has a timeout
	ctx, cancel := retry.EnsureTimeout(ctx, retry.RequestTimeout)

	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		cancel()
		return nil, fmt.Errorf("%s API key is required", c.config.Name)
	}

	model := provider.SelectModel(cfg.Model, c.config.DefaultModel, params.OverrideModel)

	// Determine base URL
	baseURL := c.config.DefaultBaseURL
	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			cancel()
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

	if c.debug {
		slog.Debug(fmt.Sprintf("%s streaming request", c.config.Name),
			"model", model,
			"base_url", baseURL,
			"request_id", params.RequestID,
		)
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
				Retryable: retry.IsRetryable(err),
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

