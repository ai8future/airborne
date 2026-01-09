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
	RAG         RAGConfig                 `yaml:"rag"`
}

// RAGConfig holds RAG (Retrieval-Augmented Generation) settings
type RAGConfig struct {
	Enabled        bool   `yaml:"enabled"`
	OllamaURL      string `yaml:"ollama_url"`
	EmbeddingModel string `yaml:"embedding_model"`
	QdrantURL      string `yaml:"qdrant_url"`
	DocboxURL      string `yaml:"docbox_url"`
	ChunkSize      int    `yaml:"chunk_size"`
	ChunkOverlap   int    `yaml:"chunk_overlap"`
	RetrievalTopK  int    `yaml:"retrieval_top_k"`
}

// ServerConfig holds server settings
type ServerConfig struct {
	GRPCPort  int    `yaml:"grpc_port"`
	AdminPort int    `yaml:"admin_port"` // HTTP admin UI port (0 to disable)
	Host      string `yaml:"host"`
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

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// File doesn't exist - continue with defaults
	} else {
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
		RAG: RAGConfig{
			Enabled:        false,
			OllamaURL:      "http://localhost:11434",
			EmbeddingModel: "nomic-embed-text",
			QdrantURL:      "http://localhost:6333",
			DocboxURL:      "http://localhost:41273",
			ChunkSize:      2000,
			ChunkOverlap:   200,
			RetrievalTopK:  5,
		},
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

	if port := os.Getenv("AIBOX_ADMIN_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			c.Server.AdminPort = p
		}
	}

	// TLS configuration
	if enabled := os.Getenv("AIBOX_TLS_ENABLED"); enabled != "" {
		if v, err := strconv.ParseBool(enabled); err == nil {
			c.TLS.Enabled = v
		}
	}
	if cert := os.Getenv("AIBOX_TLS_CERT_FILE"); cert != "" {
		c.TLS.CertFile = cert
	}
	if key := os.Getenv("AIBOX_TLS_KEY_FILE"); key != "" {
		c.TLS.KeyFile = key
	}

	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		c.Redis.Addr = addr
	}

	if pass := os.Getenv("REDIS_PASSWORD"); pass != "" {
		c.Redis.Password = pass
	}

	if db := os.Getenv("REDIS_DB"); db != "" {
		if d, err := strconv.Atoi(db); err == nil {
			c.Redis.DB = d
		}
	}

	if token := os.Getenv("AIBOX_ADMIN_TOKEN"); token != "" {
		c.Auth.AdminToken = token
	}

	if level := os.Getenv("AIBOX_LOG_LEVEL"); level != "" {
		c.Logging.Level = level
	}

	if format := os.Getenv("AIBOX_LOG_FORMAT"); format != "" {
		c.Logging.Format = format
	}

	if mode := os.Getenv("AIBOX_STARTUP_MODE"); mode != "" {
		c.StartupMode = StartupMode(mode)
	}

	// RAG configuration
	if enabled := os.Getenv("RAG_ENABLED"); enabled != "" {
		if v, err := strconv.ParseBool(enabled); err == nil {
			c.RAG.Enabled = v
		}
	}
	if url := os.Getenv("RAG_OLLAMA_URL"); url != "" {
		c.RAG.OllamaURL = url
	}
	if model := os.Getenv("RAG_EMBEDDING_MODEL"); model != "" {
		c.RAG.EmbeddingModel = model
	}
	if url := os.Getenv("RAG_QDRANT_URL"); url != "" {
		c.RAG.QdrantURL = url
	}
	if url := os.Getenv("RAG_DOCBOX_URL"); url != "" {
		c.RAG.DocboxURL = url
	}
	if size := os.Getenv("RAG_CHUNK_SIZE"); size != "" {
		if s, err := strconv.Atoi(size); err == nil {
			c.RAG.ChunkSize = s
		}
	}
	if overlap := os.Getenv("RAG_CHUNK_OVERLAP"); overlap != "" {
		if o, err := strconv.Atoi(overlap); err == nil {
			c.RAG.ChunkOverlap = o
		}
	}
	if topK := os.Getenv("RAG_RETRIEVAL_TOP_K"); topK != "" {
		if k, err := strconv.Atoi(topK); err == nil {
			c.RAG.RetrievalTopK = k
		}
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

	// Validate startup mode
	switch c.StartupMode {
	case StartupModeProduction, StartupModeDevelopment, "":
		// Valid modes
	default:
		// Log warning but treat as production (fail-safe)
		fmt.Fprintf(os.Stderr, "Warning: unrecognized startup_mode %q, defaulting to production\n", c.StartupMode)
	}

	return nil
}
