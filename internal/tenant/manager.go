package tenant

import (
	"fmt"
	"sort"
	"sync"
)

// Manager holds environment-level config and indexed tenant configs.
type Manager struct {
	Env     EnvConfig
	Tenants map[string]TenantConfig
	mu      sync.RWMutex
}

// ReloadDiff describes what changed during a config reload.
type ReloadDiff struct {
	Added     []string // Tenant IDs that were added
	Removed   []string // Tenant IDs that were removed
	Unchanged []string // Tenant IDs that remained (may have updated)
}

// Load builds a Manager by loading environment config plus all tenant config files.
// The configDir parameter can override the default configs directory.
func Load(configDir string) (*Manager, error) {
	envCfg, err := loadEnv()
	if err != nil {
		return nil, err
	}

	// Use configDir if provided, otherwise use env config
	if configDir == "" {
		configDir = envCfg.ConfigsDir
	}

	tenantCfgs, err := loadTenants(configDir)
	if err != nil {
		return nil, err
	}

	return &Manager{
		Env:     envCfg,
		Tenants: tenantCfgs,
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

// Reload reloads tenant configurations from disk without changing env config.
// Returns a diff of what changed. Thread-safe.
func (m *Manager) Reload() (ReloadDiff, error) {
	// Load new configs (this validates them)
	newTenants, err := loadTenants(m.Env.ConfigsDir)
	if err != nil {
		return ReloadDiff{}, fmt.Errorf("reload failed: %w", err)
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
