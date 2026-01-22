package tenant

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
)

// Manager holds environment-level config and indexed tenant configs.
type Manager struct {
	Env       EnvConfig
	Tenants   map[string]TenantConfig
	configDir string // effective config directory (may differ from Env.ConfigsDir if overridden)
	mu        sync.RWMutex
}

// frozenConfig matches the structure written by airborne-freeze command.
type frozenConfig struct {
	TenantConfigs []TenantConfig `json:"tenant_configs"`
	SingleTenant  bool           `json:"single_tenant"`
}

// ReloadDiff describes what changed during a config reload.
type ReloadDiff struct {
	Added     []string // Tenant IDs that were added
	Removed   []string // Tenant IDs that were removed
	Unchanged []string // Tenant IDs that remained (may have updated)
}

// Load builds a Manager by loading environment config plus all tenant config files.
// The configDir parameter can override the default configs directory.
//
// Tenant loading priority:
// 1. If AIRBORNE_USE_FROZEN=true, load from frozen config file
// 2. If DOPPLER_TOKEN is set, load from Doppler (BRAND_TENANTS → AIRBORNE_TENANT_CONFIG)
// 3. Otherwise, load from JSON/YAML files in configs directory
func Load(configDir string) (*Manager, error) {
	// Check if we should use frozen config
	if os.Getenv("AIRBORNE_USE_FROZEN") == "true" {
		frozenPath := os.Getenv("AIRBORNE_FROZEN_CONFIG_PATH")
		if frozenPath == "" {
			frozenPath = "configs/frozen.json"
		}
		fmt.Fprintf(os.Stderr, "INFO: Loading tenant configs from frozen config: %s\n", frozenPath)
		return loadFromFrozen(frozenPath)
	}

	envCfg, err := loadEnv()
	if err != nil {
		return nil, err
	}

	// Use configDir if provided, otherwise use env config
	effectiveDir := configDir
	if effectiveDir == "" {
		effectiveDir = envCfg.ConfigsDir
	}

	var tenantCfgs map[string]TenantConfig

	// Try Doppler first if configured
	if DopplerEnabled() {
		fmt.Fprintf(os.Stderr, "INFO: Loading tenant configs from Doppler API\n")
		tenantCfgs, err = LoadTenantsFromDoppler()
		if err != nil {
			return nil, fmt.Errorf("doppler tenant load: %w", err)
		}
		fmt.Fprintf(os.Stderr, "INFO: Loaded %d tenant configs from Doppler\n", len(tenantCfgs))
	} else {
		// Fall back to file-based loading
		fmt.Fprintf(os.Stderr, "INFO: DOPPLER_TOKEN not set, loading tenant configs from files in %s\n", effectiveDir)
		tenantCfgs, err = loadTenants(effectiveDir)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "INFO: Loaded %d tenant configs from files\n", len(tenantCfgs))
	}

	return &Manager{
		Env:       envCfg,
		Tenants:   tenantCfgs,
		configDir: effectiveDir, // store effective dir for Reload()
	}, nil
}

// loadFromFrozen loads tenant configs from a frozen config file.
func loadFromFrozen(path string) (*Manager, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read frozen config: %w", err)
	}

	var frozen frozenConfig
	if err := json.Unmarshal(data, &frozen); err != nil {
		return nil, fmt.Errorf("failed to parse frozen config: %w", err)
	}

	// Convert tenant list to map
	tenantCfgs := make(map[string]TenantConfig, len(frozen.TenantConfigs))
	for _, tc := range frozen.TenantConfigs {
		// Resolve ENV=/FILE= references in secrets
		if err := resolveSecrets(&tc); err != nil {
			return nil, fmt.Errorf("resolving secrets for %s: %w", tc.TenantID, err)
		}
		tenantCfgs[tc.TenantID] = tc
	}

	fmt.Fprintf(os.Stderr, "INFO: Loaded %d tenant configs from frozen file\n", len(tenantCfgs))

	// Load minimal env config (for consistency, though most fields won't be used)
	envCfg, err := loadEnv()
	if err != nil {
		// Non-fatal in frozen mode
		envCfg = EnvConfig{}
	}

	return &Manager{
		Env:       envCfg,
		Tenants:   tenantCfgs,
		configDir: "", // Not applicable in frozen mode
	}, nil
}

// Tenant retrieves config for a tenant_id (thread-safe).
func (m *Manager) Tenant(tenantID string) (TenantConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.Tenants[tenantID]
	return cfg, ok
}

// TenantCodes returns a sorted list of all loaded tenant IDs (thread-safe).
func (m *Manager) TenantCodes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	codes := make([]string, 0, len(m.Tenants))
	for code := range m.Tenants {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// TenantCount returns the number of loaded tenants (thread-safe).
func (m *Manager) TenantCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Tenants)
}

// IsSingleTenant returns true if only one tenant is configured.
func (m *Manager) IsSingleTenant() bool {
	return m.TenantCount() == 1
}

// DefaultTenant returns the first (and possibly only) tenant.
// Useful for backwards compatibility when tenant_id is not specified.
func (m *Manager) DefaultTenant() (TenantConfig, bool) {
	codes := m.TenantCodes()
	if len(codes) == 0 {
		return TenantConfig{}, false
	}
	return m.Tenant(codes[0])
}

// Reload reloads tenant configurations without changing env config.
// Uses Doppler if configured, otherwise reloads from disk.
// Returns a diff of what changed. Thread-safe.
func (m *Manager) Reload() (ReloadDiff, error) {
	var newTenants map[string]TenantConfig
	var err error

	// Use same source as initial load
	if DopplerEnabled() {
		ClearDopplerCache() // Clear cache to get fresh data
		newTenants, err = LoadTenantsFromDoppler()
		if err != nil {
			return ReloadDiff{}, fmt.Errorf("doppler reload failed: %w", err)
		}
	} else {
		// Load new configs (this validates them)
		// Use m.configDir which preserves any override from initial Load()
		newTenants, err = loadTenants(m.configDir)
		if err != nil {
			return ReloadDiff{}, fmt.Errorf("reload failed: %w", err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Calculate diff
	diff := ReloadDiff{}
	oldCodes := make(map[string]bool)
	for code := range m.Tenants {
		oldCodes[code] = true
	}

	for code := range newTenants {
		if oldCodes[code] {
			diff.Unchanged = append(diff.Unchanged, code)
			delete(oldCodes, code)
		} else {
			diff.Added = append(diff.Added, code)
		}
	}

	for code := range oldCodes {
		diff.Removed = append(diff.Removed, code)
	}

	// Sort for consistent output
	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Strings(diff.Unchanged)

	// Apply new configs
	m.Tenants = newTenants

	return diff, nil
}
