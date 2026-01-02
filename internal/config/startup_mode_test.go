package config

import "testing"

func TestStartupMode_IsProduction(t *testing.T) {
	tests := []struct {
		name     string
		mode     StartupMode
		expected bool
	}{
		{"production mode", StartupModeProduction, true},
		{"development mode", StartupModeDevelopment, false},
		{"empty string defaults to production", "", true},
		{"invalid value defaults to production", StartupMode("invalid"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.IsProduction(); got != tt.expected {
				t.Errorf("IsProduction() = %v, want %v", got, tt.expected)
			}
		})
	}
}
