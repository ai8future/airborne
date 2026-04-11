Date Created: 2026-01-20 18:08:26 +0100
TOTAL_SCORE: 84/100

# AUDIT
- Gemini FileSearchStore requests use `http.DefaultClient` without a timeout, which can hang indefinitely if upstream stalls, impacting availability and tying up goroutines.

Patch-ready diff:
```diff
diff --git a/internal/provider/gemini/filestore.go b/internal/provider/gemini/filestore.go
index f61eaa4..f2c5a3d 100644
--- a/internal/provider/gemini/filestore.go
+++ b/internal/provider/gemini/filestore.go
@@
 const (
 	fileSearchBaseURL         = "https://generativelanguage.googleapis.com/v1beta"
 	fileSearchPollingInterval = 2 * time.Second
 	fileSearchPollingTimeout  = 5 * time.Minute
+	fileSearchHTTPTimeout     = 30 * time.Second
 )
+
+var fileSearchHTTPClient = &http.Client{Timeout: fileSearchHTTPTimeout}
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
@@
-		resp2, err := http.DefaultClient.Do(req2)
+		resp2, err := fileSearchHTTPClient.Do(req2)
@@
-			resp, err := http.DefaultClient.Do(req)
+			resp, err := fileSearchHTTPClient.Do(req)
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
```

# TESTS
- Provider clients like OpenRouter lack unit coverage for basic capability wiring; add a minimal test for default configuration and feature flags.

Patch-ready diff:
```diff
diff --git a/internal/provider/openrouter/client_test.go b/internal/provider/openrouter/client_test.go
new file mode 100644
index 0000000..bcbab7f
--- /dev/null
+++ b/internal/provider/openrouter/client_test.go
@@
+package openrouter
+
+import "testing"
+
+func TestNewClient_DefaultConfig(t *testing.T) {
+	client := NewClient()
+	if client == nil {
+		t.Fatal("expected client")
+	}
+	if client.Name() != "openrouter" {
+		t.Fatalf("Name() = %q, want openrouter", client.Name())
+	}
+	if client.SupportsFileSearch() {
+		t.Fatal("SupportsFileSearch() = true, want false")
+	}
+	if client.SupportsWebSearch() {
+		t.Fatal("SupportsWebSearch() = true, want false")
+	}
+	if !client.SupportsStreaming() {
+		t.Fatal("SupportsStreaming() = false, want true")
+	}
+	if client.SupportsNativeContinuity() {
+		t.Fatal("SupportsNativeContinuity() = true, want false")
+	}
+}
```

# FIXES
- Qdrant search silently returns `(nil, nil)` on unexpected response shape, hiding upstream errors; return a descriptive error instead.

Patch-ready diff:
```diff
diff --git a/internal/rag/vectorstore/qdrant.go b/internal/rag/vectorstore/qdrant.go
index 8b0d9f9..b8a3f82 100644
--- a/internal/rag/vectorstore/qdrant.go
+++ b/internal/rag/vectorstore/qdrant.go
@@
 	resultsRaw, ok := resp["result"].([]any)
 	if !ok {
-		return nil, nil
+		return nil, fmt.Errorf("unexpected search response format")
 	}
```

# REFACTOR
- Consolidate the boilerplate in provider client constructors (e.g., OpenRouter/Fireworks/Cohere) into a small helper to reduce duplication and config drift.
- Consider extracting shared HTTP request helpers in `internal/provider/gemini/filestore.go` to centralize status handling and JSON decoding.
- Evaluate adding optional capture size limits in `internal/httpcapture` to avoid retaining large request/response bodies in memory.
- Review places using `io.ReadAll` for large payloads and document size limits near call sites for clarity.
