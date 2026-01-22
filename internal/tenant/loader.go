package tenant

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// loadTenants loads all tenant configurations from the given directory.
// Supports both JSON (.json) and YAML (.yaml, .yml) files.
func loadTenants(dir string) (map[string]TenantConfig, error) {
	return loadTenantsInternal(dir, true)
}

// loadTenantsInternal is the core loader with optional secret resolution.
func loadTenantsInternal(dir string, resolveSecretsFlag bool) (map[string]TenantConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading tenant config dir: %w", err)
	}

	result := make(map[string]TenantConfig, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		// Skip non-config files
		if ext != ".json" && ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}

		var cfg TenantConfig
		switch ext {
		case ".json":
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("decoding %s: %w", path, err)
			}
		case ".yaml", ".yml":
			if err := yaml.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("decoding %s: %w", path, err)
			}
		}

		// Normalize tenant ID to lowercase
		cfg.TenantID = strings.ToLower(strings.TrimSpace(cfg.TenantID))

		// Skip files without tenant_id (e.g., shared config files)
		if cfg.TenantID == "" {
			continue
		}

		// Resolve secrets (ENV=, FILE= patterns) if requested
		if resolveSecretsFlag {
			if err := resolveSecrets(&cfg); err != nil {
				return nil, fmt.Errorf("resolving secrets for %s: %w", path, err)
			}
		}

		// Validate (skip secret validation if not resolving)
		if err := validateTenantConfig(&cfg); err != nil {
			return nil, fmt.Errorf("validating %s: %w", path, err)
		}

		// Check for duplicates
		if _, exists := result[cfg.TenantID]; exists {
			return nil, fmt.Errorf("duplicate tenant_id %q", cfg.TenantID)
		}

		result[cfg.TenantID] = cfg
	}

	if len(result) == 0 {
		return nil, errors.New("no tenant configs found")
	}

	return result, nil
}

// validateTenantConfig validates a tenant configuration.
func validateTenantConfig(cfg *TenantConfig) error {
	// Validate tenant ID
	if cfg.TenantID == "" {
		return errors.New("tenant_id is required")
	}
	if len(cfg.TenantID) > 64 {
		return errors.New("tenant_id must be <= 64 characters")
	}

	// Validate at least one provider is configured and enabled
	hasProvider := false
	for name, pCfg := range cfg.Providers {
		if !pCfg.Enabled {
			continue
		}
		hasProvider = true

		// Validate API key is set
		if pCfg.APIKey == "" {
			return fmt.Errorf("%s.api_key is required when provider is enabled", name)
		}

		// Validate model is set
		if pCfg.Model == "" {
			return fmt.Errorf("%s.model is required when provider is enabled", name)
		}

		// Validate temperature if set
		if pCfg.Temperature != nil {
			if *pCfg.Temperature < 0 || *pCfg.Temperature > 2 {
				return fmt.Errorf("%s.temperature must be between 0 and 2", name)
			}
		}

		// Validate top_p if set
		if pCfg.TopP != nil {
			if *pCfg.TopP < 0 || *pCfg.TopP > 1 {
				return fmt.Errorf("%s.top_p must be between 0 and 1", name)
			}
		}

		// Validate max_output_tokens if set
		if pCfg.MaxOutputTokens != nil {
			if *pCfg.MaxOutputTokens < 1 || *pCfg.MaxOutputTokens > 128000 {
				return fmt.Errorf("%s.max_output_tokens must be between 1 and 128000", name)
			}
		}
	}

	if !hasProvider {
		return errors.New("at least one provider must be enabled")
	}

	// Validate failover order references valid providers
	if cfg.Failover.Enabled {
		for _, name := range cfg.Failover.Order {
			if _, ok := cfg.Providers[name]; !ok {
				return fmt.Errorf("failover.order references unknown provider %q", name)
			}
		}
	}

	return nil
}
