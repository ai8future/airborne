// Package pricing provides cost calculation for LLM API usage.
// This is a thin wrapper around github.com/ai8future/pricing_db.
package pricing

import (
	"fmt"

	pricing_db "github.com/ai8future/pricing_db"
)

// Type aliases for backwards compatibility
type ModelPricing = pricing_db.ModelPricing
type GroundingPricing = pricing_db.GroundingPricing
type PricingMetadata = pricing_db.PricingMetadata
type ProviderPricing = pricing_db.ProviderPricing

// Cost represents the calculated cost breakdown
type Cost struct {
	Model        string
	InputTokens  int64
	OutputTokens int64
	InputCost    float64
	OutputCost   float64
	TotalCost    float64
	Unknown      bool
}

// Format returns a human-readable cost breakdown
func (c Cost) Format() string {
	if c.Unknown {
		return fmt.Sprintf("Cost: unknown (model %q not in pricing data)", c.Model)
	}
	return fmt.Sprintf("Input: $%.4f (%d tokens) | Output: $%.4f (%d tokens) | Total: $%.4f",
		c.InputCost, c.InputTokens, c.OutputCost, c.OutputTokens, c.TotalCost)
}

// Pricer wraps pricing_db.Pricer for backwards compatibility
type Pricer struct {
	db *pricing_db.Pricer
}

// NewPricer creates a pricer using embedded pricing data.
// The configDir parameter is ignored - pricing_db uses go:embed.
func NewPricer(configDir string) (*Pricer, error) {
	db, err := pricing_db.NewPricer()
	if err != nil {
		return nil, err
	}
	return &Pricer{db: db}, nil
}

// Calculate computes the cost for a given model and token counts
func (p *Pricer) Calculate(model string, inputTokens, outputTokens int64) Cost {
	dbCost := p.db.Calculate(model, inputTokens, outputTokens)
	return Cost{
		Model:        dbCost.Model,
		InputTokens:  dbCost.InputTokens,
		OutputTokens: dbCost.OutputTokens,
		InputCost:    dbCost.InputCost,
		OutputCost:   dbCost.OutputCost,
		TotalCost:    dbCost.TotalCost,
		Unknown:      dbCost.Unknown,
	}
}

// CalculateGrounding computes the cost for Google grounding/web search.
// For Gemini 3: queryCount is the actual number of search queries executed.
// For Gemini 2.5 and older: queryCount should be 1 if grounding was used, 0 otherwise.
func (p *Pricer) CalculateGrounding(model string, queryCount int) float64 {
	return p.db.CalculateGrounding(model, queryCount)
}

// GetPricing returns the pricing for a model
func (p *Pricer) GetPricing(model string) (ModelPricing, bool) {
	return p.db.GetPricing(model)
}

// GetProviderMetadata returns the pricing metadata for a provider
func (p *Pricer) GetProviderMetadata(provider string) (ProviderPricing, bool) {
	return p.db.GetProviderMetadata(provider)
}

// ListProviders returns all loaded provider names
func (p *Pricer) ListProviders() []string {
	return p.db.ListProviders()
}

// ModelCount returns the total number of models loaded
func (p *Pricer) ModelCount() int {
	return p.db.ModelCount()
}

// ProviderCount returns the number of providers loaded
func (p *Pricer) ProviderCount() int {
	return p.db.ProviderCount()
}

// --- Package-level convenience functions ---

// CalculateCost calculates the USD cost for a completion.
// Returns 0 for unknown models (graceful degradation).
func CalculateCost(model string, inputTokens, outputTokens int) float64 {
	return pricing_db.CalculateCost(model, inputTokens, outputTokens)
}

// CalculateGroundingCost calculates the USD cost for grounding/web search.
// For Gemini 3: queryCount is the actual number of search queries executed.
// For Gemini 2.5 and older: queryCount should be 1 if grounding was used, 0 otherwise.
func CalculateGroundingCost(model string, queryCount int) float64 {
	return pricing_db.CalculateGroundingCost(model, queryCount)
}

// GetPricing returns the pricing for a model, if known.
func GetPricing(model string) (ModelPricing, bool) {
	return pricing_db.GetPricing(model)
}

// ListProviders returns all loaded provider names
func ListProviders() []string {
	return pricing_db.ListProviders()
}

// ModelCount returns the total number of models loaded
func ModelCount() int {
	return pricing_db.ModelCount()
}

// ProviderCount returns the number of providers loaded
func ProviderCount() int {
	return pricing_db.ProviderCount()
}
