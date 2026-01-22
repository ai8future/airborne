package config

import (
	"testing"

	"github.com/ai8future/airborne/internal/tenant"
	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
)

func TestBuild_TenantDefaults(t *testing.T) {
	tenantCfg := &tenant.TenantConfig{
		Providers: map[string]tenant.ProviderConfig{
			"openai": {
				Enabled:     true,
				APIKey:      "tenant-key",
				Model:       "gpt-4",
				Temperature: floatPtr(0.7),
			},
		},
	}

	builder := NewBuilder()
	cfg := builder.Build("openai", tenantCfg, nil)

	if cfg.APIKey != "tenant-key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "tenant-key")
	}

	if cfg.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", cfg.Model, "gpt-4")
	}

	if cfg.Temperature == nil || *cfg.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", cfg.Temperature)
	}
}

func TestBuild_RequestOverride(t *testing.T) {
	tenantCfg := &tenant.TenantConfig{
		Providers: map[string]tenant.ProviderConfig{
			"openai": {
				Enabled:     true,
				APIKey:      "tenant-key",
				Model:       "gpt-4",
				Temperature: floatPtr(0.7),
			},
		},
	}

	requestCfg := &pb.ProviderConfig{
		Model:       "gpt-4o",
		Temperature: floatPtr(0.9),
	}

	builder := NewBuilder()
	cfg := builder.Build("openai", tenantCfg, requestCfg)

	// API key from tenant should NOT be overridable
	if cfg.APIKey != "tenant-key" {
		t.Errorf("APIKey = %q, want %q (should use tenant key)", cfg.APIKey, "tenant-key")
	}

	// Model should be overridden
	if cfg.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", cfg.Model, "gpt-4o")
	}

	// Temperature should be overridden
	if cfg.Temperature == nil || *cfg.Temperature != 0.9 {
		t.Errorf("Temperature = %v, want 0.9", cfg.Temperature)
	}
}

func TestBuild_ExtraOptions_Merge(t *testing.T) {
	tenantCfg := &tenant.TenantConfig{
		Providers: map[string]tenant.ProviderConfig{
			"openai": {
				Enabled: true,
				APIKey:  "tenant-key",
				Model:   "gpt-4",
				ExtraOptions: map[string]string{
					"tenant_option": "tenant_value",
					"shared":        "from_tenant",
				},
			},
		},
	}

	requestCfg := &pb.ProviderConfig{
		ExtraOptions: map[string]string{
			"request_option": "request_value",
			"shared":         "from_request",
		},
	}

	builder := NewBuilder()
	cfg := builder.Build("openai", tenantCfg, requestCfg)

	// Tenant options should be present
	if cfg.ExtraOptions["tenant_option"] != "tenant_value" {
		t.Errorf("tenant_option not preserved")
	}

	// Request options should be present
	if cfg.ExtraOptions["request_option"] != "request_value" {
		t.Errorf("request_option not added")
	}

	// Request should override tenant for shared keys
	if cfg.ExtraOptions["shared"] != "from_request" {
		t.Errorf("shared = %q, want %q", cfg.ExtraOptions["shared"], "from_request")
	}
}

func TestBuild_NoTenantConfig(t *testing.T) {
	requestCfg := &pb.ProviderConfig{
		Model: "gpt-4o",
	}

	builder := NewBuilder()
	cfg := builder.Build("openai", nil, requestCfg)

	// Should have request values
	if cfg.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", cfg.Model, "gpt-4o")
	}

	// Should have empty API key (tenant not available)
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", cfg.APIKey)
	}
}

func floatPtr(f float64) *float64 {
	return &f
}

func floatPtr32(f float32) *float32 {
	return &f
}
