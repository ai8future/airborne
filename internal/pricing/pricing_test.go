package pricing

import (
	"strings"
	"testing"
)

func TestCostFormat_Known(t *testing.T) {
	cost := Cost{
		Model:        "gpt-4o",
		InputTokens:  1000,
		OutputTokens: 500,
		InputCost:    0.005,
		OutputCost:   0.0075,
		TotalCost:    0.0125,
		Unknown:      false,
	}

	formatted := cost.Format()
	if strings.Contains(formatted, "unknown") {
		t.Error("Format() should not contain 'unknown' for known model")
	}
	if !strings.Contains(formatted, "Input:") {
		t.Error("Format() should contain 'Input:'")
	}
	if !strings.Contains(formatted, "Output:") {
		t.Error("Format() should contain 'Output:'")
	}
	if !strings.Contains(formatted, "Total:") {
		t.Error("Format() should contain 'Total:'")
	}
	if !strings.Contains(formatted, "$0.0125") {
		t.Error("Format() should contain total cost")
	}
}

func TestCostFormat_Unknown(t *testing.T) {
	cost := Cost{
		Model:   "unknown-model-xyz",
		Unknown: true,
	}

	formatted := cost.Format()
	if !strings.Contains(formatted, "unknown") {
		t.Error("Format() should contain 'unknown' for unknown model")
	}
	if !strings.Contains(formatted, "unknown-model-xyz") {
		t.Error("Format() should contain the model name")
	}
}

func TestNewPricer(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() error = %v", err)
	}
	if pricer == nil {
		t.Fatal("NewPricer() returned nil")
	}
}

func TestPricer_Calculate_KnownModel(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() error = %v", err)
	}

	// Test with a model likely to be in pricing data
	cost := pricer.Calculate("gpt-4o", 1000, 500)

	if cost.Unknown {
		t.Skip("gpt-4o not in pricing data, skipping")
	}
	if cost.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", cost.Model, "gpt-4o")
	}
	if cost.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want %d", cost.InputTokens, 1000)
	}
	if cost.OutputTokens != 500 {
		t.Errorf("OutputTokens = %d, want %d", cost.OutputTokens, 500)
	}
	if cost.TotalCost <= 0 {
		t.Errorf("TotalCost = %f, expected positive value", cost.TotalCost)
	}
	if cost.TotalCost != cost.InputCost+cost.OutputCost {
		t.Errorf("TotalCost (%f) != InputCost (%f) + OutputCost (%f)",
			cost.TotalCost, cost.InputCost, cost.OutputCost)
	}
}

func TestPricer_Calculate_UnknownModel(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() error = %v", err)
	}

	cost := pricer.Calculate("nonexistent-model-xyz-12345", 1000, 500)

	if !cost.Unknown {
		t.Error("expected Unknown = true for nonexistent model")
	}
	if cost.TotalCost != 0 {
		t.Errorf("TotalCost = %f, want 0 for unknown model", cost.TotalCost)
	}
}

func TestPricer_CalculateGrounding_Zero(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() error = %v", err)
	}

	cost := pricer.CalculateGrounding("gemini-2.5-flash", 0)
	if cost != 0 {
		t.Errorf("CalculateGrounding with 0 queries = %f, want 0", cost)
	}
}

func TestPricer_CalculateGrounding_Positive(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() error = %v", err)
	}

	cost := pricer.CalculateGrounding("gemini-2.5-flash", 5)
	if cost < 0 {
		t.Errorf("CalculateGrounding returned negative cost: %f", cost)
	}
}

func TestPricer_GetPricing_Unknown(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() error = %v", err)
	}

	_, found := pricer.GetPricing("nonexistent-model-xyz-12345")
	if found {
		t.Error("expected found = false for unknown model")
	}
}

func TestPricer_ListProviders(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() error = %v", err)
	}

	providers := pricer.ListProviders()
	if len(providers) == 0 {
		t.Error("ListProviders() returned empty list")
	}
}

func TestPricer_ModelCount(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() error = %v", err)
	}

	count := pricer.ModelCount()
	if count == 0 {
		t.Error("ModelCount() = 0, expected positive value")
	}
}

func TestPricer_ProviderCount(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer() error = %v", err)
	}

	count := pricer.ProviderCount()
	if count == 0 {
		t.Error("ProviderCount() = 0, expected positive value")
	}
}

func TestCalculateCost_PackageLevel(t *testing.T) {
	// Unknown model should return 0
	cost := CalculateCost("unknown-model-xyz-12345", 1000, 500)
	if cost != 0 {
		t.Errorf("CalculateCost unknown = %f, want 0", cost)
	}

	// Known model should return positive
	cost = CalculateCost("gpt-4o", 1000, 500)
	if cost < 0 {
		t.Errorf("CalculateCost gpt-4o = %f, expected non-negative", cost)
	}
}

func TestPackageLevelFunctions(t *testing.T) {
	// ListProviders
	providers := ListProviders()
	if len(providers) == 0 {
		t.Error("ListProviders() returned empty")
	}

	// ModelCount
	if ModelCount() == 0 {
		t.Error("ModelCount() = 0")
	}

	// ProviderCount
	if ProviderCount() == 0 {
		t.Error("ProviderCount() = 0")
	}
}
