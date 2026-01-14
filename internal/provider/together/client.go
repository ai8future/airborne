// Package together provides the Together AI LLM provider implementation.
package together

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/compat"
)

const (
	defaultBaseURL = "https://api.together.xyz/v1"
	defaultModel   = "meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo"
)

// Client implements the provider.Provider interface for Together AI.
type Client struct {
	*compat.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {}
}

// NewClient creates a new Together AI provider client.
func NewClient(opts ...ClientOption) *Client {
	config := compat.ProviderConfig{
		Name:               "together",
		DefaultBaseURL:     defaultBaseURL,
		DefaultModel:       defaultModel,
		SupportsFileSearch: false,
		SupportsWebSearch:  false,
		SupportsStreaming:  true,
		APIKeyEnvVar:       "TOGETHER_API_KEY",
	}

	var compatOpts []compat.ClientOption
	return &Client{
		Client: compat.NewClient(config, compatOpts...),
	}
}

var _ provider.Provider = (*Client)(nil)
