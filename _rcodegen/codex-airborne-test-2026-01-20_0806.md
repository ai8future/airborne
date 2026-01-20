Date Created: 2026-01-20 08:06:28 +0100
TOTAL_SCORE: 72/100

**Scope**
- Scanned Go packages for missing `_test.go` coverage while avoiding `_studies`, `_proposals`, `_codex`, `_claude`, and `_rcodegen`.
- Untested directories include `cmd/airborne`, `internal/admin`, `internal/db`, `internal/pricing`, multiple compat-based providers, and generated clients in `gen/` + `markdown_svc/clients/go`.

**Grade Rationale**
- Strengths: solid coverage in `internal/service`, `internal/provider` core clients (OpenAI, Gemini, Anthropic), `internal/rag`, validation, retry, auth.
- Gaps: critical operational code lacks tests (admin HTTP server, DB repository logic, pricing). Compat-based providers have no regression checks for config flags.
- Risk: untested HTTP handlers and DB mapping increase regression and integration failures; pricing bugs can directly impact billing.

**Recommended Unit Tests (High Priority)**
- `internal/admin/server.go`: handler validation, error handling for nil DB, gRPC request mapping, auth metadata injection.
- `internal/pricing/pricing.go`: pricing file discovery, provider inference from filename, prefix model matching, unknown model handling, and global `Init`/`CalculateCost` behavior.
- `internal/db/repository.go` + `internal/db/postgres.go`: tenant validation, table naming, tenant repo caching, CA cert write behavior. (DB CRUD requires integration or pgx mock; see note below.)
- Compat-based providers (`internal/provider/{cerebras,cohere,deepinfra,deepseek,fireworks,grok,hyperbolic,nebius,openrouter,perplexity,together,upstage}`): verify `Name()` and support flags to prevent config regressions.

**Additional Coverage Suggestions (Lower Priority / Integration)**
- `cmd/airborne`: CLI flag parsing and wiring to server startup (integration-style).
- DB CRUD paths: use a pgx mock or disposable Postgres to validate SQL parameter mapping and error handling.
- Generated clients in `gen/` and `markdown_svc/clients/go`: typically excluded; test only if you add custom wrappers.

**Patch-Ready Diffs**
- `internal/pricing/pricing_test.go`
```diff
diff --git a/internal/pricing/pricing_test.go b/internal/pricing/pricing_test.go
new file mode 100644
index 0000000..e7db30c
--- /dev/null
+++ b/internal/pricing/pricing_test.go
@@ -0,0 +1,180 @@
+package pricing
+
+import (
+    "math"
+    "os"
+    "path/filepath"
+    "reflect"
+    "sort"
+    "sync"
+    "testing"
+)
+
+func writePricingFile(t *testing.T, dir, name, content string) string {
+    t.Helper()
+    path := filepath.Join(dir, name)
+    if err := os.WriteFile(path, []byte(content), 0600); err != nil {
+        t.Fatalf("write pricing file: %v", err)
+    }
+    return path
+}
+
+func resetPricingGlobals() {
+    defaultPricer = nil
+    initErr = nil
+    initOnce = sync.Once{}
+}
+
+func TestNewPricer_LoadsPricingFiles(t *testing.T) {
+    dir := t.TempDir()
+    writePricingFile(t, dir, "openai_pricing.json", `{
+  "provider": "openai",
+  "models": {
+    "gpt-4o": {"input_per_million": 5, "output_per_million": 15}
+  }
+}`)
+    writePricingFile(t, dir, "cohere_pricing.json", `{
+  "models": {
+    "command-r-plus": {"input_per_million": 2, "output_per_million": 4}
+  },
+  "metadata": {"updated": "2025-01-01"}
+}`)
+
+    pricer, err := NewPricer(dir)
+    if err != nil {
+        t.Fatalf("NewPricer: %v", err)
+    }
+
+    if pricer.ModelCount() != 2 {
+        t.Fatalf("ModelCount = %d, want 2", pricer.ModelCount())
+    }
+
+    providers := pricer.ListProviders()
+    sort.Strings(providers)
+    wantProviders := []string{"cohere", "openai"}
+    if !reflect.DeepEqual(providers, wantProviders) {
+        t.Fatalf("ListProviders = %v, want %v", providers, wantProviders)
+    }
+
+    if _, ok := pricer.GetPricing("gpt-4o"); !ok {
+        t.Fatalf("GetPricing did not find gpt-4o")
+    }
+}
+
+func TestPricer_Calculate_PrefixMatch(t *testing.T) {
+    dir := t.TempDir()
+    writePricingFile(t, dir, "openai_pricing.json", `{
+  "provider": "openai",
+  "models": {
+    "gpt-4o": {"input_per_million": 5, "output_per_million": 15}
+  }
+}`)
+
+    pricer, err := NewPricer(dir)
+    if err != nil {
+        t.Fatalf("NewPricer: %v", err)
+    }
+
+    cost := pricer.Calculate("gpt-4o-2024-08-06", 1000, 2000)
+    if cost.Unknown {
+        t.Fatalf("Calculate returned Unknown for prefix match")
+    }
+
+    expected := (1000*5 + 2000*15) / 1_000_000.0
+    if math.Abs(cost.TotalCost-expected) > 1e-9 {
+        t.Fatalf("TotalCost = %f, want %f", cost.TotalCost, expected)
+    }
+}
+
+func TestPricer_Calculate_UnknownModel(t *testing.T) {
+    dir := t.TempDir()
+    writePricingFile(t, dir, "openai_pricing.json", `{
+  "provider": "openai",
+  "models": {
+    "gpt-4o": {"input_per_million": 5, "output_per_million": 15}
+  }
+}`)
+
+    pricer, err := NewPricer(dir)
+    if err != nil {
+        t.Fatalf("NewPricer: %v", err)
+    }
+
+    cost := pricer.Calculate("unknown-model", 10, 20)
+    if !cost.Unknown {
+        t.Fatalf("Calculate expected Unknown for missing model")
+    }
+    if cost.TotalCost != 0 {
+        t.Fatalf("TotalCost = %f, want 0", cost.TotalCost)
+    }
+}
+
+func TestNewPricer_NoFiles(t *testing.T) {
+    _, err := NewPricer(t.TempDir())
+    if err == nil {
+        t.Fatalf("expected error for missing pricing files")
+    }
+}
+
+func TestNewPricer_InvalidJSON(t *testing.T) {
+    dir := t.TempDir()
+    writePricingFile(t, dir, "bad_pricing.json", "{")
+    if _, err := NewPricer(dir); err == nil {
+        t.Fatalf("expected error for invalid JSON")
+    }
+}
+
+func TestCalculateCost_UsesInit(t *testing.T) {
+    resetPricingGlobals()
+    dir := t.TempDir()
+    writePricingFile(t, dir, "openai_pricing.json", `{
+  "provider": "openai",
+  "models": {
+    "gpt-4o": {"input_per_million": 5, "output_per_million": 15}
+  }
+}`)
+
+    if err := Init(dir); err != nil {
+        t.Fatalf("Init: %v", err)
+    }
+
+    cost := CalculateCost("gpt-4o", 1000, 0)
+    expected := (1000 * 5) / 1_000_000.0
+    if math.Abs(cost-expected) > 1e-9 {
+        t.Fatalf("CalculateCost = %f, want %f", cost, expected)
+    }
+}
```

- `internal/db/repository_test.go`
```diff
diff --git a/internal/db/repository_test.go b/internal/db/repository_test.go
new file mode 100644
index 0000000..d7a62b8
--- /dev/null
+++ b/internal/db/repository_test.go
@@ -0,0 +1,122 @@
+package db
+
+import (
+    "errors"
+    "os"
+    "path/filepath"
+    "testing"
+)
+
+func TestNewTenantRepository_ValidInvalid(t *testing.T) {
+    client := &Client{tenantRepos: make(map[string]*Repository)}
+
+    repo, err := NewTenantRepository(client, "ai8")
+    if err != nil {
+        t.Fatalf("NewTenantRepository(valid): %v", err)
+    }
+    if repo.TenantID() != "ai8" {
+        t.Fatalf("TenantID = %q, want %q", repo.TenantID(), "ai8")
+    }
+
+    if _, err := NewTenantRepository(client, "bogus"); !errors.Is(err, ErrInvalidTenant) {
+        t.Fatalf("expected ErrInvalidTenant, got %v", err)
+    }
+}
+
+func TestRepository_TableNames(t *testing.T) {
+    client := &Client{tenantRepos: make(map[string]*Repository)}
+
+    legacy := NewRepository(client)
+    if legacy.threadsTable() != "airborne_threads" {
+        t.Fatalf("legacy threadsTable = %q", legacy.threadsTable())
+    }
+    if legacy.messagesTable() != "airborne_messages" {
+        t.Fatalf("legacy messagesTable = %q", legacy.messagesTable())
+    }
+    if legacy.filesTable() != "airborne_files" {
+        t.Fatalf("legacy filesTable = %q", legacy.filesTable())
+    }
+    if legacy.fileUploadsTable() != "airborne_file_provider_uploads" {
+        t.Fatalf("legacy fileUploadsTable = %q", legacy.fileUploadsTable())
+    }
+    if legacy.vectorStoresTable() != "airborne_thread_vector_stores" {
+        t.Fatalf("legacy vectorStoresTable = %q", legacy.vectorStoresTable())
+    }
+
+    tenant, err := NewTenantRepository(client, "ai8")
+    if err != nil {
+        t.Fatalf("NewTenantRepository: %v", err)
+    }
+    if tenant.threadsTable() != "ai8_airborne_threads" {
+        t.Fatalf("tenant threadsTable = %q", tenant.threadsTable())
+    }
+    if tenant.messagesTable() != "ai8_airborne_messages" {
+        t.Fatalf("tenant messagesTable = %q", tenant.messagesTable())
+    }
+    if tenant.filesTable() != "ai8_airborne_files" {
+        t.Fatalf("tenant filesTable = %q", tenant.filesTable())
+    }
+    if tenant.fileUploadsTable() != "ai8_airborne_file_provider_uploads" {
+        t.Fatalf("tenant fileUploadsTable = %q", tenant.fileUploadsTable())
+    }
+    if tenant.vectorStoresTable() != "ai8_airborne_thread_vector_stores" {
+        t.Fatalf("tenant vectorStoresTable = %q", tenant.vectorStoresTable())
+    }
+}
+
+func TestClient_TenantRepository_Caches(t *testing.T) {
+    client := &Client{tenantRepos: make(map[string]*Repository)}
+
+    first, err := client.TenantRepository("ai8")
+    if err != nil {
+        t.Fatalf("TenantRepository(first): %v", err)
+    }
+    second, err := client.TenantRepository("ai8")
+    if err != nil {
+        t.Fatalf("TenantRepository(second): %v", err)
+    }
+
+    if first != second {
+        t.Fatalf("TenantRepository did not cache instance")
+    }
+}
+
+func TestWriteCACertToFile(t *testing.T) {
+    cert := "-----BEGIN CERTIFICATE-----\nTEST\n-----END CERTIFICATE-----\n"
+    certPath := filepath.Join("/tmp/airborne-certs", "supabase-ca.crt")
+
+    original, _ := os.ReadFile(certPath)
+    if original != nil {
+        defer os.WriteFile(certPath, original, 0600)
+    } else {
+        defer os.Remove(certPath)
+    }
+
+    path, err := writeCACertToFile(cert)
+    if err != nil {
+        t.Fatalf("writeCACertToFile: %v", err)
+    }
+    if path != certPath {
+        t.Fatalf("cert path = %q, want %q", path, certPath)
+    }
+
+    data, err := os.ReadFile(path)
+    if err != nil {
+        t.Fatalf("read cert: %v", err)
+    }
+    if string(data) != cert {
+        t.Fatalf("cert content mismatch")
+    }
+}
```

- `internal/admin/server_test.go`
```diff
diff --git a/internal/admin/server_test.go b/internal/admin/server_test.go
new file mode 100644
index 0000000..3e46b0d
--- /dev/null
+++ b/internal/admin/server_test.go
@@ -0,0 +1,271 @@
+package admin
+
+import (
+    "context"
+    "encoding/json"
+    "errors"
+    "net/http"
+    "net/http/httptest"
+    "strings"
+    "testing"
+
+    pb "github.com/ai8future/airborne/gen/go/airborne/v1"
+    "github.com/google/uuid"
+    "google.golang.org/grpc"
+    "google.golang.org/grpc/metadata"
+)
+
+type fakeGRPCClient struct {
+    lastCtx context.Context
+    lastReq *pb.GenerateReplyRequest
+    resp    *pb.GenerateReplyResponse
+    err     error
+}
+
+func (f *fakeGRPCClient) GenerateReply(ctx context.Context, in *pb.GenerateReplyRequest, opts ...grpc.CallOption) (*pb.GenerateReplyResponse, error) {
+    f.lastCtx = ctx
+    f.lastReq = in
+    if f.err != nil {
+        return nil, f.err
+    }
+    return f.resp, nil
+}
+
+func (f *fakeGRPCClient) GenerateReplyStream(ctx context.Context, in *pb.GenerateReplyRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.GenerateReplyChunk], error) {
+    return nil, errors.New("not implemented")
+}
+
+func (f *fakeGRPCClient) SelectProvider(ctx context.Context, in *pb.SelectProviderRequest, opts ...grpc.CallOption) (*pb.SelectProviderResponse, error) {
+    return nil, errors.New("not implemented")
+}
+
+func TestHandleHealth_NoDB(t *testing.T) {
+    srv := &Server{}
+    rr := httptest.NewRecorder()
+    req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
+
+    srv.handleHealth(rr, req)
+
+    if rr.Code != http.StatusOK {
+        t.Fatalf("status = %d, want 200", rr.Code)
+    }
+
+    var resp map[string]string
+    if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
+        t.Fatalf("decode: %v", err)
+    }
+    if resp["status"] != "healthy" {
+        t.Fatalf("status = %q", resp["status"])
+    }
+    if resp["database"] != "not_configured" {
+        t.Fatalf("database = %q", resp["database"])
+    }
+}
+
+func TestHandleActivity_NoDB(t *testing.T) {
+    srv := &Server{}
+    rr := httptest.NewRecorder()
+    req := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
+
+    srv.handleActivity(rr, req)
+
+    if rr.Code != http.StatusOK {
+        t.Fatalf("status = %d, want 200", rr.Code)
+    }
+
+    var resp map[string]interface{}
+    if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
+        t.Fatalf("decode: %v", err)
+    }
+    if resp["error"] != "database not configured" {
+        t.Fatalf("error = %v", resp["error"])
+    }
+}
+
+func TestHandleDebug_Validation(t *testing.T) {
+    srv := &Server{}
+
+    rr := httptest.NewRecorder()
+    req := httptest.NewRequest(http.MethodGet, "/admin/debug/", nil)
+    srv.handleDebug(rr, req)
+    if rr.Code != http.StatusBadRequest {
+        t.Fatalf("missing id status = %d", rr.Code)
+    }
+
+    rr = httptest.NewRecorder()
+    req = httptest.NewRequest(http.MethodGet, "/admin/debug/not-a-uuid", nil)
+    srv.handleDebug(rr, req)
+    if rr.Code != http.StatusBadRequest {
+        t.Fatalf("invalid id status = %d", rr.Code)
+    }
+
+    rr = httptest.NewRecorder()
+    req = httptest.NewRequest(http.MethodGet, "/admin/debug/"+uuid.New().String(), nil)
+    srv.handleDebug(rr, req)
+    if rr.Code != http.StatusServiceUnavailable {
+        t.Fatalf("no db status = %d", rr.Code)
+    }
+}
+
+func TestHandleThread_Validation(t *testing.T) {
+    srv := &Server{}
+
+    rr := httptest.NewRecorder()
+    req := httptest.NewRequest(http.MethodGet, "/admin/thread/", nil)
+    srv.handleThread(rr, req)
+    if rr.Code != http.StatusBadRequest {
+        t.Fatalf("missing id status = %d", rr.Code)
+    }
+
+    rr = httptest.NewRecorder()
+    req = httptest.NewRequest(http.MethodGet, "/admin/thread/not-a-uuid", nil)
+    srv.handleThread(rr, req)
+    if rr.Code != http.StatusBadRequest {
+        t.Fatalf("invalid id status = %d", rr.Code)
+    }
+
+    rr = httptest.NewRecorder()
+    req = httptest.NewRequest(http.MethodGet, "/admin/thread/"+uuid.New().String(), nil)
+    srv.handleThread(rr, req)
+    if rr.Code != http.StatusServiceUnavailable {
+        t.Fatalf("no db status = %d", rr.Code)
+    }
+}
+
+func TestHandleVersion(t *testing.T) {
+    srv := &Server{version: VersionInfo{Version: "1.2.3", GitCommit: "abc", BuildTime: "now"}}
+    rr := httptest.NewRecorder()
+    req := httptest.NewRequest(http.MethodGet, "/admin/version", nil)
+
+    srv.handleVersion(rr, req)
+
+    if rr.Code != http.StatusOK {
+        t.Fatalf("status = %d", rr.Code)
+    }
+
+    var resp VersionInfo
+    if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
+        t.Fatalf("decode: %v", err)
+    }
+    if resp.Version != "1.2.3" {
+        t.Fatalf("version = %q", resp.Version)
+    }
+}
+
+func TestHandleTest_Validation(t *testing.T) {
+    srv := &Server{}
+
+    rr := httptest.NewRecorder()
+    req := httptest.NewRequest(http.MethodPost, "/admin/test", strings.NewReader("{"))
+    srv.handleTest(rr, req)
+    if rr.Code != http.StatusBadRequest {
+        t.Fatalf("invalid json status = %d", rr.Code)
+    }
+
+    rr = httptest.NewRecorder()
+    req = httptest.NewRequest(http.MethodPost, "/admin/test", strings.NewReader(`{"prompt":""}`))
+    srv.handleTest(rr, req)
+    if rr.Code != http.StatusBadRequest {
+        t.Fatalf("missing prompt status = %d", rr.Code)
+    }
+}
+
+func TestHandleTest_GRPCRequestAndAuth(t *testing.T) {
+    fake := &fakeGRPCClient{resp: &pb.GenerateReplyResponse{
+        Text:     "ok",
+        Provider: pb.Provider_PROVIDER_OPENAI,
+        Model:    "gpt-4o",
+        Usage:    &pb.Usage{InputTokens: 3, OutputTokens: 4},
+    }}
+    srv := &Server{grpcClient: fake, authToken: "token"}
+
+    rr := httptest.NewRecorder()
+    req := httptest.NewRequest(http.MethodPost, "/admin/test", strings.NewReader(`{"prompt":"hi","provider":"openai"}`))
+    srv.handleTest(rr, req)
+
+    if rr.Code != http.StatusOK {
+        t.Fatalf("status = %d", rr.Code)
+    }
+    if fake.lastReq == nil {
+        t.Fatalf("grpc request not captured")
+    }
+    if fake.lastReq.PreferredProvider != pb.Provider_PROVIDER_OPENAI {
+        t.Fatalf("PreferredProvider = %v", fake.lastReq.PreferredProvider)
+    }
+
+    md, ok := metadata.FromOutgoingContext(fake.lastCtx)
+    if !ok {
+        t.Fatalf("missing outgoing metadata")
+    }
+    if got := md.Get("authorization"); len(got) == 0 || got[0] != "Bearer token" {
+        t.Fatalf("authorization header missing")
+    }
+
+    var resp TestResponse
+    if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
+        t.Fatalf("decode: %v", err)
+    }
+    if resp.Provider != "openai" {
+        t.Fatalf("provider = %q", resp.Provider)
+    }
+    if resp.InputTokens != 3 || resp.OutputTokens != 4 {
+        t.Fatalf("token usage = %d/%d", resp.InputTokens, resp.OutputTokens)
+    }
+}
+
+func TestHandleChat_Validation(t *testing.T) {
+    srv := &Server{}
+
+    rr := httptest.NewRecorder()
+    req := httptest.NewRequest(http.MethodPost, "/admin/chat", strings.NewReader(`{"thread_id":"","message":"hi"}`))
+    srv.handleChat(rr, req)
+    if rr.Code != http.StatusBadRequest {
+        t.Fatalf("missing thread_id status = %d", rr.Code)
+    }
+
+    rr = httptest.NewRecorder()
+    req = httptest.NewRequest(http.MethodPost, "/admin/chat", strings.NewReader(`{"thread_id":"not-a-uuid","message":"hi"}`))
+    srv.handleChat(rr, req)
+    if rr.Code != http.StatusBadRequest {
+        t.Fatalf("invalid thread_id status = %d", rr.Code)
+    }
+
+    rr = httptest.NewRecorder()
+    req = httptest.NewRequest(http.MethodPost, "/admin/chat", strings.NewReader(`{"thread_id":"`+uuid.New().String()+`","message":""}`))
+    srv.handleChat(rr, req)
+    if rr.Code != http.StatusBadRequest {
+        t.Fatalf("missing message status = %d", rr.Code)
+    }
+}
+
+func TestHandleChat_DefaultsAndProvider(t *testing.T) {
+    fake := &fakeGRPCClient{resp: &pb.GenerateReplyResponse{
+        ResponseId: "resp-1",
+        Text:       "ok",
+        Provider:   pb.Provider_PROVIDER_GEMINI,
+        Model:      "gemini-1.5-pro",
+        Usage:      &pb.Usage{InputTokens: 1, OutputTokens: 2},
+    }}
+    srv := &Server{grpcClient: fake}
+
+    threadID := uuid.New().String()
+    rr := httptest.NewRecorder()
+    req := httptest.NewRequest(http.MethodPost, "/admin/chat", strings.NewReader(`{"thread_id":"`+threadID+`","message":"hi"}`))
+    srv.handleChat(rr, req)
+
+    if rr.Code != http.StatusOK {
+        t.Fatalf("status = %d", rr.Code)
+    }
+    if fake.lastReq == nil {
+        t.Fatalf("grpc request not captured")
+    }
+    if fake.lastReq.RequestId != threadID {
+        t.Fatalf("RequestId = %q", fake.lastReq.RequestId)
+    }
+    if fake.lastReq.PreferredProvider != pb.Provider_PROVIDER_GEMINI {
+        t.Fatalf("PreferredProvider = %v", fake.lastReq.PreferredProvider)
+    }
+    if fake.lastReq.Instructions == "" {
+        t.Fatalf("Instructions default missing")
+    }
+    if fake.lastReq.PreviousResponseId != "" {
+        t.Fatalf("PreviousResponseId should be empty")
+    }
+
+    var resp ChatResponse
+    if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
+        t.Fatalf("decode: %v", err)
+    }
+    if resp.ID != "resp-1" {
+        t.Fatalf("ResponseId = %q", resp.ID)
+    }
+}
```

- `internal/provider/compat_providers_test.go`
```diff
diff --git a/internal/provider/compat_providers_test.go b/internal/provider/compat_providers_test.go
new file mode 100644
index 0000000..fd9135d
--- /dev/null
+++ b/internal/provider/compat_providers_test.go
@@ -0,0 +1,73 @@
+package provider_test
+
+import (
+    "testing"
+
+    "github.com/ai8future/airborne/internal/provider"
+    "github.com/ai8future/airborne/internal/provider/cerebras"
+    "github.com/ai8future/airborne/internal/provider/cohere"
+    "github.com/ai8future/airborne/internal/provider/deepinfra"
+    "github.com/ai8future/airborne/internal/provider/deepseek"
+    "github.com/ai8future/airborne/internal/provider/fireworks"
+    "github.com/ai8future/airborne/internal/provider/grok"
+    "github.com/ai8future/airborne/internal/provider/hyperbolic"
+    "github.com/ai8future/airborne/internal/provider/nebius"
+    "github.com/ai8future/airborne/internal/provider/openrouter"
+    "github.com/ai8future/airborne/internal/provider/perplexity"
+    "github.com/ai8future/airborne/internal/provider/together"
+    "github.com/ai8future/airborne/internal/provider/upstage"
+)
+
+func TestCompatProviders_Config(t *testing.T) {
+    cases := []struct {
+        name       string
+        newClient  func() provider.Provider
+        webSearch  bool
+    }{
+        {"cerebras", func() provider.Provider { return cerebras.NewClient() }, false},
+        {"cohere", func() provider.Provider { return cohere.NewClient() }, true},
+        {"deepinfra", func() provider.Provider { return deepinfra.NewClient() }, false},
+        {"deepseek", func() provider.Provider { return deepseek.NewClient() }, false},
+        {"fireworks", func() provider.Provider { return fireworks.NewClient() }, false},
+        {"grok", func() provider.Provider { return grok.NewClient() }, false},
+        {"hyperbolic", func() provider.Provider { return hyperbolic.NewClient() }, false},
+        {"nebius", func() provider.Provider { return nebius.NewClient() }, false},
+        {"openrouter", func() provider.Provider { return openrouter.NewClient() }, false},
+        {"perplexity", func() provider.Provider { return perplexity.NewClient() }, true},
+        {"together", func() provider.Provider { return together.NewClient() }, false},
+        {"upstage", func() provider.Provider { return upstage.NewClient() }, false},
+    }
+
+    for _, tc := range cases {
+        t.Run(tc.name, func(t *testing.T) {
+            client := tc.newClient()
+            if client.Name() != tc.name {
+                t.Fatalf("Name = %q, want %q", client.Name(), tc.name)
+            }
+            if client.SupportsFileSearch() {
+                t.Fatalf("SupportsFileSearch should be false")
+            }
+            if client.SupportsWebSearch() != tc.webSearch {
+                t.Fatalf("SupportsWebSearch = %v, want %v", client.SupportsWebSearch(), tc.webSearch)
+            }
+            if !client.SupportsStreaming() {
+                t.Fatalf("SupportsStreaming should be true")
+            }
+            if client.SupportsNativeContinuity() {
+                t.Fatalf("SupportsNativeContinuity should be false")
+            }
+        })
+    }
+}
```
