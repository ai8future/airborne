// Package openrouter provides the OpenRouter LLM provider implementation.
// OpenRouter is a unified API that routes to multiple AI providers.
package openrouter

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/compat"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"
	defaultModel   = "anthropic/claude-3.5-sonnet"
)

// Client implements the provider.Provider interface for OpenRouter.
type Client struct {
	*compat.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {}
}

// NewClient creates a new OpenRouter provider client.
func NewClient(opts ...ClientOption) *Client {
	config := compat.ProviderConfig{
		Name:               "openrouter",
		DefaultBaseURL:     defaultBaseURL,
		DefaultModel:       defaultModel,
		SupportsFileSearch: false,
		SupportsWebSearch:  false,
		SupportsStreaming:  true,
		APIKeyEnvVar:       "OPENROUTER_API_KEY",
	}

	var compatOpts []compat.ClientOption
	return &Client{
		Client: compat.NewClient(config, compatOpts...),
	}
}

var _ provider.Provider = (*Client)(nil)
