// Package pricing provides cost calculation for LLM API usage.
package pricing

import (
	"strings"
)

// ModelPricing holds per-million-token pricing for a model.
type ModelPricing struct {
	Input  float64 // Price per million input tokens
	Output float64 // Price per million output tokens
}

// modelPricing maps model names to their pricing.
// Prices are per million tokens as of January 2026.
var modelPricing = map[string]ModelPricing{
	// OpenAI
	"gpt-4o":           {Input: 2.50, Output: 10.00},
	"gpt-4o-mini":      {Input: 0.15, Output: 0.60},
	"gpt-4-turbo":      {Input: 10.00, Output: 30.00},
	"gpt-4":            {Input: 30.00, Output: 60.00},
	"gpt-3.5-turbo":    {Input: 0.50, Output: 1.50},
	"o1":               {Input: 15.00, Output: 60.00},
	"o1-mini":          {Input: 1.10, Output: 4.40},
	"o1-preview":       {Input: 15.00, Output: 60.00},
	"o3-mini":          {Input: 1.10, Output: 4.40},

	// Gemini
	"gemini-2.0-flash":       {Input: 0.10, Output: 0.40},
	"gemini-2.0-flash-exp":   {Input: 0.10, Output: 0.40},
	"gemini-1.5-flash":       {Input: 0.075, Output: 0.30},
	"gemini-1.5-pro":         {Input: 1.25, Output: 5.00},
	"gemini-2.5-pro":         {Input: 1.25, Output: 5.00},
	"gemini-3-pro-preview":   {Input: 1.25, Output: 5.00},
	"gemini-pro":             {Input: 0.50, Output: 1.50},

	// Anthropic
	"claude-sonnet-4-20250514":     {Input: 3.00, Output: 15.00},
	"claude-3-5-sonnet-20241022":   {Input: 3.00, Output: 15.00},
	"claude-3-5-sonnet-20240620":   {Input: 3.00, Output: 15.00},
	"claude-3-sonnet-20240229":     {Input: 3.00, Output: 15.00},
	"claude-3-opus-20240229":       {Input: 15.00, Output: 75.00},
	"claude-3-haiku-20240307":      {Input: 0.25, Output: 1.25},
	"claude-opus-4-20250514":       {Input: 15.00, Output: 75.00},
	"claude-opus-4-5-20251101":     {Input: 15.00, Output: 75.00},

	// DeepSeek
	"deepseek-chat":       {Input: 0.14, Output: 0.28},
	"deepseek-coder":      {Input: 0.14, Output: 0.28},
	"deepseek-reasoner":   {Input: 0.55, Output: 2.19},

	// Mistral
	"mistral-large-latest":  {Input: 2.00, Output: 6.00},
	"mistral-medium-latest": {Input: 2.70, Output: 8.10},
	"mistral-small-latest":  {Input: 0.20, Output: 0.60},
	"codestral-latest":      {Input: 0.20, Output: 0.60},

	// Grok
	"grok-beta": {Input: 5.00, Output: 15.00},
	"grok-2":    {Input: 2.00, Output: 10.00},

	// Perplexity
	"llama-3.1-sonar-small-128k-online":  {Input: 0.20, Output: 0.20},
	"llama-3.1-sonar-large-128k-online":  {Input: 1.00, Output: 1.00},
	"llama-3.1-sonar-huge-128k-online":   {Input: 5.00, Output: 5.00},

	// Cohere
	"command-r-plus": {Input: 2.50, Output: 10.00},
	"command-r":      {Input: 0.15, Output: 0.60},

	// Together AI (Llama models)
	"meta-llama/Llama-3-70b-chat-hf":  {Input: 0.90, Output: 0.90},
	"meta-llama/Llama-3-8b-chat-hf":   {Input: 0.20, Output: 0.20},
}

// CalculateCost calculates the USD cost for a completion.
// Returns 0 for unknown models (graceful degradation).
func CalculateCost(model string, inputTokens, outputTokens int) float64 {
	pricing, ok := modelPricing[model]
	if !ok {
		// Try partial match for versioned models
		pricing, ok = findPricingByPrefix(model)
		if !ok {
			return 0 // Unknown model = free (graceful degradation)
		}
	}

	inputCost := float64(inputTokens) * pricing.Input / 1_000_000
	outputCost := float64(outputTokens) * pricing.Output / 1_000_000

	return inputCost + outputCost
}

// findPricingByPrefix finds pricing for models with version suffixes.
// e.g., "gpt-4o-2024-11-20" matches "gpt-4o"
func findPricingByPrefix(model string) (ModelPricing, bool) {
	// Try to match by removing date suffixes
	for knownModel, pricing := range modelPricing {
		if strings.HasPrefix(model, knownModel) {
			return pricing, true
		}
	}
	return ModelPricing{}, false
}

// GetPricing returns the pricing for a model, if known.
func GetPricing(model string) (ModelPricing, bool) {
	pricing, ok := modelPricing[model]
	if ok {
		return pricing, true
	}
	return findPricingByPrefix(model)
}

// RegisterModel registers or updates pricing for a model.
// Useful for adding new models at runtime.
func RegisterModel(model string, input, output float64) {
	modelPricing[model] = ModelPricing{Input: input, Output: output}
}
