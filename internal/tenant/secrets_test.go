package tenant

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSecret_EnvPrefix(t *testing.T) {
	t.Setenv("TEST_SECRET", "env-value")

	got, err := loadSecret("ENV=TEST_SECRET")
	if err != nil {
		t.Fatalf("ENV= loadSecret failed: %v", err)
	}
	if got != "env-value" {
		t.Fatalf("got %q, want %q", got, "env-value")
	}
}

func TestLoadSecret_EnvMissing(t *testing.T) {
	_, err := loadSecret("ENV=MISSING_SECRET_VAR_12345")
	if err == nil {
		t.Fatal("expected error for missing env secret")
	}
}

func TestLoadSecret_VarExpansion(t *testing.T) {
	t.Setenv("TEST_VAR", "var-value")

	got, err := loadSecret("${TEST_VAR}")
	if err != nil {
		t.Fatalf("${} loadSecret failed: %v", err)
	}
	if got != "var-value" {
		t.Fatalf("got %q, want %q", got, "var-value")
	}
}

func TestLoadSecret_VarExpansionMissing(t *testing.T) {
	_, err := loadSecret("${MISSING_VAR_12345}")
	if err == nil {
		t.Fatal("expected error for missing ${VAR}")
	}
}

func TestLoadSecret_Inline(t *testing.T) {
	got, err := loadSecret("inline-value")
	if err != nil {
		t.Fatalf("inline loadSecret failed: %v", err)
	}
	if got != "inline-value" {
		t.Fatalf("got %q, want %q", got, "inline-value")
	}
}

func TestLoadSecret_Empty(t *testing.T) {
	got, err := loadSecret("")
	if err != nil {
		t.Fatalf("empty loadSecret failed: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestResolveSecrets(t *testing.T) {
	t.Setenv("API_KEY", "resolved-key")

	cfg := TenantConfig{
		Providers: map[string]ProviderConfig{
			"openai": {Enabled: true, APIKey: "ENV=API_KEY", Model: "model"},
		},
	}

	if err := resolveSecrets(&cfg); err != nil {
		t.Fatalf("resolveSecrets failed: %v", err)
	}
	if cfg.Providers["openai"].APIKey != "resolved-key" {
		t.Fatalf("expected resolved API key, got %q", cfg.Providers["openai"].APIKey)
	}
}

func TestResolveSecrets_MultipleProviders(t *testing.T) {
	t.Setenv("OPENAI_KEY", "openai-key")
	t.Setenv("GEMINI_KEY", "gemini-key")

	cfg := TenantConfig{
		Providers: map[string]ProviderConfig{
			"openai": {Enabled: true, APIKey: "ENV=OPENAI_KEY", Model: "gpt-4"},
			"gemini": {Enabled: true, APIKey: "${GEMINI_KEY}", Model: "gemini-2"},
		},
	}

	if err := resolveSecrets(&cfg); err != nil {
		t.Fatalf("resolveSecrets failed: %v", err)
	}
	if cfg.Providers["openai"].APIKey != "openai-key" {
		t.Fatalf("openai APIKey = %q, want openai-key", cfg.Providers["openai"].APIKey)
	}
	if cfg.Providers["gemini"].APIKey != "gemini-key" {
		t.Fatalf("gemini APIKey = %q, want gemini-key", cfg.Providers["gemini"].APIKey)
	}
}

func TestValidateSecretPath_TraversalBlocked(t *testing.T) {
	tests := []string{
		"/etc/airborne/secrets/../../../etc/passwd",
		"../secrets/key",
		"/foo/../bar",
	}

	for _, path := range tests {
		if err := validateSecretPath(path); err == nil {
			t.Errorf("expected error for path traversal: %s", path)
		}
	}
}

func TestValidateSecretPath_AllowedPaths(t *testing.T) {
	// These would only pass if the directories existed on the system
	// This test validates the logic at least runs without panic
	_ = validateSecretPath("/etc/airborne/secrets/mykey")
	_ = validateSecretPath("/run/secrets/api_key")
	_ = validateSecretPath("/var/run/secrets/token")
}

func TestLoadSecret_FilePrefix_PathValidation(t *testing.T) {
	// Create a temp file outside allowed directories - should fail validation
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(tmpFile, []byte("secret-value"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	// This should fail because tmpDir is not in allowed directories
	_, err := loadSecret("FILE=" + tmpFile)
	if err == nil {
		t.Error("expected error for file outside allowed directories")
	}
}

func TestValidateSecretPath_SymlinkAttack(t *testing.T) {
	// This test verifies that symlinks inside an "allowed" directory
	// pointing to files outside are properly rejected.
	//
	// Attack scenario: If /etc/airborne/secrets exists, an attacker could
	// create /etc/airborne/secrets/evil -> /etc/passwd, bypassing validation
	// that only checks the path prefix without resolving symlinks.

	// Create a temp structure simulating the attack
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	outsideDir := filepath.Join(tmpDir, "outside")
	outsideFile := filepath.Join(outsideDir, "secret.txt")

	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatalf("create allowed dir: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	if err := os.WriteFile(outsideFile, []byte("sensitive-data"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	// Create symlink inside "allowed" pointing to outside file
	symlinkPath := filepath.Join(allowedDir, "evil-link")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// Temporarily override AllowedSecretDirs for this test
	originalDirs := AllowedSecretDirs
	AllowedSecretDirs = []string{allowedDir}
	defer func() { AllowedSecretDirs = originalDirs }()

	// The symlink path appears to be inside allowedDir, but resolves outside
	// validateSecretPath should reject this
	err := validateSecretPath(symlinkPath)
	if err == nil {
		t.Error("expected error for symlink pointing outside allowed directories")
	}
}

func TestValidateSecretPath_SymlinkToAllowed(t *testing.T) {
	// Verify that symlinks within allowed directories to other locations
	// within allowed directories still work correctly.
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	subDir := filepath.Join(allowedDir, "subdir")
	realFile := filepath.Join(subDir, "real.txt")

	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}
	if err := os.WriteFile(realFile, []byte("allowed-data"), 0o600); err != nil {
		t.Fatalf("write real file: %v", err)
	}

	// Create symlink inside allowed pointing to another location inside allowed
	symlinkPath := filepath.Join(allowedDir, "good-link")
	if err := os.Symlink(realFile, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// Temporarily override AllowedSecretDirs for this test
	originalDirs := AllowedSecretDirs
	AllowedSecretDirs = []string{allowedDir}
	defer func() { AllowedSecretDirs = originalDirs }()

	// This symlink should be allowed since it resolves within allowed dir
	err := validateSecretPath(symlinkPath)
	if err != nil {
		t.Errorf("expected symlink within allowed dir to pass: %v", err)
	}
}
