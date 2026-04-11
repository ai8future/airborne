Date Created: 2026-01-20 17:43:42 +0100
TOTAL_SCORE: 78/100

# Airborne Code Audit (time-boxed)

## Scope & method
- Quick scan of gRPC server, admin HTTP server, auth, config, DB persistence, providers, and deployment assets.
- Files sampled: `cmd/airborne/main.go`, `internal/admin/server.go`, `internal/server/grpc.go`, `internal/service/chat.go`, `internal/validation/url.go`, `internal/tenant/secrets.go`, `internal/db/repository.go`, `Dockerfile`, `configs/airborne.yaml`.
- No runtime tests executed (audit only).

## Score rationale
Strengths include SSRF protection for custom provider URLs, secret path validation for FILE= secrets, input size limits, and non-root container execution. Deductions are primarily for the admin HTTP surface (auth/CORS/TLS) and always-on debug payload persistence.

## Findings (ordered by severity)

### High: Admin HTTP endpoints are unauthenticated and CORS-open
Evidence: `internal/admin/server.go:61`, `internal/admin/server.go:77`, `internal/admin/server.go:242`, `internal/admin/server.go:416`.

Impact:
- If `admin.enabled` is true and the port is reachable, any caller can read cross-tenant activity/debug/thread data and trigger `/admin/chat` or `/admin/test` calls.
- With `Access-Control-Allow-Origin: *`, a browser context can call these endpoints from any origin.

Recommendation:
- Require an admin token for HTTP requests (at least for non-health endpoints).
- Add a `ReadHeaderTimeout` to mitigate slowloris.
- Keep CORS but include the custom auth header and treat auth as mandatory.

Patch-ready diff:
```diff
diff --git a/internal/admin/server.go b/internal/admin/server.go
index 36f0f6b..e1a6f94 100644
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -2,6 +2,7 @@
 package admin
 
 import (
 	"context"
+	"crypto/subtle"
 	"encoding/json"
 	"fmt"
 	"log/slog"
 	"net/http"
@@ -19,6 +20,7 @@ import (
 	"github.com/google/uuid"
 	"google.golang.org/grpc"
+	"google.golang.org/grpc/credentials"
 	"google.golang.org/grpc/credentials/insecure"
 	"google.golang.org/grpc/metadata"
 )
@@ -28,6 +30,8 @@ type Server struct {
 	server     *http.Server
 	port       int
 	grpcAddr   string
 	authToken  string
+	tlsEnabled bool
+	tlsCertFile string
 	grpcConn   *grpc.ClientConn
 	grpcClient pb.AirborneServiceClient
 	version    VersionInfo
 }
@@ -41,10 +45,12 @@ type Config struct {
 	Port      int
 	GRPCAddr  string      // Address of the gRPC server (e.g., "localhost:50051")
 	AuthToken string      // Auth token for gRPC calls
 	Version   VersionInfo // Version information
+	TLSEnabled bool
+	TLSCertFile string
 }
@@ -52,10 +58,12 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	s := &Server{
 		dbClient:  dbClient,
 		port:      cfg.Port,
 		grpcAddr:  cfg.GRPCAddr,
 		authToken: cfg.AuthToken,
+		tlsEnabled: cfg.TLSEnabled,
+		tlsCertFile: cfg.TLSCertFile,
 		version:   cfg.Version,
 	}
@@ -61,12 +69,13 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
-	// CORS middleware wrapper
+	// CORS + auth middleware wrapper
 	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
 		return func(w http.ResponseWriter, r *http.Request) {
 			w.Header().Set("Access-Control-Allow-Origin", "*")
 			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
-			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
+			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token")
 
 			if r.Method == "OPTIONS" {
 				w.WriteHeader(http.StatusOK)
 				return
 			}
 
+			if !s.authorize(w, r) {
+				return
+			}
 			h(w, r)
 		}
 	}
@@ -87,6 +96,7 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 		s.server = &http.Server{
 			Addr:         fmt.Sprintf(":%d", cfg.Port),
 			Handler:      mux,
 			ReadTimeout:  10 * time.Second,
+			ReadHeaderTimeout: 5 * time.Second,
 			WriteTimeout: 30 * time.Second,
 			IdleTimeout:  60 * time.Second,
 		}
@@ -95,6 +105,33 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	return s
 }
+
+func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
+	if r.URL.Path == "/admin/health" {
+		return true
+	}
+	if strings.TrimSpace(s.authToken) == "" {
+		http.Error(w, "admin token not configured", http.StatusUnauthorized)
+		return false
+	}
+	token := adminTokenFromRequest(r)
+	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
+		http.Error(w, "unauthorized", http.StatusUnauthorized)
+		return false
+	}
+	return true
+}
+
+func adminTokenFromRequest(r *http.Request) string {
+	auth := strings.TrimSpace(r.Header.Get("Authorization"))
+	if auth != "" {
+		lower := strings.ToLower(auth)
+		if strings.HasPrefix(lower, "bearer ") {
+			return strings.TrimSpace(auth[len("bearer "):])
+		}
+		return auth
+	}
+	return strings.TrimSpace(r.Header.Get("X-Admin-Token"))
+}
@@ -404,9 +441,20 @@ func (s *Server) getGRPCClient() (pb.AirborneServiceClient, error) {
-	conn, err := grpc.NewClient(s.grpcAddr,
-		grpc.WithTransportCredentials(insecure.NewCredentials()),
-	)
+	var creds credentials.TransportCredentials
+	if s.tlsEnabled {
+		if strings.TrimSpace(s.tlsCertFile) == "" {
+			return nil, fmt.Errorf("TLS enabled but tls cert file is empty")
+		}
+		tlsCreds, err := credentials.NewClientTLSFromFile(s.tlsCertFile, "")
+		if err != nil {
+			return nil, fmt.Errorf("failed to load TLS cert: %w", err)
+		}
+		creds = tlsCreds
+	} else {
+		creds = insecure.NewCredentials()
+	}
+	conn, err := grpc.NewClient(s.grpcAddr, grpc.WithTransportCredentials(creds))
 	if err != nil {
 		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
 	}
```
```diff
diff --git a/cmd/airborne/main.go b/cmd/airborne/main.go
index 89f552d..1727a43 100644
--- a/cmd/airborne/main.go
+++ b/cmd/airborne/main.go
@@ -86,6 +86,8 @@ func main() {
 		adminServer = admin.NewServer(components.DBClient, admin.Config{
 			Port:      cfg.Admin.Port,
 			GRPCAddr:  grpcAddr,
 			AuthToken: cfg.Auth.AdminToken,
 			Version: admin.VersionInfo{
 				Version:   Version,
 				GitCommit: GitCommit,
 				BuildTime: BuildTime,
 			},
+			TLSEnabled: cfg.TLS.Enabled,
+			TLSCertFile: cfg.TLS.CertFile,
 		})
 		go func() {
 			if err := adminServer.Start(); err != nil && err != http.ErrServerClosed {
```

### Medium: Admin gRPC client does not honor TLS
Evidence: `internal/admin/server.go:404` (uses `insecure.NewCredentials()` unconditionally).

Impact: If gRPC TLS is enabled, admin calls fail; if the gRPC port is reachable off-host and TLS is off, admin traffic is plaintext and can be intercepted.

Recommendation: Use TLS creds when `cfg.TLS.Enabled` is true (included in patch above).

### Medium: Raw provider request/response payloads persisted by default
Evidence: `internal/service/chat.go:1089`, `internal/provider/openai/client.go:302`, `internal/provider/gemini/client.go:328`, `internal/provider/anthropic/client.go:270`.

Impact: Full prompts, system instructions, rendered HTML, and tool results are stored in DB. This increases privacy/compliance exposure and makes the admin/debug endpoints highly sensitive.

Recommendation: Make debug payload persistence opt-in (or redact) and document retention. Example opt-in via `AIRBORNE_STORE_DEBUG_JSON=true`.

Patch-ready diff:
```diff
diff --git a/internal/service/chat.go b/internal/service/chat.go
index 0e8cfd3..6e2a1c3 100644
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@ -4,6 +4,7 @@ import (
 	"context"
 	"fmt"
 	"html"
 	"log/slog"
+	"os"
 	"strings"
 	"time"
@@ -1086,8 +1087,9 @@ func (s *ChatService) persistConversation(ctx context.Context, req *pb.GenerateRe
 	// Build debug info from captured JSON and rendered HTML (if available)
 	var debugInfo *db.DebugInfo
-	if len(result.RequestJSON) > 0 || len(result.ResponseJSON) > 0 || renderedHTML != "" {
+	storeDebug := strings.EqualFold(os.Getenv("AIRBORNE_STORE_DEBUG_JSON"), "true")
+	if storeDebug && (len(result.RequestJSON) > 0 || len(result.ResponseJSON) > 0 || renderedHTML != "") {
 		debugInfo = &db.DebugInfo{
 			SystemPrompt:    req.Instructions,
 			RawRequestJSON:  string(result.RequestJSON),
 			RawResponseJSON: string(result.ResponseJSON),
 			RenderedHTML:    renderedHTML,
 		}
 	}
```

### Low: Admin HTTP server is cleartext by default
Evidence: `internal/admin/server.go:86`, `configs/airborne.yaml:28`.

Impact: If the admin port is exposed beyond localhost without a TLS-terminating reverse proxy, admin traffic can be intercepted.

Recommendation: Bind to localhost by default or document TLS termination requirements explicitly.

### Low: Rate limiting disabled in static auth mode
Evidence: `internal/server/grpc.go:61` (rate limiter only created in Redis auth mode).

Impact: DoS risk for static token deployments without Redis.

Recommendation: Add optional in-memory rate limiter or allow Redis-backed limits even when static auth is used.

## Positive security notes
- SSRF protections for custom provider base URLs: `internal/service/chat.go:91`, `internal/validation/url.go:52`.
- Secret path validation for FILE= secrets: `internal/tenant/secrets.go:15`.
- Input size caps for user inputs, instructions, metadata: `internal/validation/limits.go:11`.
- Non-root runtime in container image: `Dockerfile:26`.

## Suggested follow-ups
- Decide on an explicit admin access model (token + allowlist + TLS) and document it.
- Establish a retention policy for debug payloads and PII handling.
- Consider enabling TLS by default for gRPC in production deployments.

