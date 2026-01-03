package tenant

import (
	"fmt"
	"os"
	"strconv"
)

// EnvConfig holds environment-level (process-wide) settings.
// These are shared across all tenants.
type EnvConfig struct {
	// ConfigsDir is the directory containing tenant config files
	ConfigsDir string

	// Server settings
	GRPCPort int
	Host     string

	// TLS settings
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string

	// Redis settings
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// Logging settings
	LogLevel  string
	LogFormat string

	// Admin token (for backwards compatibility - tenants can override)
	AdminToken string
}

// loadEnv loads environment configuration from environment variables.
func loadEnv() (EnvConfig, error) {
	cfg := EnvConfig{
		// Defaults
		ConfigsDir: "configs",
		GRPCPort:   50051,
		Host:       "0.0.0.0",
		RedisAddr:  "localhost:6379",
		RedisDB:    0,
		LogLevel:   "info",
		LogFormat:  "json",
	}

	// Override with environment variables
	if dir := os.Getenv("AIBOX_CONFIGS_DIR"); dir != "" {
		cfg.ConfigsDir = dir
	}

	if port := os.Getenv("AIBOX_GRPC_PORT"); port != "" {
		p, err := strconv.Atoi(port)
		if err != nil {
			return EnvConfig{}, fmt.Errorf("invalid AIBOX_GRPC_PORT: %w", err)
		}
		cfg.GRPCPort = p
	}

	if host := os.Getenv("AIBOX_HOST"); host != "" {
		cfg.Host = host
	}

	// TLS
	if os.Getenv("AIBOX_TLS_ENABLED") == "true" {
		cfg.TLSEnabled = true
	}
	if cert := os.Getenv("AIBOX_TLS_CERT_FILE"); cert != "" {
		cfg.TLSCertFile = cert
	}
	if key := os.Getenv("AIBOX_TLS_KEY_FILE"); key != "" {
		cfg.TLSKeyFile = key
	}

	// Redis
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		cfg.RedisAddr = addr
	}
	if pass := os.Getenv("REDIS_PASSWORD"); pass != "" {
		cfg.RedisPassword = pass
	}
	if db := os.Getenv("REDIS_DB"); db != "" {
		d, err := strconv.Atoi(db)
		if err != nil {
			return EnvConfig{}, fmt.Errorf("invalid REDIS_DB: %w", err)
		}
		cfg.RedisDB = d
	}

	// Logging
	if level := os.Getenv("AIBOX_LOG_LEVEL"); level != "" {
		cfg.LogLevel = level
	}
	if format := os.Getenv("AIBOX_LOG_FORMAT"); format != "" {
		cfg.LogFormat = format
	}

	// Admin token
	if token := os.Getenv("AIBOX_ADMIN_TOKEN"); token != "" {
		cfg.AdminToken = token
	}

	// Validate
	if err := cfg.validate(); err != nil {
		return EnvConfig{}, err
	}

	return cfg, nil
}

func (c EnvConfig) validate() error {
	if c.GRPCPort <= 0 || c.GRPCPort > 65535 {
		return fmt.Errorf("invalid grpc_port: %d", c.GRPCPort)
	}

	if c.TLSEnabled {
		if c.TLSCertFile == "" {
			return fmt.Errorf("TLS_CERT_FILE required when TLS is enabled")
		}
		if c.TLSKeyFile == "" {
			return fmt.Errorf("TLS_KEY_FILE required when TLS is enabled")
		}
	}

	return nil
}
