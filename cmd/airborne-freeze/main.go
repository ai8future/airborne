package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ai8future/airborne/internal/config"
	"github.com/ai8future/airborne/internal/tenant"
)

func main() {
	// Setup logging
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("Starting config freeze process...")

	// Load global config (triggers all Doppler, env vars, validation)
	slog.Info("Loading global configuration...")
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load global config", "error", err)
		os.Exit(1)
	}
	slog.Info("✓ Global config loaded successfully")

	// Load tenant manager (triggers tenant config loading from Doppler/files)
	slog.Info("Loading tenant configurations...")
	mgr, err := tenant.Load("")
	if err != nil {
		slog.Error("Failed to load tenant manager", "error", err)
		os.Exit(1)
	}

	// Get all tenants from the manager
	tenants := make([]*tenant.TenantConfig, 0, len(mgr.Tenants))
	for _, tc := range mgr.Tenants {
		tcCopy := tc
		tenants = append(tenants, &tcCopy)
	}
	slog.Info("✓ Tenant configs loaded successfully", "count", len(tenants))

	// Validate all tenant configs
	slog.Info("Validating all tenant configurations...")
	for _, t := range tenants {
		if err := validateTenantConfig(t); err != nil {
			slog.Error("Tenant config validation failed", "tenant_id", t.TenantID, "error", err)
			os.Exit(1)
		}
		slog.Info("✓ Tenant validated", "tenant_id", t.TenantID)
	}

	// Create frozen config structure
	frozen := FrozenConfig{
		GlobalConfig:   cfg,
		TenantConfigs:  tenants,
		FrozenAt:       time.Now().Format(time.RFC3339),
		SingleTenant:   mgr.IsSingleTenant(),
	}

	// Determine output path
	outputPath := os.Getenv("AIRBORNE_FROZEN_CONFIG_PATH")
	if outputPath == "" {
		outputPath = "configs/frozen.json"
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		slog.Error("Failed to create output directory", "path", outputPath, "error", err)
		os.Exit(1)
	}

	// Write frozen config
	slog.Info("Writing frozen configuration...", "path", outputPath)
	if err := writeFrozenConfig(frozen, outputPath); err != nil {
		slog.Error("Failed to write frozen config", "error", err)
		os.Exit(1)
	}

	slog.Info("✓ Config frozen successfully!", "path", outputPath)
	fmt.Println()
	fmt.Println("To use frozen config in production, set:")
	fmt.Println("  export AIRBORNE_USE_FROZEN=true")
	fmt.Printf("  export AIRBORNE_FROZEN_CONFIG_PATH=%s\n", outputPath)
}

// FrozenConfig represents a fully-resolved, validated configuration snapshot
type FrozenConfig struct {
	GlobalConfig  *config.Config           `json:"global_config"`
	TenantConfigs []*tenant.TenantConfig   `json:"tenant_configs"`
	FrozenAt      string                   `json:"frozen_at"`
	SingleTenant  bool                     `json:"single_tenant"`
}

func writeFrozenConfig(frozen FrozenConfig, path string) error {
	data, err := json.MarshalIndent(frozen, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal frozen config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write frozen config file: %w", err)
	}

	return nil
}

func validateTenantConfig(tc *tenant.TenantConfig) error {
	// Check required fields
	if tc.TenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}

	// Check at least one provider is enabled
	hasProvider := false
	for providerName, provider := range tc.Providers {
		if provider.Enabled {
			hasProvider = true
			if provider.APIKey == "" {
				return fmt.Errorf("%s enabled but api_key is empty", providerName)
			}
			if provider.Model == "" {
				return fmt.Errorf("%s enabled but model is empty", providerName)
			}
		}
	}

	if !hasProvider {
		return fmt.Errorf("at least one provider must be enabled")
	}

	return nil
}
