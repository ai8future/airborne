// Package nebius provides the Nebius AI LLM provider implementation.
package nebius

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/compat"
)

const (
	defaultBaseURL = "https://api.studio.nebius.ai/v1"
	defaultModel   = "meta-llama/Meta-Llama-3.1-70B-Instruct"
)

// Client implements the provider.Provider interface for Nebius.
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

// NewClient creates a new Nebius provider client.
func NewClient(opts ...ClientOption) *Client {
	clientOpts := &clientOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(clientOpts)
		}
	}

	config := compat.ProviderConfig{
		Name:               "nebius",
		DefaultBaseURL:     defaultBaseURL,
		DefaultModel:       defaultModel,
		SupportsFileSearch: false,
		SupportsWebSearch:  false,
		SupportsStreaming:  true,
		APIKeyEnvVar:       "NEBIUS_API_KEY",
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
