// Package grok provides the xAI Grok LLM provider implementation.
package grok

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/compat"
)

const (
	defaultBaseURL = "https://api.x.ai/v1"
	defaultModel   = "grok-2-latest"
)

// Client implements the provider.Provider interface for Grok.
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

// NewClient creates a new Grok provider client.
func NewClient(opts ...ClientOption) *Client {
	clientOpts := &clientOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(clientOpts)
		}
	}

	config := compat.ProviderConfig{
		Name:               "grok",
		DefaultBaseURL:     defaultBaseURL,
		DefaultModel:       defaultModel,
		SupportsFileSearch: false,
		SupportsWebSearch:  false,
		SupportsStreaming:  true,
		APIKeyEnvVar:       "XAI_API_KEY",
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
