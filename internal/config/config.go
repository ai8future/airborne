package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ai8future/airborne/internal/config/envutil"
	chassis "github.com/ai8future/chassis-go/v11"
	"github.com/ai8future/chassis-go/v11/call"
	"github.com/ai8future/chassis-go/v11/kafkakit"
)

// Deterministic ports derived from service name via djb2 hashing.
var (
	DefaultGRPCPort      = chassis.Port("airborne", chassis.PortGRPC)
	DefaultAdminPort     = chassis.Port("airborne", chassis.PortHTTP)
	dopplerClientFactory = func() *call.Client {
		return call.New(call.WithTimeout(10*time.Second), call.WithRetry(3, 1*time.Second))
	}
)

const minimumIdempotencyCompletedResponseRetention = 48 * time.Hour

// Config holds all server configuration
type Config struct {
	Server          ServerConfig              `yaml:"server"`
	TLS             TLSConfig                 `yaml:"tls"`
	Redis           RedisConfig               `yaml:"redis"`
	Database        DatabaseConfig            `yaml:"database"`
	Admin           AdminConfig               `yaml:"admin"`
	Auth            AuthConfig                `yaml:"auth"`
	Idempotency     IdempotencyConfig         `yaml:"idempotency"`
	RateLimits      RateLimitConfig           `yaml:"rate_limits"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
	Failover        FailoverConfig            `yaml:"failover"`
	Logging         LoggingConfig             `yaml:"logging"`
	StartupMode     StartupMode               `yaml:"startup_mode"`
	RAG             RAGConfig                 `yaml:"rag"`
	MarkdownSvcAddr string                    `yaml:"markdown_svc_addr"`

	// Event bus (kafkakit) — enables heartbeatkit + announcekit automatically.
	Kafkakit KafkakitConfig `yaml:"kafkakit"`
}

// KafkakitConfig holds event bus configuration.
type KafkakitConfig struct {
	BootstrapServers  string `yaml:"bootstrap_servers"`
	SchemaRegistryURL string `yaml:"schema_registry_url"`
	TenantID          string `yaml:"tenant_id"`
	Source            string `yaml:"source"`
}

// ToKafkakit converts to the chassis kafkakit.Config.
func (c *KafkakitConfig) ToKafkakit() kafkakit.Config {
	return kafkakit.Config{
		BootstrapServers:  c.BootstrapServers,
		SchemaRegistryURL: c.SchemaRegistryURL,
		TenantID:          c.TenantID,
		Source:            c.Source,
	}
}

// DatabaseConfig holds PostgreSQL connection settings
type DatabaseConfig struct {
	Enabled        bool   `yaml:"enabled"`
	URL            string `yaml:"url"`
	MaxConnections int    `yaml:"max_connections"`
	LogQueries     bool   `yaml:"log_queries"`
	CACert         string `yaml:"ca_cert"` // PEM-encoded CA certificate for SSL verification
}

// AdminConfig holds HTTP admin server settings
type AdminConfig struct {
	Enabled        bool     `yaml:"enabled"`
	Port           int      `yaml:"port"`
	AllowedOrigins []string `yaml:"allowed_origins"`
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

// IdempotencyConfig controls completed GenerateReply replay retention.
type IdempotencyConfig struct {
	CompletedResponseRetention time.Duration `yaml:"completed_response_retention"`
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

// Load loads configuration from file and environment variables.
// If AIRBORNE_USE_FROZEN is set to "true", loads from frozen config instead.
func Load() (*Config, error) {
	// Check if we should use frozen config
	if os.Getenv("AIRBORNE_USE_FROZEN") == "true" {
		frozenPath := os.Getenv("AIRBORNE_FROZEN_CONFIG_PATH")
		if frozenPath == "" {
			frozenPath = "configs/frozen.json"
		}
		slog.Info("Loading frozen configuration", "path", frozenPath)
		return LoadFrozen(frozenPath)
	}

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
	if err := cfg.applyEnvOverrides(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Expand environment variables in string fields
	cfg.expandEnvVars()

	// Validate
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// FrozenConfig represents a fully-resolved, validated configuration snapshot.
// This matches the structure written by airborne-freeze command.
type FrozenConfig struct {
	GlobalConfig  *Config     `json:"global_config"`
	TenantConfigs interface{} `json:"tenant_configs"` // Opaque - handled by tenant package
	FrozenAt      string      `json:"frozen_at"`
	SingleTenant  bool        `json:"single_tenant"`
}

// LoadFrozen loads a pre-frozen configuration from JSON.
// This bypasses all Doppler fetches, env var resolution, and complex loading logic.
// Use this in production after running `airborne-freeze` to generate frozen.json
func LoadFrozen(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read frozen config: %w", err)
	}

	var frozen FrozenConfig
	if err := json.Unmarshal(data, &frozen); err != nil {
		return nil, fmt.Errorf("failed to parse frozen config: %w", err)
	}

	cfg := frozen.GlobalConfig
	if cfg == nil {
		return nil, fmt.Errorf("frozen config missing global_config")
	}

	// Resolve ENV=/FILE= references in config
	cfg.expandEnvVars()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid frozen configuration: %w", err)
	}
	return cfg, nil
}

// defaultConfig returns configuration with sensible defaults
func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			GRPCPort: DefaultGRPCPort,
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
			Port:    DefaultAdminPort,
			AllowedOrigins: []string{
				"http://localhost:3000",
				"http://127.0.0.1:3000",
				"http://localhost:4848",
				"http://127.0.0.1:4848",
			},
		},
		Auth: AuthConfig{
			AuthMode: "static",
		},
		Idempotency: IdempotencyConfig{
			CompletedResponseRetention: minimumIdempotencyCompletedResponseRetention,
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
		Kafkakit: KafkakitConfig{
			Source: "airborne",
		},
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
func (c *Config) applyEnvOverrides() error {
	// Server configuration
	c.Server.GRPCPort = envutil.GetIntEnv("AIRBORNE_GRPC_PORT", c.Server.GRPCPort)
	c.Server.Host = envutil.GetStringEnv("AIRBORNE_HOST", c.Server.Host)

	// TLS configuration
	c.TLS.Enabled = envutil.GetBoolEnv("AIRBORNE_TLS_ENABLED", c.TLS.Enabled)
	c.TLS.CertFile = envutil.GetStringEnv("AIRBORNE_TLS_CERT_FILE", c.TLS.CertFile)
	c.TLS.KeyFile = envutil.GetStringEnv("AIRBORNE_TLS_KEY_FILE", c.TLS.KeyFile)

	// Redis configuration
	c.Redis.Addr = envutil.GetStringEnv("REDIS_ADDR", c.Redis.Addr)
	c.Redis.Password = envutil.GetStringEnv("REDIS_PASSWORD", c.Redis.Password)
	c.Redis.DB = envutil.GetIntEnv("REDIS_DB", c.Redis.DB)

	// Database configuration
	c.Database.Enabled = envutil.GetBoolEnv("DATABASE_ENABLED", c.Database.Enabled)

	// Database URL - try environment first, then Doppler
	if url := os.Getenv("DATABASE_URL"); url != "" {
		c.Database.URL = url
	} else {
		// Try to fetch DATABASE_URL from Doppler supabase project
		if dopplerURL := fetchDopplerSecret("supabase", "DATABASE_URL"); dopplerURL != "" {
			c.Database.URL = dopplerURL
			fmt.Fprintf(os.Stderr, "config: loaded DATABASE_URL from Doppler supabase project\n")
		}
	}

	// CA Certificate - try environment first, then Doppler
	if caCert := os.Getenv("SUPABASE_CA_CERT"); caCert != "" {
		c.Database.CACert = caCert
		fmt.Fprintf(os.Stderr, "config: loaded SUPABASE_CA_CERT from environment\n")
	} else if c.Database.URL != "" {
		// Try to fetch from Doppler supabase project
		if dopplerCert := fetchDopplerSecret("supabase", "SUPABASE_CA_CERT"); dopplerCert != "" {
			c.Database.CACert = dopplerCert
			fmt.Fprintf(os.Stderr, "config: loaded SUPABASE_CA_CERT from Doppler supabase project\n")
		}
	}

	// Database must be explicitly enabled - do not auto-enable to avoid production surprises
	if c.Database.URL != "" && !c.Database.Enabled {
		fmt.Fprintf(os.Stderr, "WARNING: DATABASE_URL is set but database is not enabled. Set DATABASE_ENABLED=true to use database persistence.\n")
	}

	c.Database.MaxConnections = envutil.GetIntEnv("DATABASE_MAX_CONNECTIONS", c.Database.MaxConnections)
	c.Database.LogQueries = envutil.GetBoolEnv("DATABASE_LOG_QUERIES", c.Database.LogQueries)

	// Admin HTTP server configuration
	c.Admin.Enabled = envutil.GetBoolEnv("ADMIN_ENABLED", c.Admin.Enabled)
	c.Admin.Port = envutil.GetIntEnv("ADMIN_PORT", c.Admin.Port)
	if origins := splitCSVEnv("ADMIN_ALLOWED_ORIGINS"); len(origins) > 0 {
		c.Admin.AllowedOrigins = origins
	}

	// Auth configuration
	c.Auth.AdminToken = envutil.GetStringEnv("AIRBORNE_ADMIN_TOKEN", c.Auth.AdminToken)
	c.Auth.AuthMode = envutil.GetStringEnv("AIRBORNE_AUTH_MODE", c.Auth.AuthMode)

	if value := os.Getenv("IDEMPOTENCY_COMPLETED_RESPONSE_RETENTION"); value != "" {
		retention, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid IDEMPOTENCY_COMPLETED_RESPONSE_RETENTION %q: %w", value, err)
		}
		c.Idempotency.CompletedResponseRetention = retention
	}

	// Logging configuration
	c.Logging.Level = envutil.GetStringEnv("AIRBORNE_LOG_LEVEL", c.Logging.Level)
	c.Logging.Format = envutil.GetStringEnv("AIRBORNE_LOG_FORMAT", c.Logging.Format)

	// Startup mode
	if mode := os.Getenv("AIRBORNE_STARTUP_MODE"); mode != "" {
		c.StartupMode = StartupMode(mode)
	}

	// RAG configuration
	c.RAG.Enabled = envutil.GetBoolEnv("RAG_ENABLED", c.RAG.Enabled)
	c.RAG.OllamaURL = envutil.GetStringEnv("RAG_OLLAMA_URL", c.RAG.OllamaURL)
	c.RAG.EmbeddingModel = envutil.GetStringEnv("RAG_EMBEDDING_MODEL", c.RAG.EmbeddingModel)
	c.RAG.QdrantURL = envutil.GetStringEnv("RAG_QDRANT_URL", c.RAG.QdrantURL)
	c.RAG.DocboxURL = envutil.GetStringEnv("RAG_DOCBOX_URL", c.RAG.DocboxURL)
	c.RAG.ChunkSize = envutil.GetIntEnv("RAG_CHUNK_SIZE", c.RAG.ChunkSize)
	c.RAG.ChunkOverlap = envutil.GetIntEnv("RAG_CHUNK_OVERLAP", c.RAG.ChunkOverlap)
	c.RAG.RetrievalTopK = envutil.GetIntEnv("RAG_RETRIEVAL_TOP_K", c.RAG.RetrievalTopK)

	// Markdown service configuration
	c.MarkdownSvcAddr = envutil.GetStringEnv("MARKDOWN_SVC_ADDR", c.MarkdownSvcAddr)

	// Kafkakit configuration
	c.Kafkakit.BootstrapServers = envutil.GetStringEnv("KAFKAKIT_BOOTSTRAP_SERVERS", c.Kafkakit.BootstrapServers)
	c.Kafkakit.SchemaRegistryURL = envutil.GetStringEnv("KAFKAKIT_SCHEMA_REGISTRY_URL", c.Kafkakit.SchemaRegistryURL)
	c.Kafkakit.TenantID = envutil.GetStringEnv("KAFKAKIT_TENANT_ID", c.Kafkakit.TenantID)
	c.Kafkakit.Source = envutil.GetStringEnv("KAFKAKIT_SOURCE", c.Kafkakit.Source)

	return nil
}

// expandEnvVars expands ${VAR} patterns in string fields
func (c *Config) expandEnvVars() {
	c.Redis.Password = expandEnv(c.Redis.Password)
	c.Database.URL = expandEnv(c.Database.URL)
	c.Database.CACert = expandEnv(c.Database.CACert)
	c.Auth.AdminToken = expandEnv(c.Auth.AdminToken)
	c.TLS.CertFile = expandEnv(c.TLS.CertFile)
	c.TLS.KeyFile = expandEnv(c.TLS.KeyFile)
}

// expandEnv expands environment variable patterns in a string.
// Supports ENV=VAR_NAME (used by frozen configs), ${VAR}, and $VAR syntax.
func expandEnv(s string) string {
	// Handle ENV= prefix (used by frozen configs)
	if strings.HasPrefix(s, "ENV=") {
		varName := strings.TrimPrefix(s, "ENV=")
		return os.Getenv(varName)
	}

	// Handle ${VAR} syntax
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		varName := s[2 : len(s)-1]
		return os.Getenv(varName)
	}

	// Handle $VAR syntax and passthrough non-variable strings
	return os.ExpandEnv(s)
}

func splitCSVEnv(key string) []string {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// validate checks configuration validity
func (c *Config) validate() error {
	if c.Server.GRPCPort <= 0 || c.Server.GRPCPort > 65535 {
		return fmt.Errorf("invalid grpc_port: %d", c.Server.GRPCPort)
	}
	if err := validateAdminAllowedOrigins(c.Admin.AllowedOrigins); err != nil {
		return err
	}
	if c.Idempotency.CompletedResponseRetention < minimumIdempotencyCompletedResponseRetention {
		return fmt.Errorf(
			"idempotency.completed_response_retention must be at least %s, got %s",
			minimumIdempotencyCompletedResponseRetention,
			c.Idempotency.CompletedResponseRetention,
		)
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
		// Fatal error - do not allow invalid startup modes
		return fmt.Errorf("invalid startup_mode %q, must be 'production' or 'development'", c.StartupMode)
	}

	return nil
}

func validateAdminAllowedOrigins(origins []string) error {
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			return fmt.Errorf("admin.allowed_origins wildcard is not allowed")
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid admin.allowed_origins entry %q", origin)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("invalid admin.allowed_origins scheme %q", parsed.Scheme)
		}
		if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
			return fmt.Errorf("admin.allowed_origins entry %q must be an origin only", origin)
		}
	}
	return nil
}

// fetchDopplerSecret fetches a single secret from Doppler.
// Returns empty string if DOPPLER_TOKEN is not set or on any error.
// Note: This runs before logger is configured, so we use fmt.Fprintf for errors.
func fetchDopplerSecret(project, secretName string) string {
	token := os.Getenv("DOPPLER_TOKEN")
	if token == "" {
		return ""
	}

	config := os.Getenv("DOPPLER_CONFIG")
	if config == "" {
		config = "prod"
	}

	endpoint, err := url.Parse("https://api.doppler.com/v3/configs/config/secrets")
	if err != nil {
		fmt.Fprintf(os.Stderr, "doppler: invalid endpoint: %v\n", err)
		return ""
	}
	query := endpoint.Query()
	query.Set("project", project)
	query.Set("config", config)
	endpoint.RawQuery = query.Encode()

	client := dopplerClientFactory()
	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "doppler: request creation failed: %v\n", err)
		return ""
	}
	req.SetBasicAuth(token, "")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "doppler: request failed: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "doppler: API error (status %d): %s\n", resp.StatusCode, string(body))
		return ""
	}

	var result struct {
		Secrets map[string]struct {
			Raw string `json:"raw"`
		} `json:"secrets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "doppler: response decode failed: %v\n", err)
		return ""
	}

	if secret, ok := result.Secrets[secretName]; ok {
		return secret.Raw
	}
	fmt.Fprintf(os.Stderr, "doppler: secret %s not found in project %s\n", secretName, project)
	return ""
}
