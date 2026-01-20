Date Created: 2026-01-20 07:56:32 +0100
TOTAL_SCORE: 62/100

## Scope
- Go backend: gRPC server, auth, tenant loading, admin HTTP server, DB persistence, RAG
- Dashboard API routes (Next.js server routes)
- Config defaults and deployment surface

## Score Rationale
- Strong points: SSRF protections for custom base URLs, rate limiting, secret-loading safeguards, input validation, and safe error sanitization.
- Major risks: unauthenticated admin HTTP endpoints with permissive CORS, plaintext gRPC by default, and tenant isolation fallback in production.

## Findings (ordered by severity)
### Critical
1) Admin HTTP server is unauthenticated and CORS is wildcard
- Evidence: `internal/admin/server.go` uses `Access-Control-Allow-Origin: *` and performs no auth checks on `/admin/*` routes.
- Impact: Anyone who can reach the admin port can read activity, debug payloads (raw request/response), threads, and can issue test/chat calls that consume tokens or expose data. With wildcard CORS, a malicious website can trigger browser-based calls and exfiltrate responses.
- Recommendation: Require an admin token on all non-health endpoints, restrict allowed origins, and avoid binding admin port publicly.

### High
2) TLS disabled and gRPC bound to all interfaces by default
- Evidence: `configs/airborne.yaml` sets `server.host: "0.0.0.0"` and `tls.enabled: false`; `internal/server/grpc.go` only uses TLS when enabled.
- Impact: API keys and traffic can traverse plaintext if the service is exposed outside a trusted network; this is a direct confidentiality risk.
- Recommendation: Enable TLS by default (or enforce TLS in production), and/or bind to localhost when TLS is disabled.

3) Tenant config load failure falls back to legacy mode in production
- Evidence: `internal/server/grpc.go` logs a warning and proceeds with `tenantMgr = nil` when tenant config fails to load.
- Impact: Intended tenant isolation can silently disable, potentially allowing cross-tenant access patterns and misrouted provider configs.
- Recommendation: Fail fast in production if tenant config cannot be loaded.

### Medium
4) Admin HTTP JSON bodies are unbounded
- Evidence: `internal/admin/server.go` decodes request bodies without a size limit in `/admin/test` and `/admin/chat`.
- Impact: Large request bodies can consume memory and degrade service (DoS).
- Recommendation: Use `http.MaxBytesReader` and disallow unknown fields.

5) Debug payload storage contains raw request/response/HTML
- Evidence: `internal/service/chat.go` persists `RawRequestJSON`, `RawResponseJSON`, `RenderedHTML`; `/admin/debug/{id}` returns this data.
- Impact: Potential PII/secrets retention; data could be more sensitive than needed for troubleshooting.
- Recommendation: Add retention/redaction policy (e.g., TTL, encryption at rest, configurable opt-out).

6) FileService skips tenant interceptor
- Evidence: `internal/auth/tenant_interceptor.go` skip list includes all FileService methods; internal RAG uses `TenantIDFromContext` fallback.
- Impact: Tenant scoping can become inconsistent (client ID vs tenant ID); risk of indexing/retrieval misrouting in multi-tenant setups.
- Recommendation: Pass tenant ID explicitly for file APIs or enable tenant interception for these routes.

### Low
7) Go version in `go.mod` and Dockerfile is set to 1.25.x
- Evidence: `go.mod` uses `go 1.25.5` and Dockerfile uses `golang:1.25-alpine`.
- Impact: Build reproducibility and tooling compatibility risk if 1.25.x is unavailable.
- Recommendation: Align to a stable Go version used in your CI/tooling.

## Patch-Ready Diffs
### 1) Require admin token + restrict CORS + cap admin body size
```diff
diff --git a/internal/admin/server.go b/internal/admin/server.go
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@
-import (
-	"context"
-	"encoding/json"
-	"fmt"
-	"log/slog"
-	"net/http"
-	"strconv"
-	"strings"
-	"time"
+import (
+	"context"
+	"crypto/subtle"
+	"encoding/json"
+	"fmt"
+	"log/slog"
+	"net/http"
+	"strconv"
+	"strings"
+	"time"
@@
 type Server struct {
 	dbClient   *db.Client
 	server     *http.Server
 	port       int
 	grpcAddr   string
 	authToken  string
+	allowedOrigins map[string]struct{}
 	grpcConn   *grpc.ClientConn
 	grpcClient pb.AirborneServiceClient
 	version    VersionInfo
 }
@@
 type Config struct {
 	Port      int
 	GRPCAddr  string      // Address of the gRPC server (e.g., "localhost:50051")
 	AuthToken string      // Auth token for gRPC calls
 	Version   VersionInfo // Version information
+	AllowedOrigins []string // Optional CORS allowlist
 }
+
+const maxAdminBodyBytes int64 = 1 << 20
@@
 func NewServer(dbClient *db.Client, cfg Config) *Server {
 	s := &Server{
 		dbClient:  dbClient,
 		port:      cfg.Port,
 		grpcAddr:  cfg.GRPCAddr,
 		authToken: cfg.AuthToken,
 		version:   cfg.Version,
+		allowedOrigins: normalizeAllowedOrigins(cfg.AllowedOrigins),
 	}
@@
-	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
+	corsHandler := func(h http.HandlerFunc, requireAuth bool) http.HandlerFunc {
 		return func(w http.ResponseWriter, r *http.Request) {
-			w.Header().Set("Access-Control-Allow-Origin", "*")
-			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
-			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
+			origin := r.Header.Get("Origin")
+			if origin != "" {
+				if !s.isOriginAllowed(origin) {
+					if r.Method == http.MethodOptions {
+						http.Error(w, "origin not allowed", http.StatusForbidden)
+						return
+					}
+				} else {
+					if s.allowsAnyOrigin() {
+						w.Header().Set("Access-Control-Allow-Origin", "*")
+					} else {
+						w.Header().Set("Access-Control-Allow-Origin", origin)
+						w.Header().Set("Vary", "Origin")
+					}
+					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
+					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
+				}
+			}
 
-			if r.Method == "OPTIONS" {
-				w.WriteHeader(http.StatusOK)
+			if r.Method == http.MethodOptions {
+				w.WriteHeader(http.StatusNoContent)
 				return
 			}
 
+			if requireAuth && !s.authorizeRequest(r) {
+				http.Error(w, "unauthorized", http.StatusUnauthorized)
+				return
+			}
+
 			h(w, r)
 		}
 	}
@@
-	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
-	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
-	mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread))
-	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
-	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion))
-	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
-	mux.HandleFunc("/admin/chat", corsHandler(s.handleChat))
+	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity, true))
+	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug, true))
+	mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread, true))
+	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth, false))
+	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion, true))
+	mux.HandleFunc("/admin/test", corsHandler(s.handleTest, true))
+	mux.HandleFunc("/admin/chat", corsHandler(s.handleChat, true))
@@
 func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
@@
-	var req TestRequest
-	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
+	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBodyBytes)
+	decoder := json.NewDecoder(r.Body)
+	decoder.DisallowUnknownFields()
+	var req TestRequest
+	if err := decoder.Decode(&req); err != nil {
@@
 func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
@@
-	var req ChatRequest
-	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
+	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBodyBytes)
+	decoder := json.NewDecoder(r.Body)
+	decoder.DisallowUnknownFields()
+	var req ChatRequest
+	if err := decoder.Decode(&req); err != nil {
@@
 }
+
+func normalizeAllowedOrigins(origins []string) map[string]struct{} {
+	if len(origins) == 0 {
+		return nil
+	}
+	allowed := make(map[string]struct{}, len(origins))
+	for _, origin := range origins {
+		trimmed := strings.TrimSpace(origin)
+		if trimmed == "" {
+			continue
+		}
+		allowed[trimmed] = struct{}{}
+	}
+	return allowed
+}
+
+func (s *Server) allowsAnyOrigin() bool {
+	if s.allowedOrigins == nil {
+		return false
+	}
+	_, ok := s.allowedOrigins["*"]
+	return ok
+}
+
+func (s *Server) isOriginAllowed(origin string) bool {
+	if s.allowedOrigins == nil {
+		return false
+	}
+	if s.allowsAnyOrigin() {
+		return true
+	}
+	_, ok := s.allowedOrigins[origin]
+	return ok
+}
+
+func (s *Server) authorizeRequest(r *http.Request) bool {
+	if s.authToken == "" {
+		return false
+	}
+	token := extractAuthToken(r)
+	if token == "" {
+		return false
+	}
+	return subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) == 1
+}
+
+func extractAuthToken(r *http.Request) string {
+	auth := strings.TrimSpace(r.Header.Get("Authorization"))
+	if auth != "" {
+		lower := strings.ToLower(auth)
+		if strings.HasPrefix(lower, "bearer ") {
+			return strings.TrimSpace(auth[len("bearer "):])
+		}
+		return auth
+	}
+	return strings.TrimSpace(r.Header.Get("X-API-Key"))
+}
```

```diff
diff --git a/internal/config/config.go b/internal/config/config.go
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@
 type AdminConfig struct {
 	Enabled bool `yaml:"enabled"`
 	Port    int  `yaml:"port"`
+	AllowedOrigins []string `yaml:"allowed_origins"`
 }
@@
 		Admin: AdminConfig{
 			Enabled: false,
-			Port:    50052,
+			Port:    50052,
+			AllowedOrigins: nil,
 		},
@@
 	if port := os.Getenv("ADMIN_PORT"); port != "" {
 		if p, err := strconv.Atoi(port); err == nil {
 			c.Admin.Port = p
 		} else {
 			slog.Warn("invalid ADMIN_PORT, using default", "value", port, "error", err)
 		}
 	}
+	if origins := os.Getenv("ADMIN_ALLOWED_ORIGINS"); origins != "" {
+		c.Admin.AllowedOrigins = splitCommaSeparated(origins)
+	}
@@
 }
+
+func splitCommaSeparated(value string) []string {
+	if value == "" {
+		return nil
+	}
+	parts := strings.Split(value, ",")
+	out := make([]string, 0, len(parts))
+	for _, part := range parts {
+		trimmed := strings.TrimSpace(part)
+		if trimmed != "" {
+			out = append(out, trimmed)
+		}
+	}
+	return out
+}
@@
 func (c *Config) expandEnvVars() {
```

```diff
diff --git a/cmd/airborne/main.go b/cmd/airborne/main.go
--- a/cmd/airborne/main.go
+++ b/cmd/airborne/main.go
@@
 		adminServer = admin.NewServer(components.DBClient, admin.Config{
 			Port:      cfg.Admin.Port,
 			GRPCAddr:  grpcAddr,
 			AuthToken: cfg.Auth.AdminToken,
+			AllowedOrigins: cfg.Admin.AllowedOrigins,
 			Version: admin.VersionInfo{
 				Version:   Version,
 				GitCommit: GitCommit,
 				BuildTime: BuildTime,
 			},
 		})
```

```diff
diff --git a/configs/airborne.yaml b/configs/airborne.yaml
--- a/configs/airborne.yaml
+++ b/configs/airborne.yaml
@@
 admin:
   enabled: false
   port: 8473              # HTTP port for /admin/activity endpoint
+  allowed_origins:
+    - "http://localhost:4848"
```

### 2) Pass admin token from dashboard server routes
```diff
diff --git a/dashboard/src/app/api/activity/route.ts b/dashboard/src/app/api/activity/route.ts
--- a/dashboard/src/app/api/activity/route.ts
+++ b/dashboard/src/app/api/activity/route.ts
@@
-const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_TOKEN = process.env.AIRBORNE_ADMIN_TOKEN;
+
+const adminHeaders: HeadersInit = {
+  "Content-Type": "application/json",
+  ...(AIRBORNE_ADMIN_TOKEN ? { Authorization: `Bearer ${AIRBORNE_ADMIN_TOKEN}` } : {}),
+};
@@
-    const response = await fetch(url, {
-      headers: {
-        "Content-Type": "application/json",
-      },
+    const response = await fetch(url, {
+      headers: adminHeaders,
       // Don't cache the response - we want fresh data every poll
       cache: "no-store",
     });
```

```diff
diff --git a/dashboard/src/app/api/debug/[id]/route.ts b/dashboard/src/app/api/debug/[id]/route.ts
--- a/dashboard/src/app/api/debug/[id]/route.ts
+++ b/dashboard/src/app/api/debug/[id]/route.ts
@@
-const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_TOKEN = process.env.AIRBORNE_ADMIN_TOKEN;
+
+const adminHeaders: HeadersInit = {
+  "Content-Type": "application/json",
+  ...(AIRBORNE_ADMIN_TOKEN ? { Authorization: `Bearer ${AIRBORNE_ADMIN_TOKEN}` } : {}),
+};
@@
-    const response = await fetch(`${AIRBORNE_ADMIN_URL}/admin/debug/${id}`, {
-      headers: {
-        "Content-Type": "application/json",
-      },
+    const response = await fetch(`${AIRBORNE_ADMIN_URL}/admin/debug/${id}`, {
+      headers: adminHeaders,
       cache: "no-store",
     });
```

```diff
diff --git a/dashboard/src/app/api/threads/[threadId]/route.ts b/dashboard/src/app/api/threads/[threadId]/route.ts
--- a/dashboard/src/app/api/threads/[threadId]/route.ts
+++ b/dashboard/src/app/api/threads/[threadId]/route.ts
@@
-const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_TOKEN = process.env.AIRBORNE_ADMIN_TOKEN;
+
+const adminHeaders: HeadersInit = {
+  "Content-Type": "application/json",
+  ...(AIRBORNE_ADMIN_TOKEN ? { Authorization: `Bearer ${AIRBORNE_ADMIN_TOKEN}` } : {}),
+};
@@
-    const response = await fetch(`${AIRBORNE_ADMIN_URL}/admin/thread/${threadId}`, {
-      headers: {
-        "Content-Type": "application/json",
-      },
+    const response = await fetch(`${AIRBORNE_ADMIN_URL}/admin/thread/${threadId}`, {
+      headers: adminHeaders,
       cache: "no-store",
     });
```

```diff
diff --git a/dashboard/src/app/api/chat/route.ts b/dashboard/src/app/api/chat/route.ts
--- a/dashboard/src/app/api/chat/route.ts
+++ b/dashboard/src/app/api/chat/route.ts
@@
-const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_TOKEN = process.env.AIRBORNE_ADMIN_TOKEN;
+
+const adminHeaders: HeadersInit = {
+  "Content-Type": "application/json",
+  ...(AIRBORNE_ADMIN_TOKEN ? { Authorization: `Bearer ${AIRBORNE_ADMIN_TOKEN}` } : {}),
+};
@@
-      const chatResponse = await fetch(`${AIRBORNE_ADMIN_URL}/admin/chat`, {
+      const chatResponse = await fetch(`${AIRBORNE_ADMIN_URL}/admin/chat`, {
         method: "POST",
-        headers: {
-          "Content-Type": "application/json",
-        },
+        headers: adminHeaders,
         body: JSON.stringify({
           thread_id: body.thread_id,
           message: body.message,
           tenant_id: body.tenant_id || "",
           provider: body.provider || "",
           system_prompt: body.system_prompt || "",
         }),
       });
@@
-        const testResponse = await fetch(`${AIRBORNE_ADMIN_URL}/admin/test`, {
+        const testResponse = await fetch(`${AIRBORNE_ADMIN_URL}/admin/test`, {
           method: "POST",
-          headers: {
-            "Content-Type": "application/json",
-          },
+          headers: adminHeaders,
           body: JSON.stringify({
             prompt: body.message,
             tenant_id: body.tenant_id || "",
             provider: body.provider || "gemini",
           }),
         });
```

```diff
diff --git a/dashboard/src/app/api/test/route.ts b/dashboard/src/app/api/test/route.ts
--- a/dashboard/src/app/api/test/route.ts
+++ b/dashboard/src/app/api/test/route.ts
@@
-const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+const AIRBORNE_ADMIN_TOKEN = process.env.AIRBORNE_ADMIN_TOKEN;
+
+const adminHeaders: HeadersInit = {
+  "Content-Type": "application/json",
+  ...(AIRBORNE_ADMIN_TOKEN ? { Authorization: `Bearer ${AIRBORNE_ADMIN_TOKEN}` } : {}),
+};
@@
-    const response = await fetch(`${AIRBORNE_ADMIN_URL}/admin/test`, {
+    const response = await fetch(`${AIRBORNE_ADMIN_URL}/admin/test`, {
       method: "POST",
-      headers: {
-        "Content-Type": "application/json",
-      },
+      headers: adminHeaders,
       body: JSON.stringify({
         prompt: body.prompt,
         tenant_id: body.tenant_id || "",
         provider: body.provider || "gemini",
       }),
     });
```

### 3) Fail fast on tenant load in production
```diff
diff --git a/internal/server/grpc.go b/internal/server/grpc.go
--- a/internal/server/grpc.go
+++ b/internal/server/grpc.go
@@
 	tenantMgr, err := tenant.Load("")
 	if err != nil {
-		slog.Warn("tenant config not loaded - running in single-tenant legacy mode", "error", err)
-		// Create an empty manager for legacy mode
-		tenantMgr = nil
+		if cfg.StartupMode.IsProduction() {
+			return nil, nil, fmt.Errorf("tenant config load failed in production: %w", err)
+		}
+		slog.Warn("tenant config not loaded - running in single-tenant legacy mode", "error", err)
+		// Create an empty manager for legacy mode
+		tenantMgr = nil
 	} else {
```

## Additional Recommendations (no diff)
- Enforce TLS in production or refuse to bind to `0.0.0.0` unless `tls.enabled: true`.
- Implement a retention policy for debug payloads (TTL or opt-in flag) and consider redaction/encryption for stored raw JSON and HTML.
- Review `ValidTenantIDs` in `internal/db/repository.go` to ensure tenant expansion doesn’t silently skip persistence.

## Notes
- No automated dependency vulnerability scans were run (e.g., `govulncheck`, `npm audit`) to keep this quick.
- Admin port exposure should be treated as internal-only even after auth hardening (firewall or private network).
