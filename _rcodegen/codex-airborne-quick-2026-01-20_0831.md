Date Created: 2026-01-20 08:31:44 +0100
TOTAL_SCORE: 82/100

## AUDIT
1) Admin HTTP endpoints are unauthenticated and the admin gRPC client ignores TLS settings. If the admin port is reachable, this exposes activity, debug payloads, and chat/test actions without auth and allows MITM when TLS is enabled on the gRPC server. Patch enforces a token on all admin HTTP routes, fails fast when the admin server is enabled without a token, and dials gRPC with TLS when configured.

```diff
--- a/cmd/airborne/main.go
+++ b/cmd/airborne/main.go
@@
 	// Start admin HTTP server if enabled
 	var adminServer *admin.Server
 	if cfg.Admin.Enabled {
+		if strings.TrimSpace(cfg.Auth.AdminToken) == "" {
+			slog.Error("admin server enabled but AIRBORNE_ADMIN_TOKEN is empty")
+			os.Exit(1)
+		}
 		// Build gRPC address for the test endpoint
 		grpcHost := cfg.Server.Host
 		if grpcHost == "" || grpcHost == "0.0.0.0" {
 			grpcHost = "127.0.0.1"
@@
 		adminServer = admin.NewServer(components.DBClient, admin.Config{
 			Port:      cfg.Admin.Port,
 			GRPCAddr:  grpcAddr,
 			AuthToken: cfg.Auth.AdminToken,
+			GRPCTLSEnabled: cfg.TLS.Enabled,
+			TLSCertFile:    cfg.TLS.CertFile,
 			Version: admin.VersionInfo{
 				Version:   Version,
 				GitCommit: GitCommit,
 				BuildTime: BuildTime,
 			},
 		})
*** End Patch
```

```diff
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
-	"google.golang.org/grpc"
-	"google.golang.org/grpc/credentials/insecure"
-	"google.golang.org/grpc/metadata"
+	"google.golang.org/grpc"
+	"google.golang.org/grpc/credentials"
+	"google.golang.org/grpc/credentials/insecure"
+	"google.golang.org/grpc/metadata"
 )
@@
 type Server struct {
 	dbClient   *db.Client
 	server     *http.Server
 	port       int
 	grpcAddr   string
 	authToken  string
+	grpcTLS    bool
+	tlsCertFile string
 	grpcConn   *grpc.ClientConn
 	grpcClient pb.AirborneServiceClient
 	version    VersionInfo
 }
@@
 type Config struct {
-	Port      int
-	GRPCAddr  string      // Address of the gRPC server (e.g., "localhost:50051")
-	AuthToken string      // Auth token for gRPC calls
-	Version   VersionInfo // Version information
+	Port           int
+	GRPCAddr       string      // Address of the gRPC server (e.g., "localhost:50051")
+	AuthToken      string      // Auth token for admin HTTP + gRPC calls
+	GRPCTLSEnabled bool        // Use TLS when connecting to gRPC server
+	TLSCertFile    string      // CA or server cert for TLS verification
+	Version        VersionInfo // Version information
 }
@@
 	s := &Server{
 		dbClient:  dbClient,
 		port:      cfg.Port,
 		grpcAddr:  cfg.GRPCAddr,
 		authToken: cfg.AuthToken,
+		grpcTLS:   cfg.GRPCTLSEnabled,
+		tlsCertFile: cfg.TLSCertFile,
 		version:   cfg.Version,
 	}
@@
 		if r.Method == "OPTIONS" {
 			w.WriteHeader(http.StatusOK)
 			return
 		}
+
+		if !s.requireAuth(w, r) {
+			return
+		}
 
 		h(w, r)
 	}
 }
@@
 func (s *Server) Shutdown(ctx context.Context) error {
 	if s.grpcConn != nil {
 		s.grpcConn.Close()
 	}
 	return s.server.Shutdown(ctx)
 }
+
+func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) bool {
+	token := strings.TrimSpace(s.authToken)
+	if token == "" {
+		http.Error(w, "admin token not configured", http.StatusUnauthorized)
+		return false
+	}
+
+	provided := extractAdminToken(r)
+	if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
+		http.Error(w, "unauthorized", http.StatusUnauthorized)
+		return false
+	}
+
+	return true
+}
+
+func extractAdminToken(r *http.Request) string {
+	auth := strings.TrimSpace(r.Header.Get("Authorization"))
+	if auth != "" {
+		lower := strings.ToLower(auth)
+		if strings.HasPrefix(lower, "bearer ") {
+			return strings.TrimSpace(auth[len("bearer "):])
+		}
+		return auth
+	}
+
+	if key := strings.TrimSpace(r.Header.Get("X-Api-Key")); key != "" {
+		return key
+	}
+
+	return ""
+}
@@
 	if s.grpcAddr == "" {
 		return nil, fmt.Errorf("gRPC address not configured")
 	}
-
-	conn, err := grpc.NewClient(s.grpcAddr,
-		grpc.WithTransportCredentials(insecure.NewCredentials()),
-	)
+
+	var dialOpts []grpc.DialOption
+	if s.grpcTLS {
+		if s.tlsCertFile == "" {
+			return nil, fmt.Errorf("grpc TLS enabled but tls_cert_file is empty")
+		}
+		creds, err := credentials.NewClientTLSFromFile(s.tlsCertFile, "")
+		if err != nil {
+			return nil, fmt.Errorf("failed to load gRPC TLS cert: %w", err)
+		}
+		dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
+	} else {
+		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
+	}
+
+	conn, err := grpc.NewClient(s.grpcAddr, dialOpts...)
 	if err != nil {
 		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
 	}
*** End Patch
```

## TESTS
1) Add coverage for the admin HTTP auth gate so unauthenticated requests are rejected and bearer tokens are accepted.

```diff
--- /dev/null
+++ b/internal/admin/server_test.go
@@
+package admin
+
+import (
+	"net/http"
+	"net/http/httptest"
+	"testing"
+)
+
+func TestAdminServer_RequiresAuth(t *testing.T) {
+	srv := NewServer(nil, Config{
+		Port:      0,
+		GRPCAddr:  "127.0.0.1:50051",
+		AuthToken: "secret",
+	})
+
+	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
+	rec := httptest.NewRecorder()
+	
+	srv.server.Handler.ServeHTTP(rec, req)
+	if rec.Code != http.StatusUnauthorized {
+		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
+	}
+}
+
+func TestAdminServer_AllowsBearerToken(t *testing.T) {
+	srv := NewServer(nil, Config{
+		Port:      0,
+		GRPCAddr:  "127.0.0.1:50051",
+		AuthToken: "secret",
+	})
+
+	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
+	req.Header.Set("Authorization", "Bearer secret")
+	rec := httptest.NewRecorder()
+
+	srv.server.Handler.ServeHTTP(rec, req)
+	if rec.Code != http.StatusOK {
+		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
+	}
+}
*** End Patch
```

2) Ensure request IDs generated during request preparation are written back to the gRPC request for downstream persistence and continuity.

```diff
--- a/internal/service/chat_test.go
+++ b/internal/service/chat_test.go
@@
 func TestHasCustomBaseURL_MultipleConfigs(t *testing.T) {
 	req := &pb.GenerateReplyRequest{
 		UserInput: "test",
 		ProviderConfigs: map[string]*pb.ProviderConfig{
 			"openai":  {Model: "gpt-4"},
 			"gemini":  {BaseUrl: "https://custom.gemini.com"},
 			"anthropic": {Model: "claude-3"},
 		},
 	}
 	if !hasCustomBaseURL(req) {
 		t.Error("expected true when any provider has base_url")
 	}
 }
+
+func TestPrepareRequest_GeneratesRequestID(t *testing.T) {
+	svc := createChatServiceWithMocks(
+		newMockProvider("openai"),
+		newMockProvider("gemini"),
+		newMockProvider("anthropic"),
+		nil,
+	)
+
+	req := &pb.GenerateReplyRequest{
+		UserInput: "hello",
+	}
+
+	prepared, err := svc.prepareRequest(context.Background(), req)
+	if err != nil {
+		t.Fatalf("prepareRequest failed: %v", err)
+	}
+	if req.RequestId == "" {
+		t.Fatal("expected RequestId to be populated")
+	}
+	if prepared.requestID != req.RequestId {
+		t.Fatalf("expected prepared requestID %q to match req.RequestId %q", prepared.requestID, req.RequestId)
+	}
+}
*** End Patch
```

## FIXES
1) Request IDs generated in `prepareRequest` are not written back to the request, so persistence falls back to a new UUID and breaks thread continuity. Patch updates the request with the generated ID so downstream logic uses the same value.

```diff
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@
 	// Validate or generate request ID
 	requestID, err := validation.ValidateOrGenerateRequestID(req.RequestId)
 	if err != nil {
 		return nil, status.Error(codes.InvalidArgument, err.Error())
 	}
+	req.RequestId = requestID
*** End Patch
```

## REFACTOR
- Centralize admin JSON responses in helper functions to remove duplicated `json.NewEncoder` boilerplate and `map[string]interface{}` usage.
- Split `internal/admin/server.go` into handler/middleware files to improve readability and reduce merge conflicts.
- Replace `db.ValidTenantIDs` with a config-driven tenant registry or DB-backed source to avoid code edits for tenant onboarding.
- Introduce an admin middleware stack (auth, CORS, request logging) to simplify future endpoints and consistent error handling.
