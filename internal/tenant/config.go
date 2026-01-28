package tenant

import "sort"

// TenantConfig defines per-tenant overrides loaded from JSON/YAML files.
type TenantConfig struct {
	TenantID        string                    `json:"tenant_id" yaml:"tenant_id"`
	DisplayName     string                    `json:"display_name" yaml:"display_name"`
	Providers       map[string]ProviderConfig `json:"providers" yaml:"providers"`
	RateLimits      RateLimitConfig           `json:"rate_limits" yaml:"rate_limits"`
	Failover        FailoverConfig            `json:"failover" yaml:"failover"`
	ImageGeneration ImageGenerationConfig     `json:"image_generation" yaml:"image_generation"`
	Metadata        map[string]string         `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// ImageGenerationConfig holds settings for AI image generation.
type ImageGenerationConfig struct {
	Enabled         bool     `json:"enabled" yaml:"enabled"`
	Provider        string   `json:"provider" yaml:"provider"`                   // "gemini" or "openai"
	Model           string   `json:"model,omitempty" yaml:"model,omitempty"`     // e.g., "gemini-2.5-flash-image", "dall-e-3"
	TriggerPhrases  []string `json:"trigger_phrases" yaml:"trigger_phrases"`     // e.g., ["@image", "generate image"]
	FallbackOnError bool     `json:"fallback_on_error" yaml:"fallback_on_error"` // Continue with text on failure
	MaxImages       int      `json:"max_images,omitempty" yaml:"max_images,omitempty"`
}

// ProviderConfig holds per-tenant provider settings.
type ProviderConfig struct {
	Enabled         bool              `json:"enabled" yaml:"enabled"`
	APIKey          string            `json:"api_key" yaml:"api_key"` // Can use ENV= or FILE= prefix
	Model           string            `json:"model" yaml:"model"`
	Temperature     *float64          `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	TopP            *float64          `json:"top_p,omitempty" yaml:"top_p,omitempty"`
	MaxOutputTokens *int              `json:"max_output_tokens,omitempty" yaml:"max_output_tokens,omitempty"`
	BaseURL         string            `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	ExtraOptions    map[string]string `json:"extra_options,omitempty" yaml:"extra_options,omitempty"`
}

// RateLimitConfig holds per-tenant rate limits.
type RateLimitConfig struct {
	RequestsPerMinute int `json:"rpm" yaml:"rpm"`
	RequestsPerDay    int `json:"rpd" yaml:"rpd"`
	TokensPerMinute   int `json:"tpm" yaml:"tpm"`
}

// FailoverConfig holds per-tenant failover settings.
type FailoverConfig struct {
	Enabled bool     `json:"enabled" yaml:"enabled"`
	Order   []string `json:"order" yaml:"order"`
}

// GetProvider returns the provider config for a given provider name.
// Returns the config and whether it exists and is enabled.
func (tc *TenantConfig) GetProvider(name string) (ProviderConfig, bool) {
	cfg, ok := tc.Providers[name]
	if !ok || !cfg.Enabled {
		return ProviderConfig{}, false
	}
	return cfg, true
}

// DefaultProvider returns the first enabled provider from the failover order,
// or the first enabled provider if no failover order is set.
func (tc *TenantConfig) DefaultProvider() (string, ProviderConfig, bool) {
	// Check failover order first
	if tc.Failover.Enabled && len(tc.Failover.Order) > 0 {
		for _, name := range tc.Failover.Order {
			if cfg, ok := tc.GetProvider(name); ok {
				return name, cfg, true
			}
		}
	}

	// Fall back to first enabled provider (deterministic order)
	names := make([]string, 0, len(tc.Providers))
	for name := range tc.Providers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cfg := tc.Providers[name]
		if cfg.Enabled {
			return name, cfg, true
		}
	}

	return "", ProviderConfig{}, false
}
