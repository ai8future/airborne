package tenant

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AllowedSecretDirs contains the allowed directories for FILE= secret paths.
// Paths outside these directories will be rejected to prevent path traversal.
var AllowedSecretDirs = []string{
	"/etc/airborne/secrets",
	"/run/secrets",
	"/var/run/secrets",
}

// validateSecretPath validates that the path is within allowed directories
// and doesn't contain path traversal sequences. It resolves symlinks to prevent
// attacks using symlinks inside allowed directories pointing outside.
func validateSecretPath(path string) error {
	// Check for path traversal sequences
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}

	// Get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Resolve symlinks to get the real path
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If file doesn't exist yet, resolve parent directory
		parentDir := filepath.Dir(absPath)
		realParent, parentErr := filepath.EvalSymlinks(parentDir)
		if parentErr != nil {
			return fmt.Errorf("cannot resolve path: %w", err)
		}
		realPath = filepath.Join(realParent, filepath.Base(absPath))
	}

	// Check against allowed directories using the real path
	for _, allowed := range AllowedSecretDirs {
		allowedReal, err := filepath.EvalSymlinks(allowed)
		if err != nil {
			continue // Skip unresolvable allowed dirs
		}
		if strings.HasPrefix(realPath, allowedReal+string(filepath.Separator)) || realPath == allowedReal {
			return nil
		}
	}

	return fmt.Errorf("path %s not in allowed directories", realPath)
}

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

// ReplaceSecretsWithReferences replaces actual secret values with ENV= references.
// Used by the freeze command to avoid storing plaintext secrets in frozen config.
func ReplaceSecretsWithReferences(cfg *TenantConfig) {
	for name, pCfg := range cfg.Providers {
		// If the API key doesn't already have a reference pattern, create one
		if !strings.HasPrefix(pCfg.APIKey, "ENV=") &&
		   !strings.HasPrefix(pCfg.APIKey, "FILE=") &&
		   !strings.HasPrefix(pCfg.APIKey, "${") {
			// Replace with ENV= reference
			envVarName := strings.ToUpper(name) + "_API_KEY"
			pCfg.APIKey = "ENV=" + envVarName
			cfg.Providers[name] = pCfg
		}
		// If it already has ENV=/FILE=/${} pattern, keep it as-is
	}
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

		// Validate path to prevent traversal attacks
		if err := validateSecretPath(path); err != nil {
			return "", fmt.Errorf("secret path validation failed: %w", err)
		}

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
