# Airborne Unit Test Plan
Date Created: 2026-01-16 12:54:59 +0100

## Scope
- Reviewed Go source files outside generated code and existing tests.
- Existing tests cover core services (auth interceptors, services, validation, RAG chunking, etc.), but several packages remain untested or only partially exercised.

## Untested or Lightly Tested Areas
- `internal/httpcapture/transport.go`: no verification of request/response capture or body restoration.
- `internal/redis/client.go`: wrapper operations not covered; `miniredis` dependency unused.
- `internal/tenant/env.go`: environment parsing, defaults, and validation untested.
- `internal/auth/static.go`: static admin token auth and metadata extraction untested.
- `internal/provider/compat/openai_compat.go`: message building, retry classification, and guard rails untested.
- `internal/provider/*` compat wrappers (grok, deepseek, etc.): no coverage for Name/capability flags.
- `internal/provider/gemini/filestore.go`: HTTP flows (create, upload fallback, list, delete, polling) untested.
- `internal/provider/openai/filestore.go`: vector store CRUD, file upload, polling logic untested.

Note: interface-only files (`internal/provider/provider.go`, `internal/rag/embedder/embedder.go`, `internal/rag/vectorstore/store.go`, `internal/rag/extractor/extractor.go`) do not need dedicated unit tests beyond compile-time usage.

## Proposed Unit Tests
### httpcapture
- RoundTrip captures request/response bodies and restores them.
- RoundTrip handles nil request body.

### redis
- Basic CRUD, counters, TTL, hash ops, scan, eval.
- `IsNil` returns true for missing keys.

### tenant/env
- Defaults when env vars are absent.
- Overrides for all env vars.
- Invalid port/redis DB errors.
- TLS enabled without cert/key errors; valid when both present.

### auth/static
- Token extraction precedence (Authorization vs x-api-key).
- `authenticate()` success path injects `ClientContextKey` with expected permissions.
- `authenticate()` missing metadata/invalid token returns Unauthenticated.
- UnaryInterceptor skips health method without requiring auth.

### provider/compat
- `buildMessages()` trims and filters history, role mapping.
- `extractText()` / `extractUsage()` with nil/empty responses.
- `isRetryableError()` classification for auth/invalid/rate/network errors.
- `GenerateReply` / `GenerateReplyStream` guard errors for missing API key and invalid base URL (no network).

### provider wrappers
- `NewClient()` capabilities (Name, SupportsFileSearch/WebSearch/Streaming/NativeContinuity) for Grok, DeepSeek, Mistral, Together, DeepInfra, Nebius, Cerebras, Cohere, Hyperbolic, Fireworks, Upstage, Perplexity, OpenRouter.

### gemini filestore
- Create request/response mapping.
- Get status computation (processing/partial).
- List pageSize query.
- Delete request.
- Upload fallback path and file ID extraction.
- `waitForOperation` timeout path (fast context timeout).

### openai filestore
- Create request/response mapping including `expires_after.days`.
- List/Get/Delete responses.
- Upload flow (files API + vector store file + polling).
- `waitForFileProcessing` timeout branch (fast context timeout).

## Patch-Ready Diffs

### 1) `internal/httpcapture/transport_test.go`
```diff
diff --git a/internal/httpcapture/transport_test.go b/internal/httpcapture/transport_test.go
new file mode 100644
index 0000000..fe4c0b2
--- /dev/null
+++ b/internal/httpcapture/transport_test.go
@@ -0,0 +1,78 @@
+package httpcapture
+
+import (
+    "bytes"
+    "io"
+    "net/http"
+    "testing"
+)
+
+type roundTripperFunc func(*http.Request) (*http.Response, error)
+
+func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
+    return f(req)
+}
+
+func TestTransportRoundTripCapturesBodies(t *testing.T) {
+    var sawRequest []byte
+    base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
+        body, err := io.ReadAll(req.Body)
+        if err != nil {
+            return nil, err
+        }
+        sawRequest = body
+        return &http.Response{
+            StatusCode: http.StatusOK,
+            Body:       io.NopCloser(bytes.NewBufferString("resp-body")),
+            Header:     make(http.Header),
+        }, nil
+    })
+
+    transport := &Transport{Base: base}
+
+    req, err := http.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString("req-body"))
+    if err != nil {
+        t.Fatalf("new request: %v", err)
+    }
+
+    resp, err := transport.RoundTrip(req)
+    if err != nil {
+        t.Fatalf("RoundTrip error: %v", err)
+    }
+
+    if string(sawRequest) != "req-body" {
+        t.Fatalf("base transport saw %q, want %q", string(sawRequest), "req-body")
+    }
+    if string(transport.RequestBody) != "req-body" {
+        t.Fatalf("captured request = %q, want %q", string(transport.RequestBody), "req-body")
+    }
+    if string(transport.ResponseBody) != "resp-body" {
+        t.Fatalf("captured response = %q, want %q", string(transport.ResponseBody), "resp-body")
+    }
+
+    respBody, err := io.ReadAll(resp.Body)
+    if err != nil {
+        t.Fatalf("read response body: %v", err)
+    }
+    if string(respBody) != "resp-body" {
+        t.Fatalf("response body = %q, want %q", string(respBody), "resp-body")
+    }
+}
+
+func TestTransportRoundTripNilRequestBody(t *testing.T) {
+    base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
+        return &http.Response{
+            StatusCode: http.StatusOK,
+            Body:       io.NopCloser(bytes.NewBufferString("ok")),
+            Header:     make(http.Header),
+        }, nil
+    })
+
+    transport := &Transport{Base: base}
+    req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
+    if err != nil {
+        t.Fatalf("new request: %v", err)
+    }
+
+    _, err = transport.RoundTrip(req)
+    if err != nil {
+        t.Fatalf("RoundTrip error: %v", err)
+    }
+
+    if transport.RequestBody != nil {
+        t.Fatalf("expected nil request body capture, got %q", string(transport.RequestBody))
+    }
+}
```

### 2) `internal/redis/client_test.go`
```diff
diff --git a/internal/redis/client_test.go b/internal/redis/client_test.go
new file mode 100644
index 0000000..8b1d1c9
--- /dev/null
+++ b/internal/redis/client_test.go
@@ -0,0 +1,123 @@
+package redis
+
+import (
+    "context"
+    "testing"
+    "time"
+
+    "github.com/alicebob/miniredis/v2"
+)
+
+func TestClientOperations(t *testing.T) {
+    s, err := miniredis.Run()
+    if err != nil {
+        t.Fatalf("start miniredis: %v", err)
+    }
+    t.Cleanup(s.Close)
+
+    client, err := NewClient(Config{Addr: s.Addr()})
+    if err != nil {
+        t.Fatalf("NewClient: %v", err)
+    }
+    t.Cleanup(func() { _ = client.Close() })
+
+    ctx := context.Background()
+
+    if err := client.Set(ctx, "key", "value", 0); err != nil {
+        t.Fatalf("Set: %v", err)
+    }
+    got, err := client.Get(ctx, "key")
+    if err != nil {
+        t.Fatalf("Get: %v", err)
+    }
+    if got != "value" {
+        t.Fatalf("Get = %q, want %q", got, "value")
+    }
+
+    count, err := client.Exists(ctx, "key", "missing")
+    if err != nil {
+        t.Fatalf("Exists: %v", err)
+    }
+    if count != 1 {
+        t.Fatalf("Exists = %d, want 1", count)
+    }
+
+    val, err := client.Incr(ctx, "counter")
+    if err != nil {
+        t.Fatalf("Incr: %v", err)
+    }
+    if val != 1 {
+        t.Fatalf("Incr = %d, want 1", val)
+    }
+
+    val, err = client.IncrBy(ctx, "counter", 2)
+    if err != nil {
+        t.Fatalf("IncrBy: %v", err)
+    }
+    if val != 3 {
+        t.Fatalf("IncrBy = %d, want 3", val)
+    }
+
+    if err := client.Expire(ctx, "key", 5*time.Second); err != nil {
+        t.Fatalf("Expire: %v", err)
+    }
+    ttl, err := client.TTL(ctx, "key")
+    if err != nil {
+        t.Fatalf("TTL: %v", err)
+    }
+    if ttl <= 0 {
+        t.Fatalf("TTL = %v, want > 0", ttl)
+    }
+
+    if err := client.HSet(ctx, "hash", "a", "1", "b", "2"); err != nil {
+        t.Fatalf("HSet: %v", err)
+    }
+    field, err := client.HGet(ctx, "hash", "a")
+    if err != nil {
+        t.Fatalf("HGet: %v", err)
+    }
+    if field != "1" {
+        t.Fatalf("HGet = %q, want %q", field, "1")
+    }
+    all, err := client.HGetAll(ctx, "hash")
+    if err != nil {
+        t.Fatalf("HGetAll: %v", err)
+    }
+    if len(all) != 2 {
+        t.Fatalf("HGetAll len = %d, want 2", len(all))
+    }
+
+    if err := client.HDel(ctx, "hash", "a"); err != nil {
+        t.Fatalf("HDel: %v", err)
+    }
+    if _, err := client.HGet(ctx, "hash", "a"); err == nil {
+        t.Fatal("expected error for missing hash field")
+    }
+
+    keys, err := client.Scan(ctx, "key*")
+    if err != nil {
+        t.Fatalf("Scan: %v", err)
+    }
+    found := false
+    for _, key := range keys {
+        if key == "key" {
+            found = true
+            break
+        }
+    }
+    if !found {
+        t.Fatalf("Scan did not return expected key")
+    }
+
+    res, err := client.Eval(ctx, "return redis.call('get', KEYS[1])", []string{"key"})
+    if err != nil {
+        t.Fatalf("Eval: %v", err)
+    }
+    evalStr, ok := res.(string)
+    if !ok {
+        t.Fatalf("Eval result type = %T, want string", res)
+    }
+    if evalStr != "value" {
+        t.Fatalf("Eval = %q, want %q", evalStr, "value")
+    }
+
+    if err := client.Del(ctx, "key", "counter"); err != nil {
+        t.Fatalf("Del: %v", err)
+    }
+}
+
+func TestIsNil(t *testing.T) {
+    s, err := miniredis.Run()
+    if err != nil {
+        t.Fatalf("start miniredis: %v", err)
+    }
+    t.Cleanup(s.Close)
+
+    client, err := NewClient(Config{Addr: s.Addr()})
+    if err != nil {
+        t.Fatalf("NewClient: %v", err)
+    }
+    t.Cleanup(func() { _ = client.Close() })
+
+    if _, err := client.Get(context.Background(), "missing"); !IsNil(err) {
+        t.Fatalf("IsNil(%v) = false, want true", err)
+    }
+}
```

### 3) `internal/tenant/env_test.go`
```diff
diff --git a/internal/tenant/env_test.go b/internal/tenant/env_test.go
new file mode 100644
index 0000000..1a2a2b8
--- /dev/null
+++ b/internal/tenant/env_test.go
@@ -0,0 +1,141 @@
+package tenant
+
+import "testing"
+
+func resetEnv(t *testing.T) {
+    t.Helper()
+    keys := []string{
+        "AIBOX_CONFIGS_DIR",
+        "AIBOX_GRPC_PORT",
+        "AIBOX_HOST",
+        "AIBOX_TLS_ENABLED",
+        "AIBOX_TLS_CERT_FILE",
+        "AIBOX_TLS_KEY_FILE",
+        "REDIS_ADDR",
+        "REDIS_PASSWORD",
+        "REDIS_DB",
+        "AIBOX_LOG_LEVEL",
+        "AIBOX_LOG_FORMAT",
+        "AIBOX_ADMIN_TOKEN",
+    }
+    for _, key := range keys {
+        t.Setenv(key, "")
+    }
+}
+
+func TestLoadEnvDefaults(t *testing.T) {
+    resetEnv(t)
+
+    cfg, err := loadEnv()
+    if err != nil {
+        t.Fatalf("loadEnv: %v", err)
+    }
+
+    if cfg.ConfigsDir != "configs" {
+        t.Fatalf("ConfigsDir = %q, want %q", cfg.ConfigsDir, "configs")
+    }
+    if cfg.GRPCPort != 50051 {
+        t.Fatalf("GRPCPort = %d, want %d", cfg.GRPCPort, 50051)
+    }
+    if cfg.Host != "0.0.0.0" {
+        t.Fatalf("Host = %q, want %q", cfg.Host, "0.0.0.0")
+    }
+    if cfg.RedisAddr != "localhost:6379" {
+        t.Fatalf("RedisAddr = %q, want %q", cfg.RedisAddr, "localhost:6379")
+    }
+    if cfg.RedisDB != 0 {
+        t.Fatalf("RedisDB = %d, want %d", cfg.RedisDB, 0)
+    }
+    if cfg.LogLevel != "info" {
+        t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "info")
+    }
+    if cfg.LogFormat != "json" {
+        t.Fatalf("LogFormat = %q, want %q", cfg.LogFormat, "json")
+    }
+    if cfg.TLSEnabled {
+        t.Fatal("TLSEnabled = true, want false")
+    }
+}
+
+func TestLoadEnvOverrides(t *testing.T) {
+    resetEnv(t)
+
+    t.Setenv("AIBOX_CONFIGS_DIR", "custom-configs")
+    t.Setenv("AIBOX_GRPC_PORT", "60000")
+    t.Setenv("AIBOX_HOST", "127.0.0.1")
+    t.Setenv("AIBOX_TLS_ENABLED", "true")
+    t.Setenv("AIBOX_TLS_CERT_FILE", "/tmp/cert.pem")
+    t.Setenv("AIBOX_TLS_KEY_FILE", "/tmp/key.pem")
+    t.Setenv("REDIS_ADDR", "redis:6379")
+    t.Setenv("REDIS_PASSWORD", "secret")
+    t.Setenv("REDIS_DB", "2")
+    t.Setenv("AIBOX_LOG_LEVEL", "debug")
+    t.Setenv("AIBOX_LOG_FORMAT", "text")
+    t.Setenv("AIBOX_ADMIN_TOKEN", "admintoken")
+
+    cfg, err := loadEnv()
+    if err != nil {
+        t.Fatalf("loadEnv: %v", err)
+    }
+
+    if cfg.ConfigsDir != "custom-configs" {
+        t.Fatalf("ConfigsDir = %q, want %q", cfg.ConfigsDir, "custom-configs")
+    }
+    if cfg.GRPCPort != 60000 {
+        t.Fatalf("GRPCPort = %d, want %d", cfg.GRPCPort, 60000)
+    }
+    if cfg.Host != "127.0.0.1" {
+        t.Fatalf("Host = %q, want %q", cfg.Host, "127.0.0.1")
+    }
+    if !cfg.TLSEnabled {
+        t.Fatal("TLSEnabled = false, want true")
+    }
+    if cfg.TLSCertFile != "/tmp/cert.pem" {
+        t.Fatalf("TLSCertFile = %q, want %q", cfg.TLSCertFile, "/tmp/cert.pem")
+    }
+    if cfg.TLSKeyFile != "/tmp/key.pem" {
+        t.Fatalf("TLSKeyFile = %q, want %q", cfg.TLSKeyFile, "/tmp/key.pem")
+    }
+    if cfg.RedisAddr != "redis:6379" {
+        t.Fatalf("RedisAddr = %q, want %q", cfg.RedisAddr, "redis:6379")
+    }
+    if cfg.RedisPassword != "secret" {
+        t.Fatalf("RedisPassword = %q, want %q", cfg.RedisPassword, "secret")
+    }
+    if cfg.RedisDB != 2 {
+        t.Fatalf("RedisDB = %d, want %d", cfg.RedisDB, 2)
+    }
+    if cfg.LogLevel != "debug" {
+        t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
+    }
+    if cfg.LogFormat != "text" {
+        t.Fatalf("LogFormat = %q, want %q", cfg.LogFormat, "text")
+    }
+    if cfg.AdminToken != "admintoken" {
+        t.Fatalf("AdminToken = %q, want %q", cfg.AdminToken, "admintoken")
+    }
+}
+
+func TestLoadEnvInvalidPort(t *testing.T) {
+    resetEnv(t)
+    t.Setenv("AIBOX_GRPC_PORT", "not-a-number")
+
+    if _, err := loadEnv(); err == nil {
+        t.Fatal("expected error for invalid AIBOX_GRPC_PORT")
+    }
+}
+
+func TestLoadEnvInvalidRedisDB(t *testing.T) {
+    resetEnv(t)
+    t.Setenv("REDIS_DB", "nope")
+
+    if _, err := loadEnv(); err == nil {
+        t.Fatal("expected error for invalid REDIS_DB")
+    }
+}
+
+func TestLoadEnvTLSValidation(t *testing.T) {
+    resetEnv(t)
+    t.Setenv("AIBOX_TLS_ENABLED", "true")
+
+    if _, err := loadEnv(); err == nil {
+        t.Fatal("expected error for missing TLS cert/key")
+    }
+
+    resetEnv(t)
+    t.Setenv("AIBOX_TLS_ENABLED", "true")
+    t.Setenv("AIBOX_TLS_CERT_FILE", "/tmp/cert.pem")
+    t.Setenv("AIBOX_TLS_KEY_FILE", "/tmp/key.pem")
+
+    if _, err := loadEnv(); err != nil {
+        t.Fatalf("unexpected error with TLS files: %v", err)
+    }
+}
```

### 4) `internal/auth/static_test.go`
```diff
diff --git a/internal/auth/static_test.go b/internal/auth/static_test.go
new file mode 100644
index 0000000..3e3f3b7
--- /dev/null
+++ b/internal/auth/static_test.go
@@ -0,0 +1,138 @@
+package auth
+
+import (
+    "context"
+    "testing"
+
+    "google.golang.org/grpc"
+    "google.golang.org/grpc/codes"
+    "google.golang.org/grpc/metadata"
+    "google.golang.org/grpc/status"
+)
+
+func TestExtractStaticToken(t *testing.T) {
+    tests := []struct {
+        name    string
+        md      metadata.MD
+        want    string
+        wantNil bool
+    }{
+        {
+            name: "bearer token",
+            md: metadata.MD{
+                "authorization": []string{"Bearer static123"},
+            },
+            want: "static123",
+        },
+        {
+            name: "authorization without bearer",
+            md: metadata.MD{
+                "authorization": []string{"rawtoken"},
+            },
+            want: "rawtoken",
+        },
+        {
+            name: "x-api-key header",
+            md: metadata.MD{
+                "x-api-key": []string{"apikey"},
+            },
+            want: "apikey",
+        },
+        {
+            name: "authorization takes precedence",
+            md: metadata.MD{
+                "authorization": []string{"Bearer authtoken"},
+                "x-api-key":     []string{"xapitoken"},
+            },
+            want: "authtoken",
+        },
+        {
+            name:    "no auth headers",
+            md:      metadata.MD{},
+            wantNil: true,
+        },
+    }
+
+    for _, tt := range tests {
+        t.Run(tt.name, func(t *testing.T) {
+            got := extractStaticToken(tt.md)
+            if tt.wantNil {
+                if got != "" {
+                    t.Fatalf("extractStaticToken() = %q, want empty", got)
+                }
+                return
+            }
+            if got != tt.want {
+                t.Fatalf("extractStaticToken() = %q, want %q", got, tt.want)
+            }
+        })
+    }
+}
+
+func TestStaticAuthenticateSuccess(t *testing.T) {
+    auth := NewStaticAuthenticator("secret")
+    md := metadata.Pairs("authorization", "Bearer secret")
+    ctx := metadata.NewIncomingContext(context.Background(), md)
+
+    gotCtx, err := auth.authenticate(ctx)
+    if err != nil {
+        t.Fatalf("authenticate: %v", err)
+    }
+
+    client, ok := gotCtx.Value(ClientContextKey).(*ClientKey)
+    if !ok || client == nil {
+        t.Fatal("expected ClientKey in context")
+    }
+    if client.ClientID != "admin" {
+        t.Fatalf("ClientID = %q, want %q", client.ClientID, "admin")
+    }
+    if client.ClientName != "static-admin" {
+        t.Fatalf("ClientName = %q, want %q", client.ClientName, "static-admin")
+    }
+    if len(client.Permissions) != 4 {
+        t.Fatalf("Permissions length = %d, want 4", len(client.Permissions))
+    }
+}
+
+func TestStaticAuthenticateMissingMetadata(t *testing.T) {
+    auth := NewStaticAuthenticator("secret")
+
+    _, err := auth.authenticate(context.Background())
+    if status.Code(err) != codes.Unauthenticated {
+        t.Fatalf("expected unauthenticated error, got %v", err)
+    }
+}
+
+func TestStaticAuthenticateInvalidToken(t *testing.T) {
+    auth := NewStaticAuthenticator("secret")
+    md := metadata.Pairs("x-api-key", "wrong")
+    ctx := metadata.NewIncomingContext(context.Background(), md)
+
+    _, err := auth.authenticate(ctx)
+    if status.Code(err) != codes.Unauthenticated {
+        t.Fatalf("expected unauthenticated error, got %v", err)
+    }
+}
+
+func TestStaticUnaryInterceptorSkipMethod(t *testing.T) {
+    auth := NewStaticAuthenticator("secret")
+    interceptor := auth.UnaryInterceptor()
+    called := false
+
+    handler := func(ctx context.Context, req interface{}) (interface{}, error) {
+        called = true
+        return "ok", nil
+    }
+
+    _, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/aibox.v1.AdminService/Health"}, handler)
+    if err != nil {
+        t.Fatalf("interceptor error: %v", err)
+    }
+    if !called {
+        t.Fatal("expected handler to be called")
+    }
+}
```

### 5) `internal/provider/compat/openai_compat_test.go`
```diff
diff --git a/internal/provider/compat/openai_compat_test.go b/internal/provider/compat/openai_compat_test.go
new file mode 100644
index 0000000..5d0e0bf
--- /dev/null
+++ b/internal/provider/compat/openai_compat_test.go
@@ -0,0 +1,196 @@
+package compat
+
+import (
+    "context"
+    "errors"
+    "testing"
+
+    openai "github.com/openai/openai-go"
+
+    "github.com/ai8future/airborne/internal/provider"
+)
+
+func TestBuildMessages(t *testing.T) {
+    history := []provider.Message{
+        {Role: "assistant", Content: "  hi  "},
+        {Role: "user", Content: "  "},
+        {Role: "user", Content: "there"},
+    }
+
+    messages := buildMessages("system", "  hello  ", history)
+    if len(messages) != 4 {
+        t.Fatalf("expected 4 messages, got %d", len(messages))
+    }
+
+    if messages[0].OfSystem == nil || messages[0].OfSystem.Content.OfString.Value != "system" {
+        t.Fatalf("expected system message with content %q", "system")
+    }
+    if messages[1].OfAssistant == nil || messages[1].OfAssistant.Content.OfString.Value != "hi" {
+        t.Fatalf("expected assistant message with content %q", "hi")
+    }
+    if messages[2].OfUser == nil || messages[2].OfUser.Content.OfString.Value != "there" {
+        t.Fatalf("expected user message with content %q", "there")
+    }
+    if messages[3].OfUser == nil || messages[3].OfUser.Content.OfString.Value != "hello" {
+        t.Fatalf("expected user message with content %q", "hello")
+    }
+}
+
+func TestExtractText(t *testing.T) {
+    resp := &openai.ChatCompletion{
+        Choices: []openai.ChatCompletionChoice{
+            {Message: openai.ChatCompletionMessage{Content: "  hello  "}},
+        },
+    }
+
+    got := extractText(resp)
+    if got != "hello" {
+        t.Fatalf("extractText() = %q, want %q", got, "hello")
+    }
+
+    if extractText(nil) != "" {
+        t.Fatal("extractText(nil) should be empty")
+    }
+    if extractText(&openai.ChatCompletion{}) != "" {
+        t.Fatal("extractText(empty) should be empty")
+    }
+}
+
+func TestExtractUsage(t *testing.T) {
+    resp := &openai.ChatCompletion{
+        Usage: openai.ChatCompletionUsage{
+            PromptTokens:     5,
+            CompletionTokens: 7,
+            TotalTokens:      12,
+        },
+    }
+
+    usage := extractUsage(resp)
+    if usage == nil {
+        t.Fatal("expected non-nil usage")
+    }
+    if usage.InputTokens != 5 || usage.OutputTokens != 7 || usage.TotalTokens != 12 {
+        t.Fatalf("unexpected usage: %+v", usage)
+    }
+
+    usage = extractUsage(nil)
+    if usage == nil {
+        t.Fatal("expected non-nil usage for nil response")
+    }
+}
+
+func TestIsRetryableError(t *testing.T) {
+    tests := []struct {
+        name string
+        err  error
+        want bool
+    }{
+        {"nil", nil, false},
+        {"context canceled", context.Canceled, false},
+        {"context deadline", context.DeadlineExceeded, false},
+        {"auth error", errors.New("401 unauthorized"), false},
+        {"invalid request", errors.New("invalid_request"), false},
+        {"rate limit", errors.New("429 rate limit"), true},
+        {"server error", errors.New("500 internal"), true},
+        {"network timeout", errors.New("connection timeout"), true},
+    }
+
+    for _, tt := range tests {
+        t.Run(tt.name, func(t *testing.T) {
+            got := isRetryableError(tt.err)
+            if got != tt.want {
+                t.Fatalf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
+            }
+        })
+    }
+}
+
+func TestGenerateReplyMissingAPIKey(t *testing.T) {
+    client := NewClient(ProviderConfig{
+        Name:           "test",
+        DefaultBaseURL: "https://example.com",
+        DefaultModel:   "model",
+    })
+
+    _, err := client.GenerateReply(context.Background(), provider.GenerateParams{
+        Config: provider.ProviderConfig{APIKey: ""},
+    })
+    if err == nil {
+        t.Fatal("expected error for missing API key")
+    }
+}
+
+func TestGenerateReplyInvalidBaseURL(t *testing.T) {
+    client := NewClient(ProviderConfig{
+        Name:           "test",
+        DefaultBaseURL: "https://example.com",
+        DefaultModel:   "model",
+    })
+
+    _, err := client.GenerateReply(context.Background(), provider.GenerateParams{
+        Config: provider.ProviderConfig{
+            APIKey:  "key",
+            BaseURL: "ftp://example.com",
+        },
+    })
+    if err == nil {
+        t.Fatal("expected error for invalid base URL")
+    }
+}
+
+func TestGenerateReplyStreamMissingAPIKey(t *testing.T) {
+    client := NewClient(ProviderConfig{
+        Name:           "test",
+        DefaultBaseURL: "https://example.com",
+        DefaultModel:   "model",
+    })
+
+    _, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{
+        Config: provider.ProviderConfig{APIKey: ""},
+    })
+    if err == nil {
+        t.Fatal("expected error for missing API key")
+    }
+}
```

### 6) `internal/provider/providers_test.go`
```diff
diff --git a/internal/provider/providers_test.go b/internal/provider/providers_test.go
new file mode 100644
index 0000000..2c6d5c7
--- /dev/null
+++ b/internal/provider/providers_test.go
@@ -0,0 +1,91 @@
+package provider
+
+import (
+    "testing"
+
+    "github.com/ai8future/airborne/internal/provider/cerebras"
+    "github.com/ai8future/airborne/internal/provider/cohere"
+    "github.com/ai8future/airborne/internal/provider/deepinfra"
+    "github.com/ai8future/airborne/internal/provider/deepseek"
+    "github.com/ai8future/airborne/internal/provider/fireworks"
+    "github.com/ai8future/airborne/internal/provider/grok"
+    "github.com/ai8future/airborne/internal/provider/hyperbolic"
+    "github.com/ai8future/airborne/internal/provider/mistral"
+    "github.com/ai8future/airborne/internal/provider/nebius"
+    "github.com/ai8future/airborne/internal/provider/openrouter"
+    "github.com/ai8future/airborne/internal/provider/perplexity"
+    "github.com/ai8future/airborne/internal/provider/together"
+    "github.com/ai8future/airborne/internal/provider/upstage"
+)
+
+func TestCompatProviderCapabilities(t *testing.T) {
+    cases := []struct {
+        name            string
+        client          Provider
+        wantName        string
+        wantFileSearch  bool
+        wantWebSearch   bool
+        wantStreaming   bool
+    }{
+        {"grok", grok.NewClient(), "grok", false, false, true},
+        {"deepseek", deepseek.NewClient(), "deepseek", false, false, true},
+        {"mistral", mistral.NewClient(), "mistral", false, false, true},
+        {"together", together.NewClient(), "together", false, false, true},
+        {"deepinfra", deepinfra.NewClient(), "deepinfra", false, false, true},
+        {"nebius", nebius.NewClient(), "nebius", false, false, true},
+        {"cerebras", cerebras.NewClient(), "cerebras", false, false, true},
+        {"cohere", cohere.NewClient(), "cohere", false, true, true},
+        {"hyperbolic", hyperbolic.NewClient(), "hyperbolic", false, false, true},
+        {"fireworks", fireworks.NewClient(), "fireworks", false, false, true},
+        {"upstage", upstage.NewClient(), "upstage", false, false, true},
+        {"perplexity", perplexity.NewClient(), "perplexity", false, true, true},
+        {"openrouter", openrouter.NewClient(), "openrouter", false, false, true},
+    }
+
+    for _, tc := range cases {
+        t.Run(tc.name, func(t *testing.T) {
+            if tc.client == nil {
+                t.Fatal("expected non-nil client")
+            }
+            if got := tc.client.Name(); got != tc.wantName {
+                t.Fatalf("Name() = %q, want %q", got, tc.wantName)
+            }
+            if got := tc.client.SupportsFileSearch(); got != tc.wantFileSearch {
+                t.Fatalf("SupportsFileSearch() = %v, want %v", got, tc.wantFileSearch)
+            }
+            if got := tc.client.SupportsWebSearch(); got != tc.wantWebSearch {
+                t.Fatalf("SupportsWebSearch() = %v, want %v", got, tc.wantWebSearch)
+            }
+            if got := tc.client.SupportsStreaming(); got != tc.wantStreaming {
+                t.Fatalf("SupportsStreaming() = %v, want %v", got, tc.wantStreaming)
+            }
+            if tc.client.SupportsNativeContinuity() {
+                t.Fatal("SupportsNativeContinuity() = true, want false")
+            }
+        })
+    }
+}
```

### 7) `internal/provider/gemini/filestore_test.go`
```diff
diff --git a/internal/provider/gemini/filestore_test.go b/internal/provider/gemini/filestore_test.go
new file mode 100644
index 0000000..24d9b54
--- /dev/null
+++ b/internal/provider/gemini/filestore_test.go
@@ -0,0 +1,236 @@
+package gemini
+
+import (
+    "context"
+    "encoding/json"
+    "net/http"
+    "net/http/httptest"
+    "strings"
+    "sync"
+    "testing"
+    "time"
+)
+
+func TestCreateFileSearchStoreSuccess(t *testing.T) {
+    var (
+        gotMethod string
+        gotPath   string
+        gotKey    string
+        gotName   string
+        mu        sync.Mutex
+    )
+
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        mu.Lock()
+        gotMethod = r.Method
+        gotPath = r.URL.Path
+        gotKey = r.URL.Query().Get("key")
+        mu.Unlock()
+
+        body := map[string]string{}
+        _ = json.NewDecoder(r.Body).Decode(&body)
+        mu.Lock()
+        gotName = body["displayName"]
+        mu.Unlock()
+
+        resp := fileSearchStoreResponse{
+            Name:               "fileSearchStores/store-123",
+            DisplayName:        "My Store",
+            CreateTime:         "2024-01-02T03:04:05Z",
+            TotalDocumentCount: 2,
+        }
+        _ = json.NewEncoder(w).Encode(resp)
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
+    result, err := CreateFileSearchStore(context.Background(), cfg, "My Store")
+    if err != nil {
+        t.Fatalf("CreateFileSearchStore: %v", err)
+    }
+
+    if result.StoreID != "store-123" {
+        t.Fatalf("StoreID = %q, want %q", result.StoreID, "store-123")
+    }
+    if result.Name != "My Store" {
+        t.Fatalf("Name = %q, want %q", result.Name, "My Store")
+    }
+    if result.DocumentCount != 2 {
+        t.Fatalf("DocumentCount = %d, want 2", result.DocumentCount)
+    }
+
+    mu.Lock()
+    method := gotMethod
+    path := gotPath
+    key := gotKey
+    name := gotName
+    mu.Unlock()
+
+    if method != http.MethodPost {
+        t.Fatalf("method = %q, want %q", method, http.MethodPost)
+    }
+    if path != "/fileSearchStores" {
+        t.Fatalf("path = %q, want %q", path, "/fileSearchStores")
+    }
+    if key != "key" {
+        t.Fatalf("key = %q, want %q", key, "key")
+    }
+    if name != "My Store" {
+        t.Fatalf("displayName = %q, want %q", name, "My Store")
+    }
+}
+
+func TestGetFileSearchStoreStatus(t *testing.T) {
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        resp := fileSearchStoreResponse{
+            Name:                   "fileSearchStores/store-9",
+            DisplayName:            "Store",
+            CreateTime:             "2024-01-02T03:04:05Z",
+            TotalDocumentCount:     3,
+            ProcessedDocumentCount: 1,
+            FailedDocumentCount:    1,
+        }
+        _ = json.NewEncoder(w).Encode(resp)
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
+    result, err := GetFileSearchStore(context.Background(), cfg, "store-9")
+    if err != nil {
+        t.Fatalf("GetFileSearchStore: %v", err)
+    }
+    if result.Status != "partial" {
+        t.Fatalf("Status = %q, want %q", result.Status, "partial")
+    }
+}
+
+func TestListFileSearchStoresLimit(t *testing.T) {
+    var gotPageSize string
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        gotPageSize = r.URL.Query().Get("pageSize")
+        resp := struct {
+            FileSearchStores []fileSearchStoreResponse `json:"fileSearchStores"`
+        }{
+            FileSearchStores: []fileSearchStoreResponse{{
+                Name:               "fileSearchStores/store-1",
+                DisplayName:        "Store One",
+                CreateTime:         "2024-01-02T03:04:05Z",
+                TotalDocumentCount: 1,
+            }},
+        }
+        _ = json.NewEncoder(w).Encode(resp)
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
+    result, err := ListFileSearchStores(context.Background(), cfg, 5)
+    if err != nil {
+        t.Fatalf("ListFileSearchStores: %v", err)
+    }
+    if gotPageSize != "5" {
+        t.Fatalf("pageSize = %q, want %q", gotPageSize, "5")
+    }
+    if len(result) != 1 {
+        t.Fatalf("len(result) = %d, want 1", len(result))
+    }
+    if result[0].StoreID != "store-1" {
+        t.Fatalf("StoreID = %q, want %q", result[0].StoreID, "store-1")
+    }
+}
+
+func TestDeleteFileSearchStoreSuccess(t *testing.T) {
+    var gotMethod string
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        gotMethod = r.Method
+        w.WriteHeader(http.StatusOK)
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
+    if err := DeleteFileSearchStore(context.Background(), cfg, "store-1", false); err != nil {
+        t.Fatalf("DeleteFileSearchStore: %v", err)
+    }
+    if gotMethod != http.MethodDelete {
+        t.Fatalf("method = %q, want %q", gotMethod, http.MethodDelete)
+    }
+}
+
+func TestUploadFileToFileSearchStoreFallback(t *testing.T) {
+    var calls int
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        calls++
+        if calls == 1 {
+            w.WriteHeader(http.StatusInternalServerError)
+            _, _ = w.Write([]byte("fail"))
+            return
+        }
+        resp := operationResponse{
+            Response: map[string]interface{}{
+                "name": "fileSearchStores/store-1/files/file-1",
+            },
+        }
+        _ = json.NewEncoder(w).Encode(resp)
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
+    result, err := UploadFileToFileSearchStore(context.Background(), cfg, "store-1", "doc.txt", "text/plain", strings.NewReader("data"))
+    if err != nil {
+        t.Fatalf("UploadFileToFileSearchStore: %v", err)
+    }
+    if calls != 2 {
+        t.Fatalf("calls = %d, want 2", calls)
+    }
+    if result.FileID != "file-1" {
+        t.Fatalf("FileID = %q, want %q", result.FileID, "file-1")
+    }
+    if result.StoreID != "store-1" {
+        t.Fatalf("StoreID = %q, want %q", result.StoreID, "store-1")
+    }
+}
+
+func TestWaitForOperationTimeout(t *testing.T) {
+    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
+    defer cancel()
+
+    status, err := waitForOperation(ctx, FileStoreConfig{APIKey: "key", BaseURL: "http://example.com"}, "operations/op-1")
+    if err == nil {
+        t.Fatal("expected timeout error")
+    }
+    if status != "in_progress" {
+        t.Fatalf("status = %q, want %q", status, "in_progress")
+    }
+}
```

### 8) `internal/provider/openai/filestore_test.go`
```diff
diff --git a/internal/provider/openai/filestore_test.go b/internal/provider/openai/filestore_test.go
new file mode 100644
index 0000000..e542f3a
--- /dev/null
+++ b/internal/provider/openai/filestore_test.go
@@ -0,0 +1,284 @@
+package openai
+
+import (
+    "context"
+    "encoding/json"
+    "net/http"
+    "net/http/httptest"
+    "strings"
+    "sync"
+    "testing"
+    "time"
+
+    openaiapi "github.com/openai/openai-go"
+    "github.com/openai/openai-go/option"
+)
+
+func writeJSON(w http.ResponseWriter, payload map[string]interface{}) {
+    w.Header().Set("Content-Type", "application/json")
+    _ = json.NewEncoder(w).Encode(payload)
+}
+
+func writeVectorStore(w http.ResponseWriter, id, name string) {
+    writeJSON(w, map[string]interface{}{
+        "id":             id,
+        "object":         "vector_store",
+        "created_at":     1700000000,
+        "last_active_at": 1700000000,
+        "name":           name,
+        "status":         "completed",
+        "usage_bytes":    0,
+        "metadata":       map[string]interface{}{},
+        "file_counts": map[string]interface{}{
+            "cancelled":   0,
+            "completed":   0,
+            "failed":      0,
+            "in_progress": 0,
+            "total":       0,
+        },
+    })
+}
+
+func writeVectorStoreFile(w http.ResponseWriter, id, storeID, status string) {
+    writeJSON(w, map[string]interface{}{
+        "id":              id,
+        "object":          "vector_store.file",
+        "created_at":      1700000000,
+        "status":          status,
+        "usage_bytes":     0,
+        "vector_store_id": storeID,
+        "last_error": map[string]interface{}{
+            "code":    "server_error",
+            "message": "none",
+        },
+        "attributes": map[string]interface{}{},
+        "chunking_strategy": map[string]interface{}{
+            "type": "other",
+        },
+    })
+}
+
+func writeFileObject(w http.ResponseWriter, id, filename string) {
+    writeJSON(w, map[string]interface{}{
+        "id":         id,
+        "bytes":      10,
+        "created_at": 1700000000,
+        "filename":   filename,
+        "object":     "file",
+        "purpose":    "assistants",
+        "status":     "uploaded",
+    })
+}
+
+func TestCreateVectorStoreSuccess(t *testing.T) {
+    var (
+        gotMethod string
+        gotPath   string
+        gotBody   map[string]interface{}
+        mu        sync.Mutex
+    )
+
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        mu.Lock()
+        gotMethod = r.Method
+        gotPath = r.URL.Path
+        mu.Unlock()
+
+        body := map[string]interface{}{}
+        _ = json.NewDecoder(r.Body).Decode(&body)
+        mu.Lock()
+        gotBody = body
+        mu.Unlock()
+
+        writeVectorStore(w, "vs_1", "store-name")
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL, ExpirationDays: 7}
+    result, err := CreateVectorStore(context.Background(), cfg, "store-name")
+    if err != nil {
+        t.Fatalf("CreateVectorStore: %v", err)
+    }
+    if result.StoreID != "vs_1" {
+        t.Fatalf("StoreID = %q, want %q", result.StoreID, "vs_1")
+    }
+
+    mu.Lock()
+    method := gotMethod
+    path := gotPath
+    body := gotBody
+    mu.Unlock()
+
+    if method != http.MethodPost {
+        t.Fatalf("method = %q, want %q", method, http.MethodPost)
+    }
+    if path != "/vector_stores" {
+        t.Fatalf("path = %q, want %q", path, "/vector_stores")
+    }
+    if body["name"] != "store-name" {
+        t.Fatalf("name = %v, want %q", body["name"], "store-name")
+    }
+    expires, ok := body["expires_after"].(map[string]interface{})
+    if !ok {
+        t.Fatal("expected expires_after in request body")
+    }
+    if int(expires["days"].(float64)) != 7 {
+        t.Fatalf("expires_after.days = %v, want 7", expires["days"])
+    }
+}
+
+func TestCreateVectorStoreMissingAPIKey(t *testing.T) {
+    if _, err := CreateVectorStore(context.Background(), FileStoreConfig{}, "store"); err == nil {
+        t.Fatal("expected error for missing API key")
+    }
+}
+
+func TestListVectorStoresSuccess(t *testing.T) {
+    var gotLimit string
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        gotLimit = r.URL.Query().Get("limit")
+        writeJSON(w, map[string]interface{}{
+            "data": []interface{}{map[string]interface{}{
+                "id":             "vs_1",
+                "object":         "vector_store",
+                "created_at":     1700000000,
+                "last_active_at": 1700000000,
+                "name":           "store-name",
+                "status":         "completed",
+                "usage_bytes":    0,
+                "metadata":       map[string]interface{}{},
+                "file_counts": map[string]interface{}{
+                    "cancelled":   0,
+                    "completed":   0,
+                    "failed":      0,
+                    "in_progress": 0,
+                    "total":       0,
+                },
+            }},
+            "has_more": false,
+        })
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
+    result, err := ListVectorStores(context.Background(), cfg, 1)
+    if err != nil {
+        t.Fatalf("ListVectorStores: %v", err)
+    }
+    if gotLimit != "1" {
+        t.Fatalf("limit = %q, want %q", gotLimit, "1")
+    }
+    if len(result) != 1 {
+        t.Fatalf("len(result) = %d, want 1", len(result))
+    }
+    if result[0].StoreID != "vs_1" {
+        t.Fatalf("StoreID = %q, want %q", result[0].StoreID, "vs_1")
+    }
+}
+
+func TestGetVectorStoreSuccess(t *testing.T) {
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        writeVectorStore(w, "vs_1", "store-name")
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
+    result, err := GetVectorStore(context.Background(), cfg, "vs_1")
+    if err != nil {
+        t.Fatalf("GetVectorStore: %v", err)
+    }
+    if result.Name != "store-name" {
+        t.Fatalf("Name = %q, want %q", result.Name, "store-name")
+    }
+}
+
+func TestDeleteVectorStoreSuccess(t *testing.T) {
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        writeJSON(w, map[string]interface{}{
+            "id":      "vs_1",
+            "deleted": true,
+            "object":  "vector_store.deleted",
+        })
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
+    if err := DeleteVectorStore(context.Background(), cfg, "vs_1"); err != nil {
+        t.Fatalf("DeleteVectorStore: %v", err)
+    }
+}
+
+func TestUploadFileToVectorStoreSuccess(t *testing.T) {
+    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        switch {
+        case r.Method == http.MethodPost && r.URL.Path == "/files":
+            writeFileObject(w, "file_1", "doc.txt")
+        case r.Method == http.MethodPost && r.URL.Path == "/vector_stores/vs_1/files":
+            writeVectorStoreFile(w, "vsf_1", "vs_1", "in_progress")
+        case r.Method == http.MethodGet && r.URL.Path == "/vector_stores/vs_1/files/vsf_1":
+            writeVectorStoreFile(w, "vsf_1", "vs_1", "completed")
+        default:
+            http.NotFound(w, r)
+        }
+    }))
+    defer srv.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
+    result, err := UploadFileToVectorStore(context.Background(), cfg, "vs_1", "doc.txt", strings.NewReader("hello"))
+    if err != nil {
+        t.Fatalf("UploadFileToVectorStore: %v", err)
+    }
+    if result.FileID != "file_1" {
+        t.Fatalf("FileID = %q, want %q", result.FileID, "file_1")
+    }
+    if result.Status != "completed" {
+        t.Fatalf("Status = %q, want %q", result.Status, "completed")
+    }
+}
+
+func TestWaitForFileProcessingTimeout(t *testing.T) {
+    client := openaiapi.NewClient(option.WithAPIKey("key"))
+    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
+    defer cancel()
+
+    status, err := waitForFileProcessing(ctx, client, "vs_1", "vsf_1")
+    if err == nil {
+        t.Fatal("expected timeout error")
+    }
+    if status != "in_progress" {
+        t.Fatalf("status = %q, want %q", status, "in_progress")
+    }
+}
```

## Notes and Risks
- `UploadFileToVectorStore` and `waitForFileProcessing` use a 2 second polling interval. Tests above include a single polling loop, so expect roughly 2 seconds of runtime for that test.
- Gemini `waitForOperation` uses a 2 second polling interval as well. The proposed timeout test avoids waiting by using a very short context deadline.
- All HTTP tests use `httptest` with `BaseURL` overrides to avoid real network calls.
