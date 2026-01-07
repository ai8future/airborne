package tenant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTenantJSON(t *testing.T, dir, filename, tenantID string) {
	t.Helper()
	cfg := TenantConfig{
		TenantID: tenantID,
		Providers: map[string]ProviderConfig{
			"openai": {Enabled: true, APIKey: "key", Model: "model"},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestManagerTenantCodes(t *testing.T) {
	mgr := &Manager{
		Tenants: map[string]TenantConfig{
			"b": {TenantID: "b"},
			"a": {TenantID: "a"},
			"c": {TenantID: "c"},
		},
	}

	codes := mgr.TenantCodes()
	if !reflect.DeepEqual(codes, []string{"a", "b", "c"}) {
		t.Fatalf("TenantCodes() = %v, want sorted [a b c]", codes)
	}
}

func TestManagerTenantCount(t *testing.T) {
	mgr := &Manager{
		Tenants: map[string]TenantConfig{
			"a": {TenantID: "a"},
			"b": {TenantID: "b"},
		},
	}

	if got := mgr.TenantCount(); got != 2 {
		t.Fatalf("TenantCount() = %d, want 2", got)
	}
}

func TestManagerIsSingleTenant(t *testing.T) {
	mgr := &Manager{
		Tenants: map[string]TenantConfig{
			"a": {TenantID: "a"},
		},
	}

	if !mgr.IsSingleTenant() {
		t.Error("IsSingleTenant() should be true for 1 tenant")
	}

	mgr.Tenants["b"] = TenantConfig{TenantID: "b"}
	if mgr.IsSingleTenant() {
		t.Error("IsSingleTenant() should be false for 2 tenants")
	}
}

func TestManagerTenant(t *testing.T) {
	mgr := &Manager{
		Tenants: map[string]TenantConfig{
			"a": {TenantID: "a", DisplayName: "Tenant A"},
		},
	}

	t.Run("existing tenant", func(t *testing.T) {
		cfg, ok := mgr.Tenant("a")
		if !ok {
			t.Fatal("expected tenant to exist")
		}
		if cfg.DisplayName != "Tenant A" {
			t.Fatalf("DisplayName = %q, want Tenant A", cfg.DisplayName)
		}
	})

	t.Run("non-existent tenant", func(t *testing.T) {
		_, ok := mgr.Tenant("z")
		if ok {
			t.Fatal("expected tenant not to exist")
		}
	})
}

func TestManagerDefaultTenant(t *testing.T) {
	mgr := &Manager{
		Tenants: map[string]TenantConfig{
			"b": {TenantID: "b"},
			"a": {TenantID: "a"},
		},
	}

	def, ok := mgr.DefaultTenant()
	if !ok {
		t.Fatal("expected default tenant")
	}
	// Should return first alphabetically
	if def.TenantID != "a" {
		t.Fatalf("DefaultTenant().TenantID = %q, want a (first sorted)", def.TenantID)
	}
}

func TestManagerDefaultTenant_Empty(t *testing.T) {
	mgr := &Manager{
		Tenants: map[string]TenantConfig{},
	}

	_, ok := mgr.DefaultTenant()
	if ok {
		t.Fatal("expected no default tenant for empty manager")
	}
}

func TestManagerReload(t *testing.T) {
	dir := t.TempDir()
	writeTenantJSON(t, dir, "t1.json", "t1")
	writeTenantJSON(t, dir, "t2.json", "t2")

	initial, err := loadTenants(dir)
	if err != nil {
		t.Fatalf("loadTenants failed: %v", err)
	}

	mgr := &Manager{Env: EnvConfig{ConfigsDir: dir}, Tenants: initial, configDir: dir}

	// Remove t1 and add t3
	if err := os.Remove(filepath.Join(dir, "t1.json")); err != nil {
		t.Fatalf("remove t1: %v", err)
	}
	writeTenantJSON(t, dir, "t3.json", "t3")

	diff, err := mgr.Reload()
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if !reflect.DeepEqual(diff.Added, []string{"t3"}) {
		t.Fatalf("diff.Added = %v, want [t3]", diff.Added)
	}
	if !reflect.DeepEqual(diff.Removed, []string{"t1"}) {
		t.Fatalf("diff.Removed = %v, want [t1]", diff.Removed)
	}
	if !reflect.DeepEqual(diff.Unchanged, []string{"t2"}) {
		t.Fatalf("diff.Unchanged = %v, want [t2]", diff.Unchanged)
	}

	// Verify manager state updated
	if _, ok := mgr.Tenant("t3"); !ok {
		t.Error("expected t3 to exist after reload")
	}
	if _, ok := mgr.Tenant("t1"); ok {
		t.Error("expected t1 to not exist after reload")
	}
}

func TestManagerReload_Error(t *testing.T) {
	mgr := &Manager{Env: EnvConfig{ConfigsDir: "/nonexistent/path"}, Tenants: make(map[string]TenantConfig), configDir: "/nonexistent/path"}

	_, err := mgr.Reload()
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	writeTenantJSON(t, dir, "tenant.json", "test-tenant")

	t.Setenv("AIBOX_CONFIGS_DIR", dir)

	mgr, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if mgr.TenantCount() != 1 {
		t.Fatalf("TenantCount = %d, want 1", mgr.TenantCount())
	}
	if _, ok := mgr.Tenant("test-tenant"); !ok {
		t.Fatal("expected test-tenant to exist")
	}
}

func TestLoad_WithOverrideDir(t *testing.T) {
	dir := t.TempDir()
	writeTenantJSON(t, dir, "tenant.json", "override-tenant")

	// Set env to different dir (which doesn't exist)
	t.Setenv("AIBOX_CONFIGS_DIR", "/nonexistent")

	mgr, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if _, ok := mgr.Tenant("override-tenant"); !ok {
		t.Fatal("expected override-tenant to exist")
	}
}
