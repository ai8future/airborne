package config

import (
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/tenant"
	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
)

// Builder builds provider configurations by merging tenant defaults with request overrides.
type Builder struct{}

// NewBuilder creates a config builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Build creates a provider config by merging tenant defaults with request overrides.
// Request overrides take precedence except for API keys (security constraint).
func (b *Builder) Build(
	providerName string,
	tenantCfg *tenant.TenantConfig,
	requestCfg *pb.ProviderConfig,
) provider.ProviderConfig {
	cfg := provider.ProviderConfig{}

	// Apply tenant defaults
	if tenantCfg != nil {
		if pCfg, ok := tenantCfg.GetProvider(providerName); ok {
			cfg.APIKey = pCfg.APIKey
			cfg.Model = pCfg.Model
			cfg.Temperature = pCfg.Temperature
			cfg.TopP = pCfg.TopP
			cfg.MaxOutputTokens = pCfg.MaxOutputTokens
			cfg.BaseURL = pCfg.BaseURL

			// SECURITY: Deep copy ExtraOptions to prevent data races and tenant data leakage
			// Maps are reference types - direct assignment would share mutable state across goroutines
			if pCfg.ExtraOptions != nil {
				cfg.ExtraOptions = make(map[string]string, len(pCfg.ExtraOptions))
				for k, v := range pCfg.ExtraOptions {
					cfg.ExtraOptions[k] = v
				}
			}
		}
	}

	// Apply request overrides (except API key for security)
	if requestCfg != nil {
		// SECURITY: API keys must come from server-side tenant config, not requests
		// Commenting out to document this security constraint
		// if requestCfg.ApiKey != "" {
		// 	cfg.APIKey = requestCfg.ApiKey
		// }

		if requestCfg.Model != "" {
			cfg.Model = requestCfg.Model
		}

		if requestCfg.Temperature != nil {
			cfg.Temperature = requestCfg.Temperature
		}

		if requestCfg.TopP != nil {
			cfg.TopP = requestCfg.TopP
		}

		if requestCfg.MaxOutputTokens != nil {
			maxTokens := int(*requestCfg.MaxOutputTokens)
			cfg.MaxOutputTokens = &maxTokens
		}

		if requestCfg.BaseUrl != "" {
			cfg.BaseURL = requestCfg.BaseUrl
		}

		// Merge extra options (additive, request overrides tenant for same keys)
		if len(requestCfg.ExtraOptions) > 0 {
			if cfg.ExtraOptions == nil {
				cfg.ExtraOptions = make(map[string]string)
			}
			for k, v := range requestCfg.ExtraOptions {
				cfg.ExtraOptions[k] = v
			}
		}
	}

	return cfg
}
