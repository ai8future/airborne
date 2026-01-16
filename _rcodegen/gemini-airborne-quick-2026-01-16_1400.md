Date Created: 2026-01-16 14:00:00

# 1. AUDIT

## Critical: High-Overhead API Key Validation
**File:** `internal/auth/keys.go`
**Issue:** The `ValidateKey` function uses `bcrypt.CompareHashAndPassword` on every request. Bcrypt is designed to be slow to prevent brute-force attacks, which makes it unsuitable for high-throughput API key validation (hot path).
**Recommendation:** Switch to SHA-256. API keys are high-entropy (32 bytes of random data), so a fast hash like SHA-256 is secure enough.
**Note:** This is a breaking change for existing keys (hashes will be incompatible). Since this is a new codebase, we assume a migration is acceptable or not needed yet.

```diff
--- internal/auth/keys.go
+++ internal/auth/keys.go
@@ -3,6 +3,8 @@
 import (
 	"context"
 	"crypto/rand"
+	"crypto/sha256"
+	"crypto/subtle"
 	"encoding/hex"
 	"encoding/json"
 	"fmt"
@@ -10,7 +12,6 @@
 
 	"github.com/ai8future/airborne/internal/redis"
-	"golang.org/x/crypto/bcrypt"
 )
 
 const (
@@ -73,12 +74,8 @@
 	}
 
 	// Hash the secret for storage
-	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
-	if err != nil {
-		return "", nil, fmt.Errorf("failed to hash secret: %w", err)
-	}
+	hash := sha256.Sum256([]byte(secret))
+	hashStr := hex.EncodeToString(hash[:])
 
 	// Create key record
 	key := &ClientKey{
 		KeyID:       keyID,
 		ClientID:    clientID,
 		ClientName:  clientName,
-		SecretHash:  string(hash),
+		SecretHash:  hashStr,
 		Permissions: permissions,
 		RateLimits:  limits,
 		CreatedAt:   time.Now().UTC(),
@@ -118,8 +115,9 @@
 	}
 
 	// Verify secret
-	if err := bcrypt.CompareHashAndPassword([]byte(key.SecretHash), []byte(secret)); err != nil {
+	hash := sha256.Sum256([]byte(secret))
+	expected := []byte(key.SecretHash)
+	actual := []byte(hex.EncodeToString(hash[:]))
+	if subtle.ConstantTimeCompare(expected, actual) != 1 {
 		return nil, ErrInvalidKey
 	}
```

## Medium: Dangerous Redis Key Scanning
**File:** `internal/redis/client.go`
**Issue:** `Scan` implementation iterates all keys and returns them in a single slice. For a large database, this will consume excessive memory and potential OOM.
**Recommendation:** Implement an iterator pattern or pagination for `Scan` or `ListKeys`.

# 2. TESTS

## Missing Test: Static Authenticator
**File:** `internal/auth/static_test.go`
**Description:** `StaticAuthenticator` in `internal/auth/static.go` has complex logic for token extraction and context injection but no tests.

```go
package auth

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestStaticAuthenticator_Authenticate(t *testing.T) {
	token := "secret-token-123"
	auth := NewStaticAuthenticator(token)

	tests := []struct {
		name      string
		md        metadata.MD
		wantError bool
	}{
		{
			name:      "Valid Token",
			md:        metadata.Pairs("authorization", "Bearer "+token),
			wantError: false,
		},
		{
			name:      "Valid API Key Header",
			md:        metadata.Pairs("x-api-key", token),
			wantError: false,
		},
		{
			name:      "Invalid Token",
			md:        metadata.Pairs("authorization", "Bearer wrong"),
			wantError: true,
		},
		{
			name:      "Missing Token",
			md:        metadata.Pairs(),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), tt.md)
			_, err := auth.authenticate(ctx)
			if (err != nil) != tt.wantError {
				t.Errorf("authenticate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
```

## Missing Test: Cohere Provider Client
**File:** `internal/provider/cohere/client_test.go`
**Description:** Smoke test for Cohere client creation.

```go
package cohere

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestWithDebugLogging(t *testing.T) {
	client := NewClient(WithDebugLogging(true))
	if client == nil {
		t.Fatal("NewClient with options returned nil")
	}
}
```

# 3. FIXES

## Feature: Config Environment Variable Defaults
**File:** `internal/config/config.go`
**Issue:** `expandEnv` only supports direct substitution `${VAR}`, missing the common shell pattern `${VAR:-default}`.
**Fix:** Add support for default values.

```diff
--- internal/config/config.go
+++ internal/config/config.go
@@ -211,6 +211,15 @@
 func expandEnv(s string) string {
 	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
 		varName := s[2 : len(s)-1]
+		defaultValue := ""
+		
+		// Support ${VAR:-default}
+		if parts := strings.SplitN(varName, ":-", 2); len(parts) == 2 {
+			varName = parts[0]
+			defaultValue = parts[1]
+		}
+
-		return os.Getenv(varName)
+		if val := os.Getenv(varName); val != "" {
+			return val
+		}
+		return defaultValue
 	}
 	return os.ExpandEnv(s)
 }
```

# 4. REFACTOR

## Provider Duplication
**Location:** `internal/provider/*`
**Observation:** Most provider clients (`cohere`, `deepseek`, `mistral`, etc.) are thin wrappers around `internal/provider/compat`.
**Recommendation:** Introduce a generic `Factory` or `Registry` in `internal/provider` that takes a configuration (BaseURL, Model, APIKeyEnv) and returns a `Provider`. This would eliminate the need for separate packages for every compliant provider, reducing codebase size and maintenance.

## Rate Limiting Accuracy
**Location:** `internal/auth/ratelimit.go`
**Observation:** The current implementation uses a "Fixed Window" algorithm (resetting counter at window expiration).
**Recommendation:** Consider "Sliding Window Log" or "Sliding Window Counter" if stricter rate limiting is required. The current approach allows up to 2x the limit at window boundaries (e.g., end of one minute and start of the next).
