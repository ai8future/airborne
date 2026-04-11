Date Created: 2026-01-23 12:00:00
TOTAL_SCORE: 80/100

# AUDIT

### [Security] Potential Sensitive Data Leak in Config Loading
**Severity:** Medium
**File:** `internal/config/config.go`

The `fetchDopplerSecret` function reads and logs the entire response body to `os.Stderr` when the status code is not 200 OK. If the Doppler API returns sensitive debugging information or if the response is unexpectedly large, this is unsafe and noisy.

```go
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			fmt.Fprintf(os.Stderr, "doppler: API error (status %d): %s\n", resp.StatusCode, string(body))
			return ""
		}
====
		if resp.StatusCode != http.StatusOK {
			// Do not log the full body to avoid leaking sensitive info or flooding logs
			fmt.Fprintf(os.Stderr, "doppler: API error (status %d)\n", resp.StatusCode)
			return ""
		}
>>>>

### [Reliability] Fixed Stack Buffer in Panic Recovery
**Severity:** Low
**File:** `internal/server/grpc.go`

The `recoveryInterceptor` uses a fixed 4096-byte buffer for stack traces. Deep stacks will be truncated, potentially hiding the root cause of the panic.

```go
<<<<
			if r := recover() {
				// Log stack trace
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				slog.Error("panic recovered",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(buf[:n]),
				)
====
			if r := recover() {
				// Log stack trace
				const size = 64 << 10
				buf := make([]byte, size)
				n := runtime.Stack(buf, false)
				slog.Error("panic recovered",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(buf[:n]),
				)
>>>>

### [Security] Dangerous Development Code
**Severity:** Low (as it appears unused)
**File:** `internal/server/grpc.go`

The functions `developmentAuthInterceptor` and `developmentAuthStreamInterceptor` bypass authentication entirely. While they log a warning and are not currently called in `NewGRPCServer`, their presence in the production codebase is a risk. Ideally, these should be behind a build tag or removed.

# TESTS

### [Coverage] Test `ENV=` Expansion in Config
**File:** `internal/config/config_test.go`

The `expandEnv` function supports `ENV=` prefix (likely for frozen configs), but this is not covered by existing tests.

```go
<<<<
		if cfg.TLS.KeyFile != "/expanded/key.pem" {
			t.Errorf("expected expanded TLS.KeyFile, got %s", cfg.TLS.KeyFile)
		}
}

func TestLoad_MultipleEnvOverrides(t *testing.T) {
====
		if cfg.TLS.KeyFile != "/expanded/key.pem" {
			t.Errorf("expected expanded TLS.KeyFile, got %s", cfg.TLS.KeyFile)
		}
}

func TestLoad_EnvExpansion_Prefix(t *testing.T) {
	dir := t.TempDir()

	// Create config with ENV= syntax
	cfgYAML := `
auth:
  admin_token: ENV=TEST_PREFIX_TOKEN
`

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("AIRBORNE_CONFIG", cfgPath)
	t.Setenv("TEST_PREFIX_TOKEN", "prefix-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Auth.AdminToken != "prefix-secret" {
		t.Errorf("expected expanded Auth.AdminToken from ENV= prefix, got %s", cfg.Auth.AdminToken)
	}
}

func TestLoad_MultipleEnvOverrides(t *testing.T) {
>>>>

# FIXES

### [Security] Sanitize Doppler Error Logging
**File:** `internal/config/config.go`

Prevent potential leakage of sensitive data in error responses.

```go
<<<<
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "doppler: API error (status %d): %s\n", resp.StatusCode, string(body))
		return ""
	}
====
	if resp.StatusCode != http.StatusOK {
		// Limit body output to avoid log flooding/leakage
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		fmt.Fprintf(os.Stderr, "doppler: API error (status %d): %s\n", resp.StatusCode, string(body))
		return ""
	}
>>>>

### [Reliability] Increase Stack Trace Buffer
**File:** `internal/server/grpc.go`

Increase the buffer size for panic recovery to capture full stack traces.

```go
<<<<
			if r := recover(); r != nil {
				// Log stack trace
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				slog.Error("panic recovered",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(buf[:n]),
				)
				err = status.Errorf(codes.Internal, "internal error")
			}
====
			if r := recover(); r != nil {
				// Log stack trace
				const size = 64 << 10 // 64KB
				buf := make([]byte, size)
				n := runtime.Stack(buf, false)
				slog.Error("panic recovered",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(buf[:n]),
				)
				err = status.Errorf(codes.Internal, "internal error")
			}
>>>>

# REFACTOR

### 1. Extract Development Interceptors
**Location:** `internal/server/grpc.go`
**Suggestion:** Move `developmentAuthInterceptor` and `developmentAuthStreamInterceptor` to a separate file, e.g., `internal/server/dev_interceptors.go`. Use Go build tags (e.g., `//go:build !production`) to ensure this code is strictly excluded from production builds. This eliminates the risk of accidental usage.

### 2. Simplify `NewGRPCServer`
**Location:** `internal/server/grpc.go`
**Suggestion:** The `NewGRPCServer` function is becoming a "god function" (approx 150 lines). It handles auth setup, RAG setup, DB connections, and service registration.
- Extract `initAuth` to handle the conditional logic between Redis vs Static auth.
- Extract `initRAG` to handle the complex RAG service initialization.
- This will make the main server setup flow much easier to read and test.

### 3. Unify Auth Logic
**Location:** `internal/auth/`
**Suggestion:** The `Authenticator` (Redis-based) and `StaticAuthenticator` (token-based) share similar patterns but implement them separately. Define a common `AuthProvider` interface that both implementations satisfy. This would simplify the interceptor logic in `grpc.go` which currently has to branch based on `cfg.Auth.AuthMode`.

```