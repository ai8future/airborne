# Code Fix Report
Date Created: 2026-01-08 00:05:43 +0100

## Overview
Reviewed tenant loading, auth interceptors, and RAG file ingestion paths for correctness in multi-tenant deployments and file upload workflows.

## Findings and Fixes
1) Tenant ID normalization mismatch
- Problem: The tenant interceptor lowercases incoming tenant_id values, but tenant configs are stored as-is.
- Impact: Tenants with mixed/uppercase IDs cannot be resolved; requests fail with "tenant not found".
- Fix: Normalize tenant_id to lower-case (trimmed) during config load, and add a regression test.

2) FileService blocked by tenant interceptor in multi-tenant mode
- Problem: FileService RPCs do not include tenant_id fields, but the tenant interceptor requires one when multiple tenants exist.
- Impact: File store creation/upload/delete fails for multi-tenant setups.
- Fix: Skip tenant interception for FileService RPCs so auth-based tenant lookup can proceed.

3) RAG ingestion point ID collisions for same filename
- Problem: Point IDs are generated from filename + store_id only, causing collisions when multiple files with the same name are uploaded to a store.
- Impact: New uploads can overwrite existing vectors, leading to data loss and incorrect retrieval.
- Fix: Introduce FileID in ingestion params, generate a unique FileID during upload, and use it for point IDs and payload metadata. Added a test to validate payload storage.

## Patch Diffs
```diff
diff --git a/internal/tenant/loader.go b/internal/tenant/loader.go
--- a/internal/tenant/loader.go
+++ b/internal/tenant/loader.go
@@ -49,9 +49,11 @@ func loadTenants(dir string) (map[string]TenantConfig, error) {
 		case ".yaml", ".yml":
 			if err := yaml.Unmarshal(raw, &cfg); err != nil {
 				return nil, fmt.Errorf("decoding %s: %w", path, err)
 			}
 		}
 
-		// Skip files without tenant_id (e.g., shared config files)
-		if cfg.TenantID == "" {
+		tenantID := strings.ToLower(strings.TrimSpace(cfg.TenantID))
+		// Skip files without tenant_id (e.g., shared config files)
+		if tenantID == "" {
 			continue
 		}
+		cfg.TenantID = tenantID
 
 		// Resolve secrets (ENV=, FILE= patterns)
 		if err := resolveSecrets(&cfg); err != nil {
 			return nil, fmt.Errorf("resolving secrets for %s: %w", path, err)
 		}

diff --git a/internal/tenant/loader_test.go b/internal/tenant/loader_test.go
--- a/internal/tenant/loader_test.go
+++ b/internal/tenant/loader_test.go
@@ -79,6 +79,27 @@ func TestLoadTenants_JSONAndYAML(t *testing.T) {
 	}
 }
 
+func TestLoadTenants_NormalizesTenantID(t *testing.T) {
+	dir := t.TempDir()
+
+	cfg := `{"tenant_id":"TenantA","providers":{"openai":{"enabled":true,"api_key":"key","model":"gpt-4o"}}}`
+	if err := os.WriteFile(filepath.Join(dir, "tenant.json"), []byte(cfg), 0o600); err != nil {
+		t.Fatalf("write config: %v", err)
+	}
+
+	configs, err := loadTenants(dir)
+	if err != nil {
+		t.Fatalf("loadTenants failed: %v", err)
+	}
+	if _, ok := configs["tenanta"]; !ok {
+		t.Fatalf("expected normalized tenant_id key, got keys: %v", configs)
+	}
+	if configs["tenanta"].TenantID != "tenanta" {
+		t.Fatalf("expected TenantID to be normalized, got %q", configs["tenanta"].TenantID)
+	}
+}
+
 func TestLoadTenants_SkipsEmptyTenantID(t *testing.T) {
 	dir := t.TempDir()
 
 	// Config without tenant_id
 	noID := `{"providers":{"openai":{"enabled":true,"api_key":"key","model":"gpt-4o"}}}`
@@ -27,9 +27,14 @@ func NewTenantInterceptor(mgr *tenant.Manager) *TenantInterceptor {
 		manager: mgr,
 		skipMethods: map[string]bool{
 			"/aibox.v1.AdminService/Health":  true,
 			"/aibox.v1.AdminService/Ready":   true,
 			"/aibox.v1.AdminService/Version": true,
+			"/aibox.v1.FileService/CreateFileStore": true,
+			"/aibox.v1.FileService/UploadFile":      true,
+			"/aibox.v1.FileService/DeleteFileStore": true,
+			"/aibox.v1.FileService/GetFileStore":    true,
+			"/aibox.v1.FileService/ListFileStores":  true,
 		},
 	}
 }

diff --git a/internal/rag/service.go b/internal/rag/service.go
--- a/internal/rag/service.go
+++ b/internal/rag/service.go
@@ -105,6 +105,9 @@ type IngestParams struct {
 	// ThreadID is the conversation thread ID (optional, for scoping).
 	ThreadID string
 
+	// FileID is an optional unique identifier for the file.
+	FileID string
+
 	// File is the file content to ingest.
 	File io.Reader
 
 	// Filename is the original filename.
@@ -167,12 +170,17 @@ func (s *Service) Ingest(ctx context.Context, params IngestParams) (*IngestResult, error) {
 	if err != nil {
 		return nil, fmt.Errorf("generate embeddings: %w", err)
 	}
 
 	// Create points for vector store
+	fileID := strings.TrimSpace(params.FileID)
+	if fileID == "" {
+		fileID = fmt.Sprintf("%s_%s", params.Filename, params.StoreID)
+	}
 	points := make([]vectorstore.Point, len(chunks))
 	for i, chunk := range chunks {
 		points[i] = vectorstore.Point{
-			ID:     fmt.Sprintf("%s_%s_%d", params.Filename, params.StoreID, chunk.Index),
+			ID:     fmt.Sprintf("%s_%d", fileID, chunk.Index),
 			Vector: embeddings[i],
 			Payload: map[string]any{
 				"tenant_id":   params.TenantID,
 				"thread_id":   params.ThreadID,
 				"store_id":    params.StoreID,
+				"file_id":     fileID,
 				"filename":    params.Filename,
 				"chunk_index": chunk.Index,
 				"text":        chunk.Text,
 				"char_start":  chunk.Start,
 				"char_end":    chunk.End,
 			},
 		}
 	}

diff --git a/internal/service/files.go b/internal/service/files.go
--- a/internal/service/files.go
+++ b/internal/service/files.go
@@ -3,8 +3,10 @@ package service
 import (
 	"bytes"
 	"context"
+	"crypto/rand"
+	"encoding/hex"
 	"fmt"
 	"io"
 	"log/slog"
 	"time"
@@ -15,6 +17,15 @@ import (
 	"github.com/cliffpyles/aibox/internal/rag"
 )
 
 // maxUploadBytes is the maximum allowed file upload size (100MB).
 const maxUploadBytes int64 = 100 * 1024 * 1024
+
+func generateFileID() (string, error) {
+	buf := make([]byte, 16)
+	if _, err := rand.Read(buf); err != nil {
+		return "", err
+	}
+	return "file_" + hex.EncodeToString(buf), nil
+}
@@ -116,6 +127,11 @@ func (s *FileService) UploadFile(stream pb.FileService_UploadFileServer) error {
 
 	buf.Write(chunk)
 	}
 
+	fileID, err := generateFileID()
+	if err != nil {
+		return fmt.Errorf("generate file id: %w", err)
+	}
+
 	// Get tenant ID from auth context
 	tenantID := auth.TenantIDFromContext(ctx)
 
 	// Ingest the file via RAG service
 	result, err := s.ragService.Ingest(ctx, rag.IngestParams{
 		StoreID:  metadata.StoreId,
 		TenantID: tenantID,
+		FileID:   fileID,
 		File:     &buf,
 		Filename: metadata.Filename,
 		MIMEType: metadata.MimeType,
 	})
@@ -150,7 +166,7 @@ func (s *FileService) UploadFile(stream pb.FileService_UploadFileServer) error {
 	)
 
 	return stream.SendAndClose(&pb.UploadFileResponse{
-		FileId:   fmt.Sprintf("%s_%s", metadata.StoreId, metadata.Filename),
+		FileId:   fileID,
 		Filename: metadata.Filename,
 		StoreId:  metadata.StoreId,
 		Status:   "ready",
 	})
 }

diff --git a/internal/rag/service_test.go b/internal/rag/service_test.go
--- a/internal/rag/service_test.go
+++ b/internal/rag/service_test.go
@@ -245,6 +245,7 @@ func TestService_Ingest_PointMetadata(t *testing.T) {
 	_, err := svc.Ingest(ctx, IngestParams{
 		StoreID:  "store1",
 		TenantID: "tenant1",
 		ThreadID: "thread1",
+		FileID:   "file-123",
 		File:     bytes.NewReader([]byte("content")),
 		Filename: "test.pdf",
 		MIMEType: "application/pdf",
 	})
@@ -271,6 +272,9 @@ func TestService_Ingest_PointMetadata(t *testing.T) {
 	if p.Payload["store_id"] != "store1" {
 		t.Errorf("expected store_id=store1, got %v", p.Payload["store_id"])
 	}
+	if p.Payload["file_id"] != "file-123" {
+		t.Errorf("expected file_id=file-123, got %v", p.Payload["file_id"])
+	}
 	if p.Payload["filename"] != "test.pdf" {
 		t.Errorf("expected filename=test.pdf, got %v", p.Payload["filename"])
 	}
 	if _, ok := p.Payload["text"]; !ok {
 		t.Error("expected text in payload")
 	}
```

## Suggested Verification
- go test ./internal/tenant/...
- go test ./internal/rag/...
- go test ./internal/service/...
- go test ./...
