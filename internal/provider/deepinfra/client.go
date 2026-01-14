// Package deepinfra provides the DeepInfra LLM provider implementation.
package deepinfra

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/compat"
)

const (
	defaultBaseURL = "https://api.deepinfra.com/v1/openai"
	defaultModel   = "meta-llama/Meta-Llama-3.1-70B-Instruct"
)

// Client implements the provider.Provider interface for DeepInfra.
type Client struct {
	*compat.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {}
}

// NewClient creates a new DeepInfra provider client.
func NewClient(opts ...ClientOption) *Client {
	config := compat.ProviderConfig{
		Name:               "deepinfra",
		DefaultBaseURL:     defaultBaseURL,
		DefaultModel:       defaultModel,
		SupportsFileSearch: false,
		SupportsWebSearch:  false,
		SupportsStreaming:  true,
		APIKeyEnvVar:       "DEEPINFRA_API_KEY",
	}

	var compatOpts []compat.ClientOption
	return &Client{
		Client: compat.NewClient(config, compatOpts...),
	}
}

var _ provider.Provider = (*Client)(nil)
