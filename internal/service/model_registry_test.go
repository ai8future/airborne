package service

import (
	"encoding/json"
	"testing"

	"github.com/ai8future/airborne/internal/db"
	"github.com/ai8future/airborne/internal/provider"
)

// TestMergeRegistryParams proves the alias-merge precedence: a registered
// alias substitutes its base_model_id, its params fill ONLY fields the
// request/tenant config left unset, and explicit request values always win.
func TestMergeRegistryParams(t *testing.T) {
	alias := &db.Model{
		ID:          "fast",
		TenantID:    "ai8",
		BaseModelID: ptrTo("gpt-4o"),
		Params:      json.RawMessage(`{"temperature":0.2,"top_p":0.9,"max_output_tokens":1024}`),
		IsActive:    true,
	}

	t.Run("nil model passes through unchanged", func(t *testing.T) {
		cfg := provider.ProviderConfig{Model: "unregistered", Temperature: ptrTo(0.7)}
		got := mergeRegistryParams(cfg, nil)
		if got.Model != "unregistered" || got.Temperature == nil || *got.Temperature != 0.7 {
			t.Errorf("got %+v, want cfg unchanged", got)
		}
	})

	t.Run("registry defaults fill unset fields and base model substitutes", func(t *testing.T) {
		got := mergeRegistryParams(provider.ProviderConfig{Model: "fast"}, alias)
		if got.Model != "gpt-4o" {
			t.Errorf("Model = %q, want gpt-4o (base_model_id substituted)", got.Model)
		}
		if got.Temperature == nil || *got.Temperature != 0.2 {
			t.Errorf("Temperature = %v, want 0.2 (registry default)", got.Temperature)
		}
		if got.TopP == nil || *got.TopP != 0.9 {
			t.Errorf("TopP = %v, want 0.9 (registry default)", got.TopP)
		}
		if got.MaxOutputTokens == nil || *got.MaxOutputTokens != 1024 {
			t.Errorf("MaxOutputTokens = %v, want 1024 (registry default)", got.MaxOutputTokens)
		}
	})

	t.Run("request values win over registry defaults", func(t *testing.T) {
		cfg := provider.ProviderConfig{
			Model:           "fast",
			Temperature:     ptrTo(0.7),
			TopP:            ptrTo(0.5),
			MaxOutputTokens: ptrTo(2048),
		}
		got := mergeRegistryParams(cfg, alias)
		if got.Model != "gpt-4o" {
			t.Errorf("Model = %q, want gpt-4o (base model still substitutes)", got.Model)
		}
		if *got.Temperature != 0.7 || *got.TopP != 0.5 || *got.MaxOutputTokens != 2048 {
			t.Errorf("got (temp %v, top_p %v, max %v), want request values (0.7, 0.5, 2048)",
				*got.Temperature, *got.TopP, *got.MaxOutputTokens)
		}
	})

	t.Run("max_tokens fallback key fills MaxOutputTokens", func(t *testing.T) {
		m := &db.Model{ID: "compact", Params: json.RawMessage(`{"max_tokens":512}`)}
		got := mergeRegistryParams(provider.ProviderConfig{Model: "compact"}, m)
		if got.MaxOutputTokens == nil || *got.MaxOutputTokens != 512 {
			t.Errorf("MaxOutputTokens = %v, want 512 (max_tokens fallback)", got.MaxOutputTokens)
		}
	})

	t.Run("alias without base_model_id keeps requested id", func(t *testing.T) {
		m := &db.Model{ID: "tuned", Params: json.RawMessage(`{"temperature":0.1}`)}
		got := mergeRegistryParams(provider.ProviderConfig{Model: "tuned"}, m)
		if got.Model != "tuned" {
			t.Errorf("Model = %q, want tuned (no base model to substitute)", got.Model)
		}
		if got.Temperature == nil || *got.Temperature != 0.1 {
			t.Errorf("Temperature = %v, want 0.1", got.Temperature)
		}
	})
}
