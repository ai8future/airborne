Date Created: 2026-01-15 12:00:00

# Unit Test Analysis & Proposal

## Analysis
The codebase contains several critical components that currently lack comprehensive unit tests. Specifically:
1.  **`internal/provider/compat`**: This package powers multiple AI providers (Mistral, Deepseek, etc.) via OpenAI compatibility but has no tests itself. This is a high-risk gap.
2.  **`internal/httpcapture`**: The debugging transport layer is untested.
3.  **`internal/redis`**: The Redis client wrapper lacks tests. Since `miniredis` is available in `go.mod`, we can easily add high-fidelity unit tests.

## Proposed Tests

### 1. OpenAI Compatibility Layer Tests (`internal/provider/compat`)
Tests the `buildMessages` logic, error handling retry classification, and response extraction helpers.

### 2. HTTP Capture Tests (`internal/httpcapture`)
Verifies that the transport correctly captures request and response bodies without interfering with the data stream.

### 3. Redis Client Tests (`internal/redis`)
Uses `miniredis` to verify all Redis wrapper methods (Get, Set, Incr, etc.) work as expected without needing a live Redis server.

## Patch-Ready Diffs

```diff
diff --git a/internal/httpcapture/transport_test.go b/internal/httpcapture/transport_test.go
new file mode 100644
index 0000000..e69de29
--- /dev/null
+++ b/internal/httpcapture/transport_test.go
@@ -0,0 +1,78 @@
+package httpcapture
+
+import (
+	"bytes"
+	"io"
+	"net/http"
+	"testing"
+)
+
+type mockTransport struct {
+	roundTripFunc func(*http.Request) (*http.Response, error)
+}
+
+func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
+	return m.roundTripFunc(req)
+}
+
+func TestTransport_RoundTrip(t *testing.T) {
+	t.Run("captures request and response bodies", func(t *testing.T) {
+		reqBodyContent := []byte("request payload")
+		respBodyContent := []byte("response payload")
+
+		mockBase := &mockTransport{
+			roundTripFunc: func(req *http.Request) (*http.Response, error) {
+				// Verify request body can be read
+				sentBody, err := io.ReadAll(req.Body)
+				if err != nil {
+					t.Fatalf("failed to read sent body: %v", err)
+				}
+				if !bytes.Equal(sentBody, reqBodyContent) {
+					t.Errorf("got sent body %q, want %q", sentBody, reqBodyContent)
+				}
+
+				return &http.Response{
+					StatusCode: 200,
+					Body:       io.NopCloser(bytes.NewReader(respBodyContent)),
+				}, nil
+			},
+		}
+
+		tr := New()
+		tr.Base = mockBase
+
+		req, _ := http.NewRequest("POST", "http://example.com", bytes.NewReader(reqBodyContent))
+		client := &http.Client{Transport: tr}
+
+		resp, err := client.Do(req)
+		if err != nil {
+			t.Fatalf("client.Do failed: %v", err)
+		}
+		defer resp.Body.Close()
+
+		// Verify RequestBody was captured
+		if !bytes.Equal(tr.RequestBody, reqBodyContent) {
+			t.Errorf("captured request body %q, want %q", tr.RequestBody, reqBodyContent)
+		}
+
+		// Verify ResponseBody was captured
+		if !bytes.Equal(tr.ResponseBody, respBodyContent) {
+			t.Errorf("captured response body %q, want %q", tr.ResponseBody, respBodyContent)
+		}
+
+		// Verify response body can still be read by caller
+		receivedBody, err := io.ReadAll(resp.Body)
+		if err != nil {
+			t.Fatalf("failed to read response body: %v", err)
+		}
+		if !bytes.Equal(receivedBody, respBodyContent) {
+			t.Errorf("caller read body %q, want %q", receivedBody, respBodyContent)
+		}
+	})
+
+	t.Run("handles nil bodies", func(t *testing.T) {
+		mockBase := &mockTransport{
+			roundTripFunc: func(req *http.Request) (*http.Response, error) {
+				return &http.Response{
+					StatusCode: 200,
+					Body:       nil,
+				}, nil
+			},
+		}
+
+		tr := New()
+		tr.Base = mockBase
+
+		req, _ := http.NewRequest("GET", "http://example.com", nil)
+		_, err := tr.RoundTrip(req)
+		if err != nil {
+			t.Fatalf("RoundTrip failed: %v", err)
+		}
+
+		if len(tr.RequestBody) != 0 {
+			t.Error("expected empty captured request body")
+		}
+		if len(tr.ResponseBody) != 0 {
+			t.Error("expected empty captured response body")
+		}
+	})
+}
diff --git a/internal/provider/compat/openai_compat_test.go b/internal/provider/compat/openai_compat_test.go
new file mode 100644
index 0000000..e69de29
--- /dev/null
+++ b/internal/provider/compat/openai_compat_test.go
@@ -0,0 +1,130 @@
+package compat
+
+import (
+	"errors"
+	"testing"
+
+	"github.com/ai8future/airborne/internal/provider"
+	"github.com/openai/openai-go"
+)
+
+func TestBuildMessages(t *testing.T) {
+	tests := []struct {
+		name         string
+		instructions string
+		input        string
+		history      []provider.Message
+		wantCount    int
+	}{
+		{
+			name:         "input only",
+			instructions: "",
+			input:        "hello",
+			history:      nil,
+			wantCount:    1,
+		},
+		{
+			name:         "instructions and input",
+			instructions: "be nice",
+			input:        "hello",
+			history:      nil,
+			wantCount:    2,
+		},
+		{
+			name:         "full history",
+			instructions: "sys",
+			input:        "new input",
+			history: []provider.Message{
+				{Role: "user", Content: "u1"},
+				{Role: "assistant", Content: "a1"},
+			},
+			wantCount: 4, // sys + u1 + a1 + new
+		},
+		{
+			name:         "skips empty history",
+			instructions: "",
+			input:        "hi",
+			history: []provider.Message{
+				{Role: "user", Content: ""},
+				{Role: "assistant", Content: "  "},
+			},
+			wantCount: 1, // just input
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := buildMessages(tt.instructions, tt.input, tt.history)
+			if len(got) != tt.wantCount {
+				t.Errorf("buildMessages() count = %d, want %d", len(got), tt.wantCount)
+			}
+		})
+	}
+}
+
+func TestIsRetryableError(t *testing.T) {
+	tests := []struct {
+		name string
+		err  error
+		want bool
+	}{
+		{"nil", nil, false},
+		{"429", errors.New("429 Too Many Requests"), true},
+		{"500", errors.New("500 Internal Server Error"), true},
+		{"503", errors.New("503 Service Unavailable"), true},
+		{"timeout", errors.New("context deadline exceeded"), false}, // Context errors handled separately in loop
+		{"connection reset", errors.New("connection reset by peer"), true},
+		{"401", errors.New("401 Unauthorized"), false},
+		{"400", errors.New("400 Bad Request"), false},
+		{"invalid json", errors.New("invalid json"), false},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := isRetryableError(tt.err); got != tt.want {
+				t.Errorf("isRetryableError(%q) = %v, want %v", tt.err, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestExtractText(t *testing.T) {
+	t.Run("extracts content", func(t *testing.T) {
+		resp := &openai.ChatCompletion{
+			Choices: []openai.ChatCompletionChoice{
+				{Message: openai.ChatCompletionMessage{Content: " hello "}},
+			},
+		}
+		if got := extractText(resp); got != "hello" {
+			t.Errorf("extractText() = %q, want %q", got, "hello")
+		}
+	})
+
+	t.Run("handles nil/empty", func(t *testing.T) {
+		if got := extractText(nil); got != "" {
+			t.Errorf("extractText(nil) = %q", got)
+		}
+		if got := extractText(&openai.ChatCompletion{}); got != "" {
+			t.Errorf("extractText(empty) = %q", got)
+		}
+	})
+}
+
+func TestNewClient(t *testing.T) {
+	cfg := ProviderConfig{
+		Name:              "test-provider",
+		SupportsStreaming: true,
+	}
+	c := NewClient(cfg, WithDebugLogging(true))
+
+	if c.Name() != "test-provider" {
+		t.Errorf("Name() = %q, want %q", c.Name(), "test-provider")
+	}
+	if !c.SupportsStreaming() {
+		t.Error("SupportsStreaming() = false, want true")
+	}
+	if c.SupportsFileSearch() {
+		t.Error("SupportsFileSearch() = true, want false")
+	}
+	// Verify internal state
+	if !c.debug {
+		t.Error("debug mode not enabled by option")
+	}
+}
diff --git a/internal/redis/client_test.go b/internal/redis/client_test.go
new file mode 100644
index 0000000..e69de29
--- /dev/null
+++ b/internal/redis/client_test.go
@@ -0,0 +1,95 @@
+package redis
+
+import (
+	"context"
+	"testing"
+	"time"
+
+	"github.com/alicebob/miniredis/v2"
+)
+
+func setupTestRedis(t *testing.T) (*Client, *miniredis.Miniredis) {
+	mr, err := miniredis.Run()
+	if err != nil {
+		t.Fatalf("failed to start miniredis: %v", err)
+	}
+	
+	client, err := NewClient(Config{
+		Addr: mr.Addr(),
+	})
+	if err != nil {
+		t.Fatalf("failed to create client: %v", err)
+	}
+
+	return client, mr
+}
+
+func TestClient_BasicOps(t *testing.T) {
+	client, mr := setupTestRedis(t)
+	defer mr.Close()
+	defer client.Close()
+
+	ctx := context.Background()
+
+	// Test Set and Get
+	if err := client.Set(ctx, "key1", "value1", 0); err != nil {
+		t.Fatalf("Set failed: %v", err)
+	}
+
+	val, err := client.Get(ctx, "key1")
+	if err != nil {
+		t.Fatalf("Get failed: %v", err)
+	}
+	if val != "value1" {
+		t.Errorf("Get = %q, want %q", val, "value1")
+	}
+
+	// Test IsNil
+	_, err = client.Get(ctx, "nonexistent")
+	if !IsNil(err) {
+		t.Errorf("Expected IsNil(err) to be true for missing key, got err: %v", err)
+	}
+}
+
+func TestClient_Counters(t *testing.T) {
+	client, mr := setupTestRedis(t)
+	defer mr.Close()
+	defer client.Close()
+
+	ctx := context.Background()
+
+	n, err := client.Incr(ctx, "counter")
+	if err != nil {
+		t.Fatalf("Incr failed: %v", err)
+	}
+	if n != 1 {
+		t.Errorf("Incr = %d, want 1", n)
+	}
+
+	n, err = client.IncrBy(ctx, "counter", 10)
+	if err != nil {
+		t.Fatalf("IncrBy failed: %v", err)
+	}
+	if n != 11 {
+		t.Errorf("IncrBy = %d, want 11", n)
+	}
+}
+
+func TestClient_HashOps(t *testing.T) {
+	client, mr := setupTestRedis(t)
+	defer mr.Close()
+	defer client.Close()
+
+	ctx := context.Background()
+
+	if err := client.HSet(ctx, "user:1", "name", "Alice", "age", "30"); err != nil {
+		t.Fatalf("HSet failed: %v", err)
+	}
+
+	name, err := client.HGet(ctx, "user:1", "name")
+	if err != nil {
+		t.Fatalf("HGet failed: %v", err)
+	}
+	if name != "Alice" {
+		t.Errorf("HGet name = %q, want %q", name, "Alice")
+	}
+
+	all, err := client.HGetAll(ctx, "user:1")
+	if err != nil {
+		t.Fatalf("HGetAll failed: %v", err)
+	}
+	if len(all) != 2 {
+		t.Errorf("HGetAll len = %d, want 2", len(all))
+	}
+}
+
+func TestClient_Expiration(t *testing.T) {
+	client, mr := setupTestRedis(t)
+	defer mr.Close()
+	defer client.Close()
+
+	ctx := context.Background()
+
+	client.Set(ctx, "temp", "val", time.Minute)
+	ttl, err := client.TTL(ctx, "temp")
+	if err != nil {
+		t.Fatalf("TTL failed: %v", err)
+	}
+	if ttl <= 0 {
+		t.Errorf("expected positive TTL, got %v", ttl)
+	}
+
+	mr.FastForward(2 * time.Minute)
+	exists, _ := client.Exists(ctx, "temp")
+	if exists != 0 {
+		t.Error("key should have expired")
+	}
+}
```
