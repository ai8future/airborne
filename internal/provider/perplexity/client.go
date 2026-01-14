// Package perplexity provides the Perplexity AI LLM provider implementation.
// Perplexity specializes in search-augmented responses.
package perplexity

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/compat"
)

const (
	defaultBaseURL = "https://api.perplexity.ai"
	defaultModel   = "llama-3.1-sonar-large-128k-online"
)

// Client implements the provider.Provider interface for Perplexity.
type Client struct {
	*compat.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {}
}

// NewClient creates a new Perplexity provider client.
func NewClient(opts ...ClientOption) *Client {
	config := compat.ProviderConfig{
		Name:               "perplexity",
		DefaultBaseURL:     defaultBaseURL,
		DefaultModel:       defaultModel,
		SupportsFileSearch: false,
		SupportsWebSearch:  true, // Perplexity has built-in web search
		SupportsStreaming:  true,
		APIKeyEnvVar:       "PERPLEXITY_API_KEY",
	}

	var compatOpts []compat.ClientOption
	return &Client{
		Client: compat.NewClient(config, compatOpts...),
	}
}

var _ provider.Provider = (*Client)(nil)
