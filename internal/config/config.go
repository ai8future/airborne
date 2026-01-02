package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all server configuration
type Config struct {
	Server      ServerConfig              `yaml:"server"`
	TLS         TLSConfig                 `yaml:"tls"`
	Redis       RedisConfig               `yaml:"redis"`
	Auth        AuthConfig                `yaml:"auth"`
	RateLimits  RateLimitConfig           `yaml:"rate_limits"`
	Providers   map[string]ProviderConfig `yaml:"providers"`
	Failover    FailoverConfig            `yaml:"failover"`
	Logging     LoggingConfig             `yaml:"logging"`
	StartupMode StartupMode               `yaml:"startup_mode"`
}

// ServerConfig holds server settings
type ServerConfig struct {
	GRPCPort int    `yaml:"grpc_port"`
	Host     string `yaml:"host"`
}

// TLSConfig holds TLS settings
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// AuthConfig holds authentication settings
type AuthConfig struct {
	AdminToken string `yaml:"admin_token"`
}

// RateLimitConfig holds default rate limits
type RateLimitConfig struct {
	DefaultRPM int `yaml:"default_rpm"` // Requests per minute
	DefaultRPD int `yaml:"default_rpd"` // Requests per day
	DefaultTPM int `yaml:"default_tpm"` // Tokens per minute
}

// ProviderConfig holds provider-specific settings
type ProviderConfig struct {
	Enabled      bool   `yaml:"enabled"`
	DefaultModel string `yaml:"default_model"`
	BaseURL      string `yaml:"base_url"`
}

// FailoverConfig holds failover settings
type FailoverConfig struct {
	Enabled      bool     `yaml:"enabled"`
	DefaultOrder []string `yaml:"default_order"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load loads configuration from file and environment variables
func Load() (*Config, error) {
	cfg := defaultConfig()

	// Try to load from file
	configPath := os.Getenv("AIBOX_CONFIG")
	if configPath == "" {
		configPath = "configs/aibox.yaml"
	}

	if data, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// Override with environment variables
	cfg.applyEnvOverrides()

	// Expand environment variables in string fields
	cfg.expandEnvVars()

	// Validate
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// defaultConfig returns configuration with sensible defaults
func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			GRPCPort: 50051,
			Host:     "0.0.0.0",
		},
		TLS: TLSConfig{
			Enabled: false,
		},
		Redis: RedisConfig{
			Addr: "localhost:6379",
			DB:   0,
		},
		RateLimits: RateLimitConfig{
			DefaultRPM: 60,
			DefaultRPD: 10000,
			DefaultTPM: 100000,
		},
		Providers: map[string]ProviderConfig{
			"openai": {
				Enabled:      true,
				DefaultModel: "gpt-4o",
			},
			"gemini": {
				Enabled:      true,
				DefaultModel: "gemini-2.0-flash",
			},
			"anthropic": {
				Enabled:      true,
				DefaultModel: "claude-sonnet-4-20250514",
			},
		},
		Failover: FailoverConfig{
			Enabled:      true,
			DefaultOrder: []string{"openai", "gemini", "anthropic"},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		StartupMode: StartupModeProduction,
	}
}

// applyEnvOverrides applies environment variable overrides
func (c *Config) applyEnvOverrides() {
	if port := os.Getenv("AIBOX_GRPC_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			c.Server.GRPCPort = p
		}
	}

	if host := os.Getenv("AIBOX_HOST"); host != "" {
		c.Server.Host = host
	}

	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		c.Redis.Addr = addr
	}

	if pass := os.Getenv("REDIS_PASSWORD"); pass != "" {
		c.Redis.Password = pass
	}

	if token := os.Getenv("AIBOX_ADMIN_TOKEN"); token != "" {
		c.Auth.AdminToken = token
	}

	if level := os.Getenv("AIBOX_LOG_LEVEL"); level != "" {
		c.Logging.Level = level
	}
}

// expandEnvVars expands ${VAR} patterns in string fields
func (c *Config) expandEnvVars() {
	c.Redis.Password = expandEnv(c.Redis.Password)
	c.Auth.AdminToken = expandEnv(c.Auth.AdminToken)
	c.TLS.CertFile = expandEnv(c.TLS.CertFile)
	c.TLS.KeyFile = expandEnv(c.TLS.KeyFile)
}

// expandEnv expands ${VAR} patterns in a string
func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		varName := s[2 : len(s)-1]
		return os.Getenv(varName)
	}
	return os.ExpandEnv(s)
}

// validate checks configuration validity
func (c *Config) validate() error {
	if c.Server.GRPCPort <= 0 || c.Server.GRPCPort > 65535 {
		return fmt.Errorf("invalid grpc_port: %d", c.Server.GRPCPort)
	}

	if c.TLS.Enabled {
		if c.TLS.CertFile == "" {
			return fmt.Errorf("tls.cert_file required when TLS is enabled")
		}
		if c.TLS.KeyFile == "" {
			return fmt.Errorf("tls.key_file required when TLS is enabled")
		}
	}

	return nil
}
