# Remaining Audit Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix remaining Medium/Low security issues from audit reports and clean up report files

**Architecture:** Fix token accounting bypass, health checks, SSRF protection, collection name validation, and various edge cases. Remove fixed items from reports and delete empty report files.

**Tech Stack:** Go, gRPC, Redis, Qdrant, Docker

---

## Summary of Issues to Fix

| Priority | Issue | Task |
|----------|-------|------|
| High | Token-per-minute bypass (streaming + defaults) | Task 1 |
| Medium | Container health checks broken | Task 2 |
| Medium | base_url override enables SSRF | Task 3 |
| Medium | Qdrant collection name injection | Task 4 |
| Medium | RAG TopK hardcoded to 5 | Task 5 |
| Low | Chunker panic edge case | Task 6 |
| Low | Dev mode fails without Redis | Task 7 |
| Low | RAG_ENABLED env cannot disable | Task 8 |
| Cleanup | Remove fixed items from reports | Task 9 |

---

## Task 1: Fix Token-Per-Minute Rate Limiting

**Files:**
- Modify: `internal/auth/ratelimit.go`
- Modify: `internal/service/chat.go`
- Test: `internal/auth/ratelimit_test.go`

**Issue:** TPM limits are bypassed because (1) RecordTokens returns early when limit=0 without applying defaults, and (2) streaming responses never record token usage.

**Step 1: Fix RecordTokens to apply default TPM**

In `internal/auth/ratelimit.go`, find the `RecordTokens` function and modify:

```go
// RecordTokens records token usage for TPM limiting
func (r *RateLimiter) RecordTokens(ctx context.Context, clientID string, tokens int64, limit int) error {
	if !r.enabled {
		return nil
	}

	// Apply default limit if per-client limit is zero
	effectiveLimit := limit
	if effectiveLimit == 0 {
		effectiveLimit = r.defaultLimits.TokensPerMinute
	}
	if effectiveLimit == 0 {
		return nil
	}

	// ... rest of function uses effectiveLimit instead of limit
```

**Step 2: Record tokens on stream completion**

In `internal/service/chat.go`, in `GenerateReplyStream`, find the `ChunkTypeComplete` case and add token recording:

```go
case provider.ChunkTypeComplete:
	// Record token usage for rate limiting
	if s.rateLimiter != nil && chunk.Usage != nil {
		if client := auth.ClientFromContext(ctx); client != nil {
			_ = s.rateLimiter.RecordTokens(ctx, client.ClientID, chunk.Usage.TotalTokens, client.RateLimits.TokensPerMinute)
		}
	}
	pbChunk = &pb.GenerateReplyChunk{
		// ... existing code
```

**Step 3: Run tests**

```bash
go test ./internal/auth/... -v -run "RateLimit"
go test ./internal/service/... -v
```

**Step 4: Commit**

```bash
git add internal/auth/ratelimit.go internal/service/chat.go
git commit -m "fix: apply default TPM and record streaming token usage"
```

---

## Task 2: Fix Container Health Checks

**Files:**
- Modify: `cmd/aibox/main.go`
- Modify: `Dockerfile`
- Modify: `docker-compose.yml`

**Issue:** Dockerfile uses curl against gRPC port (HTTP check fails on gRPC), and docker-compose uses `--health-check` flag that doesn't exist.

**Step 1: Add --health-check flag to main.go**

Add at the top of main():

```go
func main() {
	healthCheck := flag.Bool("health-check", false, "Check gRPC health and exit")
	flag.Parse()

	// ... existing logger setup ...

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	if *healthCheck {
		if err := runHealthCheck(cfg); err != nil {
			slog.Error("health check failed", "error", err)
			os.Exit(1)
		}
		return
	}

	// ... rest of main ...
}
```

Add the health check function:

```go
func runHealthCheck(cfg *config.Config) error {
	host := cfg.Server.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, cfg.Server.GRPCPort)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var creds credentials.TransportCredentials
	if cfg.TLS.Enabled {
		tlsCreds, err := credentials.NewClientTLSFromFile(cfg.TLS.CertFile, "")
		if err != nil {
			return fmt.Errorf("load tls cert: %w", err)
		}
		creds = tlsCreds
	} else {
		creds = insecure.NewCredentials()
	}

	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(creds), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	resp, err := client.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		return fmt.Errorf("health rpc: %w", err)
	}
	if resp.GetStatus() != "healthy" {
		return fmt.Errorf("health status %q", resp.GetStatus())
	}
	return nil
}
```

Add imports:
```go
import (
	"flag"
	// ... existing imports ...
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)
```

**Step 2: Update Dockerfile**

Replace the HEALTHCHECK line:
```dockerfile
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD /app/aibox --health-check
```

**Step 3: Update docker-compose.yml**

The healthcheck should already use --health-check, but verify it matches:
```yaml
healthcheck:
  test: ["CMD", "/app/aibox", "--health-check"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 10s
```

**Step 4: Commit**

```bash
git add cmd/aibox/main.go Dockerfile docker-compose.yml
git commit -m "fix: implement --health-check flag for container probes"
```

---

## Task 3: Restrict base_url Override (SSRF Prevention)

**Files:**
- Modify: `internal/service/chat.go`

**Issue:** Any client can set `provider_configs.base_url`, causing server to contact arbitrary endpoints.

**Step 1: Add base_url check function**

```go
func hasCustomBaseURL(req *pb.GenerateReplyRequest) bool {
	for _, cfg := range req.ProviderConfigs {
		if cfg != nil && strings.TrimSpace(cfg.GetBaseUrl()) != "" {
			return true
		}
	}
	return false
}
```

**Step 2: Require admin permission for custom base_url**

In `GenerateReply`:
```go
func (s *ChatService) GenerateReply(ctx context.Context, req *pb.GenerateReplyRequest) (*pb.GenerateReplyResponse, error) {
	if err := auth.RequirePermission(ctx, auth.PermissionChat); err != nil {
		return nil, err
	}
	if hasCustomBaseURL(req) {
		if err := auth.RequirePermission(ctx, auth.PermissionAdmin); err != nil {
			return nil, status.Error(codes.PermissionDenied, "custom base_url requires admin permission")
		}
	}
	// ... rest of function
```

In `GenerateReplyStream`:
```go
func (s *ChatService) GenerateReplyStream(req *pb.GenerateReplyRequest, stream pb.AIBoxService_GenerateReplyStreamServer) error {
	ctx := stream.Context()
	if err := auth.RequirePermission(ctx, auth.PermissionChatStream); err != nil {
		return err
	}
	if hasCustomBaseURL(req) {
		if err := auth.RequirePermission(ctx, auth.PermissionAdmin); err != nil {
			return status.Error(codes.PermissionDenied, "custom base_url requires admin permission")
		}
	}
	// ... rest of function
```

**Step 3: Run tests**

```bash
go test ./internal/service/... -v
```

**Step 4: Commit**

```bash
git add internal/service/chat.go
git commit -m "security: require admin permission for custom base_url (SSRF prevention)"
```

---

## Task 4: Validate Qdrant Collection Names

**Files:**
- Modify: `internal/rag/service.go`
- Create: `internal/rag/service_validation_test.go`

**Issue:** Unvalidated tenant/store IDs are used in Qdrant URLs, allowing path manipulation.

**Step 1: Add validation function**

```go
import (
	"regexp"
)

const maxCollectionPartLen = 128

var collectionPartPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func validateCollectionParts(tenantID, storeID string) error {
	tenantID = strings.TrimSpace(tenantID)
	storeID = strings.TrimSpace(storeID)

	if tenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if storeID == "" {
		return fmt.Errorf("store_id is required")
	}
	if len(tenantID) > maxCollectionPartLen {
		return fmt.Errorf("tenant_id exceeds %d characters", maxCollectionPartLen)
	}
	if len(storeID) > maxCollectionPartLen {
		return fmt.Errorf("store_id exceeds %d characters", maxCollectionPartLen)
	}
	if !collectionPartPattern.MatchString(tenantID) {
		return fmt.Errorf("tenant_id contains invalid characters")
	}
	if !collectionPartPattern.MatchString(storeID) {
		return fmt.Errorf("store_id contains invalid characters")
	}
	return nil
}
```

**Step 2: Call validation in all public methods**

Add to `Ingest`, `Retrieve`, `CreateStore`, `DeleteStore`, `StoreInfo`:
```go
if err := validateCollectionParts(params.TenantID, params.StoreID); err != nil {
	return nil, err
}
```

**Step 3: Run tests**

```bash
go test ./internal/rag/... -v
```

**Step 4: Commit**

```bash
git add internal/rag/service.go
git commit -m "security: validate tenant/store IDs before Qdrant operations"
```

---

## Task 5: Use Configured RAG TopK

**Files:**
- Modify: `internal/service/chat.go`

**Issue:** `retrieveRAGContext` hardcodes `TopK: 5`, ignoring configured defaults.

**Step 1: Fix TopK to use service default**

Change in `retrieveRAGContext`:
```go
return s.ragService.Retrieve(ctx, rag.RetrieveParams{
	StoreID:  storeID,
	TenantID: auth.TenantIDFromContext(ctx),
	Query:    query,
	TopK:     0, // Use service default
})
```

**Step 2: Run tests**

```bash
go test ./internal/service/... -v
```

**Step 3: Commit**

```bash
git add internal/service/chat.go
git commit -m "fix: use configured RAG TopK instead of hardcoded 5"
```

---

## Task 6: Fix Chunker Panic Edge Case

**Files:**
- Modify: `internal/rag/chunker/chunker.go`
- Test: `internal/rag/chunker/chunker_test.go`

**Issue:** If `chunkText` is smaller than `MinChunkSize` and no chunk has been appended, accessing `chunks[len(chunks)-1]` panics.

**Step 1: Guard overlap backtracking**

Find the line with `chunks[len(chunks)-1]` and add guard:
```go
// Move start forward, accounting for overlap
start = end - opts.Overlap
if len(chunks) > 0 && start <= chunks[len(chunks)-1].Start {
	// Prevent infinite loop if overlap is too large
	start = end
}
```

**Step 2: Add test for edge case**

```go
func TestChunk_SmallTextNoPanic(t *testing.T) {
	// Text smaller than MinChunkSize should not panic
	result, err := Chunk("hi", ChunkOptions{
		MaxChunkSize: 100,
		MinChunkSize: 50,
		Overlap:      10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected at least one chunk")
	}
}
```

**Step 3: Run tests**

```bash
go test ./internal/rag/chunker/... -v
```

**Step 4: Commit**

```bash
git add internal/rag/chunker/chunker.go internal/rag/chunker/chunker_test.go
git commit -m "fix: prevent chunker panic on small text"
```

---

## Task 7: Fix Dev Mode Without Redis

**Files:**
- Modify: `internal/server/grpc.go`

**Issue:** In development mode with Redis unavailable, `auth.RequirePermission` always fails because no client is injected.

**Step 1: Add development auth interceptors**

After the tenant interceptor section, add:
```go
// Inject a dev client when auth is disabled in development mode
if authenticator == nil && !cfg.StartupMode.IsProduction() {
	unaryInterceptors = append(unaryInterceptors, developmentAuthInterceptor())
	streamInterceptors = append(streamInterceptors, developmentAuthStreamInterceptor())
}
```

Add the interceptor functions:
```go
func developmentAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		client := &auth.ClientKey{
			ClientID:   "dev",
			ClientName: "development",
			Permissions: []auth.Permission{
				auth.PermissionAdmin,
				auth.PermissionChat,
				auth.PermissionChatStream,
				auth.PermissionFiles,
			},
		}
		ctx = context.WithValue(ctx, auth.ClientContextKey, client)
		return handler(ctx, req)
	}
}

func developmentAuthStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		client := &auth.ClientKey{
			ClientID:   "dev",
			ClientName: "development",
			Permissions: []auth.Permission{
				auth.PermissionAdmin,
				auth.PermissionChat,
				auth.PermissionChatStream,
				auth.PermissionFiles,
			},
		}
		ctx := context.WithValue(ss.Context(), auth.ClientContextKey, client)
		wrapped := &wrappedStream{ServerStream: ss, ctx: ctx}
		return handler(srv, wrapped)
	}
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *wrappedStream) Context() context.Context {
	return s.ctx
}
```

**Step 2: Run tests**

```bash
go test ./internal/server/... -v
```

**Step 3: Commit**

```bash
git add internal/server/grpc.go
git commit -m "fix: inject dev client when Redis unavailable in dev mode"
```

---

## Task 8: Fix RAG_ENABLED Env Override

**Files:**
- Modify: `internal/config/config.go`

**Issue:** Setting `RAG_ENABLED=false` has no effect because env override only turns feature on.

**Step 1: Parse RAG_ENABLED with ParseBool**

Find the RAG_ENABLED section and replace:
```go
// RAG configuration
if enabled := os.Getenv("RAG_ENABLED"); enabled != "" {
	if v, err := strconv.ParseBool(enabled); err == nil {
		c.RAG.Enabled = v
	}
}
```

**Step 2: Run tests**

```bash
go test ./internal/config/... -v
```

**Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "fix: RAG_ENABLED env can now disable RAG"
```

---

## Task 9: Clean Up Audit Reports

**Files:**
- Modify/Delete: All files in `_codex/`

**Step 1: Remove fixed issues from each report**

For each report file, remove sections that have been fixed. After Task 1-8, the following issues are fixed:
- FileService auth, tenant, size limits (0.4.5)
- Path traversal (0.4.6)
- AdminService auth (0.4.7)
- API keys in requests (0.4.8)
- Token accounting (Task 1)
- Health checks (Task 2)
- SSRF prevention (Task 3)
- Collection name validation (Task 4)
- RAG TopK (Task 5)
- Chunker panic (Task 6)
- Dev mode (Task 7)
- RAG_ENABLED (Task 8)

**Step 2: Delete empty report files**

If a report becomes empty after removing fixed issues, delete it:
```bash
rm _codex/<empty-file>.md
```

**Step 3: Keep test and refactor reports**

The following are suggestions, not bugs - keep them:
- `code_test_report_2026-01-07-16.md` - test proposals
- `small_code_test_report_2026-01-07-16.md` - test proposals
- `code_refactor_report_2026-01-07-16.md` - refactor suggestions
- `small_code_refactor_report_2026-01-07-16.md` - refactor suggestions

**Step 4: Commit**

```bash
git add _codex/
git commit -m "docs: clean up audit reports, remove fixed issues"
```

---

## Execution Order

1. **Task 1**: Token rate limiting (high priority security)
2. **Task 2**: Health checks (infrastructure)
3. **Task 3**: SSRF prevention (security)
4. **Task 4**: Collection name validation (security)
5. **Task 5**: RAG TopK fix (quick fix)
6. **Task 6**: Chunker panic (edge case)
7. **Task 7**: Dev mode fix (developer experience)
8. **Task 8**: RAG_ENABLED fix (quick fix)
9. **Task 9**: Report cleanup (documentation)

**Total:** 9 tasks fixing ~11 remaining issues + report cleanup
