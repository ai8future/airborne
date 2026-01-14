// Package cohere provides the Cohere LLM provider implementation.
package cohere

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/compat"
)

const (
	defaultBaseURL = "https://api.cohere.ai/v1"
	defaultModel   = "command-r-plus"
)

// Client implements the provider.Provider interface for Cohere.
type Client struct {
	*compat.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {}
}

// NewClient creates a new Cohere provider client.
func NewClient(opts ...ClientOption) *Client {
	config := compat.ProviderConfig{
		Name:               "cohere",
		DefaultBaseURL:     defaultBaseURL,
		DefaultModel:       defaultModel,
		SupportsFileSearch: false,
		SupportsWebSearch:  true, // Cohere has connectors for web search
		SupportsStreaming:  true,
		APIKeyEnvVar:       "COHERE_API_KEY",
	}

	var compatOpts []compat.ClientOption
	return &Client{
		Client: compat.NewClient(config, compatOpts...),
	}
}

var _ provider.Provider = (*Client)(nil)
