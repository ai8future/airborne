Date Created: 2026-01-18 21:52:48 +0100
TOTAL_SCORE: 82/100

# AUDIT - Security and code quality issues with PATCH-READY DIFFS
- Admin HTTP endpoints are unauthenticated and accept any origin; /admin/debug exposes raw request/response payloads and /admin/test accepts unbounded JSON input. Patch adds an auth gate, loopback-only fallback when no token is configured, no-store caching on debug responses, request size limits with strict decoding, and rejects unsupported providers.

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
+	"encoding/json"
+	"errors"
+	"fmt"
+	"log/slog"
+	"net"
+	"net/http"
+	"strconv"
+	"strings"
+	"time"
@@
 )
+
+const maxTestBodyBytes = 1 << 20 // 1MB
@@
-	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
-	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
-	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
-	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
+	mux.HandleFunc("/admin/activity", corsHandler(s.withAuth(s.handleActivity)))
+	mux.HandleFunc("/admin/debug/", corsHandler(s.withAuth(s.handleDebug)))
+	mux.HandleFunc("/admin/health", corsHandler(s.withAuth(s.handleHealth)))
+	mux.HandleFunc("/admin/test", corsHandler(s.withAuth(s.handleTest)))
@@
 }
+
+func (s *Server) withAuth(h http.HandlerFunc) http.HandlerFunc {
+	return func(w http.ResponseWriter, r *http.Request) {
+		if s.authToken == "" {
+			if !isLocalRequest(r.RemoteAddr) {
+				http.Error(w, "unauthorized", http.StatusUnauthorized)
+				return
+			}
+			h(w, r)
+			return
+		}
+
+		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
+		if authHeader == "" {
+			http.Error(w, "unauthorized", http.StatusUnauthorized)
+			return
+		}
+
+		lower := strings.ToLower(authHeader)
+		if !strings.HasPrefix(lower, "bearer ") {
+			http.Error(w, "unauthorized", http.StatusUnauthorized)
+			return
+		}
+
+		token := strings.TrimSpace(authHeader[len("bearer "):])
+		if token == "" || token != s.authToken {
+			http.Error(w, "unauthorized", http.StatusUnauthorized)
+			return
+		}
+
+		h(w, r)
+	}
+}
+
+func isLocalRequest(remoteAddr string) bool {
+	if remoteAddr == "" {
+		return false
+	}
+
+	host := remoteAddr
+	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
+		host = h
+	}
+
+	if strings.EqualFold(host, "localhost") {
+		return true
+	}
+
+	ip := net.ParseIP(host)
+	if ip == nil {
+		return false
+	}
+
+	return ip.IsLoopback()
+}
@@
 func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
 	if r.Method != http.MethodGet {
 		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
 		return
 	}
+
+	w.Header().Set("Cache-Control", "no-store")
@@
 func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
 	if r.Method != http.MethodPost {
 		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
 		return
 	}
 
 	// Parse request body
 	var req TestRequest
-	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
+	r.Body = http.MaxBytesReader(w, r.Body, maxTestBodyBytes)
+	decoder := json.NewDecoder(r.Body)
+	decoder.DisallowUnknownFields()
+	if err := decoder.Decode(&req); err != nil {
+		status := http.StatusBadRequest
+		var maxErr *http.MaxBytesError
+		if errors.As(err, &maxErr) {
+			status = http.StatusRequestEntityTooLarge
+		}
 		w.Header().Set("Content-Type", "application/json")
-		w.WriteHeader(http.StatusBadRequest)
+		w.WriteHeader(status)
 		json.NewEncoder(w).Encode(TestResponse{
 			Error: "invalid request body: " + err.Error(),
 		})
 		return
 	}
@@
 	switch strings.ToLower(req.Provider) {
 	case "gemini", "":
 		grpcReq.PreferredProvider = pb.Provider_PROVIDER_GEMINI
 	case "openai":
 		grpcReq.PreferredProvider = pb.Provider_PROVIDER_OPENAI
 	case "anthropic":
 		grpcReq.PreferredProvider = pb.Provider_PROVIDER_ANTHROPIC
+	default:
+		w.Header().Set("Content-Type", "application/json")
+		w.WriteHeader(http.StatusBadRequest)
+		json.NewEncoder(w).Encode(TestResponse{
+			Error: "unsupported provider",
+		})
+		return
 	}
```

# TESTS - Proposed unit tests for untested code with PATCH-READY DIFFS
- Add auth gate coverage for the admin HTTP server.

```diff
diff --git a/internal/admin/server_test.go b/internal/admin/server_test.go
new file mode 100644
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
+func TestServer_WithAuthToken(t *testing.T) {
+	srv := NewServer(nil, Config{
+		Port:      0,
+		AuthToken: "test-token",
+	})
+
+	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
+	req.RemoteAddr = "127.0.0.1:1234"
+	rec := httptest.NewRecorder()
+	srv.server.Handler.ServeHTTP(rec, req)
+	if rec.Code != http.StatusUnauthorized {
+		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
+	}
+
+	req = httptest.NewRequest(http.MethodGet, "/admin/health", nil)
+	req.RemoteAddr = "127.0.0.1:1234"
+	req.Header.Set("Authorization", "Bearer test-token")
+	rec = httptest.NewRecorder()
+	srv.server.Handler.ServeHTTP(rec, req)
+	if rec.Code != http.StatusOK {
+		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
+	}
+}
```

- Add coverage for large payload capture behavior in httpcapture (bounded capture, full payload passthrough).

```diff
diff --git a/internal/httpcapture/transport_test.go b/internal/httpcapture/transport_test.go
--- a/internal/httpcapture/transport_test.go
+++ b/internal/httpcapture/transport_test.go
@@
 func TestTransport_Client(t *testing.T) {
 	tr := New()
 	client := tr.Client()
 	if client.Transport != tr {
 		t.Error("client transport mismatch")
 	}
 }
+
+func TestTransport_RoundTrip_TruncatesCapture(t *testing.T) {
+	payload := bytes.Repeat([]byte("a"), maxCaptureBytes+32)
+
+	mock := &mockTransport{
+		roundTripFunc: func(req *http.Request) (*http.Response, error) {
+			body, err := io.ReadAll(req.Body)
+			if err != nil {
+				return nil, err
+			}
+			if len(body) != len(payload) {
+				t.Errorf("expected full request body length %d, got %d", len(payload), len(body))
+			}
+			req.Body.Close()
+
+			return &http.Response{
+				StatusCode: http.StatusOK,
+				Body:       io.NopCloser(bytes.NewReader(payload)),
+			}, nil
+		},
+	}
+
+	tr := New()
+	tr.Base = mock
+
+	req, err := http.NewRequest("POST", "http://example.com", bytes.NewReader(payload))
+	if err != nil {
+		t.Fatalf("failed to create request: %v", err)
+	}
+
+	resp, err := tr.RoundTrip(req)
+	if err != nil {
+		t.Fatalf("RoundTrip failed: %v", err)
+	}
+	defer resp.Body.Close()
+
+	if len(tr.RequestBody) != maxCaptureBytes {
+		t.Errorf("expected truncated request capture length %d, got %d", maxCaptureBytes, len(tr.RequestBody))
+	}
+	if len(tr.ResponseBody) != maxCaptureBytes {
+		t.Errorf("expected truncated response capture length %d, got %d", maxCaptureBytes, len(tr.ResponseBody))
+	}
+
+	body, err := io.ReadAll(resp.Body)
+	if err != nil {
+		t.Fatalf("failed to read response body: %v", err)
+	}
+	if len(body) != len(payload) {
+		t.Errorf("expected full response body length %d, got %d", len(payload), len(body))
+	}
+}
```

# FIXES - Bugs, issues, and code smells with fixes and PATCH-READY DIFFS
- httpcapture currently buffers full request/response bodies and assumes Base is non-nil. Patch captures only a bounded prefix while preserving full payloads for callers and safely defaults Base when unset.

```diff
diff --git a/internal/httpcapture/transport.go b/internal/httpcapture/transport.go
--- a/internal/httpcapture/transport.go
+++ b/internal/httpcapture/transport.go
@@
 import (
 	"bytes"
 	"io"
 	"net/http"
 )
+
+const maxCaptureBytes = 1 << 20
+
+func captureBody(body io.ReadCloser) ([]byte, io.ReadCloser, error) {
+	if body == nil {
+		return nil, nil, nil
+	}
+	captured, err := io.ReadAll(io.LimitReader(body, maxCaptureBytes))
+	if err != nil {
+		return nil, body, err
+	}
+	restored := io.NopCloser(io.MultiReader(bytes.NewReader(captured), body))
+	return captured, restored, nil
+}
@@
 func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
+	base := t.Base
+	if base == nil {
+		base = http.DefaultTransport
+	}
 	// Capture request body if present
 	if req.Body != nil {
-		body, err := io.ReadAll(req.Body)
-		if err != nil {
-			return nil, err
-		}
-		t.RequestBody = body
-		// Restore the body so the SDK can read it
-		req.Body = io.NopCloser(bytes.NewReader(body))
+		body, restored, err := captureBody(req.Body)
+		if err != nil {
+			return nil, err
+		}
+		t.RequestBody = body
+		// Restore the body so the SDK can read it
+		req.Body = restored
 	}
 
 	// Make the actual request
-	resp, err := t.Base.RoundTrip(req)
+	resp, err := base.RoundTrip(req)
 	if err != nil {
 		return nil, err
 	}
@@
 	if resp.Body != nil {
-		body, err := io.ReadAll(resp.Body)
-		if err != nil {
-			resp.Body.Close()
-			return nil, err
-		}
-		t.ResponseBody = body
-		// Restore the body so the SDK can read it
-		resp.Body = io.NopCloser(bytes.NewReader(body))
+		body, restored, err := captureBody(resp.Body)
+		if err != nil {
+			resp.Body.Close()
+			return nil, err
+		}
+		t.ResponseBody = body
+		// Restore the body so the SDK can read it
+		resp.Body = restored
 	}
```

- Repository row iteration errors are dropped, which can mask partial reads. Patch adds rows.Err handling for message and activity queries.

```diff
diff --git a/internal/db/repository.go b/internal/db/repository.go
--- a/internal/db/repository.go
+++ b/internal/db/repository.go
@@
 	for rows.Next() {
 		var msg Message
 		err := rows.Scan(
 			&msg.ID,
 			&msg.ThreadID,
@@
 		}
 		messages = append(messages, msg)
 	}
+	if err := rows.Err(); err != nil {
+		return nil, fmt.Errorf("failed to iterate messages: %w", err)
+	}
 	return messages, nil
 }
@@
 	for rows.Next() {
 		var entry ActivityEntry
 		err := rows.Scan(
 			&entry.ID,
@@
 		}
 		entries = append(entries, entry)
 	}
+	if err := rows.Err(); err != nil {
+		return nil, fmt.Errorf("failed to iterate activity feed: %w", err)
+	}
 	return entries, nil
 }
@@
 	for rows.Next() {
 		var entry ActivityEntry
 		err := rows.Scan(
 			&entry.ID,
@@
 		}
 		entries = append(entries, entry)
 	}
+	if err := rows.Err(); err != nil {
+		return nil, fmt.Errorf("failed to iterate activity feed by tenant: %w", err)
+	}
 	return entries, nil
 }
```

# REFACTOR - Opportunities to improve code quality (no diffs needed)
- Extract a shared JSON response helper for admin handlers to centralize error formatting and headers.
- Add an admin config struct for CORS allowlist and request size to avoid hardcoded values.
- Consolidate repeated activity-feed scanning logic into a helper to reduce duplication.
- Consider exposing httpcapture truncation metadata (captured vs total size) for clearer debug diagnostics.
