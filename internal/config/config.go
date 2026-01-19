package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all server configuration
type Config struct {
	Server          ServerConfig              `yaml:"server"`
	TLS             TLSConfig                 `yaml:"tls"`
	Redis           RedisConfig               `yaml:"redis"`
	Database        DatabaseConfig            `yaml:"database"`
	Admin           AdminConfig               `yaml:"admin"`
	Auth            AuthConfig                `yaml:"auth"`
	RateLimits      RateLimitConfig           `yaml:"rate_limits"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
	Failover        FailoverConfig            `yaml:"failover"`
	Logging         LoggingConfig             `yaml:"logging"`
	StartupMode     StartupMode               `yaml:"startup_mode"`
	RAG             RAGConfig                 `yaml:"rag"`
	MarkdownSvcAddr string                    `yaml:"markdown_svc_addr"`
}

// DatabaseConfig holds PostgreSQL connection settings
type DatabaseConfig struct {
	Enabled        bool   `yaml:"enabled"`
	URL            string `yaml:"url"`
	MaxConnections int    `yaml:"max_connections"`
	LogQueries     bool   `yaml:"log_queries"`
}

// AdminConfig holds HTTP admin server settings
type AdminConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
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
	AuthMode   string `yaml:"auth_mode"` // "static" (default) or "redis"
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
	configPath := os.Getenv("AIRBORNE_CONFIG")
	if configPath == "" {
		configPath = "configs/airborne.yaml"
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
		Database: DatabaseConfig{
			Enabled:        false,
			MaxConnections: 10,
			LogQueries:     false,
		},
		Admin: AdminConfig{
			Enabled: false,
			Port:    50052,
		},
		Auth: AuthConfig{
			AuthMode: "static",
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
				DefaultModel: "gemini-3-pro-preview",
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
	if port := os.Getenv("AIRBORNE_GRPC_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			c.Server.GRPCPort = p
		} else {
			slog.Warn("invalid AIRBORNE_GRPC_PORT, using default", "value", port, "error", err)
		}
	}

	if host := os.Getenv("AIRBORNE_HOST"); host != "" {
		c.Server.Host = host
	}

	// TLS configuration
	if enabled := os.Getenv("AIRBORNE_TLS_ENABLED"); enabled != "" {
		if v, err := strconv.ParseBool(enabled); err == nil {
			c.TLS.Enabled = v
		} else {
			slog.Warn("invalid AIRBORNE_TLS_ENABLED, using default", "value", enabled, "error", err)
		}
	}
	if cert := os.Getenv("AIRBORNE_TLS_CERT_FILE"); cert != "" {
		c.TLS.CertFile = cert
	}
	if key := os.Getenv("AIRBORNE_TLS_KEY_FILE"); key != "" {
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
		} else {
			slog.Warn("invalid REDIS_DB, using default", "value", db, "error", err)
		}
	}

	// Database configuration
	if enabled := os.Getenv("DATABASE_ENABLED"); enabled != "" {
		if v, err := strconv.ParseBool(enabled); err == nil {
			c.Database.Enabled = v
		} else {
			slog.Warn("invalid DATABASE_ENABLED, using default", "value", enabled, "error", err)
		}
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		c.Database.URL = url
	} else {
		// Try to fetch DATABASE_URL from Doppler supabase project
		if dopplerURL := fetchDopplerSecret("supabase", "DATABASE_URL"); dopplerURL != "" {
			c.Database.URL = dopplerURL
			slog.Info("loaded DATABASE_URL from Doppler supabase project")
		}
	}
	// Auto-enable database if URL is configured
	if c.Database.URL != "" && !c.Database.Enabled {
		c.Database.Enabled = true
		slog.Info("auto-enabled database persistence")
	}
	if maxConn := os.Getenv("DATABASE_MAX_CONNECTIONS"); maxConn != "" {
		if n, err := strconv.Atoi(maxConn); err == nil {
			c.Database.MaxConnections = n
		} else {
			slog.Warn("invalid DATABASE_MAX_CONNECTIONS, using default", "value", maxConn, "error", err)
		}
	}
	if logQueries := os.Getenv("DATABASE_LOG_QUERIES"); logQueries != "" {
		if v, err := strconv.ParseBool(logQueries); err == nil {
			c.Database.LogQueries = v
		} else {
			slog.Warn("invalid DATABASE_LOG_QUERIES, using default", "value", logQueries, "error", err)
		}
	}

	// Admin HTTP server configuration
	if enabled := os.Getenv("ADMIN_ENABLED"); enabled != "" {
		if v, err := strconv.ParseBool(enabled); err == nil {
			c.Admin.Enabled = v
		} else {
			slog.Warn("invalid ADMIN_ENABLED, using default", "value", enabled, "error", err)
		}
	}
	if port := os.Getenv("ADMIN_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			c.Admin.Port = p
		} else {
			slog.Warn("invalid ADMIN_PORT, using default", "value", port, "error", err)
		}
	}

	if token := os.Getenv("AIRBORNE_ADMIN_TOKEN"); token != "" {
		c.Auth.AdminToken = token
	}

	if mode := os.Getenv("AIRBORNE_AUTH_MODE"); mode != "" {
		c.Auth.AuthMode = mode
	}

	if level := os.Getenv("AIRBORNE_LOG_LEVEL"); level != "" {
		c.Logging.Level = level
	}

	if format := os.Getenv("AIRBORNE_LOG_FORMAT"); format != "" {
		c.Logging.Format = format
	}

	if mode := os.Getenv("AIRBORNE_STARTUP_MODE"); mode != "" {
		c.StartupMode = StartupMode(mode)
	}

	// RAG configuration
	if enabled := os.Getenv("RAG_ENABLED"); enabled != "" {
		if v, err := strconv.ParseBool(enabled); err == nil {
			c.RAG.Enabled = v
		} else {
			slog.Warn("invalid RAG_ENABLED, using default", "value", enabled, "error", err)
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
		} else {
			slog.Warn("invalid RAG_CHUNK_SIZE, using default", "value", size, "error", err)
		}
	}
	if overlap := os.Getenv("RAG_CHUNK_OVERLAP"); overlap != "" {
		if o, err := strconv.Atoi(overlap); err == nil {
			c.RAG.ChunkOverlap = o
		} else {
			slog.Warn("invalid RAG_CHUNK_OVERLAP, using default", "value", overlap, "error", err)
		}
	}
	if topK := os.Getenv("RAG_RETRIEVAL_TOP_K"); topK != "" {
		if k, err := strconv.Atoi(topK); err == nil {
			c.RAG.RetrievalTopK = k
		} else {
			slog.Warn("invalid RAG_RETRIEVAL_TOP_K, using default", "value", topK, "error", err)
		}
	}

	// Markdown service configuration
	if addr := os.Getenv("MARKDOWN_SVC_ADDR"); addr != "" {
		c.MarkdownSvcAddr = addr
	}
}

// expandEnvVars expands ${VAR} patterns in string fields
func (c *Config) expandEnvVars() {
	c.Redis.Password = expandEnv(c.Redis.Password)
	c.Database.URL = expandEnv(c.Database.URL)
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

// fetchDopplerSecret fetches a single secret from Doppler.
// Returns empty string if DOPPLER_TOKEN is not set or on any error.
func fetchDopplerSecret(project, secretName string) string {
	token := os.Getenv("DOPPLER_TOKEN")
	if token == "" {
		return ""
	}

	config := os.Getenv("DOPPLER_CONFIG")
	if config == "" {
		config = "prod"
	}

	url := fmt.Sprintf("https://api.doppler.com/v3/configs/config/secrets?project=%s&config=%s", project, config)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Debug("doppler request creation failed", "error", err)
		return ""
	}
	req.SetBasicAuth(token, "")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("doppler request failed", "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Debug("doppler API error", "status", resp.StatusCode, "body", string(body))
		return ""
	}

	var result struct {
		Secrets map[string]struct {
			Raw string `json:"raw"`
		} `json:"secrets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Debug("doppler response decode failed", "error", err)
		return ""
	}

	if secret, ok := result.Secrets[secretName]; ok {
		return secret.Raw
	}
	return ""
}
