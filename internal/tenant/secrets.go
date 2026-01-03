package tenant

import (
	"fmt"
	"os"
	"strings"
)

// resolveSecrets loads API keys from ENV=, FILE=, or inline values.
func resolveSecrets(cfg *TenantConfig) error {
	for name, pCfg := range cfg.Providers {
		resolved, err := loadSecret(pCfg.APIKey)
		if err != nil {
			return fmt.Errorf("%s api_key: %w", name, err)
		}
		pCfg.APIKey = resolved
		cfg.Providers[name] = pCfg
	}
	return nil
}

// loadSecret resolves a secret value from ENV=, FILE=, or inline.
func loadSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}

	// Handle ENV= prefix
	if strings.HasPrefix(value, "ENV=") {
		envVar := strings.TrimPrefix(value, "ENV=")
		v := os.Getenv(envVar)
		if v == "" {
			return "", fmt.Errorf("environment variable %s not set", envVar)
		}
		return v, nil
	}

	// Handle FILE= prefix
	if strings.HasPrefix(value, "FILE=") {
		path := strings.TrimSpace(strings.TrimPrefix(value, "FILE="))
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", path, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	// Handle ${VAR} expansion
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		varName := value[2 : len(value)-1]
		v := os.Getenv(varName)
		if v == "" {
			return "", fmt.Errorf("environment variable %s not set", varName)
		}
		return v, nil
	}

	// Return as-is (inline value)
	return value, nil
}
