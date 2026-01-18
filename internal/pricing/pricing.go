// Package pricing provides cost calculation for LLM API usage.
// Pricing data is loaded from JSON files in the configs directory.
package pricing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ModelPricing holds per-token costs for a model (in USD per million tokens)
type ModelPricing struct {
	InputPerMillion  float64 `json:"input_per_million"`
	OutputPerMillion float64 `json:"output_per_million"`
}

// Cost represents the calculated cost breakdown
type Cost struct {
	Model        string
	InputTokens  int64
	OutputTokens int64
	InputCost    float64
	OutputCost   float64
	TotalCost    float64
	Unknown      bool // true if model not found in pricing data
}

// PricingMetadata contains source and update information
type PricingMetadata struct {
	Updated string `json:"updated"`
	Source  string `json:"source,omitempty"`
}

// ProviderPricing holds all pricing data for a single provider
type ProviderPricing struct {
	Provider string                  `json:"provider"`
	Models   map[string]ModelPricing `json:"models"`
	Metadata PricingMetadata         `json:"metadata,omitempty"`
}

// Pricer calculates LLM API costs across all providers
type Pricer struct {
	models    map[string]ModelPricing
	providers map[string]ProviderPricing
	mu        sync.RWMutex
}

// pricingFile represents the JSON structure
type pricingFile struct {
	Provider string                  `json:"provider,omitempty"`
	Models   map[string]ModelPricing `json:"models"`
	Metadata PricingMetadata         `json:"metadata,omitempty"`
}

// Package-level pricer instance (initialized lazily or via Init)
var (
	defaultPricer *Pricer
	initOnce      sync.Once
	initErr       error
)

// Init initializes the pricing system from JSON files in configDir.
// Should be called once at startup. If not called, CalculateCost will
// attempt lazy initialization from "configs" directory.
func Init(configDir string) error {
	initOnce.Do(func() {
		defaultPricer, initErr = NewPricer(configDir)
	})
	return initErr
}

// NewPricer creates a pricer, loading pricing from config files.
// Dynamically discovers all *_pricing.json files.
func NewPricer(configDir string) (*Pricer, error) {
	models := make(map[string]ModelPricing)
	providers := make(map[string]ProviderPricing)

	// Dynamically discover all *_pricing.json files
	files, err := filepath.Glob(filepath.Join(configDir, "*_pricing.json"))
	if err != nil {
		return nil, fmt.Errorf("glob pricing files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no pricing files found in %s", configDir)
	}

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		var file pricingFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}

		filename := filepath.Base(path)

		// Infer provider name from filename if not in JSON
		providerName := file.Provider
		if providerName == "" {
			providerName = strings.TrimSuffix(filename, "_pricing.json")
		}

		providers[providerName] = ProviderPricing{
			Provider: providerName,
			Models:   file.Models,
			Metadata: file.Metadata,
		}

		// Merge models into flat lookup
		for model, pricing := range file.Models {
			models[model] = pricing
		}
	}

	return &Pricer{models: models, providers: providers}, nil
}

// Calculate computes the cost for a given model and token counts
func (p *Pricer) Calculate(model string, inputTokens, outputTokens int64) Cost {
	p.mu.RLock()
	defer p.mu.RUnlock()

	pricing, ok := p.models[model]
	if !ok {
		// Try prefix match for versioned models
		pricing, ok = p.findPricingByPrefix(model)
		if !ok {
			return Cost{Model: model, InputTokens: inputTokens, OutputTokens: outputTokens, Unknown: true}
		}
	}

	inputCost := float64(inputTokens) * pricing.InputPerMillion / 1_000_000
	outputCost := float64(outputTokens) * pricing.OutputPerMillion / 1_000_000

	return Cost{
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		InputCost:    inputCost,
		OutputCost:   outputCost,
		TotalCost:    inputCost + outputCost,
	}
}

// findPricingByPrefix finds pricing for models with version suffixes.
func (p *Pricer) findPricingByPrefix(model string) (ModelPricing, bool) {
	for knownModel, pricing := range p.models {
		if strings.HasPrefix(model, knownModel) {
			return pricing, true
		}
	}
	return ModelPricing{}, false
}

// GetPricing returns the pricing for a model
func (p *Pricer) GetPricing(model string) (ModelPricing, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	pricing, ok := p.models[model]
	if ok {
		return pricing, true
	}
	return p.findPricingByPrefix(model)
}

// ListProviders returns all loaded provider names
func (p *Pricer) ListProviders() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	names := make([]string, 0, len(p.providers))
	for name := range p.providers {
		names = append(names, name)
	}
	return names
}

// ModelCount returns the total number of models loaded
func (p *Pricer) ModelCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.models)
}

// --- Package-level convenience functions (backwards compatible) ---

// ensureInitialized lazily initializes the default pricer
func ensureInitialized() {
	initOnce.Do(func() {
		defaultPricer, initErr = NewPricer("configs")
		if initErr != nil {
			// Log but don't fail - CalculateCost will return 0 for unknown models
			defaultPricer = &Pricer{
				models:    make(map[string]ModelPricing),
				providers: make(map[string]ProviderPricing),
			}
		}
	})
}

// CalculateCost calculates the USD cost for a completion.
// Returns 0 for unknown models (graceful degradation).
func CalculateCost(model string, inputTokens, outputTokens int) float64 {
	ensureInitialized()
	cost := defaultPricer.Calculate(model, int64(inputTokens), int64(outputTokens))
	return cost.TotalCost
}

// GetPricing returns the pricing for a model, if known.
func GetPricing(model string) (ModelPricing, bool) {
	ensureInitialized()
	return defaultPricer.GetPricing(model)
}
