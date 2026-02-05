Date Created: 2026-01-24 10:05
TOTAL_SCORE: 82/100

# 1. AUDIT - Security and code quality issues

## [High] CA Certificate written to predictable /tmp location
**File:** `internal/db/postgres.go`

The function `writeCACertToFile` writes the CA certificate to a fixed path `/tmp/airborne-certs/supabase-ca.crt`. This creates a race condition if multiple instances start simultaneously and a security risk if other users on the system can modify this file before it is read (TOCTOU).

```go
--- internal/db/postgres.go
+++ internal/db/postgres.go
@@ -107,7 +107,11 @@
 func writeCACertToFile(certPEM string) (string, error) {
 	// Use a stable path so we don't create multiple files on restarts
-	certDir := "/tmp/airborne-certs"
+	// Use User-specific temp dir to avoid collisions and security issues
+	certDir := filepath.Join(os.TempDir(), "airborne-certs-" + strconv.Itoa(os.Getuid()))
 	if err := os.MkdirAll(certDir, 0700); err != nil {
 		return "", fmt.Errorf("failed to create cert directory: %w", err)
 	}
```

## [Medium] Insecure CORS Configuration in Admin Server
**File:** `internal/admin/server.go`

The Admin server enables CORS for `*` (all origins). For an administrative interface, this should be restricted to known dashboard domains to prevent CSRF/XSS attacks from malicious sites.

```go
--- internal/admin/server.go
+++ internal/admin/server.go
@@ -62,7 +62,7 @@
 	// CORS middleware wrapper
 	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
 		return func(w http.ResponseWriter, r *http.Request) {
-			w.Header().Set("Access-Control-Allow-Origin", "*")
+			w.Header().Set("Access-Control-Allow-Origin", os.Getenv("ADMIN_ALLOWED_ORIGIN")) // Default to stricter value or env
 			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
 			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
```

# 2. TESTS - Proposed unit tests for untested code

## [Critical] Missing Database Repository Tests
**File:** `internal/db/tenant_test.go` (New File)

The `internal/db` package has no tests. While full DB testing requires a running instance or complex mocking, we can at least test the tenant repository logic.

```go
package db

import (
	"testing"
)

func TestNewTenantRepository_ValidatesTenantID(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		wantErr  bool
	}{
		{"valid_ai8", "ai8", false},
		{"valid_email4ai", "email4ai", false},
		{"valid_zztest", "zztest", false},
		{"invalid_tenant", "invalid", true},
		{"empty_tenant", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewTenantRepository(nil, tt.tenantID)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewTenantRepository() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRepository_TableNames(t *testing.T) {
	// Create repo manually since we pass nil client
	repo := &Repository{
		tenantID:    "ai8",
		tablePrefix: "ai8_airborne",
	}

	if got := repo.threadsTable(); got != "ai8_airborne_threads" {
		t.Errorf("threadsTable() = %v, want %v", got, "ai8_airborne_threads")
	}
	if got := repo.messagesTable(); got != "ai8_airborne_messages" {
		t.Errorf("messagesTable() = %v, want %v", got, "ai8_airborne_messages")
	}
}
```

# 3. FIXES - Bugs, issues, and code smells

## [Medium] Dangerous Development Interceptors in Production Code
**File:** `internal/server/grpc.go`

The `developmentAuthInterceptor` bypasses authentication. While not currently wired in `NewGRPCServer`, it poses a risk if accidentally used. Add a build tag to ensure it never compiles into production binaries.

```go
--- internal/server/grpc.go
+++ internal/server/grpc.go
@@ -213,6 +213,8 @@
 // WARNING: This function bypasses authentication entirely. It is intended ONLY for
 // local development and testing. NEVER wire this into NewGRPCServer for production builds.
 // If you need to use this, ensure it's behind a build tag or explicit development mode check.
+//
+//go:build !production
 func developmentAuthInterceptor() grpc.UnaryServerInterceptor {
 	slog.Warn("SECURITY: Development authentication interceptor is active - do not use in production")
 	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
```

## [Low] Hardcoded Defaults in Logic
**File:** `internal/config/config.go`

`fetchDopplerSecret` uses a default timeout but doesn't handle context cancellation if the caller provides one (though it constructs its own client). It prints to `Stderr` instead of returning errors to the caller to decide logging strategy.

```go
--- internal/config/config.go
+++ internal/config/config.go
@@ -250,7 +250,7 @@
 	if resp.StatusCode != http.StatusOK {
 		body, _ := io.ReadAll(resp.Body)
-		fmt.Fprintf(os.Stderr, "doppler: API error (status %d): %s\n", resp.StatusCode, string(body))
+		// Use slog or structured logging if available, or just silence/debug
+		// fmt.Fprintf(os.Stderr, "doppler: API error (status %d): %s\n", resp.StatusCode, string(body))
 		return ""
 	}
```

# 4. REFACTOR - Opportunities to improve code quality

## Extract Admin File Handling Logic
**Location:** `internal/admin/server.go` -> `internal/service/chat.go`

The `handleChatWithFile` method in `admin/server.go` manually constructs a Gemini client and makes direct API calls, bypassing the `internal/service` layer and `internal/provider` abstractions. This duplication means fixes to the main chat service (e.g., logging, metrics, grounding) won't apply to the admin dashboard's file chat.
**Action:** Update `ChatService` (gRPC) to support file attachments (URI/MIME) in its `GenerateReply` request, or create a dedicated `UploadAndChat` method, and have the Admin server delegate to the Service layer.

## Interface for Database Repository
**Location:** `internal/db/repository.go`

The `Repository` struct relies directly on `*Client` (concrete `pgxpool`).
**Action:** Create a `DBProvider` or `QueryExecutor` interface. This would allow `internal/db` tests to run without a live Postgres connection using mocks (like `pgxmock`), significantly improving testability of the data access layer.

## Simplify NewGRPCServer
**Location:** `internal/server/grpc.go`

`NewGRPCServer` is 200+ lines long.
**Action:** Extract helper functions: `setupAuth(cfg)`, `setupRAG(cfg)`, `setupServices(...)`. This would make the entry point readable and easier to test individual component setups.

```
