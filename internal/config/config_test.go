package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_DefaultValues(t *testing.T) {
	// Point to non-existent config to use defaults
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Server defaults
	if cfg.Server.GRPCPort != 50051 {
		t.Errorf("expected default GRPCPort 50051, got %d", cfg.Server.GRPCPort)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected default Host 0.0.0.0, got %s", cfg.Server.Host)
	}

	// TLS defaults
	if cfg.TLS.Enabled {
		t.Error("expected TLS disabled by default")
	}

	// Redis defaults
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("expected default Redis.Addr localhost:6379, got %s", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 0 {
		t.Errorf("expected default Redis.DB 0, got %d", cfg.Redis.DB)
	}

	// Logging defaults
	if cfg.Logging.Level != "info" {
		t.Errorf("expected default Logging.Level info, got %s", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("expected default Logging.Format json, got %s", cfg.Logging.Format)
	}

	// RAG defaults
	if cfg.RAG.Enabled {
		t.Error("expected RAG disabled by default")
	}
	if cfg.RAG.ChunkSize != 2000 {
		t.Errorf("expected default RAG.ChunkSize 2000, got %d", cfg.RAG.ChunkSize)
	}

	// StartupMode default
	if cfg.StartupMode != StartupModeProduction {
		t.Errorf("expected default StartupMode production, got %s", cfg.StartupMode)
	}
}

func TestLoad_TLSEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("AIRBORNE_TLS_ENABLED", "true")
	t.Setenv("AIRBORNE_TLS_CERT_FILE", "/path/to/cert.pem")
	t.Setenv("AIRBORNE_TLS_KEY_FILE", "/path/to/key.pem")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if !cfg.TLS.Enabled {
		t.Error("expected TLS.Enabled true from env")
	}
	if cfg.TLS.CertFile != "/path/to/cert.pem" {
		t.Errorf("expected TLS.CertFile from env, got %s", cfg.TLS.CertFile)
	}
	if cfg.TLS.KeyFile != "/path/to/key.pem" {
		t.Errorf("expected TLS.KeyFile from env, got %s", cfg.TLS.KeyFile)
	}
}

func TestLoad_TLSEnvOverrides_DisableTLS(t *testing.T) {
	dir := t.TempDir()

	// Create config with TLS enabled
	cfgYAML := `
tls:
  enabled: true
  cert_file: /yaml/cert.pem
  key_file: /yaml/key.pem
`
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("AIRBORNE_CONFIG", cfgPath)

	// Disable TLS via env
	t.Setenv("AIRBORNE_TLS_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.TLS.Enabled {
		t.Error("expected TLS.Enabled false from env override")
	}
}

func TestLoad_RedisDBEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("REDIS_DB", "5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Redis.DB != 5 {
		t.Errorf("expected Redis.DB 5 from env, got %d", cfg.Redis.DB)
	}
}

func TestLoad_RedisDBEnvOverride_InvalidValue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("REDIS_DB", "not-a-number")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Should keep default when invalid
	if cfg.Redis.DB != 0 {
		t.Errorf("expected Redis.DB 0 (default) for invalid env, got %d", cfg.Redis.DB)
	}
}

func TestLoad_LogFormatEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("AIRBORNE_LOG_FORMAT", "text")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Logging.Format != "text" {
		t.Errorf("expected Logging.Format text from env, got %s", cfg.Logging.Format)
	}
}

func TestLoad_LogLevelEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("AIRBORNE_LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Logging.Level != "debug" {
		t.Errorf("expected Logging.Level debug from env, got %s", cfg.Logging.Level)
	}
}

func TestLoad_MissingConfigFile_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	nonexistentPath := filepath.Join(dir, "does_not_exist.yaml")
	t.Setenv("AIRBORNE_CONFIG", nonexistentPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() should not fail for missing config file: %v", err)
	}

	// Verify defaults are used
	if cfg.Server.GRPCPort != 50051 {
		t.Errorf("expected default port, got %d", cfg.Server.GRPCPort)
	}
}

func TestLoad_ConfigReadError_Fails(t *testing.T) {
	dir := t.TempDir()

	// Create a directory with the config name (will cause read error)
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.Mkdir(configPath, 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	t.Setenv("AIRBORNE_CONFIG", configPath)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when config path is a directory")
	}
}

func TestLoad_InvalidYAML_Fails(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Write invalid YAML
	if err := os.WriteFile(cfgPath, []byte("server: {invalid: yaml: content}"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("AIRBORNE_CONFIG", cfgPath)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()

	// Create YAML config
	cfgYAML := `
server:
  grpc_port: 9000
  host: 127.0.0.1
redis:
  addr: redis.local:6379
  db: 2
logging:
  level: warn
  format: json
`
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("AIRBORNE_CONFIG", cfgPath)

	// Override some values via env
	t.Setenv("AIRBORNE_GRPC_PORT", "8888")
	t.Setenv("REDIS_DB", "7")
	t.Setenv("AIRBORNE_LOG_FORMAT", "text")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Env should override YAML
	if cfg.Server.GRPCPort != 8888 {
		t.Errorf("expected GRPCPort 8888 from env, got %d", cfg.Server.GRPCPort)
	}
	if cfg.Redis.DB != 7 {
		t.Errorf("expected Redis.DB 7 from env, got %d", cfg.Redis.DB)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("expected Format text from env, got %s", cfg.Logging.Format)
	}

	// Non-overridden values should come from YAML
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected Host from YAML, got %s", cfg.Server.Host)
	}
	if cfg.Redis.Addr != "redis.local:6379" {
		t.Errorf("expected Redis.Addr from YAML, got %s", cfg.Redis.Addr)
	}
	if cfg.Logging.Level != "warn" {
		t.Errorf("expected Level from YAML, got %s", cfg.Logging.Level)
	}
}

func TestLoad_TLSValidation_EnabledWithoutCert(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("AIRBORNE_TLS_ENABLED", "true")
	// Don't set cert file

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error when TLS enabled without cert")
	}
}

func TestLoad_TLSValidation_EnabledWithoutKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("AIRBORNE_TLS_ENABLED", "true")
	t.Setenv("AIRBORNE_TLS_CERT_FILE", "/path/to/cert.pem")
	// Don't set key file

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error when TLS enabled without key")
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("AIRBORNE_GRPC_PORT", "99999")

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error for invalid port")
	}
}

func TestLoad_RAGEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("RAG_ENABLED", "true")
	t.Setenv("RAG_OLLAMA_URL", "http://ollama.local:11434")
	t.Setenv("RAG_EMBEDDING_MODEL", "custom-model")
	t.Setenv("RAG_QDRANT_URL", "http://qdrant.local:6333")
	t.Setenv("RAG_DOCBOX_URL", "http://docbox.local:41273")
	t.Setenv("RAG_CHUNK_SIZE", "1500")
	t.Setenv("RAG_CHUNK_OVERLAP", "150")
	t.Setenv("RAG_RETRIEVAL_TOP_K", "10")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if !cfg.RAG.Enabled {
		t.Error("expected RAG.Enabled true from env")
	}
	if cfg.RAG.OllamaURL != "http://ollama.local:11434" {
		t.Errorf("expected RAG.OllamaURL from env, got %s", cfg.RAG.OllamaURL)
	}
	if cfg.RAG.EmbeddingModel != "custom-model" {
		t.Errorf("expected RAG.EmbeddingModel from env, got %s", cfg.RAG.EmbeddingModel)
	}
	if cfg.RAG.QdrantURL != "http://qdrant.local:6333" {
		t.Errorf("expected RAG.QdrantURL from env, got %s", cfg.RAG.QdrantURL)
	}
	if cfg.RAG.DocboxURL != "http://docbox.local:41273" {
		t.Errorf("expected RAG.DocboxURL from env, got %s", cfg.RAG.DocboxURL)
	}
	if cfg.RAG.ChunkSize != 1500 {
		t.Errorf("expected RAG.ChunkSize 1500 from env, got %d", cfg.RAG.ChunkSize)
	}
	if cfg.RAG.ChunkOverlap != 150 {
		t.Errorf("expected RAG.ChunkOverlap 150 from env, got %d", cfg.RAG.ChunkOverlap)
	}
	if cfg.RAG.RetrievalTopK != 10 {
		t.Errorf("expected RAG.RetrievalTopK 10 from env, got %d", cfg.RAG.RetrievalTopK)
	}
}

func TestLoad_RAGEnabled_DisableViaEnv(t *testing.T) {
	dir := t.TempDir()

	// Create config with RAG enabled
	cfgYAML := `
rag:
  enabled: true
  ollama_url: http://localhost:11434
`
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("AIRBORNE_CONFIG", cfgPath)

	// Disable RAG via env
	t.Setenv("RAG_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.RAG.Enabled {
		t.Error("expected RAG.Enabled false from env override")
	}
}

func TestLoad_StartupModeEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("AIRBORNE_STARTUP_MODE", "development")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.StartupMode != StartupModeDevelopment {
		t.Errorf("expected StartupMode development from env, got %s", cfg.StartupMode)
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	dir := t.TempDir()

	// Create config with env var syntax
	cfgYAML := `
redis:
  password: ${TEST_REDIS_PASS}
auth:
  admin_token: ${TEST_ADMIN_TOKEN}
tls:
  cert_file: ${TEST_TLS_CERT}
  key_file: ${TEST_TLS_KEY}
`
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("AIRBORNE_CONFIG", cfgPath)
	t.Setenv("TEST_REDIS_PASS", "secret-password")
	t.Setenv("TEST_ADMIN_TOKEN", "admin-secret")
	t.Setenv("TEST_TLS_CERT", "/expanded/cert.pem")
	t.Setenv("TEST_TLS_KEY", "/expanded/key.pem")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Redis.Password != "secret-password" {
		t.Errorf("expected expanded Redis.Password, got %s", cfg.Redis.Password)
	}
	if cfg.Auth.AdminToken != "admin-secret" {
		t.Errorf("expected expanded Auth.AdminToken, got %s", cfg.Auth.AdminToken)
	}
	if cfg.TLS.CertFile != "/expanded/cert.pem" {
		t.Errorf("expected expanded TLS.CertFile, got %s", cfg.TLS.CertFile)
	}
	if cfg.TLS.KeyFile != "/expanded/key.pem" {
		t.Errorf("expected expanded TLS.KeyFile, got %s", cfg.TLS.KeyFile)
	}
}

func TestLoad_MultipleEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))

	// Set multiple env overrides at once
	t.Setenv("AIRBORNE_GRPC_PORT", "9001")
	t.Setenv("AIRBORNE_HOST", "192.168.1.1")
	t.Setenv("REDIS_ADDR", "redis.example.com:6379")
	t.Setenv("REDIS_PASSWORD", "mypassword")
	t.Setenv("REDIS_DB", "3")
	t.Setenv("AIRBORNE_ADMIN_TOKEN", "supersecret")
	t.Setenv("AIRBORNE_LOG_LEVEL", "error")
	t.Setenv("AIRBORNE_LOG_FORMAT", "text")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Server.GRPCPort != 9001 {
		t.Errorf("expected GRPCPort 9001, got %d", cfg.Server.GRPCPort)
	}
	if cfg.Server.Host != "192.168.1.1" {
		t.Errorf("expected Host 192.168.1.1, got %s", cfg.Server.Host)
	}
	if cfg.Redis.Addr != "redis.example.com:6379" {
		t.Errorf("expected Redis.Addr redis.example.com:6379, got %s", cfg.Redis.Addr)
	}
	if cfg.Redis.Password != "mypassword" {
		t.Errorf("expected Redis.Password mypassword, got %s", cfg.Redis.Password)
	}
	if cfg.Redis.DB != 3 {
		t.Errorf("expected Redis.DB 3, got %d", cfg.Redis.DB)
	}
	if cfg.Auth.AdminToken != "supersecret" {
		t.Errorf("expected Auth.AdminToken supersecret, got %s", cfg.Auth.AdminToken)
	}
	if cfg.Logging.Level != "error" {
		t.Errorf("expected Logging.Level error, got %s", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("expected Logging.Format text, got %s", cfg.Logging.Format)
	}
}

func TestLoad_GRPCPortEnvOverride_InvalidValue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIRBORNE_CONFIG", filepath.Join(dir, "nonexistent.yaml"))
	t.Setenv("AIRBORNE_GRPC_PORT", "not-a-port")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Should keep default when invalid
	if cfg.Server.GRPCPort != 50051 {
		t.Errorf("expected default port 50051 for invalid env, got %d", cfg.Server.GRPCPort)
	}
}
