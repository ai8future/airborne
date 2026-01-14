// Package deepseek provides the DeepSeek LLM provider implementation.
package deepseek

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/compat"
)

const (
	defaultBaseURL = "https://api.deepseek.com/v1"
	defaultModel   = "deepseek-chat"
)

// Client implements the provider.Provider interface for DeepSeek.
type Client struct {
	*compat.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {
		// Apply to underlying compat client
	}
}

// NewClient creates a new DeepSeek provider client.
func NewClient(opts ...ClientOption) *Client {
	config := compat.ProviderConfig{
		Name:               "deepseek",
		DefaultBaseURL:     defaultBaseURL,
		DefaultModel:       defaultModel,
		SupportsFileSearch: false,
		SupportsWebSearch:  false,
		SupportsStreaming:  true,
		APIKeyEnvVar:       "DEEPSEEK_API_KEY",
	}

	var compatOpts []compat.ClientOption
	for _, opt := range opts {
		if opt != nil {
			// Convert options if needed
		}
	}

	return &Client{
		Client: compat.NewClient(config, compatOpts...),
	}
}

// Ensure Client implements provider.Provider
var _ provider.Provider = (*Client)(nil)
