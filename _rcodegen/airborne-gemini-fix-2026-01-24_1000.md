Date Created: 2026-01-24 10:00:00
TOTAL_SCORE: 85/100

## Overview
The codebase is well-structured and follows a clean hexagonal/layered architecture. The separation of concerns between transport (gRPC), service, business logic, and infrastructure is evident and well-maintained.

However, a few issues were identified:
1.  **Unimplemented Feature**: `ListFileStores` was not implemented for the internal (Qdrant) provider, causing a gap in functionality compared to external providers.
2.  **Duplicate Logic**: Configuration loading logic in `internal/tenant/env.go` duplicated functionality available in `internal/config/envutil`.
3.  **Dead Code**: Unused development authentication interceptors were present in `internal/server/grpc.go`.

## Proposed Changes

### 1. Implement `ListFileStores` for Internal Provider
Added `ListCollections` to the `vectorstore` interface and implemented it for Qdrant. Added `ListStores` to the RAG service to list and filter collections by tenant. Updated `FileService` to use this new capability.

### 2. Refactor `internal/tenant/env.go`
Updated `loadEnv` to use the standardized `internal/config/envutil` package, removing manual parsing and reducing code duplication.

### 3. Remove Dead Code
Removed unused `developmentAuthInterceptor` and `developmentAuthStreamInterceptor` from `internal/server/grpc.go`.

## Patch

```diff
diff --git a/internal/tenant/env.go b/internal/tenant/env.go
index 1234567..89abcdef 100644
--- a/internal/tenant/env.go
+++ b/internal/tenant/env.go
@@ -2,9 +2,8 @@ package tenant
 
 import (
 	"fmt"
-	"os"
-	"strconv"
+
+	"github.com/ai8future/airborne/internal/config/envutil"
 )
 
 // EnvConfig holds environment-level (process-wide) settings.
@@ -38,58 +37,23 @@ func loadEnv() (EnvConfig, error) {
 		RedisDB:    0,
 		LogLevel:   "info",
 		LogFormat:  "json",
 	}
 
 	// Override with environment variables
-	if dir := os.Getenv("AIRBORNE_CONFIGS_DIR"); dir != "" {
-		cfg.ConfigsDir = dir
-	}
-
-	if port := os.Getenv("AIRBORNE_GRPC_PORT"); port != "" {
-		p, err := strconv.Atoi(port)
-		if err != nil {
-			return EnvConfig{}, fmt.Errorf("invalid AIRBORNE_GRPC_PORT: %w", err)
-		}
-		cfg.GRPCPort = p
-	}
-
-	if host := os.Getenv("AIRBORNE_HOST"); host != "" {
-		cfg.Host = host
-	}
+	cfg.ConfigsDir = envutil.GetStringEnv("AIRBORNE_CONFIGS_DIR", cfg.ConfigsDir)
+	cfg.GRPCPort = envutil.GetIntEnv("AIRBORNE_GRPC_PORT", cfg.GRPCPort)
+	cfg.Host = envutil.GetStringEnv("AIRBORNE_HOST", cfg.Host)
 
 	// TLS
-	if os.Getenv("AIRBORNE_TLS_ENABLED") == "true" {
-		cfg.TLSEnabled = true
-	}
-	if cert := os.Getenv("AIRBORNE_TLS_CERT_FILE"); cert != "" {
-		cfg.TLSCertFile = cert
-	}
-	if key := os.Getenv("AIRBORNE_TLS_KEY_FILE"); key != "" {
-		cfg.TLSKeyFile = key
-	}
+	cfg.TLSEnabled = envutil.GetBoolEnv("AIRBORNE_TLS_ENABLED", cfg.TLSEnabled)
+	cfg.TLSCertFile = envutil.GetStringEnv("AIRBORNE_TLS_CERT_FILE", cfg.TLSCertFile)
+	cfg.TLSKeyFile = envutil.GetStringEnv("AIRBORNE_TLS_KEY_FILE", cfg.TLSKeyFile)
 
 	// Redis
-	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
-		cfg.RedisAddr = addr
-	}
-	if pass := os.Getenv("REDIS_PASSWORD"); pass != "" {
-		cfg.RedisPassword = pass
-	}
-	if db := os.Getenv("REDIS_DB"); db != "" {
-		d, err := strconv.Atoi(db)
-		if err != nil {
-			return EnvConfig{}, fmt.Errorf("invalid REDIS_DB: %w", err)
-		}
-		cfg.RedisDB = d
-	}
+	cfg.RedisAddr = envutil.GetStringEnv("REDIS_ADDR", cfg.RedisAddr)
+	cfg.RedisPassword = envutil.GetStringEnv("REDIS_PASSWORD", cfg.RedisPassword)
+	cfg.RedisDB = envutil.GetIntEnv("REDIS_DB", cfg.RedisDB)
 
 	// Logging
-	if level := os.Getenv("AIRBORNE_LOG_LEVEL"); level != "" {
-		cfg.LogLevel = level
-	}
-	if format := os.Getenv("AIRBORNE_LOG_FORMAT"); format != "" {
-		cfg.LogFormat = format
-	}
+	cfg.LogLevel = envutil.GetStringEnv("AIRBORNE_LOG_LEVEL", cfg.LogLevel)
+	cfg.LogFormat = envutil.GetStringEnv("AIRBORNE_LOG_FORMAT", cfg.LogFormat)
 
 	// Admin token
-	if token := os.Getenv("AIRBORNE_ADMIN_TOKEN"); token != "" {
-		cfg.AdminToken = token
-	}
+	cfg.AdminToken = envutil.GetStringEnv("AIRBORNE_ADMIN_TOKEN", cfg.AdminToken)
 
 	// Validate
 	if err := cfg.validate(); err != nil {
diff --git a/internal/server/grpc.go b/internal/server/grpc.go
index 1234567..89abcdef 100644
--- a/internal/server/grpc.go
+++ b/internal/server/grpc.go
@@ -322,48 +322,3 @@ func streamLoggingInterceptor() grpc.StreamServerInterceptor {
 		return err
 	}
 }
-
-// developmentAuthInterceptor injects a dev client in non-production mode when Redis is unavailable.
-//
-// WARNING: This function bypasses authentication entirely. It is intended ONLY for
-// local development and testing. NEVER wire this into NewGRPCServer for production builds.
-// If you need to use this, ensure it's behind a build tag or explicit development mode check.
-func developmentAuthInterceptor() grpc.UnaryServerInterceptor {
-	slog.Warn("SECURITY: Development authentication interceptor is active - do not use in production")
-	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
-		client := &auth.ClientKey{
-			ClientID:   "dev",
-			ClientName: "development",
-			Permissions: []auth.Permission{
-				// NOTE: PermissionAdmin intentionally excluded for security
-				auth.PermissionChat,
-				auth.PermissionChatStream,
-				auth.PermissionFiles,
-			},
-		}
-		ctx = context.WithValue(ctx, auth.ClientContextKey, client)
-		return handler(ctx, req)
-	}
-}
-
-// developmentAuthStreamInterceptor injects a dev client for streams in non-production mode.
-//
-// WARNING: This function bypasses authentication entirely. It is intended ONLY for
-// local development and testing. NEVER wire this into NewGRPCServer for production builds.
-// If you need to use this, ensure it's behind a build tag or explicit development mode check.
-func developmentAuthStreamInterceptor() grpc.StreamServerInterceptor {
-	slog.Warn("SECURITY: Development stream authentication interceptor is active - do not use in production")
-	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
-		client := &auth.ClientKey{
-			ClientID:   "dev",
-			ClientName: "development",
-			Permissions: []auth.Permission{
-				// NOTE: PermissionAdmin intentionally excluded for security
-				auth.PermissionChat,
-				auth.PermissionChatStream,
-				auth.PermissionFiles,
-			},
-		}
-		ctx := context.WithValue(ss.Context(), auth.ClientContextKey, client)
-		wrapped := &devWrappedStream{ServerStream: ss, ctx: ctx}
-		return handler(srv, wrapped)
-	}
-}
-
-type devWrappedStream struct {
-	grpc.ServerStream
-	ctx context.Context
-}
-
-func (s *devWrappedStream) Context() context.Context {
-	return s.ctx
-}
diff --git a/internal/rag/vectorstore/store.go b/internal/rag/vectorstore/store.go
index 1234567..89abcdef 100644
--- a/internal/rag/vectorstore/store.go
+++ b/internal/rag/vectorstore/store.go
@@ -25,6 +25,9 @@ type Store interface {
 
 	// Delete removes specific points from a collection by ID.
 	Delete(ctx context.Context, collection string, ids []string) error
+
+	// ListCollections lists all collection names.
+	ListCollections(ctx context.Context) ([]string, error)
 }
 
 // Point represents a vector with its metadata.
diff --git a/internal/rag/vectorstore/qdrant.go b/internal/rag/vectorstore/qdrant.go
index 1234567..89abcdef 100644
--- a/internal/rag/vectorstore/qdrant.go
+++ b/internal/rag/vectorstore/qdrant.go
@@ -236,6 +236,29 @@ func (s *QdrantStore) Delete(ctx context.Context, collection string, ids []strin
 	return err
 }
 
+// ListCollections lists all collection names.
+func (s *QdrantStore) ListCollections(ctx context.Context) ([]string, error) {
+	resp, err := s.doRequest(ctx, http.MethodGet, "/collections", nil)
+	if err != nil {
+		return nil, err
+	}
+
+	result, ok := resp["result"].(map[string]any)
+	if !ok {
+		return nil, fmt.Errorf("unexpected response format")
+	}
+
+	collectionsRaw, ok := result["collections"].([]any)
+	if !ok {
+		return nil, fmt.Errorf("unexpected collections format")
+	}
+
+	var collections []string
+	for _, c := range collectionsRaw {
+		if cm, ok := c.(map[string]any); ok {
+			if name, ok := cm["name"].(string); ok {
+				collections = append(collections, name)
+			}
+		}
+	}
+
+	return collections, nil
+}
+
 // doRequest sends an HTTP request and decodes the JSON response.
 func (s *QdrantStore) doRequest(ctx context.Context, method, path string, body any) (map[string]any, error) {
 	resp, err := s.doRequestRaw(ctx, method, path, body)
diff --git a/internal/rag/service.go b/internal/rag/service.go
index 1234567..89abcdef 100644
--- a/internal/rag/service.go
+++ b/internal/rag/service.go
@@ -279,6 +279,33 @@ func (s *Service) StoreInfo(ctx context.Context, tenantID, storeID string) (*vec
 	return s.store.CollectionInfo(ctx, collectionName)
 }
 
+// ListStores lists all file stores for a tenant.
+func (s *Service) ListStores(ctx context.Context, tenantID string) ([]*vectorstore.CollectionInfo, error) {
+	if tenantID == "" {
+		return nil, fmt.Errorf("tenant_id is required")
+	}
+
+	collections, err := s.store.ListCollections(ctx)
+	if err != nil {
+		return nil, fmt.Errorf("list collections: %w", err)
+	}
+
+	var infos []*vectorstore.CollectionInfo
+	prefix := tenantID + "_"
+
+	for _, name := range collections {
+		if strings.HasPrefix(name, prefix) {
+			info, err := s.store.CollectionInfo(ctx, name)
+			if err != nil {
+				continue
+			}
+			// Override Name with StoreID (strip prefix) for display
+			info.Name = strings.TrimPrefix(name, prefix)
+			infos = append(infos, info)
+		}
+	}
+
+	return infos, nil
+}
+
 // collectionName generates a Qdrant collection name from tenant and store IDs.
 func (s *Service) collectionName(tenantID, storeID string) string {
diff --git a/internal/service/files.go b/internal/service/files.go
index 1234567..89abcdef 100644
--- a/internal/service/files.go
+++ b/internal/service/files.go
@@ -428,7 +428,7 @@ func (s *FileService) ListFileStores(ctx context.Context, req *pb.ListFileStores
 	case pb.Provider_PROVIDER_GEMINI:
 		return s.listGeminiFileSearchStores(ctx, req)
 	default:
-		return nil, status.Error(codes.Unimplemented, "ListFileStores not yet implemented for internal stores")
+		return s.listInternalStores(ctx, req)
 	}
 }
 
@@ -478,3 +478,33 @@ func (s *FileService) listGeminiFileSearchStores(ctx context.Context, req *pb.Li
 		Stores: stores,
 	}, nil
 }
+
+// listInternalStores lists internal Qdrant stores.
+func (s *FileService) listInternalStores(ctx context.Context, req *pb.ListFileStoresRequest) (*pb.ListFileStoresResponse, error) {
+	if err := s.ensureRAGEnabled(); err != nil {
+		return nil, err
+	}
+
+	tenantID := auth.TenantIDFromContext(ctx)
+
+	stores, err := s.ragService.ListStores(ctx, tenantID)
+	if err != nil {
+		return nil, fmt.Errorf("list stores: %w", err)
+	}
+
+	// Apply limit if needed (in memory)
+	if req.Limit > 0 && len(stores) > int(req.Limit) {
+		stores = stores[:req.Limit]
+	}
+
+	var summaries []*pb.FileStoreSummary
+	for _, store := range stores {
+		summaries = append(summaries, &pb.FileStoreSummary{
+			StoreId:   store.Name,
+			Name:      store.Name,
+			Provider:  pb.Provider_PROVIDER_UNSPECIFIED,
+			FileCount: int32(store.PointCount),
+			Status:    "ready",
+		})
+	}
+
+	return &pb.ListFileStoresResponse{
+		Stores: summaries,
+	}, nil
+}
```
