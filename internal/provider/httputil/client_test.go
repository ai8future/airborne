package httputil

import (
	"strings"
	"testing"
)

func TestNewCapturedClientConfig(t *testing.T) {
	tests := []struct {
		name        string
		apiKey      string
		baseURL     string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid config with base url",
			apiKey:  "test-key-123",
			baseURL: "https://api.openai.com",
			wantErr: false,
		},
		{
			name:        "empty api key",
			apiKey:      "",
			baseURL:     "https://api.openai.com",
			wantErr:     true,
			errContains: "API key is required",
		},
		{
			name:        "invalid base url scheme",
			apiKey:      "test-key",
			baseURL:     "ftp://api.openai.com",
			wantErr:     true,
			errContains: "invalid base URL",
		},
		{
			name:        "invalid base url format",
			apiKey:      "test-key",
			baseURL:     "not-a-url",
			wantErr:     true,
			errContains: "invalid base URL",
		},
		{
			name:    "empty base url is valid",
			apiKey:  "test-key",
			baseURL: "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := NewCapturedClientConfig(tt.apiKey, tt.baseURL)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.APIKey != tt.apiKey {
				t.Errorf("APIKey = %q, want %q", cfg.APIKey, tt.apiKey)
			}

			if cfg.BaseURL != tt.baseURL {
				t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, tt.baseURL)
			}

			if cfg.HTTPClient == nil {
				t.Error("HTTPClient is nil")
			}

			if cfg.Capture == nil {
				t.Error("Capture is nil")
			}
		})
	}
}
