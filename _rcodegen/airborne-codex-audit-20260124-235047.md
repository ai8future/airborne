Date Created: 2026-01-24 23:50:44 CET
TOTAL_SCORE: 72/100

# Airborne Code Audit (Condensed)

## Scope
- Quick scan of Go backend, admin HTTP server, and Next.js dashboard API routes.
- Focused on auth boundaries, network exposure, and SSRF/unsafe URL handling.

## Findings (ordered by severity)

1) CRITICAL: Admin HTTP server exposes privileged endpoints without authentication
- Evidence: `internal/admin/server.go` registers `/admin/*` handlers with no auth checks; CORS is wide-open and allows cross-origin calls.
- Impact: Anyone with network access can read activity/debug/thread data across tenants, trigger chat/test/upload, and consume provider quota/API keys by supplying `tenant_id`.
- Notes: Admin HTTP server binds to all interfaces (`Addr: :port`) when enabled, so exposure depends on infra/port mapping.

2) HIGH: Dashboard API routes proxy admin endpoints without auth or session checks
- Evidence: `dashboard/src/app/api/*.ts` forwards to `/admin/*` without authorization headers or access control.
- Impact: If the dashboard is deployed publicly (or the API routes are exposed), any caller can access admin data/actions.
- Recommendation: Require a server-side admin token and add app-level auth (session/JWT) for dashboard access.

3) MEDIUM: Admin HTTP server always uses insecure gRPC transport
- Evidence: `internal/admin/server.go` uses `grpc.WithTransportCredentials(insecure.NewCredentials())` unconditionally.
- Impact: Admin token and request data travel in cleartext if gRPC is not strictly localhost or a TLS-terminating mesh; also breaks when gRPC TLS is enabled.
- Recommendation: Use TLS credentials when gRPC TLS is enabled, with configurable CA cert path.

4) LOW: RAG internal service base URLs are not validated
- Evidence: `internal/rag/embedder/ollama.go`, `internal/rag/vectorstore/qdrant.go` accept BaseURL without validation.
- Impact: If config is user-controlled (multi-tenant or injected), can be used for SSRF to internal services.
- Recommendation: Reuse `validation.ValidateProviderURL` or allowlist internal hosts explicitly.

## Patch-ready diffs

### 1) Require admin token for HTTP admin endpoints
```diff
diff --git a/internal/admin/server.go b/internal/admin/server.go
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@
-import (
-    "bytes"
-    "context"
-    "encoding/json"
-    "fmt"
-    "io"
-    "log/slog"
-    "mime/multipart"
-    "net/http"
-    "strconv"
-    "strings"
-    "time"
+import (
+    "bytes"
+    "context"
+    "crypto/subtle"
+    "encoding/json"
+    "fmt"
+    "io"
+    "log/slog"
+    "mime/multipart"
+    "net/http"
+    "strconv"
+    "strings"
+    "time"
@@
-    // CORS middleware wrapper
-    corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
+    // CORS middleware wrapper
+    corsHandler := func(h http.HandlerFunc, requireAuth bool) http.HandlerFunc {
         return func(w http.ResponseWriter, r *http.Request) {
             w.Header().Set("Access-Control-Allow-Origin", "*")
             w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
-            w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
+            w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token")
 
             if r.Method == "OPTIONS" {
                 w.WriteHeader(http.StatusOK)
                 return
             }
+
+            if requireAuth && !s.requireAdminAuth(w, r) {
+                return
+            }
 
             h(w, r)
         }
     }
@@
-    mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
-    mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
-    mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread))
-    mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
-    mux.HandleFunc("/admin/version", corsHandler(s.handleVersion))
-    mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
-    mux.HandleFunc("/admin/chat", corsHandler(s.handleChat))
-    mux.HandleFunc("/admin/upload", corsHandler(s.handleUpload))
+    mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity, true))
+    mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug, true))
+    mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread, true))
+    mux.HandleFunc("/admin/health", corsHandler(s.handleHealth, false))
+    mux.HandleFunc("/admin/version", corsHandler(s.handleVersion, true))
+    mux.HandleFunc("/admin/test", corsHandler(s.handleTest, true))
+    mux.HandleFunc("/admin/chat", corsHandler(s.handleChat, true))
+    mux.HandleFunc("/admin/upload", corsHandler(s.handleUpload, true))
@@
 func (s *Server) Shutdown(ctx context.Context) error {
     if s.grpcConn != nil {
         s.grpcConn.Close()
     }
     return s.server.Shutdown(ctx)
 }
+
+func (s *Server) requireAdminAuth(w http.ResponseWriter, r *http.Request) bool {
+    if s.authToken == "" {
+        http.Error(w, "admin token not configured", http.StatusServiceUnavailable)
+        return false
+    }
+
+    token := extractAdminToken(r)
+    if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
+        http.Error(w, "unauthorized", http.StatusUnauthorized)
+        return false
+    }
+    return true
+}
+
+func extractAdminToken(r *http.Request) string {
+    auth := strings.TrimSpace(r.Header.Get("Authorization"))
+    if auth != "" {
+        lower := strings.ToLower(auth)
+        if strings.HasPrefix(lower, "bearer ") {
+            return strings.TrimSpace(auth[len("bearer "):])
+        }
+        return auth
+    }
+    return strings.TrimSpace(r.Header.Get("X-Admin-Token"))
+}
```

### 2) Add admin token header to dashboard API routes
```diff
diff --git a/dashboard/src/lib/admin.ts b/dashboard/src/lib/admin.ts
new file mode 100644
--- /dev/null
+++ b/dashboard/src/lib/admin.ts
@@
+export const AIRBORNE_ADMIN_URL =
+  process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+
+export function adminURL(path: string): string {
+  if (!path.startsWith("/")) {
+    return `${AIRBORNE_ADMIN_URL}/${path}`;
+  }
+  return `${AIRBORNE_ADMIN_URL}${path}`;
+}
+
+export function adminHeaders(extra?: HeadersInit): HeadersInit {
+  const headers = new Headers(extra);
+  const token = process.env.AIRBORNE_ADMIN_TOKEN;
+  if (token) {
+    headers.set("Authorization", `Bearer ${token}`);
+  }
+  return headers;
+}
+
Diff highlights for dashboard API routes:
- Use `adminURL("/admin/..." )` instead of direct `AIRBORNE_ADMIN_URL`.
- Add `headers: adminHeaders(...)` to all admin fetches.

Example (apply pattern to all `dashboard/src/app/api/*/route.ts` files):

@@
-import { NextRequest, NextResponse } from "next/server";
-
-const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
+import { NextRequest, NextResponse } from "next/server";
+import { adminHeaders, adminURL } from "@/lib/admin";
@@
-    let url = `${AIRBORNE_ADMIN_URL}/admin/activity?limit=${limit}`;
+    let url = adminURL(`/admin/activity?limit=${limit}`);
@@
-    const response = await fetch(url, {
-      headers: {
-        "Content-Type": "application/json",
-      },
-      cache: "no-store",
-    });
+    const response = await fetch(url, {
+      headers: adminHeaders({
+        "Content-Type": "application/json",
+      }),
+      cache: "no-store",
+    });
```

## Additional recommendations (no diff)
- Bind admin HTTP server to loopback by default (or add explicit `admin.host`) to avoid accidental exposure.
- Add TLS support for admin HTTP server or run behind a TLS-terminating proxy.
- Add request body size limits for admin JSON endpoints to reduce DoS potential.

## Suggested tests
- Manual: `curl /admin/health` should be 200; `/admin/activity` should return 401 without token and 200 with token.
- Dashboard: API routes succeed when `AIRBORNE_ADMIN_TOKEN` is set in the server environment.
