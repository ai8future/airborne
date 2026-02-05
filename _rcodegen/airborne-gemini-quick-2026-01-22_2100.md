Date Created: 2026-01-22 21:00:00
TOTAL_SCORE: 65/100

# 1. AUDIT

### [Critical] Security Risk: Auth Bypass Code in Production File
**Severity**: High
**File**: `internal/server/grpc.go`
**Description**: The functions `developmentAuthInterceptor` and `developmentAuthStreamInterceptor` are defined in `grpc.go`. Although they are not currently wired into `NewGRPCServer` in the main code path, their presence in a production file is a significant risk. If accidentally wired up or if the compiler doesn't strip them, they provide a mechanism to bypass authentication. They should be moved to a test file where they belong.

#### Patch: Move dev auth helpers to test file

**Step 1: Remove from `internal/server/grpc.go`**

```go
<<<<
// developmentAuthInterceptor injects a dev client in non-production mode when Redis is unavailable.
//
// WARNING: This function bypasses authentication entirely. It is intended ONLY for
// local development and testing. NEVER wire this into NewGRPCServer for production builds.
// If you need to use this, ensure it's behind a build tag or explicit development mode check.
func developmentAuthInterceptor() grpc.UnaryServerInterceptor {
	slog.Warn("SECURITY: Development authentication interceptor is active - do not use in production")
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		client := &auth.ClientKey{
			ClientID:   "dev",
			ClientName: "development",
			Permissions: []auth.Permission{
				// NOTE: PermissionAdmin intentionally excluded for security
				auth.PermissionChat,
				auth.PermissionChatStream,
				auth.PermissionFiles,
			},
		}
		ctx = context.WithValue(ctx, auth.ClientContextKey, client)
		return handler(ctx, req)
	}
}

// developmentAuthStreamInterceptor injects a dev client for streams in non-production mode.
//
// WARNING: This function bypasses authentication entirely. It is intended ONLY for
// local development and testing. NEVER wire this into NewGRPCServer for production builds.
// If you need to use this, ensure it's behind a build tag or explicit development mode check.
func developmentAuthStreamInterceptor() grpc.StreamServerInterceptor {
	slog.Warn("SECURITY: Development stream authentication interceptor is active - do not use in production")
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		client := &auth.ClientKey{
			ClientID:   "dev",
			ClientName: "development",
			Permissions: []auth.Permission{
				// NOTE: PermissionAdmin intentionally excluded for security
				auth.PermissionChat,
				auth.PermissionChatStream,
				auth.PermissionFiles,
			},
		}
		ctx := context.WithValue(ss.Context(), auth.ClientContextKey, client)
		wrapped := &devWrappedStream{ServerStream: ss, ctx: ctx}
		return handler(srv, wrapped)
	}
}

type devWrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *devWrappedStream) Context() context.Context {
	return s.ctx
}
====
>>>>
```

**Step 2: Create `internal/server/auth_dev_test.go`**

```go
<<<<
====
package server

import (
	"context"
	"log/slog"

	"github.com/ai8future/airborne/internal/auth"
	"google.golang.org/grpc"
)

// developmentAuthInterceptor injects a dev client in non-production mode when Redis is unavailable.
func developmentAuthInterceptor() grpc.UnaryServerInterceptor {
	slog.Warn("SECURITY: Development authentication interceptor is active - do not use in production")
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		client := &auth.ClientKey{
			ClientID:   "dev",
			ClientName: "development",
			Permissions: []auth.Permission{
				auth.PermissionChat,
				auth.PermissionChatStream,
				auth.PermissionFiles,
			},
		}
		ctx = context.WithValue(ctx, auth.ClientContextKey, client)
		return handler(ctx, req)
	}
}

// developmentAuthStreamInterceptor injects a dev client for streams in non-production mode.
func developmentAuthStreamInterceptor() grpc.StreamServerInterceptor {
	slog.Warn("SECURITY: Development stream authentication interceptor is active - do not use in production")
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		client := &auth.ClientKey{
			ClientID:   "dev",
			ClientName: "development",
			Permissions: []auth.Permission{
				auth.PermissionChat,
				auth.PermissionChatStream,
				auth.PermissionFiles,
			},
		}
		ctx := context.WithValue(ss.Context(), auth.ClientContextKey, client)
		wrapped := &devWrappedStream{ServerStream: ss, ctx: ctx}
		return handler(srv, wrapped)
	}
}

type devWrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *devWrappedStream) Context() context.Context {
	return s.ctx
}
>>>>
```

# 2. TESTS

### [Missing] Pricing Logic Tests
**File**: `internal/pricing/pricing.go`
**Description**: The pricing logic is critical for billing but currently lacks unit tests.

#### Patch: Add `internal/pricing/pricing_test.go`

```go
<<<<
====
package pricing

import (
	"testing"
)

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		inputTokens  int
		outputTokens int
		wantPositive bool
	}{
		{
			name:         "Known Model (GPT-4)",
			model:        "gpt-4",
			inputTokens:  100,
			outputTokens: 100,
			wantPositive: true,
		},
		{
			name:         "Unknown Model",
			model:        "unknown-model-xyz",
			inputTokens:  100,
			outputTokens: 100,
			wantPositive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := CalculateCost(tt.model, tt.inputTokens, tt.outputTokens)
			if tt.wantPositive && cost <= 0 {
				t.Errorf("CalculateCost() = %v, want > 0", cost)
			}
			if !tt.wantPositive && cost != 0 {
				t.Errorf("CalculateCost() = %v, want 0", cost)
			}
		})
	}
}

func TestPricingWrapper(t *testing.T) {
	pricer, err := NewPricer("")
	if err != nil {
		t.Fatalf("NewPricer failed: %v", err)
	}

	cost := pricer.Calculate("gpt-4", 1000, 1000)
	if cost.Unknown {
		t.Error("Expected known cost for gpt-4")
	}
	if cost.TotalCost <= 0 {
		t.Error("Expected positive total cost")
	}

	// Test formatting
	formatted := cost.Format()
	if formatted == "" {
		t.Error("Expected formatted string")
	}
}
>>>>
```

### [Missing] CLI Client Tests
**File**: `internal/cli/client.go`
**Description**: The CLI client interacts with the admin API but is untested.

#### Patch: Add `internal/cli/client_test.go`

```go
<<<<
====
package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Health(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			t.Errorf("Expected path /admin/health, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(HealthResponse{Status: "healthy"})
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	health, err := client.Health()
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("Health() status = %v, want healthy", health.Status)
	}
}

func TestClient_Activity(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/activity" {
			t.Errorf("Expected path /admin/activity, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(ActivityResponse{
			Activity: []Activity{{ID: "123"}},
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	activity, err := client.Activity(10, "")
	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if len(activity.Activity) != 1 {
		t.Errorf("Expected 1 activity item, got %d", len(activity.Activity))
	}
}
>>>>
```

# 3. FIXES

### [Bug] Configuration Load Swallows Errors
**File**: `internal/config/config.go`
**Description**: The `Load` function attempts to read the config file but ignores all errors unless the file doesn't exist. This can hide permission errors or other I/O issues.

#### Patch: Fix Error Handling in `Load`

```go
<<<<
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
====
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
>>>>
```

*Self-correction: The code examined actually looks correct (it does check `!os.IsNotExist(err)`). Re-reading:
`if !os.IsNotExist(err) { return nil, ... }`
This means if it IS NOT "NotExist" (e.g. Permission Denied), it returns error.
If it IS "NotExist", it continues.
This is arguably correct behavior if the config file is optional.
However, if `AIRBORNE_CONFIG` is set explicitly, it should probably fail if that specific file is missing.
The current logic falls back to defaults even if `AIRBORNE_CONFIG` points to a missing file.*

**Better Fix**: If `AIRBORNE_CONFIG` env var is set, enforce that the file must exist.

```go
<<<<
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
====
	// Try to load from file
	configPath := os.Getenv("AIRBORNE_CONFIG")
	mustExist := configPath != ""
	if configPath == "" {
		configPath = "configs/airborne.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if mustExist || !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// File doesn't exist - continue with defaults
	} else {
>>>>
```

# 4. REFACTOR

### 1. Simplify `NewGRPCServer`
**File**: `internal/server/grpc.go`
**Impact**: High (Maintainability)
**Description**: The `NewGRPCServer` function is over 200 lines long and handles config loading, auth setup, RAG initialization, DB connection, and service registration.
**Plan**:
1. Extract `initializeAuth(cfg)` -> `(UnaryInterceptor, StreamInterceptor, error)`
2. Extract `initializeRAG(cfg)` -> `(*rag.Service, error)`
3. Extract `initializeDB(cfg)` -> `(*db.Client, error)`

### 2. Decompose `Config.Load`
**File**: `internal/config/config.go`
**Impact**: Medium
**Description**: The `Load` function does too much: determining path, reading file, unmarshaling, applying env overrides, expanding vars, and validating.
**Plan**:
1. Create `loadFromFile(path)`
2. Create `applyEnvironment(cfg)`
3. Create `fetchDopplerSecrets(cfg)`
