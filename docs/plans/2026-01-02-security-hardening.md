# Security Hardening Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix critical and high-priority security vulnerabilities identified in the code audit, focusing on authentication bypass, input validation, and credential handling.

**Architecture:** We'll add a startup mode configuration to prevent running without authentication, implement input size validation middleware, fix the rate limiting race condition with atomic Redis operations, and add proper error sanitization. Each fix is isolated and testable.

**Tech Stack:** Go 1.22+, gRPC, Redis, bcrypt

---

## Task 1: Prevent Authentication Bypass When Redis Unavailable

**Files:**
- Create: `internal/config/startup_mode.go`
- Modify: `internal/server/grpc.go:29-51`
- Modify: `internal/config/config.go:63-68`
- Create: `internal/server/grpc_test.go`

**Step 1: Write the failing test for startup mode enforcement**

Create `internal/server/grpc_test.go`:

```go
package server

import (
	"testing"

	"github.com/cliffpyles/aibox/internal/config"
)

func TestNewGRPCServer_FailsWithoutRedisInProductionMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 50051,
			Host:     "127.0.0.1",
		},
		StartupMode: config.StartupModeProduction,
		Redis: config.RedisConfig{
			Addr: "invalid:6379", // Will fail to connect
		},
	}

	_, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err == nil {
		t.Fatal("expected error when Redis unavailable in production mode")
	}
}

func TestNewGRPCServer_AllowsNoRedisInDevelopmentMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 50051,
			Host:     "127.0.0.1",
		},
		StartupMode: config.StartupModeDevelopment,
		Redis: config.RedisConfig{
			Addr: "invalid:6379", // Will fail to connect
		},
	}

	server, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("development mode should allow missing Redis: %v", err)
	}
	if server == nil {
		t.Fatal("server should not be nil")
	}
	server.Stop()
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestNewGRPCServer -v`
Expected: FAIL - `StartupMode` field doesn't exist

**Step 3: Add StartupMode to config**

Create `internal/config/startup_mode.go`:

```go
package config

// StartupMode defines how the server behaves with missing dependencies
type StartupMode string

const (
	// StartupModeProduction requires all dependencies (Redis, etc.) to be available
	StartupModeProduction StartupMode = "production"

	// StartupModeDevelopment allows running without optional dependencies
	StartupModeDevelopment StartupMode = "development"
)

// IsProduction returns true if running in production mode
func (m StartupMode) IsProduction() bool {
	return m == StartupModeProduction || m == ""
}
```

**Step 4: Modify config struct in `internal/config/config.go`**

Add to Config struct after line 21:

```go
StartupMode StartupMode `yaml:"startup_mode"`
```

Add to defaultConfig() after Logging block (~line 145):

```go
StartupMode: StartupModeProduction,
```

**Step 5: Modify `internal/server/grpc.go` to enforce startup mode**

Replace lines 36-51 with:

```go
	redisClient, err := redis.NewClient(redis.Config{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err != nil {
		if cfg.StartupMode.IsProduction() {
			return nil, fmt.Errorf("redis required in production mode: %w", err)
		}
		slog.Warn("Redis not available - auth and rate limiting disabled (development mode)", "error", err)
	} else {
		keyStore = auth.NewKeyStore(redisClient)
		rateLimiter = auth.NewRateLimiter(redisClient, auth.RateLimits{
			RequestsPerMinute: cfg.RateLimits.DefaultRPM,
			RequestsPerDay:    cfg.RateLimits.DefaultRPD,
			TokensPerMinute:   cfg.RateLimits.DefaultTPM,
		}, true)
		authenticator = auth.NewAuthenticator(keyStore, rateLimiter)
	}
```

Add `"fmt"` to imports at top of file.

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestNewGRPCServer -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/config/startup_mode.go internal/config/config.go internal/server/grpc.go internal/server/grpc_test.go
git commit -m "fix(security): require Redis in production mode to prevent auth bypass

BREAKING: Server now fails to start without Redis unless startup_mode=development.
This prevents the authentication bypass vulnerability where Redis unavailability
disabled all authentication and rate limiting."
```

---

## Task 2: Add Input Size Validation

**Files:**
- Create: `internal/validation/limits.go`
- Create: `internal/validation/limits_test.go`
- Modify: `internal/service/chat.go:41-50`

**Step 1: Write the failing test for input validation**

Create `internal/validation/limits_test.go`:

```go
package validation

import (
	"strings"
	"testing"
)

func TestValidateGenerateRequest_RejectsOversizedInput(t *testing.T) {
	tests := []struct {
		name        string
		userInput   string
		instruction string
		historyLen  int
		wantErr     bool
	}{
		{
			name:      "normal input passes",
			userInput: "Hello, how are you?",
			wantErr:   false,
		},
		{
			name:      "oversized user input rejected",
			userInput: strings.Repeat("x", MaxUserInputBytes+1),
			wantErr:   true,
		},
		{
			name:        "oversized instructions rejected",
			userInput:   "test",
			instruction: strings.Repeat("x", MaxInstructionsBytes+1),
			wantErr:     true,
		},
		{
			name:       "too many history items rejected",
			userInput:  "test",
			historyLen: MaxHistoryCount + 1,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGenerateRequest(tt.userInput, tt.instruction, tt.historyLen)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGenerateRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/validation/... -v`
Expected: FAIL - package doesn't exist

**Step 3: Implement validation package**

Create `internal/validation/limits.go`:

```go
package validation

import (
	"errors"
	"fmt"
)

const (
	// MaxUserInputBytes is the maximum size of user input (100KB)
	MaxUserInputBytes = 100 * 1024

	// MaxInstructionsBytes is the maximum size of system instructions (50KB)
	MaxInstructionsBytes = 50 * 1024

	// MaxHistoryCount is the maximum number of conversation history messages
	MaxHistoryCount = 100

	// MaxMetadataEntries is the maximum number of metadata key-value pairs
	MaxMetadataEntries = 50
)

var (
	ErrUserInputTooLarge    = errors.New("user_input exceeds maximum size")
	ErrInstructionsTooLarge = errors.New("instructions exceed maximum size")
	ErrHistoryTooLong       = errors.New("conversation_history exceeds maximum length")
	ErrMetadataTooLarge     = errors.New("metadata exceeds maximum entries")
)

// ValidateGenerateRequest validates size limits for a generate request
func ValidateGenerateRequest(userInput, instructions string, historyCount int) error {
	if len(userInput) > MaxUserInputBytes {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrUserInputTooLarge, len(userInput), MaxUserInputBytes)
	}

	if len(instructions) > MaxInstructionsBytes {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrInstructionsTooLarge, len(instructions), MaxInstructionsBytes)
	}

	if historyCount > MaxHistoryCount {
		return fmt.Errorf("%w: %d messages (max %d)", ErrHistoryTooLong, historyCount, MaxHistoryCount)
	}

	return nil
}

// ValidateMetadata validates metadata size limits
func ValidateMetadata(metadata map[string]string) error {
	if len(metadata) > MaxMetadataEntries {
		return fmt.Errorf("%w: %d entries (max %d)", ErrMetadataTooLarge, len(metadata), MaxMetadataEntries)
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/validation/... -v`
Expected: PASS

**Step 5: Commit validation package**

```bash
git add internal/validation/
git commit -m "feat(validation): add input size validation package

Adds constants and validation functions for request size limits:
- MaxUserInputBytes: 100KB
- MaxInstructionsBytes: 50KB
- MaxHistoryCount: 100 messages
- MaxMetadataEntries: 50 entries"
```

**Step 6: Integrate validation into chat service**

Modify `internal/service/chat.go`. Add import:

```go
"github.com/cliffpyles/aibox/internal/validation"
```

Replace lines 47-50 with:

```go
	// Validate input sizes
	if err := validation.ValidateGenerateRequest(
		req.UserInput,
		req.Instructions,
		len(req.ConversationHistory),
	); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate metadata
	if err := validation.ValidateMetadata(req.Metadata); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate non-empty (existing check)
	if strings.TrimSpace(req.UserInput) == "" {
		return nil, status.Error(codes.InvalidArgument, "user_input is required")
	}
```

**Step 7: Add same validation to streaming endpoint**

Modify `internal/service/chat.go` GenerateReplyStream. Replace lines 128-130 with:

```go
	// Validate input sizes
	if err := validation.ValidateGenerateRequest(
		req.UserInput,
		req.Instructions,
		len(req.ConversationHistory),
	); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	if err := validation.ValidateMetadata(req.Metadata); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate non-empty
	if strings.TrimSpace(req.UserInput) == "" {
		return status.Error(codes.InvalidArgument, "user_input is required")
	}
```

**Step 8: Run build to verify**

Run: `go build ./...`
Expected: Success

**Step 9: Commit integration**

```bash
git add internal/service/chat.go
git commit -m "fix(security): add input size validation to prevent DoS

Validates all incoming requests against size limits to prevent
resource exhaustion attacks via oversized payloads."
```

---

## Task 3: Fix Rate Limiting Race Condition with Atomic Redis Operations

**Files:**
- Create: `internal/auth/ratelimit_test.go`
- Modify: `internal/auth/ratelimit.go:89-108`
- Modify: `internal/redis/client.go` (add Lua script support)

**Step 1: Write test for atomic rate limiting**

Create `internal/auth/ratelimit_test.go`:

```go
package auth

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_AtomicIncrement(t *testing.T) {
	// This test verifies the rate limiter uses atomic operations.
	// In a real test, you'd use a Redis mock or test container.
	// For now, this documents the expected behavior.

	t.Run("increment and expire are atomic", func(t *testing.T) {
		// The checkLimitAtomic function should:
		// 1. Increment counter
		// 2. Set TTL if new key
		// 3. Return current count
		// All in a single atomic operation

		// This is a placeholder for integration tests
		t.Skip("requires Redis test container - see integration tests")
	})
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	t.Run("keys expire after window", func(t *testing.T) {
		// Keys should automatically expire after the rate limit window
		t.Skip("requires Redis test container - see integration tests")
	})
}
```

**Step 2: Add SetNX method to Redis client**

Modify `internal/redis/client.go`. Add after the Expire method (~line 90):

```go
// SetNXWithExpiry atomically sets a key only if it doesn't exist, with expiration
func (c *Client) SetNXWithExpiry(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, key, value, expiration).Result()
}

// Eval executes a Lua script
func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	return c.rdb.Eval(ctx, script, keys, args...).Result()
}
```

**Step 3: Run build to verify Redis changes**

Run: `go build ./internal/redis/...`
Expected: Success

**Step 4: Commit Redis changes**

```bash
git add internal/redis/client.go
git commit -m "feat(redis): add SetNX and Eval methods for atomic operations"
```

**Step 5: Implement atomic rate limiting**

Replace the `checkLimit` function in `internal/auth/ratelimit.go` (lines 89-110) with:

```go
// rateLimitScript is a Lua script for atomic rate limiting
// It increments the counter and sets TTL atomically, returning the new count
const rateLimitScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])

local current = redis.call('INCR', key)
if current == 1 then
    redis.call('EXPIRE', key, window)
end

return current
`

// checkLimit checks and increments a rate limit counter atomically
func (r *RateLimiter) checkLimit(ctx context.Context, clientID, limitType string, limit int, window time.Duration) error {
	key := fmt.Sprintf("%s%s:%s", rateLimitPrefix, clientID, limitType)
	windowSeconds := int(window.Seconds())

	result, err := r.redis.Eval(ctx, rateLimitScript, []string{key}, limit, windowSeconds)
	if err != nil {
		return fmt.Errorf("failed to check rate limit: %w", err)
	}

	count, ok := result.(int64)
	if !ok {
		return fmt.Errorf("unexpected result type from rate limit script")
	}

	if int(count) > limit {
		return ErrRateLimitExceeded
	}

	return nil
}
```

**Step 6: Run build to verify**

Run: `go build ./internal/auth/...`
Expected: Success

**Step 7: Commit atomic rate limiting**

```bash
git add internal/auth/ratelimit.go internal/auth/ratelimit_test.go
git commit -m "fix(security): use atomic Lua script for rate limiting

Replaces separate INCR+EXPIRE calls with atomic Lua script to prevent
race conditions where TTL might not be set on first request."
```

---

## Task 4: Sanitize Error Messages to Prevent Information Leakage

**Files:**
- Create: `internal/errors/sanitize.go`
- Create: `internal/errors/sanitize_test.go`
- Modify: `internal/service/chat.go:105`

**Step 1: Write test for error sanitization**

Create `internal/errors/sanitize_test.go`:

```go
package errors

import (
	"errors"
	"testing"
)

func TestSanitizeForClient(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "generic error returns generic message",
			err:      errors.New("connection refused to api.openai.com:443"),
			expected: "provider temporarily unavailable",
		},
		{
			name:     "nil error returns empty",
			err:      nil,
			expected: "",
		},
		{
			name:     "api key error sanitized",
			err:      errors.New("invalid API key: sk-proj-xxxxx"),
			expected: "authentication failed with provider",
		},
		{
			name:     "rate limit preserved",
			err:      errors.New("rate limit exceeded"),
			expected: "rate limit exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeForClient(tt.err)
			if result != tt.expected {
				t.Errorf("SanitizeForClient() = %q, want %q", result, tt.expected)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/errors/... -v`
Expected: FAIL - package doesn't exist

**Step 3: Implement error sanitization**

Create `internal/errors/sanitize.go`:

```go
package errors

import (
	"log/slog"
	"strings"
)

// clientSafePatterns maps error patterns to client-safe messages
var clientSafePatterns = map[string]string{
	"rate limit":    "rate limit exceeded",
	"quota":         "quota exceeded",
	"timeout":       "request timed out",
	"context dead":  "request cancelled",
	"invalid api":   "authentication failed with provider",
	"unauthorized":  "authentication failed with provider",
	"forbidden":     "access denied by provider",
	"not found":     "resource not found",
}

// SanitizeForClient converts internal errors to client-safe messages
// It logs the full error server-side and returns a sanitized version
func SanitizeForClient(err error) string {
	if err == nil {
		return ""
	}

	errLower := strings.ToLower(err.Error())

	// Check for known safe patterns
	for pattern, safeMsg := range clientSafePatterns {
		if strings.Contains(errLower, pattern) {
			slog.Debug("sanitizing error for client",
				"original", err.Error(),
				"sanitized", safeMsg,
			)
			return safeMsg
		}
	}

	// Log the full error, return generic message
	slog.Error("provider error (sanitized for client)", "error", err)
	return "provider temporarily unavailable"
}

// WrapForLogging wraps an error with context for logging but returns sanitized version
func WrapForLogging(err error, context string) (logMsg, clientMsg string) {
	if err == nil {
		return "", ""
	}
	logMsg = context + ": " + err.Error()
	clientMsg = SanitizeForClient(err)
	return logMsg, clientMsg
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/errors/... -v`
Expected: PASS

**Step 5: Commit error sanitization package**

```bash
git add internal/errors/
git commit -m "feat(errors): add error sanitization for client responses

Prevents internal error details from leaking to clients while
maintaining full error logging server-side."
```

**Step 6: Integrate into chat service**

Modify `internal/service/chat.go`. Add import:

```go
sanitize "github.com/cliffpyles/aibox/internal/errors"
```

Replace line 105:

```go
return nil, status.Errorf(codes.Internal, "provider error: %v", err)
```

With:

```go
slog.Error("provider request failed",
	"provider", selectedProvider.Name(),
	"error", err,
	"request_id", req.RequestId,
)
return nil, status.Error(codes.Internal, sanitize.SanitizeForClient(err))
```

Also update the streaming error in GenerateReplyStream. Find the error return (~line 161) and update:

```go
return status.Error(codes.Internal, sanitize.SanitizeForClient(err))
```

**Step 7: Run build to verify**

Run: `go build ./...`
Expected: Success

**Step 8: Commit integration**

```bash
git add internal/service/chat.go
git commit -m "fix(security): sanitize provider errors returned to clients

Prevents internal error details (connection strings, API keys, etc.)
from being exposed in error responses."
```

---

## Task 5: Add Request ID Validation and Generation

**Files:**
- Modify: `internal/validation/limits.go`
- Modify: `internal/validation/limits_test.go`
- Modify: `internal/service/chat.go`

**Step 1: Add request ID validation test**

Add to `internal/validation/limits_test.go`:

```go
func TestValidateOrGenerateRequestID(t *testing.T) {
	tests := []struct {
		name        string
		requestID   string
		wantValid   bool
		wantChanged bool
	}{
		{
			name:        "empty generates new ID",
			requestID:   "",
			wantValid:   true,
			wantChanged: true,
		},
		{
			name:        "valid UUID passes through",
			requestID:   "550e8400-e29b-41d4-a716-446655440000",
			wantValid:   true,
			wantChanged: false,
		},
		{
			name:        "invalid characters rejected",
			requestID:   "request<script>alert(1)</script>",
			wantValid:   false,
			wantChanged: false,
		},
		{
			name:        "too long rejected",
			requestID:   strings.Repeat("a", 129),
			wantValid:   false,
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidateOrGenerateRequestID(tt.requestID)
			if tt.wantValid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.wantValid && err == nil {
				t.Errorf("expected error, got valid result: %s", result)
			}
			if tt.wantChanged && result == tt.requestID {
				t.Error("expected ID to be changed/generated")
			}
			if !tt.wantChanged && tt.wantValid && result != tt.requestID {
				t.Errorf("expected ID to pass through, got %s", result)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/validation/... -v -run TestValidateOrGenerateRequestID`
Expected: FAIL - function doesn't exist

**Step 3: Implement request ID validation**

Add to `internal/validation/limits.go`:

```go
import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
)

const (
	// ... existing constants ...

	// MaxRequestIDLength is the maximum length of a request ID
	MaxRequestIDLength = 128
)

var (
	// ... existing errors ...

	ErrInvalidRequestID = errors.New("invalid request_id format")

	// requestIDPattern allows alphanumeric, hyphens, underscores
	requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)
)

// ValidateOrGenerateRequestID validates an existing request ID or generates a new one
func ValidateOrGenerateRequestID(requestID string) (string, error) {
	if requestID == "" {
		return generateRequestID()
	}

	if len(requestID) > MaxRequestIDLength {
		return "", fmt.Errorf("%w: exceeds %d characters", ErrInvalidRequestID, MaxRequestIDLength)
	}

	if !requestIDPattern.MatchString(requestID) {
		return "", fmt.Errorf("%w: contains invalid characters", ErrInvalidRequestID)
	}

	return requestID, nil
}

// generateRequestID generates a new random request ID
func generateRequestID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate request ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/validation/... -v`
Expected: PASS

**Step 5: Commit validation**

```bash
git add internal/validation/
git commit -m "feat(validation): add request ID validation and generation

Validates request IDs to prevent log injection and generates
secure random IDs when not provided."
```

**Step 6: Integrate into chat service**

Modify `internal/service/chat.go` GenerateReply function. After the input validation block, add:

```go
	// Validate or generate request ID
	requestID, err := validation.ValidateOrGenerateRequestID(req.RequestId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
```

Then update the params block to use `requestID` instead of `req.RequestId`:

```go
	RequestID:           requestID,
```

Do the same for GenerateReplyStream.

**Step 7: Run build to verify**

Run: `go build ./...`
Expected: Success

**Step 8: Commit integration**

```bash
git add internal/service/chat.go
git commit -m "fix(security): validate request IDs to prevent log injection"
```

---

## Task 6: Remove Unused Code and Fix Minor Issues

**Files:**
- Modify: `internal/provider/anthropic/client.go:347-356` (remove unused function)
- Modify: `internal/auth/ratelimit.go:78,101` (handle ignored errors)

**Step 1: Remove unused extractTextFromValue function**

Delete lines 346-356 from `internal/provider/anthropic/client.go` (the `extractTextFromValue` function).

**Step 2: Run build to verify**

Run: `go build ./internal/provider/...`
Expected: Success

**Step 3: Commit removal**

```bash
git add internal/provider/anthropic/client.go
git commit -m "chore: remove unused extractTextFromValue function"
```

**Step 4: Fix ignored Expire errors in rate limiter**

The Expire errors in `internal/auth/ratelimit.go` are now handled by the Lua script (Task 3), so this step is complete.

---

## Task 7: Update Configuration Documentation

**Files:**
- Modify: `configs/aibox.yaml`

**Step 1: Add startup_mode documentation**

Add after the `logging` block in `configs/aibox.yaml`:

```yaml
# Startup mode controls dependency requirements
# - production: requires all dependencies (Redis, etc.) to be available (default)
# - development: allows running without optional dependencies
startup_mode: "production"
```

**Step 2: Commit documentation**

```bash
git add configs/aibox.yaml
git commit -m "docs: add startup_mode configuration option"
```

---

## Final Verification

**Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass

**Step 2: Run build**

Run: `go build ./...`
Expected: Success

**Step 3: Run vet**

Run: `go vet ./...`
Expected: No issues

**Step 4: Final commit with version bump**

Read VERSION, increment patch version, update CHANGELOG, commit and push.

---

## Summary of Security Fixes

| Issue | Severity | Fix |
|-------|----------|-----|
| Auth bypass when Redis unavailable | Critical | Startup mode enforcement |
| No input size validation | High | Validation package with limits |
| Rate limiting race condition | Medium | Atomic Lua script |
| Error message leakage | Medium | Error sanitization |
| Log injection via request ID | Medium | Request ID validation |
| Unused code | Low | Removed |
