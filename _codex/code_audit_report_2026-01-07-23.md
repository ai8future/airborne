# AIBox Code Audit Report
Date Created: 2026-01-07 23:21:06 +0100

## Scope
- Reviewed Go services in `cmd`, `internal`, API protos, configs, and Docker assets.
- Excluded `_studies`/`_proposals` per agent rules.
- Tests not executed for this audit.

## Findings (ordered by severity)
### High
1) Tenant boundary not enforced for API keys; tenant discovery before auth enables cross-tenant access.
- Evidence: `internal/auth/keys.go:36`, `internal/auth/tenant_interceptor.go:83`, `internal/server/grpc.go:91`.
- Impact: any valid API key can access any tenant by setting `tenant_id`; unauthenticated callers can probe tenant IDs.
- Recommendation: bind keys to tenant IDs and enforce match after authentication; move auth interceptors before tenant resolution; allow tenant ID via metadata for non-tenant-aware RPCs.

2) Multi-tenant file operations are blocked and the FileService API contract does not match implementation.
- Evidence: `internal/auth/tenant_interceptor.go:106`, `internal/service/files.go:33`, `api/proto/aibox/v1/files.proto:9`.
- Impact: `CreateFileStore/UploadFile/DeleteFileStore/GetFileStore` cannot resolve a tenant in multitenant mode; provider/config fields are silently ignored; `ListFileStores` is unimplemented.
- Recommendation: accept tenant IDs via metadata (or add `tenant_id` to file RPCs), validate unsupported provider fields to avoid silent misuse, and implement/adjust the list API or update docs to clarify scope.

3) Default deployment variables and configuration are miswired; provider settings in `configs/aibox.yaml` are unused.
- Evidence: `configs/aibox.yaml:18`, `configs/email4ai.json:1`, `docker-compose.yml:13`, `internal/service/chat.go:454`.
- Impact: docker-compose sets `OPENAI_API_KEY`/`GEMINI_API_KEY`/`ANTHROPIC_API_KEY`, but tenant config expects `EMAIL4AI_*` vars; if tenant config fails to load the server falls back to legacy mode with no provider API keys and all requests fail.
- Recommendation: align env var names, or fail fast in production when tenant configs cannot load; if legacy mode is intended, wire `config.Providers` into provider defaults.

### Medium
4) Config file read errors are silently ignored.
- Evidence: `internal/config/config.go:99`.
- Impact: unreadable/missing config files fall back to defaults (e.g., TLS disabled) with no warning, leading to insecure or surprising runtime behavior.
- Recommendation: return an error when the config file exists but cannot be read.

5) Failover is not applied consistently; streaming fallback and tenant failover order are ignored.
- Evidence: `internal/service/chat.go:149`, `internal/service/chat.go:281`, `internal/service/chat.go:428`, `internal/tenant/config.go:25`.
- Impact: failover settings are only partially honored (unary only, hard-coded order), causing avoidable outages for streaming clients.
- Recommendation: use tenant failover order when present and attempt fallback on stream initialization failures.

6) Go toolchain versions are invalid.
- Evidence: `go.mod:3`, `Dockerfile:5`.
- Impact: builds fail because `1.25` toolchains/images do not exist.
- Recommendation: pin to a real Go release (e.g., 1.22) consistently.

### Low/Medium
7) RAG context is appended after size validation.
- Evidence: `internal/service/chat.go:71`, `internal/service/chat.go:105`, `internal/service/chat.go:244`.
- Impact: with larger RAG settings, the effective prompt can exceed configured size limits; provider requests then fail or inflate costs.
- Recommendation: revalidate after RAG context injection.

8) Tenant ID case normalization is inconsistent between load and lookup.
- Evidence: `internal/auth/tenant_interceptor.go:94`, `internal/tenant/loader.go:54`.
- Impact: tenant IDs with uppercase characters in config are unreachable because requests are normalized to lowercase.
- Recommendation: normalize tenant IDs on load.

9) FileService provider fields are ignored and `ListFileStores` is unimplemented.
- Evidence: `internal/service/files.go:33`, `api/proto/aibox/v1/files.proto:23`.
- Impact: client SDKs expect provider-backed file stores that are not supported in this implementation.
- Recommendation: reject unsupported provider settings up front, or update the proto/docs to describe self-hosted Qdrant-only behavior.

10) Logging config in `configs/aibox.yaml` is ignored.
- Evidence: `cmd/aibox/main.go:43`, `configs/aibox.yaml:47`.
- Impact: operators cannot control log level/format via config.
- Recommendation: apply `cfg.Logging` when initializing the logger.

## Patch-ready diffs

### Patch 1: Enforce tenant scoping, allow tenant ID via metadata, and reorder interceptors
```diff
--- a/internal/auth/keys.go
+++ b/internal/auth/keys.go
@@
 type ClientKey struct {
 	KeyID        string            `json:"key_id"`
 	ClientID     string            `json:"client_id"`
 	ClientName   string            `json:"client_name"`
+	TenantID     string            `json:"tenant_id,omitempty"`
 	SecretHash   string            `json:"secret_hash"`
 	Permissions  []Permission      `json:"permissions"`
 	RateLimits   RateLimits        `json:"rate_limits"`
 	ProviderKeys map[string]string `json:"provider_keys,omitempty"` // Encrypted provider API keys
 	CreatedAt    time.Time         `json:"created_at"`
 	ExpiresAt    *time.Time        `json:"expires_at,omitempty"`
 	Metadata     map[string]string `json:"metadata,omitempty"`
 }
@@
 func (s *KeyStore) GenerateAPIKey(ctx context.Context, clientID, clientName string, permissions []Permission, limits RateLimits) (string, *ClientKey, error) {
+	return s.GenerateTenantAPIKey(ctx, "", clientID, clientName, permissions, limits)
+}
+
+// GenerateTenantAPIKey generates a new API key scoped to a tenant ID (optional).
+func (s *KeyStore) GenerateTenantAPIKey(ctx context.Context, tenantID, clientID, clientName string, permissions []Permission, limits RateLimits) (string, *ClientKey, error) {
 	// Generate key ID and secret
 	keyID, err := generateRandomString(8)
@@
 	key := &ClientKey{
 		KeyID:       keyID,
 		ClientID:    clientID,
 		ClientName:  clientName,
+		TenantID:    tenantID,
 		SecretHash:  string(hash),
 		Permissions: permissions,
 		RateLimits:  limits,
 		CreatedAt:   time.Now().UTC(),
 		Metadata:    make(map[string]string),
 	}
```

```diff
--- a/internal/auth/tenant_interceptor.go
+++ b/internal/auth/tenant_interceptor.go
@@
 import (
 	"context"
 	"strings"
 
 	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
 	"github.com/cliffpyles/aibox/internal/tenant"
 	"google.golang.org/grpc"
 	"google.golang.org/grpc/codes"
+	"google.golang.org/grpc/metadata"
 	"google.golang.org/grpc/status"
 )
@@
 		// Resolve tenant
-		tenantCfg, err := t.resolveTenant(tenantID)
+		tenantCfg, err := t.resolveTenant(ctx, tenantID)
 		if err != nil {
 			return nil, err
 		}
@@
-			cfg, err := s.interceptor.resolveTenant(tenantID)
+			cfg, err := s.interceptor.resolveTenant(s.ServerStream.Context(), tenantID)
 			if err != nil {
 				return err
 			}
@@
-func (t *TenantInterceptor) resolveTenant(tenantID string) (*tenant.TenantConfig, error) {
-	// If tenant_id is empty, check for single-tenant mode
-	if tenantID == "" {
-		if t.manager.IsSingleTenant() {
-			cfg, _ := t.manager.DefaultTenant()
-			return &cfg, nil
-		}
-		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
-	}
-
-	// Normalize tenant_id
-	tenantID = strings.ToLower(strings.TrimSpace(tenantID))
+func (t *TenantInterceptor) resolveTenant(ctx context.Context, tenantID string) (*tenant.TenantConfig, error) {
+	tenantID = strings.TrimSpace(tenantID)
+	if tenantID == "" {
+		tenantID = tenantIDFromMetadata(ctx)
+	}
+
+	// If tenant_id is empty, check for single-tenant mode
+	if tenantID == "" {
+		if t.manager.IsSingleTenant() {
+			cfg, _ := t.manager.DefaultTenant()
+			return &cfg, nil
+		}
+		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
+	}
+
+	// Normalize tenant_id
+	tenantID = normalizeTenantID(tenantID)
@@
-	return &cfg, nil
+	if err := enforceTenantClaim(ctx, tenantID); err != nil {
+		return nil, err
+	}
+
+	return &cfg, nil
 }
+
+func normalizeTenantID(tenantID string) string {
+	return strings.ToLower(strings.TrimSpace(tenantID))
+}
+
+func tenantIDFromMetadata(ctx context.Context) string {
+	md, ok := metadata.FromIncomingContext(ctx)
+	if !ok {
+		return ""
+	}
+	for _, key := range []string{"x-tenant-id", "tenant-id", "x-tenant"} {
+		if values := md.Get(key); len(values) > 0 {
+			return strings.TrimSpace(values[0])
+		}
+	}
+	return ""
+}
+
+func enforceTenantClaim(ctx context.Context, tenantID string) error {
+	client := ClientFromContext(ctx)
+	if client == nil {
+		return nil
+	}
+	if client.HasPermission(PermissionAdmin) {
+		return nil
+	}
+
+	claim := strings.TrimSpace(client.TenantID)
+	if claim == "" && client.Metadata != nil {
+		claim = strings.TrimSpace(client.Metadata["tenant_id"])
+	}
+	if claim == "" {
+		claim = strings.TrimSpace(client.ClientID)
+	}
+	if claim == "" {
+		return nil
+	}
+	if normalizeTenantID(claim) != tenantID {
+		return status.Error(codes.PermissionDenied, "tenant_id not authorized")
+	}
+	return nil
+}
@@
 func TenantIDFromContext(ctx context.Context) string {
 	if cfg := TenantFromContext(ctx); cfg != nil && cfg.TenantID != "" {
 		return cfg.TenantID
 	}
-	if client := ClientFromContext(ctx); client != nil && client.ClientID != "" {
-		return client.ClientID
+	if client := ClientFromContext(ctx); client != nil {
+		if client.TenantID != "" {
+			return client.TenantID
+		}
+		if client.ClientID != "" {
+			return client.ClientID
+		}
 	}
 	return "default"
 }
```

```diff
--- a/internal/server/grpc.go
+++ b/internal/server/grpc.go
@@
-	// Add tenant interceptor first (validates tenant before auth)
-	if tenantInterceptor != nil {
-		unaryInterceptors = append(unaryInterceptors, tenantInterceptor.UnaryInterceptor())
-		streamInterceptors = append(streamInterceptors, tenantInterceptor.StreamInterceptor())
-	}
-
-	// Add auth interceptors if Redis is available
-	if authenticator != nil {
-		unaryInterceptors = append(unaryInterceptors, authenticator.UnaryInterceptor())
-		streamInterceptors = append(streamInterceptors, authenticator.StreamInterceptor())
-	} else if !cfg.StartupMode.IsProduction() {
-		// Inject a dev client when auth is disabled in development mode
-		unaryInterceptors = append(unaryInterceptors, developmentAuthInterceptor())
-		streamInterceptors = append(streamInterceptors, developmentAuthStreamInterceptor())
-	}
+	// Add auth interceptors if Redis is available
+	if authenticator != nil {
+		unaryInterceptors = append(unaryInterceptors, authenticator.UnaryInterceptor())
+		streamInterceptors = append(streamInterceptors, authenticator.StreamInterceptor())
+	} else if !cfg.StartupMode.IsProduction() {
+		// Inject a dev client when auth is disabled in development mode
+		unaryInterceptors = append(unaryInterceptors, developmentAuthInterceptor())
+		streamInterceptors = append(streamInterceptors, developmentAuthStreamInterceptor())
+	}
+
+	// Add tenant interceptor after auth (prevents tenant enumeration, enables tenant scoping)
+	if tenantInterceptor != nil {
+		unaryInterceptors = append(unaryInterceptors, tenantInterceptor.UnaryInterceptor())
+		streamInterceptors = append(streamInterceptors, tenantInterceptor.StreamInterceptor())
+	}
```

### Patch 2: Fail fast on unreadable config files
```diff
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@
-	if data, err := os.ReadFile(configPath); err == nil {
-		if err := yaml.Unmarshal(data, cfg); err != nil {
-			return nil, fmt.Errorf("failed to parse config file: %w", err)
-		}
-	}
+	data, err := os.ReadFile(configPath)
+	if err != nil {
+		if !os.IsNotExist(err) {
+			return nil, fmt.Errorf("failed to read config file: %w", err)
+		}
+	} else {
+		if err := yaml.Unmarshal(data, cfg); err != nil {
+			return nil, fmt.Errorf("failed to parse config file: %w", err)
+		}
+	}
```

### Patch 3: Honor logging config
```diff
--- a/cmd/aibox/main.go
+++ b/cmd/aibox/main.go
@@
 import (
 	"context"
 	"flag"
 	"fmt"
 	"log/slog"
 	"net"
 	"os"
 	"os/signal"
+	"strings"
 	"syscall"
 	"time"
@@
-	// Set up structured logging
-	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
-		Level: slog.LevelInfo,
-	}))
-	slog.SetDefault(logger)
-
-	// Load configuration
-	cfg, err := config.Load()
-	if err != nil {
-		slog.Error("failed to load configuration", "error", err)
-		os.Exit(1)
-	}
+	// Load configuration
+	cfg, err := config.Load()
+	if err != nil {
+		fmt.Fprintf(os.Stderr, "failed to load configuration: %v\n", err)
+		os.Exit(1)
+	}
+
+	// Set up structured logging (after config load)
+	logger := buildLogger(cfg)
+	slog.SetDefault(logger)
@@
 func main() {
@@
 }
+
+func buildLogger(cfg *config.Config) *slog.Logger {
+	level := slog.LevelInfo
+	switch strings.ToLower(cfg.Logging.Level) {
+	case "debug":
+		level = slog.LevelDebug
+	case "warn", "warning":
+		level = slog.LevelWarn
+	case "error":
+		level = slog.LevelError
+	}
+
+	opts := &slog.HandlerOptions{Level: level}
+	switch strings.ToLower(cfg.Logging.Format) {
+	case "text":
+		return slog.New(slog.NewTextHandler(os.Stdout, opts))
+	default:
+		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
+	}
+}
```

### Patch 4: Apply tenant failover order, add streaming fallback, and revalidate RAG prompt size
```diff
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@
 	// Retrieve RAG context for non-OpenAI providers
 	var ragChunks []rag.RetrieveResult
 	instructions := req.Instructions
 	if req.EnableFileSearch && strings.TrimSpace(req.FileStoreId) != "" && selectedProvider.Name() != "openai" {
@@
 		}
 	}
+	if instructions != req.Instructions {
+		if err := validation.ValidateGenerateRequest(
+			req.UserInput,
+			instructions,
+			len(req.ConversationHistory),
+		); err != nil {
+			return nil, status.Error(codes.InvalidArgument, err.Error())
+		}
+	}
@@
-			fallbackProvider := s.getFallbackProvider(selectedProvider.Name(), req.FallbackProvider)
+			fallbackProvider := s.getFallbackProvider(ctx, selectedProvider.Name(), req.FallbackProvider)
 			if fallbackProvider != nil {
@@
 func (s *ChatService) GenerateReplyStream(req *pb.GenerateReplyRequest, stream pb.AIBoxService_GenerateReplyStreamServer) error {
@@
 	// Retrieve RAG context for non-OpenAI providers
 	var ragChunks []rag.RetrieveResult
 	instructions := req.Instructions
 	if req.EnableFileSearch && strings.TrimSpace(req.FileStoreId) != "" && selectedProvider.Name() != "openai" {
@@
 		}
 	}
+	if instructions != req.Instructions {
+		if err := validation.ValidateGenerateRequest(
+			req.UserInput,
+			instructions,
+			len(req.ConversationHistory),
+		); err != nil {
+			return status.Error(codes.InvalidArgument, err.Error())
+		}
+	}
@@
-	// Generate streaming reply
-	streamChunks, err := selectedProvider.GenerateReplyStream(ctx, params)
-	if err != nil {
-		return status.Error(codes.Internal, sanitize.SanitizeForClient(err))
-	}
+	// Generate streaming reply (with optional fallback on init failure)
+	streamProvider := selectedProvider
+	streamChunks, err := streamProvider.GenerateReplyStream(ctx, params)
+	if err != nil && req.EnableFailover {
+		fallbackProvider := s.getFallbackProvider(ctx, selectedProvider.Name(), req.FallbackProvider)
+		if fallbackProvider != nil {
+			params.Config = s.buildProviderConfig(ctx, req, fallbackProvider.Name())
+			streamProvider = fallbackProvider
+			streamChunks, err = streamProvider.GenerateReplyStream(ctx, params)
+		}
+	}
+	if err != nil {
+		return status.Error(codes.Internal, sanitize.SanitizeForClient(err))
+	}
@@
-			pbChunk = &pb.GenerateReplyChunk{
+			pbChunk = &pb.GenerateReplyChunk{
 				Chunk: &pb.GenerateReplyChunk_Complete{
 					Complete: &pb.StreamComplete{
 						ResponseId: chunk.ResponseID,
 						Model:      chunk.Model,
-						Provider:   mapProviderToProto(selectedProvider.Name()),
+						Provider:   mapProviderToProto(streamProvider.Name()),
 						FinalUsage: convertUsage(chunk.Usage),
 					},
 				},
 			}
@@
-func (s *ChatService) getFallbackProvider(primary string, specified pb.Provider) provider.Provider {
+func (s *ChatService) getFallbackProvider(ctx context.Context, primary string, specified pb.Provider) provider.Provider {
 	if specified != pb.Provider_PROVIDER_UNSPECIFIED {
 		switch specified {
 		case pb.Provider_PROVIDER_OPENAI:
 			return s.openaiProvider
 		case pb.Provider_PROVIDER_GEMINI:
 			return s.geminiProvider
 		case pb.Provider_PROVIDER_ANTHROPIC:
 			return s.anthropicProvider
 		}
 	}
+
+	// Tenant-configured fallback order
+	if tenantCfg := auth.TenantFromContext(ctx); tenantCfg != nil && tenantCfg.Failover.Enabled {
+		for _, name := range tenantCfg.Failover.Order {
+			if name == primary {
+				continue
+			}
+			if p := s.providerByName(name); p != nil {
+				return p
+			}
+		}
+	}
@@
 	// Default fallback order
 	switch primary {
 	case "openai":
 		return s.geminiProvider
 	case "gemini":
 		return s.openaiProvider
 	case "anthropic":
 		return s.openaiProvider
 	default:
 		return s.geminiProvider
 	}
 }
+
+func (s *ChatService) providerByName(name string) provider.Provider {
+	switch name {
+	case "openai":
+		return s.openaiProvider
+	case "gemini":
+		return s.geminiProvider
+	case "anthropic":
+		return s.anthropicProvider
+	default:
+		return nil
+	}
+}
```

### Patch 5: Normalize tenant IDs on load
```diff
--- a/internal/tenant/loader.go
+++ b/internal/tenant/loader.go
@@
-		// Skip files without tenant_id (e.g., shared config files)
-		if cfg.TenantID == "" {
+		cfg.TenantID = strings.ToLower(strings.TrimSpace(cfg.TenantID))
+		// Skip files without tenant_id (e.g., shared config files)
+		if cfg.TenantID == "" {
 			continue
 		}
```

### Patch 6: Validate unsupported provider fields in FileService
```diff
--- a/internal/service/files.go
+++ b/internal/service/files.go
@@
 import (
 	"bytes"
 	"context"
 	"fmt"
 	"io"
 	"log/slog"
 	"time"
 
 	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
 	"github.com/cliffpyles/aibox/internal/auth"
 	"github.com/cliffpyles/aibox/internal/rag"
+	"google.golang.org/grpc/codes"
+	"google.golang.org/grpc/status"
 )
@@
 	// Check permission
 	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
 		return nil, err
 	}
+	if err := validateProvider(req.Provider); err != nil {
+		return nil, err
+	}
@@
 	if metadata.Filename == "" {
 		return fmt.Errorf("filename is required")
 	}
+	if err := validateProvider(metadata.Provider); err != nil {
+		return err
+	}
@@
 	// Check permission
 	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
 		return nil, err
 	}
+	if err := validateProvider(req.Provider); err != nil {
+		return nil, err
+	}
@@
 	// Check permission
 	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
 		return nil, err
 	}
+	if err := validateProvider(req.Provider); err != nil {
+		return nil, err
+	}
@@
 }
+
+func validateProvider(provider pb.Provider) error {
+	if provider != pb.Provider_PROVIDER_UNSPECIFIED {
+		return status.Error(codes.InvalidArgument, "only self-hosted stores are supported; set provider=PROVIDER_UNSPECIFIED")
+	}
+	return nil
+}
```

### Patch 7: Fix Go toolchain versions
```diff
--- a/go.mod
+++ b/go.mod
@@
-go 1.25.5
+go 1.22
```

```diff
--- a/Dockerfile
+++ b/Dockerfile
@@
-FROM golang:1.25-alpine AS builder
+FROM golang:1.22-alpine AS builder
```

### Patch 8: Align docker-compose env vars with tenant config
```diff
--- a/docker-compose.yml
+++ b/docker-compose.yml
@@
 	  environment:
 	    - REDIS_PASSWORD=${REDIS_PASSWORD:-}
 	    - AIBOX_ADMIN_TOKEN=${AIBOX_ADMIN_TOKEN:-}
 	    - OPENAI_API_KEY=${OPENAI_API_KEY:-}
 	    - GEMINI_API_KEY=${GEMINI_API_KEY:-}
 	    - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}
+	    - EMAIL4AI_OPENAI_API_KEY=${OPENAI_API_KEY:-}
+	    - EMAIL4AI_GEMINI_API_KEY=${GEMINI_API_KEY:-}
+	    - EMAIL4AI_ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}
 	    # RAG configuration
```

## Suggested follow-ups
- Add a documented key-management flow so `TenantID` is set for keys (e.g., admin RPC or provisioning script).
- Decide whether admin keys should be tenant-scoped or global, then enforce consistently.
- Either implement provider-backed file stores or update `api/proto/aibox/v1/files.proto` and docs to define Qdrant-only semantics.
- If `config.Providers` and `config.Failover` are intended to be used in single-tenant/legacy mode, wire them into `ChatService` (or remove to avoid misconfiguration).

## Suggested verification
1) `go test ./...`
2) `go test -race ./...`
3) `docker build .`
4) `docker-compose up` and verify `/aibox.v1.AdminService/Ready` and a sample `GenerateReply` for the configured tenant.
