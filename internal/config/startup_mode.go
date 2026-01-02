package config

// StartupMode defines how the server behaves with missing dependencies
type StartupMode string

const (
	// StartupModeProduction requires all dependencies (Redis, etc.) to be available
	StartupModeProduction StartupMode = "production"

	// StartupModeDevelopment allows running without optional dependencies
	StartupModeDevelopment StartupMode = "development"
)

// IsProduction returns true if running in production mode
// Unknown/invalid modes default to production for safety
func (m StartupMode) IsProduction() bool {
	return m != StartupModeDevelopment
}
