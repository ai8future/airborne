Date Created: 2026-01-19 21:16:00
TOTAL_SCORE: 78/100

# 1. AUDIT

### [CRITICAL] Performance/DoS Vulnerability: Bcrypt on Hot Path
**Severity:** Critical
**File:** `internal/auth/interceptor.go`

The `Authenticator` calls `keyStore.ValidateKey` on every gRPC request. `ValidateKey` uses `bcrypt.CompareHashAndPassword` (cost 10 by default), which consumes significant CPU (~50-100ms per call). This severely limits throughput and exposes the service to Denial of Service (DoS) attacks.

**Remediation:** Implement a short-lived in-memory cache for validated API keys to bypass bcrypt for repeated requests.


```go
// Authenticator handles API key authentication
type Authenticator struct {
	keyStore    *KeyStore
	rateLimiter *RateLimiter
	skipMethods map[string]bool
}

// NewAuthenticator creates a new authenticator
func NewAuthenticator(keyStore *KeyStore, rateLimiter *RateLimiter) *Authenticator {
	return &Authenticator{
		keyStore:    keyStore,
		rateLimiter: rateLimiter,
		skipMethods: map[string]bool{
			"/airborne.v1.AdminService/Health": true,
			// Version removed - requires authentication with PermissionAdmin
		},
	}
}

// Authenticator handles API key authentication
type Authenticator struct {
	keyStore    *KeyStore
	rateLimiter *RateLimiter
	skipMethods map[string]bool
	// Simple LRU-like cache: map[apiKey]client
	// Note: In production, use a proper LRU with expiration
	cache   sync.Map 
}

// NewAuthenticator creates a new authenticator
func NewAuthenticator(keyStore *KeyStore, rateLimiter *RateLimiter) *Authenticator {
	return &Authenticator{
		keyStore:    keyStore,
		rateLimiter: rateLimiter,
		skipMethods: map[string]bool{
			"/airborne.v1.AdminService/Health": true,
			// Version removed - requires authentication with PermissionAdmin
		},
	}
}
```

```go
	// Validate key
	client, err := a.keyStore.ValidateKey(ctx, apiKey)
	if err != nil {
		slog.Debug("authentication failed", "error", err)
	}

	// Check cache first
	if val, ok := a.cache.Load(apiKey); ok {
		return val.(*ClientKey), nil
	}

	// Validate key
	client, err := a.keyStore.ValidateKey(ctx, apiKey)
	if err != nil {
		slog.Debug("authentication failed", "error", err)
	}
```

```go
	return client, nil
}

	// Cache success
	a.cache.Store(apiKey, client)
	return client, nil
}
```
*(Note: You will need to add "sync" to imports)*

# 2. TESTS

### Missing Unit Tests for OpenAI Logic
**File:** `internal/provider/openai/helpers_test.go` (New File)

The `internal/provider/openai` package has complex logic for prompt building and citation stripping that is not exported and thus hard to test from outside. I propose adding an internal test file.

```go
package openai

import (
	"testing"
	"time"

	"github.com/ai8future/airborne/internal/provider"
)

func TestBuildUserPrompt(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		history  []provider.Message
		expected string
	}{
		{
			name:     "Single message",
			input:    "Hello world",
			history:  nil,
			expected: "Hello world",
		},
		{
			name:  "With history",
			input: "What is the weather?",
			history: []provider.Message{
				{Role: "user", Content: "Hi"},
				{Role: "assistant", Content: "Hello!"},
			},
			expected: "Previous conversation:\n\nUser: Hi\n\nAssistant: Hello!\n\n---\n\nNew message:\n\nWhat is the weather?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildUserPrompt(tt.input, tt.history)
			if got != tt.expected {
				t.Errorf("buildUserPrompt() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestStripCitationMarkers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No markers",
			input:    "Just plain text",
			expected: "Just plain text",
		},
		{
			name:     "Single marker",
			input:    "Here is the data fileciteturn2file0",
			expected: "Here is the data ",
		},
		{
			name:     "Complex marker",
			input:    "Analysis fileciteturn0file0turn1file0 shows...",
			expected: "Analysis  shows...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCitationMarkers(tt.input)
			if got != tt.expected {
				t.Errorf("stripCitationMarkers() = %q, want %q", got, tt.expected)
			}
		})
	}
}
```

# 3. FIXES

### Insecure Temporary Directory Usage
**Severity:** Medium
**File:** `internal/db/postgres.go`

The function `writeCACertToFile` uses a hardcoded path `/tmp/airborne-certs`. In a multi-user environment, this allows for potential pre-creation attacks or permission issues.

```go
func writeCACertToFile(certPEM string) (string, error) {
	// Use a stable path so we don't create multiple files on restarts

certDir := "/tmp/airborne-certs"
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create cert directory: %w", err)
	}


certPath := filepath.Join(certDir, "supabase-ca.crt")
}

func writeCACertToFile(certPEM string) (string, error) {
	// Use os.MkdirTemp to ensure a safe, unique directory

certDir, err := os.MkdirTemp("", "airborne-certs-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp cert directory: %w", err)
	}


certPath := filepath.Join(certDir, "supabase-ca.crt")
}
```

# 4. REFACTOR

### 1. Externalize Provider Defaults
The `defaultConfig` function in `internal/config/config.go` contains hardcoded provider settings (e.g., specific model versions like `claude-sonnet-4-20250514`).
**Suggestion:** Move these defaults to a separate `defaults.yaml` file embedded in the binary or purely rely on the external configuration file. This prevents code changes just to update a default model version.

### 2. Interface Segregation for Provider
The `provider.Provider` interface in `internal/provider/provider.go` is very large (God Interface), mixing RAG, Search, Streaming, and standard generation.
**Suggestion:** Split into smaller interfaces:
- `Generator` (GenerateReply)
- `Streamer` (GenerateReplyStream)
- `Searcher` (FileSearch, WebSearch)
- `ToolExecutor`
This would make it easier to add lightweight providers that don't support all features.

### 3. Dynamic SQL in Repository
`internal/db/repository.go` uses `fmt.Sprintf` to inject table names. While validated, this prevents the database from preparing statements efficiently effectively and is a bad habit.
**Suggestion:** Use PostgreSQL schemas (`CREATE SCHEMA tenant_id`) instead of prefixed tables. This allows using constant table names (`tenant.messages`) and switching the `search_path` per request, which is safer and cleaner.

```