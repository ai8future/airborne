# Audit Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 8 verified issues from the _codex audit reports

**Architecture:** Small, targeted fixes across config, auth, service layers - no major refactoring needed

**Tech Stack:** Go, gRPC interceptors, slog logging

---

## Summary of Verified Issues

| # | Issue | Severity | File |
|---|-------|----------|------|
| 1 | TLS env overrides missing | HIGH | internal/config/config.go |
| 2 | Config file read errors silently ignored | MEDIUM | internal/config/config.go |
| 3 | Logging config not applied | MEDIUM | cmd/aibox/main.go |
| 4 | Rate limiter accepts negative tokens | MEDIUM | internal/auth/ratelimit.go |
| 5 | Tenant ID normalization inconsistent | MEDIUM | internal/tenant/loader.go |
| 6 | FileService blocked by tenant interceptor | MEDIUM | internal/auth/tenant_interceptor.go |
| 7 | SelectProvider lacks permission check | LOW | internal/service/chat.go |
| 8 | RAG point ID collisions (same filename) | MEDIUM | internal/rag/service.go, internal/service/files.go |

---

### Task 1: Add TLS Environment Variable Overrides

**Files:**
- Modify: `internal/config/config.go` (applyEnvOverrides function)

**Step 1: Add TLS env overrides**

Add after the AIBOX_HOST handling (around line 113):

```go
// TLS configuration
if enabled := os.Getenv("AIBOX_TLS_ENABLED"); enabled != "" {
	if v, err := strconv.ParseBool(enabled); err == nil {
		c.TLS.Enabled = v
	}
}
if cert := os.Getenv("AIBOX_TLS_CERT_FILE"); cert != "" {
	c.TLS.CertFile = cert
}
if key := os.Getenv("AIBOX_TLS_KEY_FILE"); key != "" {
	c.TLS.KeyFile = key
}
```

**Step 2: Add REDIS_DB and AIBOX_LOG_FORMAT overrides**

Add after REDIS_PASSWORD handling:

```go
if db := os.Getenv("REDIS_DB"); db != "" {
	if d, err := strconv.Atoi(db); err == nil {
		c.Redis.DB = d
	}
}
```

Add after AIBOX_LOG_LEVEL handling:

```go
if format := os.Getenv("AIBOX_LOG_FORMAT"); format != "" {
	c.Logging.Format = format
}
```

**Step 3: Verify build**

Run: `go build ./...`

**Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "fix: add TLS and logging env overrides to config"
```

---

### Task 2: Fix Config File Read Error Handling

**Files:**
- Modify: `internal/config/config.go` (Load function around line 99)

**Step 1: Change error handling**

Replace the silent ignore pattern:

```go
// Before:
if data, err := os.ReadFile(configPath); err == nil {
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
}

// After:
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
```

**Step 2: Verify build**

Run: `go build ./...`

**Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "fix: fail on config read errors (not just missing files)"
```

---

### Task 3: Apply Logging Config

**Files:**
- Modify: `cmd/aibox/main.go`

**Step 1: Add strings import if needed**

Ensure `"strings"` is in the imports.

**Step 2: Add configureLogger function**

Add after the main function:

```go
func configureLogger(cfg config.LoggingConfig) {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}
```

**Step 3: Call configureLogger after config load**

Find where cfg is loaded and add:

```go
configureLogger(cfg.Logging)
```

**Step 4: Remove the hardcoded logger setup**

Remove the existing slog.New(slog.NewJSONHandler...) setup that happens before config load.

**Step 5: Verify build**

Run: `go build ./...`

**Step 6: Commit**

```bash
git add cmd/aibox/main.go
git commit -m "fix: apply logging config (level/format) from config file"
```

---

### Task 4: Add Negative Token Check to Rate Limiter

**Files:**
- Modify: `internal/auth/ratelimit.go`

**Step 1: Add validation in RecordTokens**

Find the RecordTokens function and add early return:

```go
func (r *RateLimiter) RecordTokens(ctx context.Context, clientID string, tokens int64, limit int) error {
	if !r.enabled {
		return nil
	}
	if tokens <= 0 {
		return nil // Ignore non-positive token counts
	}
	// ... rest of function
```

**Step 2: Verify build**

Run: `go build ./...`

**Step 3: Commit**

```bash
git add internal/auth/ratelimit.go
git commit -m "fix: ignore non-positive token counts in rate limiter"
```

---

### Task 5: Normalize Tenant ID on Load

**Files:**
- Modify: `internal/tenant/loader.go`

**Step 1: Add normalization in loadTenants**

Find where cfg.TenantID is used and normalize it:

```go
// After unmarshaling, normalize the tenant ID
cfg.TenantID = strings.ToLower(strings.TrimSpace(cfg.TenantID))

// Skip files without tenant_id
if cfg.TenantID == "" {
	continue
}
```

**Step 2: Ensure strings import exists**

Add `"strings"` to imports if not present.

**Step 3: Verify build and run tests**

Run: `go build ./... && go test ./internal/tenant/...`

**Step 4: Commit**

```bash
git add internal/tenant/loader.go
git commit -m "fix: normalize tenant_id to lowercase on config load"
```

---

### Task 6: Skip FileService in Tenant Interceptor

**Files:**
- Modify: `internal/auth/tenant_interceptor.go`

**Step 1: Add FileService methods to skipMethods**

Find the skipMethods map in NewTenantInterceptor and add:

```go
skipMethods: map[string]bool{
	"/aibox.v1.AdminService/Health":         true,
	"/aibox.v1.AdminService/Ready":          true,
	"/aibox.v1.AdminService/Version":        true,
	"/aibox.v1.FileService/CreateFileStore": true,
	"/aibox.v1.FileService/UploadFile":      true,
	"/aibox.v1.FileService/DeleteFileStore": true,
	"/aibox.v1.FileService/GetFileStore":    true,
	"/aibox.v1.FileService/ListFileStores":  true,
},
```

**Step 2: Verify build**

Run: `go build ./...`

**Step 3: Commit**

```bash
git add internal/auth/tenant_interceptor.go
git commit -m "fix: skip tenant interception for FileService RPCs"
```

---

### Task 7: Add Permission Check to SelectProvider

**Files:**
- Modify: `internal/service/chat.go`

**Step 1: Add permission check to SelectProvider**

Find the SelectProvider function and add at the start:

```go
func (s *ChatService) SelectProvider(ctx context.Context, req *pb.SelectProviderRequest) (*pb.SelectProviderResponse, error) {
	if err := auth.RequirePermission(ctx, auth.PermissionChat); err != nil {
		return nil, err
	}
	// ... rest of function
```

**Step 2: Verify build**

Run: `go build ./...`

**Step 3: Commit**

```bash
git add internal/service/chat.go
git commit -m "fix: require chat permission for SelectProvider RPC"
```

---

### Task 8: Add FileID to RAG Ingestion

**Files:**
- Modify: `internal/rag/service.go`
- Modify: `internal/service/files.go`

**Step 1: Add FileID field to IngestParams**

In service.go, add to IngestParams struct:

```go
// FileID is an optional unique identifier for the file.
// If empty, defaults to filename_storeID for backwards compatibility.
FileID string
```

**Step 2: Add payloadFileID constant**

Add to the payload constants:

```go
payloadFileID = "file_id"
```

**Step 3: Update Ingest function to use FileID**

In the Ingest function, update point ID generation:

```go
// Generate fileID for unique point IDs
fileID := strings.TrimSpace(params.FileID)
if fileID == "" {
	fileID = fmt.Sprintf("%s_%s", params.Filename, params.StoreID)
}

points := make([]vectorstore.Point, len(chunks))
for i, chunk := range chunks {
	points[i] = vectorstore.Point{
		ID:     fmt.Sprintf("%s_%d", fileID, chunk.Index),
		Vector: embeddings[i],
		Payload: map[string]any{
			payloadTenantID:   params.TenantID,
			payloadThreadID:   params.ThreadID,
			payloadStoreID:    params.StoreID,
			payloadFileID:     fileID,
			payloadFilename:   params.Filename,
			// ... rest of fields
		},
	}
}
```

**Step 4: Add generateFileID to files.go**

Add helper function and imports:

```go
import (
	"crypto/rand"
	"encoding/hex"
)

func generateFileID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "file_" + hex.EncodeToString(buf), nil
}
```

**Step 5: Use FileID in UploadFile**

In UploadFile, generate and use FileID:

```go
fileID, err := generateFileID()
if err != nil {
	return fmt.Errorf("generate file id: %w", err)
}

result, err := s.ragService.Ingest(ctx, rag.IngestParams{
	StoreID:  metadata.StoreId,
	TenantID: tenantID,
	FileID:   fileID,
	// ... rest
})

// Update response to use fileID
return stream.SendAndClose(&pb.UploadFileResponse{
	FileId:   fileID,
	// ...
})
```

**Step 6: Verify build**

Run: `go build ./...`

**Step 7: Commit**

```bash
git add internal/rag/service.go internal/service/files.go
git commit -m "fix: add unique FileID to prevent RAG point collisions"
```

---

## Verification

After all tasks complete:

```bash
go build ./...
go test ./internal/... -short
```

## Update Reports

After implementing, update the _codex reports:
- Remove fixed items
- Add "Date Updated: 2026-01-08" to each modified report
- Increment VERSION and update CHANGELOG.md
