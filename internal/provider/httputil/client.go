package httputil

import (
	"fmt"
	"net/http"

	"github.com/ai8future/airborne/internal/httpcapture"
	"github.com/ai8future/airborne/internal/validation"
)

// CapturedClientConfig holds validated configuration for provider clients.
type CapturedClientConfig struct {
	APIKey     string
	BaseURL    string // Empty means use provider default
	HTTPClient *http.Client
	Capture    *httpcapture.Transport
}

// NewCapturedClientConfig validates and creates a client configuration with HTTP capture.
// Callers convert this to provider-specific SDK options.
func NewCapturedClientConfig(apiKey, baseURL string) (*CapturedClientConfig, error) {
	// Validate API key
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	// Validate base URL if provided
	if baseURL != "" {
		if err := validation.ValidateProviderURL(baseURL); err != nil {
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
	}

	// Create HTTP capture
	capture := httpcapture.New()

	return &CapturedClientConfig{
		APIKey:     apiKey,
		BaseURL:    baseURL,
		HTTPClient: capture.Client(),
		Capture:    capture,
	}, nil
}
