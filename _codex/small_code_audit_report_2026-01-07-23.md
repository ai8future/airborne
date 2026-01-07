# Small Code Audit Report
Date Created: 2026-01-07 23:03:36 +0100

## Scope
- Go services: config, auth, tenant, service layer, gRPC server, RAG, providers
- Focus: security posture + code quality issues with patch-ready fixes

## Findings

### 1) TLS env overrides are ignored (High)
Impact: Deployments relying on `AIBOX_TLS_*` env vars run without TLS because the config loader does not apply those overrides. This is a silent security misconfiguration.

Recommendation: Apply TLS + Redis DB + log format overrides in `applyEnvOverrides` to honor env-based hardening.

Patch-ready diff:
```diff
diff --git a/internal/config/config.go b/internal/config/config.go
index 0000000..0000000 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@
 	if host := os.Getenv("AIBOX_HOST"); host != "" {
 		c.Server.Host = host
 	}
+
+	if enabled := os.Getenv("AIBOX_TLS_ENABLED"); enabled != "" {
+		if v, err := strconv.ParseBool(enabled); err == nil {
+			c.TLS.Enabled = v
+		}
+	}
+	if cert := os.Getenv("AIBOX_TLS_CERT_FILE"); cert != "" {
+		c.TLS.CertFile = cert
+	}
+	if key := os.Getenv("AIBOX_TLS_KEY_FILE"); key != "" {
+		c.TLS.KeyFile = key
+	}
 
 	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
 		c.Redis.Addr = addr
 	}
 
 	if pass := os.Getenv("REDIS_PASSWORD"); pass != "" {
 		c.Redis.Password = pass
 	}
+
+	if db := os.Getenv("REDIS_DB"); db != "" {
+		if d, err := strconv.Atoi(db); err == nil {
+			c.Redis.DB = d
+		}
+	}
@@
 	if level := os.Getenv("AIBOX_LOG_LEVEL"); level != "" {
 		c.Logging.Level = level
 	}
+
+	if format := os.Getenv("AIBOX_LOG_FORMAT"); format != "" {
+		c.Logging.Format = format
+	}
```

### 2) Tenant interceptor blocks FileService RPCs in multi-tenant mode (Medium)
Impact: FileService requests do not carry `tenant_id`, so the tenant interceptor rejects them before auth can inject tenant context. This breaks file operations and can pressure operators to disable tenant validation.

Recommendation: Skip FileService methods in `TenantInterceptor`. FileService already uses `auth.TenantIDFromContext` for tenant scoping.

Patch-ready diff:
```diff
diff --git a/internal/auth/tenant_interceptor.go b/internal/auth/tenant_interceptor.go
index 0000000..0000000 100644
--- a/internal/auth/tenant_interceptor.go
+++ b/internal/auth/tenant_interceptor.go
@@
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
```

### 3) SelectProvider lacks permission gate (Low)
Impact: Any authenticated API key can call `SelectProvider`, even if it only has file or admin scopes. This is inconsistent with chat endpoints that enforce `PermissionChat`.

Recommendation: Require `PermissionChat` (or admin) for provider selection.

Patch-ready diff:
```diff
diff --git a/internal/service/chat.go b/internal/service/chat.go
index 0000000..0000000 100644
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@
 func (s *ChatService) SelectProvider(ctx context.Context, req *pb.SelectProviderRequest) (*pb.SelectProviderResponse, error) {
+	if err := auth.RequirePermission(ctx, auth.PermissionChat); err != nil {
+		return nil, err
+	}
 	// Check for trigger phrases
 	content := strings.ToLower(req.Content)
```
