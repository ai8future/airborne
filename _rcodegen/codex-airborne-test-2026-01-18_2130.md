Date Created: 2026-01-18 21:30:39 +0100
TOTAL_SCORE: 74/100

## Scope (quick pass)
- Focused scan on untested Go packages with concrete logic: `internal/pricing`, `internal/db/models`, `internal/admin`, `internal/tenant/doppler`.
- Skipped deep integration-heavy areas (DB repository + provider filestore HTTP workflows) beyond light notes to keep time under the 10% cap.

## Overall Grade Rationale (74/100)
- Strong baseline coverage in most service/provider code and validation/rag/redis/httpcapture.
- Several high-value modules remain untested: pricing, admin HTTP handlers, tenant Doppler client, DB models and DB repository. These have production-facing behavior and error handling that should be exercised.
- Missing tests are mostly unit-testable without external services, so coverage gap is fixable without big refactors.

## Untested / Under-tested Areas
- `internal/pricing/pricing.go`: parsing, prefix matching, cost calculation, lazy init behavior.
- `internal/db/models.go`: citation JSON helpers, thread/message constructors, truncation helpers.
- `internal/admin/server.go`: handler input validation, error responses, gRPC request mapping.
- `internal/tenant/doppler.go`: retry logic, caching, Doppler API parsing.
- `internal/db/repository.go`: requires integration harness or a small abstraction to enable pgx pool mocking (no unit tests currently).
- `internal/provider/openai/filestore.go` + `internal/provider/gemini/filestore.go`: HTTP workflows and polling logic lack tests; would benefit from httptest servers.

## Proposed Unit Tests (short list)
- Pricing: build pricer from temp JSON configs, validate provider inference, prefix matching, unknown-model behavior, and package-level `Init` + `CalculateCost`.
- DB models: round-trip citation JSON, pointer safety, constructor defaults, and truncation logic.
- Admin server: handler method validation, nil-repo behavior, and gRPC request wiring (provider selection, prompt required, error handling).
- Doppler client: retry/backoff decisions, cache hits, and JSON parsing on success/error paths.

## Patch-ready diffs

```diff
diff --git a/internal/pricing/pricing_test.go b/internal/pricing/pricing_test.go
new file mode 100644
index 0000000..5b1a909
--- /dev/null
+++ b/internal/pricing/pricing_test.go
@@ -0,0 +1,150 @@
+package pricing
+
+import (
+\t"math"
+\t"os"
+\t"path/filepath"
+\t"reflect"
+\t"sort"
+\t"sync"
+\t"testing"
+)
+
+func writePricingFile(t *testing.T, dir, name, content string) {
+\tt.Helper()
+\tpath := filepath.Join(dir, name)
+\tif err := os.WriteFile(path, []byte(content), 0o644); err != nil {
+\t\tt.Fatalf("write pricing file: %v", err)
+\t}
+}
+
+func resetPricingGlobals() {
+\tdefaultPricer = nil
+\tinitErr = nil
+\tinitOnce = sync.Once{}
+}
+
+func TestNewPricer_NoFiles(t *testing.T) {
+\tpricer, err := NewPricer(t.TempDir())
+\tif err == nil {
+\t\tt.Fatalf("expected error, got nil")
+\t}
+\tif pricer != nil {
+\t\tt.Fatalf("expected nil pricer on error")
+\t}
+}
+
+func TestNewPricer_LoadsProvidersAndCalculates(t *testing.T) {
+\tdir := t.TempDir()
+\twritePricingFile(t, dir, "openai_pricing.json", `{
+  "provider": "openai",
+  "models": {
+    "gpt-4o": {"input_per_million": 5, "output_per_million": 15},
+    "gpt-4o-mini": {"input_per_million": 0.15, "output_per_million": 0.6}
+  },
+  "metadata": {"updated": "2026-01-01"}
+}`)
+\twritePricingFile(t, dir, "custom_pricing.json", `{
+  "models": {
+    "custom-model": {"input_per_million": 1, "output_per_million": 2}
+  }
+}`)
+
+\tpricer, err := NewPricer(dir)
+\tif err != nil {
+\t\tt.Fatalf("NewPricer: %v", err)
+\t}
+
+\tif pricer.ModelCount() != 3 {
+\t\tt.Fatalf("ModelCount() = %d, want 3", pricer.ModelCount())
+\t}
+
+\tproviders := pricer.ListProviders()
+\tsort.Strings(providers)
+\twantProviders := []string{"custom", "openai"}
+\tif !reflect.DeepEqual(providers, wantProviders) {
+\t\tt.Fatalf("ListProviders() = %v, want %v", providers, wantProviders)
+\t}
+
+\tcost := pricer.Calculate("gpt-4o", 1000, 2000)
+\tif cost.Unknown {
+\t\tt.Fatal("Calculate returned Unknown for known model")
+\t}
+\texpected := (1000.0*5 + 2000.0*15) / 1_000_000.0
+\tif math.Abs(cost.TotalCost-expected) > 1e-9 {
+\t\tt.Fatalf("TotalCost = %f, want %f", cost.TotalCost, expected)
+\t}
+
+\tprefix := pricer.Calculate("gpt-4o-2024-08-06", 1_000_000, 0)
+\tif prefix.InputCost != 5 {
+\t\tt.Fatalf("prefix InputCost = %f, want 5", prefix.InputCost)
+\t}
+
+\tunknown := pricer.Calculate("unknown-model", 1, 1)
+\tif !unknown.Unknown {
+\t\tt.Fatal("expected Unknown for unknown model")
+\t}
+\tif unknown.TotalCost != 0 {
+\t\tt.Fatalf("unknown TotalCost = %f, want 0", unknown.TotalCost)
+\t}
+
+\tif _, ok := pricer.GetPricing("gpt-4o-2024-08-06"); !ok {
+\t\tt.Fatal("GetPricing should match by prefix")
+\t}
+}
+
+func TestInitAndCalculateCost(t *testing.T) {
+\tresetPricingGlobals()
+\tt.Cleanup(resetPricingGlobals)
+
+\tdir := t.TempDir()
+\twritePricingFile(t, dir, "openai_pricing.json", `{
+  "provider": "openai",
+  "models": {
+    "gpt-4o": {"input_per_million": 5, "output_per_million": 15}
+  }
+}`)
+
+\tif err := Init(dir); err != nil {
+\t\tt.Fatalf("Init: %v", err)
+\t}
+
+\tif got := CalculateCost("gpt-4o", 1_000_000, 0); got != 5 {
+\t\tt.Fatalf("CalculateCost = %f, want 5", got)
+\t}
+}
```

```diff
diff --git a/internal/db/models_test.go b/internal/db/models_test.go
new file mode 100644
index 0000000..71e8b3d
--- /dev/null
+++ b/internal/db/models_test.go
@@ -0,0 +1,182 @@
+package db
+
+import (
+\t"encoding/json"
+\t"testing"
+
+\t"github.com/google/uuid"
+)
+
+func strPtr(s string) *string {
+\treturn &s
+}
+
+func TestParseCitations(t *testing.T) {
+\tvalid := `[{"type":"url","url":"https://example.com","title":"Example","snippet":"hello"}]`
+
+\ttests := []struct {
+\t\tname    string
+\t\tinput   *string
+\t\twantLen int
+\t\twantErr bool
+\t}{
+\t\t{"nil", nil, 0, false},
+\t\t{"empty", strPtr(""), 0, false},
+\t\t{"valid", strPtr(valid), 1, false},
+\t\t{"invalid", strPtr("{bad}"), 0, true},
+\t}
+
+\tfor _, tt := range tests {
+\t\tt.Run(tt.name, func(t *testing.T) {
+\t\t\tgot, err := ParseCitations(tt.input)
+\t\t\tif (err != nil) != tt.wantErr {
+\t\t\t\tt.Fatalf("ParseCitations error = %v, wantErr %v", err, tt.wantErr)
+\t\t\t}
+\t\t\tif len(got) != tt.wantLen {
+\t\t\t\tt.Fatalf("ParseCitations len = %d, want %d", len(got), tt.wantLen)
+\t\t\t}
+\t\t})
+\t}
+}
+
+func TestCitationsToJSON(t *testing.T) {
+\tif got, err := CitationsToJSON(nil); err != nil || got != nil {
+\t\tt.Fatalf("CitationsToJSON(nil) = %v, %v", got, err)
+\t}
+
+\tcitations := []Citation{{
+\t\tType:    "url",
+\t\tURL:     "https://example.com",
+\t\tTitle:   "Example",
+\t\tSnippet: "hello",
+\t}}
+
+\tjsonStr, err := CitationsToJSON(citations)
+\tif err != nil {
+\t\tt.Fatalf("CitationsToJSON: %v", err)
+\t}
+\tif jsonStr == nil || *jsonStr == "" {
+\t\tt.Fatal("expected non-empty JSON string")
+\t}
+
+\tvar roundTrip []Citation
+\tif err := json.Unmarshal([]byte(*jsonStr), &roundTrip); err != nil {
+\t\tt.Fatalf("unmarshal: %v", err)
+\t}
+\tif len(roundTrip) != 1 || roundTrip[0].URL != citations[0].URL {
+\t\tt.Fatalf("round-trip mismatch: %+v", roundTrip)
+\t}
+}
+
+func TestNewThread(t *testing.T) {
+\tthread := NewThread("tenant", "user")
+\tif thread.ID == uuid.Nil {
+\t\tt.Fatal("expected non-nil thread ID")
+\t}
+\tif thread.TenantID != "tenant" || thread.UserID != "user" {
+\t\tt.Fatalf("unexpected tenant/user: %s/%s", thread.TenantID, thread.UserID)
+\t}
+\tif thread.Status != ThreadStatusActive {
+\t\tt.Fatalf("status = %s, want %s", thread.Status, ThreadStatusActive)
+\t}
+\tif thread.CreatedAt.IsZero() || thread.UpdatedAt.IsZero() {
+\t\tt.Fatal("expected timestamps to be set")
+\t}
+}
+
+func TestNewMessage(t *testing.T) {
+\tthreadID := uuid.New()
+\tmsg := NewMessage(threadID, RoleUser, "hello")
+\tif msg.ID == uuid.Nil {
+\t\tt.Fatal("expected non-nil message ID")
+\t}
+\tif msg.ThreadID != threadID {
+\t\tt.Fatalf("thread ID = %s, want %s", msg.ThreadID, threadID)
+\t}
+\tif msg.Role != RoleUser || msg.Content != "hello" {
+\t\tt.Fatalf("unexpected role/content: %s/%s", msg.Role, msg.Content)
+\t}
+\tif msg.CreatedAt.IsZero() {
+\t\tt.Fatal("expected CreatedAt to be set")
+\t}
+}
+
+func TestSetAssistantMetrics(t *testing.T) {
+\tmsg := &Message{}
+\tmsg.SetAssistantMetrics("openai", "gpt-4o", 10, 20, 30, 0.25, "resp")
+
+\tif msg.Provider == nil || *msg.Provider != "openai" {
+\t\tt.Fatalf("Provider = %v", msg.Provider)
+\t}
+\tif msg.Model == nil || *msg.Model != "gpt-4o" {
+\t\tt.Fatalf("Model = %v", msg.Model)
+\t}
+\tif msg.TotalTokens == nil || *msg.TotalTokens != 30 {
+\t\tt.Fatalf("TotalTokens = %v", msg.TotalTokens)
+\t}
+\tif msg.ResponseID == nil || *msg.ResponseID != "resp" {
+\t\tt.Fatalf("ResponseID = %v", msg.ResponseID)
+\t}
+
+\tmsg = &Message{}
+\tmsg.SetAssistantMetrics("openai", "gpt-4o", 1, 2, 3, 0.1, "")
+\tif msg.ResponseID != nil {
+\t\tt.Fatalf("expected nil ResponseID for empty input, got %v", msg.ResponseID)
+\t}
+}
+
+func TestTruncateContent(t *testing.T) {
+\tmsg := &Message{Content: "hello world"}
+\tif got := msg.TruncateContent(20); got != "hello world" {
+\t\tt.Fatalf("TruncateContent = %q, want %q", got, "hello world")
+\t}
+\tif got := msg.TruncateContent(5); got != "hello..." {
+\t\tt.Fatalf("TruncateContent = %q, want %q", got, "hello...")
+\t}
+}
```

```diff
diff --git a/internal/admin/server_test.go b/internal/admin/server_test.go
new file mode 100644
index 0000000..cc3f813
--- /dev/null
+++ b/internal/admin/server_test.go
@@ -0,0 +1,220 @@
+package admin
+
+import (
+\t"context"
+\t"encoding/json"
+\t"errors"
+\t"net/http"
+\t"net/http/httptest"
+\t"strings"
+\t"testing"
+
+\tpb "github.com/ai8future/airborne/gen/go/airborne/v1"
+\t"google.golang.org/grpc"
+)
+
+type fakeAirborneClient struct {
+\tlastReq *pb.GenerateReplyRequest
+\tresp    *pb.GenerateReplyResponse
+\terr     error
+}
+
+func (f *fakeAirborneClient) GenerateReply(ctx context.Context, in *pb.GenerateReplyRequest, opts ...grpc.CallOption) (*pb.GenerateReplyResponse, error) {
+\tf.lastReq = in
+\treturn f.resp, f.err
+}
+
+func (f *fakeAirborneClient) GenerateReplyStream(ctx context.Context, in *pb.GenerateReplyRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.GenerateReplyChunk], error) {
+\treturn nil, errors.New("not implemented")
+}
+
+func (f *fakeAirborneClient) SelectProvider(ctx context.Context, in *pb.SelectProviderRequest, opts ...grpc.CallOption) (*pb.SelectProviderResponse, error) {
+\treturn nil, errors.New("not implemented")
+}
+
+func TestHandleActivity_NoRepo(t *testing.T) {
+\ts := NewServer(nil, Config{Port: 0})
+\treq := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
+\trr := httptest.NewRecorder()
+
+\ts.handleActivity(rr, req)
+
+\tif rr.Code != http.StatusOK {
+\t\tt.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
+\t}
+
+\tvar payload struct {
+\t\tActivity []interface{} `json:"activity"`
+\t\tError    string        `json:"error"`
+\t}
+\tif err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
+\t\tt.Fatalf("decode: %v", err)
+\t}
+\tif payload.Error == "" {
+\t\tt.Fatal("expected error when repo is nil")
+\t}
+\tif len(payload.Activity) != 0 {
+\t\tt.Fatalf("expected empty activity, got %d", len(payload.Activity))
+\t}
+}
+
+func TestHandleActivity_MethodNotAllowed(t *testing.T) {
+\ts := NewServer(nil, Config{Port: 0})
+\treq := httptest.NewRequest(http.MethodPost, "/admin/activity", nil)
+\trr := httptest.NewRecorder()
+
+\ts.handleActivity(rr, req)
+
+\tif rr.Code != http.StatusMethodNotAllowed {
+\t\tt.Fatalf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
+\t}
+}
+
+func TestHandleHealth_NoRepo(t *testing.T) {
+\ts := NewServer(nil, Config{Port: 0})
+\treq := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
+\trr := httptest.NewRecorder()
+
+\ts.handleHealth(rr, req)
+
+\tif rr.Code != http.StatusOK {
+\t\tt.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
+\t}
+
+\tvar payload struct {
+\t\tStatus   string `json:"status"`
+\t\tDatabase string `json:"database"`
+\t}
+\tif err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
+\t\tt.Fatalf("decode: %v", err)
+\t}
+\tif payload.Status != "healthy" || payload.Database != "not_configured" {
+\t\tt.Fatalf("unexpected health payload: %+v", payload)
+\t}
+}
+
+func TestHandleDebug_Errors(t *testing.T) {
+\ttests := []struct {
+\t\tname       string
+\t\tpath       string
+\t\twantStatus int
+\t}{
+\t\t{"missing id", "/admin/debug/", http.StatusBadRequest},
+\t\t{"invalid id", "/admin/debug/not-a-uuid", http.StatusBadRequest},
+\t\t{"no repo", "/admin/debug/11111111-1111-1111-1111-111111111111", http.StatusServiceUnavailable},
+\t}
+
+\tfor _, tt := range tests {
+\t\tt.Run(tt.name, func(t *testing.T) {
+\t\t\ts := NewServer(nil, Config{Port: 0})
+\t\t\treq := httptest.NewRequest(http.MethodGet, tt.path, nil)
+\t\t\trr := httptest.NewRecorder()
+
+\t\t\ts.handleDebug(rr, req)
+
+\t\t\tif rr.Code != tt.wantStatus {
+\t\t\t\tt.Fatalf("status = %d, want %d", rr.Code, tt.wantStatus)
+\t\t\t}
+\t\t})
+\t}
+}
+
+func TestHandleTest_Errors(t *testing.T) {
+\ttests := []struct {
+\t\tname       string
+\t\tmethod     string
+\t\tbody       string
+\t\tsetup      func(*Server)
+\t\twantStatus int
+\t}{
+\t\t{"method not allowed", http.MethodGet, "", nil, http.StatusMethodNotAllowed},
+\t\t{"bad json", http.MethodPost, "{", nil, http.StatusBadRequest},
+\t\t{"missing prompt", http.MethodPost, `{"prompt":""}`, nil, http.StatusBadRequest},
+\t\t{"grpc not configured", http.MethodPost, `{"prompt":"hello"}`, nil, http.StatusServiceUnavailable},
+\t}
+
+\tfor _, tt := range tests {
+\t\tt.Run(tt.name, func(t *testing.T) {
+\t\t\ts := NewServer(nil, Config{Port: 0})
+\t\t\tif tt.setup != nil {
+\t\t\t\ttt.setup(s)
+\t\t\t}
+
+\t\t\treq := httptest.NewRequest(tt.method, "/admin/test", strings.NewReader(tt.body))
+\t\t\trr := httptest.NewRecorder()
+
+\t\t\ts.handleTest(rr, req)
+
+\t\t\tif rr.Code != tt.wantStatus {
+\t\t\t\tt.Fatalf("status = %d, want %d", rr.Code, tt.wantStatus)
+\t\t\t}
+\t\t})
+\t}
+}
+
+func TestHandleTest_Success(t *testing.T) {
+\ts := NewServer(nil, Config{Port: 0})
+\tfake := &fakeAirborneClient{resp: &pb.GenerateReplyResponse{
+\t\tText:     "pong",
+\t\tModel:    "gpt-4o",
+\t\tProvider: pb.Provider_PROVIDER_OPENAI,
+\t\tUsage: &pb.Usage{
+\t\t\tInputTokens:  11,
+\t\t\tOutputTokens: 22,
+\t\t},
+\t}}
+\ts.grpcClient = fake
+
+\tbody := `{"prompt":"ping","tenant_id":"tenant","provider":"openai"}`
+\treq := httptest.NewRequest(http.MethodPost, "/admin/test", strings.NewReader(body))
+\trr := httptest.NewRecorder()
+
+\ts.handleTest(rr, req)
+
+\tif rr.Code != http.StatusOK {
+\t\tt.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
+\t}
+
+\tvar resp TestResponse
+\tif err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
+\t\tt.Fatalf("decode: %v", err)
+\t}
+\tif resp.Reply != "pong" || resp.Provider != "openai" || resp.Model != "gpt-4o" {
+\t\tt.Fatalf("unexpected response: %+v", resp)
+\t}
+\tif resp.InputTokens != 11 || resp.OutputTokens != 22 {
+\t\tt.Fatalf("unexpected token usage: %+v", resp)
+\t}
+
+\tif fake.lastReq == nil {
+\t\tt.Fatal("expected GenerateReply to be called")
+\t}
+\tif fake.lastReq.PreferredProvider != pb.Provider_PROVIDER_OPENAI {
+\t\tt.Fatalf("PreferredProvider = %v, want openai", fake.lastReq.PreferredProvider)
+\t}
+\tif fake.lastReq.UserInput != "ping" {
+\t\tt.Fatalf("UserInput = %q", fake.lastReq.UserInput)
+\t}
+\tif fake.lastReq.RequestId == "" {
+\t\tt.Fatal("expected RequestId to be populated")
+\t}
+}
```

```diff
diff --git a/internal/tenant/doppler_test.go b/internal/tenant/doppler_test.go
new file mode 100644
index 0000000..70ec9c0
--- /dev/null
+++ b/internal/tenant/doppler_test.go
@@ -0,0 +1,195 @@
+package tenant
+
+import (
+\t"io"
+\t"net/http"
+\t"strings"
+\t"sync/atomic"
+\t"testing"
+)
+
+type roundTripFunc func(*http.Request) (*http.Response, error)
+
+func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
+\treturn f(req)
+}
+
+func TestIsRetryableError(t *testing.T) {
+\tif !isRetryableError(500) {
+\t\tt.Fatal("expected 500 to be retryable")
+\t}
+\tif !isRetryableError(429) {
+\t\tt.Fatal("expected 429 to be retryable")
+\t}
+\tif isRetryableError(404) {
+\t\tt.Fatal("expected 404 to be non-retryable")
+\t}
+}
+
+func TestDopplerClientDoFetch_Success(t *testing.T) {
+\tclient := &dopplerClient{
+\t\ttoken:  "token",
+\t\tconfig: "prod",
+\t\thttpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
+\t\t\tif !strings.Contains(req.URL.String(), "project=testproj") {
+\t\t\t\tt.Fatalf("unexpected URL: %s", req.URL.String())
+\t\t\t}
+\t\t\tuser, _, ok := req.BasicAuth()
+\t\t\tif !ok || user != "token" {
+\t\t\t\tt.Fatalf("unexpected basic auth: %v", ok)
+\t\t\t}
+\t\t\tbody := `{"secrets":{"API_KEY":{"raw":"VALUE"}}}`
+\t\t\treturn &http.Response{
+\t\t\t\tStatusCode: http.StatusOK,
+\t\t\t\tBody:       io.NopCloser(strings.NewReader(body)),
+\t\t\t\tHeader:     make(http.Header),
+\t\t\t}, nil
+\t\t})},
+\t\tcache: make(map[string]map[string]string),
+\t}
+
+\tsecrets, status, err := client.doFetch("testproj")
+\tif err != nil {
+\t\tt.Fatalf("doFetch: %v", err)
+\t}
+\tif status != http.StatusOK {
+\t\tt.Fatalf("status = %d, want %d", status, http.StatusOK)
+\t}
+\tif secrets["API_KEY"] != "VALUE" {
+\t\tt.Fatalf("unexpected secrets: %v", secrets)
+\t}
+}
+
+func TestDopplerClientDoFetch_ErrorStatus(t *testing.T) {
+\tclient := &dopplerClient{
+\t\ttoken:  "token",
+\t\tconfig: "prod",
+\t\thttpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
+\t\t\tbody := "boom"
+\t\t\treturn &http.Response{
+\t\t\t\tStatusCode: http.StatusInternalServerError,
+\t\t\t\tBody:       io.NopCloser(strings.NewReader(body)),
+\t\t\t\tHeader:     make(http.Header),
+\t\t\t}, nil
+\t\t})},
+\t\tcache: make(map[string]map[string]string),
+\t}
+
+\t_, status, err := client.doFetch("testproj")
+\tif err == nil {
+\t\tt.Fatal("expected error for non-200 status")
+\t}
+\tif status != http.StatusInternalServerError {
+\t\tt.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
+\t}
+}
+
+func TestDopplerClientFetchProjectSecrets_Caches(t *testing.T) {
+\tvar calls int32
+\tclient := &dopplerClient{
+\t\ttoken:  "token",
+\t\tconfig: "prod",
+\t\thttpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
+\t\t\tatomic.AddInt32(&calls, 1)
+\t\t\tbody := `{"secrets":{"API_KEY":{"raw":"VALUE"}}}`
+\t\t\treturn &http.Response{
+\t\t\t\tStatusCode: http.StatusOK,
+\t\t\t\tBody:       io.NopCloser(strings.NewReader(body)),
+\t\t\t\tHeader:     make(http.Header),
+\t\t\t}, nil
+\t\t})},
+\t\tcache: make(map[string]map[string]string),
+\t}
+
+\tif _, err := client.fetchProjectSecrets("testproj"); err != nil {
+\t\tt.Fatalf("fetchProjectSecrets: %v", err)
+\t}
+\tif _, err := client.fetchProjectSecrets("testproj"); err != nil {
+\t\tt.Fatalf("fetchProjectSecrets (cached): %v", err)
+\t}
+\tif calls != 1 {
+\t\tt.Fatalf("expected 1 HTTP call, got %d", calls)
+\t}
+}
+
+func TestDopplerClientFetchWithRetry_SucceedsAfterRetry(t *testing.T) {
+\tvar calls int32
+\tclient := &dopplerClient{
+\t\ttoken:  "token",
+\t\tconfig: "prod",
+\t\thttpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
+\t\t\tcall := atomic.AddInt32(&calls, 1)
+\t\t\tif call == 1 {
+\t\t\t\treturn &http.Response{
+\t\t\t\t\tStatusCode: http.StatusInternalServerError,
+\t\t\t\t\tBody:       io.NopCloser(strings.NewReader("boom")),
+\t\t\t\t\tHeader:     make(http.Header),
+\t\t\t\t}, nil
+\t\t\t}
+\t\t\tbody := `{"secrets":{"API_KEY":{"raw":"VALUE"}}}`
+\t\t\treturn &http.Response{
+\t\t\t\tStatusCode: http.StatusOK,
+\t\t\t\tBody:       io.NopCloser(strings.NewReader(body)),
+\t\t\t\tHeader:     make(http.Header),
+\t\t\t}, nil
+\t\t})},
+\t\tcache: make(map[string]map[string]string),
+\t}
+
+\tsecrets, err := client.fetchWithRetry("testproj")
+\tif err != nil {
+\t\tt.Fatalf("fetchWithRetry: %v", err)
+\t}
+\tif secrets["API_KEY"] != "VALUE" {
+\t\tt.Fatalf("unexpected secrets: %v", secrets)
+\t}
+\tif calls < 2 {
+\t\tt.Fatalf("expected retry, got %d call(s)", calls)
+\t}
+}
```

## Additional Gaps (no diffs yet)
- `internal/db/repository.go`: Suggest a small test harness using `testcontainers-go` to start Postgres and validate transactional behavior (`PersistConversationTurnWithDebug`, activity feeds, debug data). If unit-only is required, introduce a thin interface around `pgxpool.Pool` so a mock can be injected (code change).
- `internal/provider/openai/filestore.go` + `internal/provider/gemini/filestore.go`: Use `httptest.Server` and custom transports to simulate API responses, with tests for polling, error parsing, and base URL overrides.

## Notes
- All diffs are new tests only; no production code edits.
- The admin handler tests focus on nil-repo/error paths plus a success path with a fake gRPC client.
