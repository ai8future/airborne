// Package mistral provides the Mistral AI LLM provider implementation.
package mistral

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/compat"
)

const (
	defaultBaseURL = "https://api.mistral.ai/v1"
	defaultModel   = "mistral-large-latest"
)

// Client implements the provider.Provider interface for Mistral.
type Client struct {
	*compat.Client
}

// ClientOption configures a Client.
type ClientOption func(*clientOptions)

type clientOptions struct {
	debug bool
}

// WithDebugLogging enables verbose payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(opts *clientOptions) {
		opts.debug = enabled
	}
}

// NewClient creates a new Mistral provider client.
func NewClient(opts ...ClientOption) *Client {
	clientOpts := &clientOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(clientOpts)
		}
	}

	config := compat.ProviderConfig{
		Name:               "mistral",
		DefaultBaseURL:     defaultBaseURL,
		DefaultModel:       defaultModel,
		SupportsFileSearch: false,
		SupportsWebSearch:  false,
		SupportsStreaming:  true,
		APIKeyEnvVar:       "MISTRAL_API_KEY",
	}

	var compatOpts []compat.ClientOption
	if clientOpts.debug {
		compatOpts = append(compatOpts, compat.WithDebugLogging(true))
	}

	return &Client{
		Client: compat.NewClient(config, compatOpts...),
	}
}

var _ provider.Provider = (*Client)(nil)
