Date Created: 2026-01-20 17:55:15 +0100
TOTAL_SCORE: 72/100

**Quick Read**
- Scope: fast scan of Go packages to identify directories without tests and high-value untested logic.
- Overall: good coverage for provider clients, validation, retry, and tenant config; gaps in admin server, db layer, pricing, Doppler loader, and several provider wrappers/file-store helpers.
- Grade drivers: strong unit tests in many internal packages, but core persistence/admin flows lack tests and could regress unnoticed.

**Score Rationale**
- Strengths: provider clients and validation logic have focused tests; retry, auth, and service layers have basic coverage.
- Gaps: db repository/model helpers, pricing engine, admin HTTP utilities, Doppler loader, and file-store helpers are untested.
- Risk: untested persistence + admin endpoints + external API wrappers (file stores) are high-impact.

**Untested / Under-tested Areas**
- `internal/admin/server.go`: buildCompressedHistory logic and admin handlers are untested.
- `internal/db/*`: repository SQL helpers, models helpers (ParseCitations, SetAssistantMetrics), and tenant repo caching have no tests.
- `internal/pricing/pricing.go`: pricing file discovery, prefix matching, and unknown-model handling are untested.
- `internal/tenant/doppler.go`: retry/backoff, caching, and tenant config fetch/parse are untested.
- `cmd/airborne/main.go`: configureLogger + runHealthCheck have no tests (could be unit-tested with small seams).
- Provider wrappers without tests: `internal/provider/{deepinfra,deepseek,perplexity,openrouter,cohere,fireworks,together,upstage,nebius,hyperbolic,grok,cerebras}`.
- File-store helpers: `internal/provider/openai/filestore.go`, `internal/provider/gemini/filestore.go` lack test coverage (can be tested with httptest + baseURL overrides).

**Proposed Unit Tests (High Value)**
- `internal/pricing/pricing.go`
  - NewPricer fails on empty dir; loads multiple files and infers provider name from filename.
  - Calculate cost math; prefix model match; unknown model returns Unknown.
- `internal/db/models.go`
  - ParseCitations nil/empty/invalid/valid; CitationsToJSON empty vs populated.
  - NewThread/NewMessage set IDs, timestamps, defaults; SetAssistantMetrics populates derived fields.
  - TruncateContent handles long strings with ellipsis.
- `internal/admin/server.go`
  - buildCompressedHistory truncates older assistant responses and updates previousResponseID.
  - buildCompressedHistory drops assistant messages when over limit and retains users.
- `internal/tenant/doppler.go`
  - fetchProjectSecrets caches results and avoids duplicate HTTP calls.
  - fetchWithRetry stops on non-retryable error (4xx), retries on retryable (optional).
  - LoadTenantsFromDoppler loads configs from cache and normalizes tenant_id.
- `internal/db/repository.go` / `internal/db/postgres.go`
  - NewTenantRepository validation, tenant table naming, and Client.TenantRepository caching.
- Additional (no diffs below to keep scope small):
  - `cmd/airborne/main.go` runHealthCheck with mocked gRPC server and TLS toggle.
  - File-store helpers in openai/gemini using httptest and BaseURL overrides.
  - Provider wrapper smoke tests to assert Name/Supports* flags for each compat client.

**Patch-Ready Diffs**

```diff
diff --git a/internal/pricing/pricing_test.go b/internal/pricing/pricing_test.go
new file mode 100644
index 0000000..6a86f9c
--- /dev/null
+++ b/internal/pricing/pricing_test.go
@@
+package pricing
+
+import (
+\t"encoding/json"
+\t"math"
+\t"os"
+\t"path/filepath"
+\t"testing"
+)
+
+func writePricingFile(t *testing.T, dir, name string, file pricingFile) {
+\tt.Helper()
+\tdata, err := json.Marshal(file)
+\tif err != nil {
+\t\tt.Fatalf("marshal pricing file: %v", err)
+\t}
+\tpath := filepath.Join(dir, name)
+\tif err := os.WriteFile(path, data, 0600); err != nil {
+\t\tt.Fatalf("write pricing file: %v", err)
+\t}
+}
+
+func containsString(values []string, want string) bool {
+\tfor _, v := range values {
+\t\tif v == want {
+\t\t\treturn true
+\t\t}
+\t}
+\treturn false
+}
+
+func floatEqual(a, b, tol float64) bool {
+\treturn math.Abs(a-b) <= tol
+}
+
+func TestNewPricer_NoFiles(t *testing.T) {
+\t_, err := NewPricer(t.TempDir())
+\tif err == nil {
+\t\tt.Fatal("expected error when no pricing files exist")
+\t}
+}
+
+func TestNewPricer_LoadsAndCalculates(t *testing.T) {
+\tdir := t.TempDir()
+
+\twritePricingFile(t, dir, "openai_pricing.json", pricingFile{
+\t\tProvider: "openai",
+\t\tModels: map[string]ModelPricing{
+\t\t\t"gpt-4o": {InputPerMillion: 10, OutputPerMillion: 30},
+\t\t},
+\t})
+
+\twritePricingFile(t, dir, "custom_pricing.json", pricingFile{
+\t\tModels: map[string]ModelPricing{
+\t\t\t"custom-model": {InputPerMillion: 1.5, OutputPerMillion: 2.5},
+\t\t},
+\t})
+
+\tpricer, err := NewPricer(dir)
+\tif err != nil {
+\t\tt.Fatalf("NewPricer failed: %v", err)
+\t}
+
+\tif pricer.ModelCount() != 2 {
+\t\tt.Fatalf("ModelCount = %d, want 2", pricer.ModelCount())
+\t}
+
+\tproviders := pricer.ListProviders()
+\tif !containsString(providers, "openai") || !containsString(providers, "custom") {
+\t\tt.Fatalf("ListProviders missing expected entries: %v", providers)
+\t}
+
+\tcost := pricer.Calculate("gpt-4o", 1000, 2000)
+\tif cost.Unknown {
+\t\tt.Fatal("expected model to be known")
+\t}
+\tif !floatEqual(cost.TotalCost, 0.07, 1e-9) {
+\t\tt.Fatalf("TotalCost = %f, want 0.07", cost.TotalCost)
+\t}
+
+\tprefixCost := pricer.Calculate("gpt-4o-2024-08-06", 1000, 0)
+\tif prefixCost.Unknown {
+\t\tt.Fatal("expected prefix model to be known")
+\t}
+\tif !floatEqual(prefixCost.TotalCost, 0.01, 1e-9) {
+\t\tt.Fatalf("prefix TotalCost = %f, want 0.01", prefixCost.TotalCost)
+\t}
+
+\tif _, ok := pricer.GetPricing("gpt-4o-2024-08-06"); !ok {
+\t\tt.Fatal("expected GetPricing to match prefix model")
+\t}
+
+\tunknown := pricer.Calculate("missing-model", 10, 20)
+\tif !unknown.Unknown {
+\t\tt.Fatal("expected unknown model to be marked Unknown")
+\t}
+\tif unknown.TotalCost != 0 {
+\t\tt.Fatalf("unknown TotalCost = %f, want 0", unknown.TotalCost)
+\t}
+}
```

```diff
diff --git a/internal/db/models_test.go b/internal/db/models_test.go
new file mode 100644
index 0000000..a63f6d3
--- /dev/null
+++ b/internal/db/models_test.go
@@
+package db
+
+import (
+\t"encoding/json"
+\t"testing"
+\t"time"
+
+\t"github.com/google/uuid"
+)
+
+func TestParseCitations(t *testing.T) {
+\tif got, err := ParseCitations(nil); err != nil || got != nil {
+\t\tt.Fatalf("ParseCitations(nil) = %v, %v, want nil, nil", got, err)
+\t}
+
+\tempty := ""
+\tif got, err := ParseCitations(&empty); err != nil || got != nil {
+\t\tt.Fatalf("ParseCitations(empty) = %v, %v, want nil, nil", got, err)
+\t}
+
+\tbad := "{"
+\tif _, err := ParseCitations(&bad); err == nil {
+\t\tt.Fatal("expected error for invalid citations JSON")
+\t}
+
+\tgood := `[{"type":"url","url":"https://example.com","title":"Example"}]`
+\tparsed, err := ParseCitations(&good)
+\tif err != nil {
+\t\tt.Fatalf("ParseCitations(valid) error: %v", err)
+\t}
+\tif len(parsed) != 1 || parsed[0].URL != "https://example.com" {
+\t\tt.Fatalf("unexpected parsed citations: %+v", parsed)
+\t}
+}
+
+func TestCitationsToJSON(t *testing.T) {
+\tif got, err := CitationsToJSON(nil); err != nil || got != nil {
+\t\tt.Fatalf("CitationsToJSON(nil) = %v, %v, want nil, nil", got, err)
+\t}
+
+\tinput := []Citation{{Type: "url", URL: "https://example.com"}}
+\tjsonStr, err := CitationsToJSON(input)
+\tif err != nil {
+\t\tt.Fatalf("CitationsToJSON error: %v", err)
+\t}
+\tif jsonStr == nil {
+\t\tt.Fatal("expected non-nil JSON string")
+\t}
+
+\tvar roundTrip []Citation
+\tif err := json.Unmarshal([]byte(*jsonStr), &roundTrip); err != nil {
+\t\tt.Fatalf("unmarshal JSON: %v", err)
+\t}
+\tif len(roundTrip) != 1 || roundTrip[0].URL != "https://example.com" {
+\t\tt.Fatalf("unexpected round-trip citations: %+v", roundTrip)
+\t}
+}
+
+func TestNewThreadAndMessage(t *testing.T) {
+\tstart := time.Now()
+\tthread := NewThread("user-1")
+\tif thread.ID == uuid.Nil {
+\t\tt.Fatal("expected thread ID to be set")
+\t}
+\tif thread.UserID != "user-1" {
+\t\tt.Fatalf("UserID = %q, want user-1", thread.UserID)
+\t}
+\tif thread.Status != ThreadStatusActive {
+\t\tt.Fatalf("Status = %q, want %q", thread.Status, ThreadStatusActive)
+\t}
+\tif thread.MessageCount != 0 {
+\t\tt.Fatalf("MessageCount = %d, want 0", thread.MessageCount)
+\t}
+\tif thread.CreatedAt.Before(start) {
+\t\tt.Fatalf("CreatedAt %v is before start %v", thread.CreatedAt, start)
+\t}
+\tif !thread.UpdatedAt.Equal(thread.CreatedAt) {
+\t\tt.Fatal("UpdatedAt should match CreatedAt")
+\t}
+
+\tmsg := NewMessage(thread.ID, RoleUser, "hello")
+\tif msg.ID == uuid.Nil {
+\t\tt.Fatal("expected message ID to be set")
+\t}
+\tif msg.ThreadID != thread.ID {
+\t\tt.Fatalf("ThreadID mismatch: %v vs %v", msg.ThreadID, thread.ID)
+\t}
+\tif msg.Role != RoleUser {
+\t\tt.Fatalf("Role = %q, want %q", msg.Role, RoleUser)
+\t}
+\tif msg.Content != "hello" {
+\t\tt.Fatalf("Content = %q, want hello", msg.Content)
+\t}
+\tif time.Since(msg.CreatedAt) > time.Second {
+\t\tt.Fatalf("CreatedAt too old: %v", msg.CreatedAt)
+\t}
+}
+
+func TestSetAssistantMetrics(t *testing.T) {
+\tmsg := &Message{}
+\tmsg.SetAssistantMetrics("openai", "gpt-4o", 10, 20, 150, 0.25, "resp-123")
+
+\tif msg.Provider == nil || *msg.Provider != "openai" {
+\t\tt.Fatalf("Provider = %v, want openai", msg.Provider)
+\t}
+\tif msg.Model == nil || *msg.Model != "gpt-4o" {
+\t\tt.Fatalf("Model = %v, want gpt-4o", msg.Model)
+\t}
+\tif msg.TotalTokens == nil || *msg.TotalTokens != 30 {
+\t\tt.Fatalf("TotalTokens = %v, want 30", msg.TotalTokens)
+\t}
+\tif msg.ResponseID == nil || *msg.ResponseID != "resp-123" {
+\t\tt.Fatalf("ResponseID = %v, want resp-123", msg.ResponseID)
+\t}
+
+\tmsg2 := &Message{}
+\tmsg2.SetAssistantMetrics("openai", "gpt-4o", 1, 2, 3, 0.01, "")
+\tif msg2.ResponseID != nil {
+\t\tt.Fatal("expected ResponseID to remain nil when empty responseID provided")
+\t}
+}
+
+func TestMessageTruncateContent(t *testing.T) {
+\tmsg := &Message{Content: "hello world"}
+\tif got := msg.TruncateContent(20); got != "hello world" {
+\t\tt.Fatalf("TruncateContent(20) = %q, want hello world", got)
+\t}
+\tif got := msg.TruncateContent(5); got != "hello..." {
+\t\tt.Fatalf("TruncateContent(5) = %q, want hello...", got)
+\t}
+}
```

```diff
diff --git a/internal/admin/server_test.go b/internal/admin/server_test.go
new file mode 100644
index 0000000..d9c7db4
--- /dev/null
+++ b/internal/admin/server_test.go
@@
+package admin
+
+import (
+\t"fmt"
+\t"strings"
+\t"testing"
+\t"time"
+
+\t"github.com/ai8future/airborne/internal/db"
+)
+
+func strPtr(value string) *string {
+\treturn &value
+}
+
+func TestBuildCompressedHistory_TruncatesOldAssistantResponses(t *testing.T) {
+\tlongText := strings.Repeat("a", 600)
+\tprev := ""
+\tmsgs := []db.Message{
+\t\t{Role: "user", Content: "hello", CreatedAt: time.Unix(1, 0)},
+\t\t{Role: "assistant", Content: longText, ResponseID: strPtr("resp-1"), CreatedAt: time.Unix(2, 0)},
+\t\t{Role: "assistant", Content: longText, ResponseID: strPtr("resp-2"), CreatedAt: time.Unix(3, 0)},
+\t\t{Role: "assistant", Content: longText, ResponseID: strPtr("resp-3"), CreatedAt: time.Unix(4, 0)},
+\t\t{Role: "assistant", Content: longText, ResponseID: strPtr("resp-4"), CreatedAt: time.Unix(5, 0)},
+\t}
+
+\thistory := buildCompressedHistory(msgs, &prev)
+
+\tif prev != "resp-4" {
+\t\tt.Fatalf("previousResponseID = %q, want resp-4", prev)
+\t}
+\tif len(history) != 5 {
+\t\tt.Fatalf("history length = %d, want 5", len(history))
+\t}
+
+\tlast := history[len(history)-1]
+\tif !strings.HasSuffix(last.Content, "...") {
+\t\tt.Fatalf("expected truncated content to end with ellipsis, got %q", last.Content)
+\t}
+\tif len(last.Content) != 503 {
+\t\tt.Fatalf("truncated content length = %d, want 503", len(last.Content))
+\t}
+}
+
+func TestBuildCompressedHistory_DropsAssistantMessagesWhenOverLimit(t *testing.T) {
+\tvar msgs []db.Message
+\tfor i := 0; i < 7; i++ {
+\t\tresp := fmt.Sprintf("resp-%d", i)
+\t\tmsgs = append(msgs, db.Message{
+\t\t\tRole:       "assistant",
+\t\t\tContent:    "assistant reply",
+\t\t\tResponseID: &resp,
+\t\t\tCreatedAt:  time.Unix(int64(i+1), 0),
+\t\t})
+\t\tmsgs = append(msgs, db.Message{
+\t\t\tRole:      "user",
+\t\t\tContent:   "user message",
+\t\t\tCreatedAt: time.Unix(int64(i+10), 0),
+\t\t})
+\t}
+
+\tprev := ""
+\thistory := buildCompressedHistory(msgs, &prev)
+
+\tif len(history) != 7 {
+\t\tt.Fatalf("history length = %d, want 7", len(history))
+\t}
+\tfor _, msg := range history {
+\t\tif msg.Role == "assistant" {
+\t\t\tt.Fatal("expected assistant messages to be dropped when over limit")
+\t\t}
+\t}
+\tif prev == "" {
+\t\tt.Fatal("expected previousResponseID to be updated")
+\t}
+}
```

```diff
diff --git a/internal/tenant/doppler_test.go b/internal/tenant/doppler_test.go
new file mode 100644
index 0000000..5f0e1fa
--- /dev/null
+++ b/internal/tenant/doppler_test.go
@@
+package tenant
+
+import (
+\t"io"
+\t"net/http"
+\t"strings"
+\t"sync"
+\t"testing"
+)
+
+type roundTripperFunc func(*http.Request) (*http.Response, error)
+
+func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
+\treturn f(req)
+}
+
+func jsonResponse(status int, body string) *http.Response {
+\treturn &http.Response{
+\t\tStatusCode: status,
+\t\tBody:       io.NopCloser(strings.NewReader(body)),
+\t\tHeader:     make(http.Header),
+\t}
+}
+
+func TestFetchProjectSecretsCaches(t *testing.T) {
+\tvar calls int
+\tclient := &dopplerClient{
+\t\ttoken:  "token",
+\t\tconfig: "prod",
+\t\thttpClient: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
+\t\t\tcalls++
+\t\t\treturn jsonResponse(http.StatusOK, `{"secrets":{"API_KEY":{"raw":"secret"}}}`), nil
+\t\t})},
+\t\tcache: make(map[string]map[string]string),
+\t}
+
+\tfirst, err := client.fetchProjectSecrets("project")
+\tif err != nil {
+\t\tt.Fatalf("fetchProjectSecrets error: %v", err)
+\t}
+\tif first["API_KEY"] != "secret" {
+\t\tt.Fatalf("unexpected secret: %v", first)
+\t}
+
+\tsecond, err := client.fetchProjectSecrets("project")
+\tif err != nil {
+\t\tt.Fatalf("fetchProjectSecrets second error: %v", err)
+\t}
+\tif second["API_KEY"] != "secret" {
+\t\tt.Fatalf("unexpected secret on second fetch: %v", second)
+\t}
+\tif calls != 1 {
+\t\tt.Fatalf("expected 1 HTTP call, got %d", calls)
+\t}
+}
+
+func TestFetchWithRetryStopsOnClientError(t *testing.T) {
+\tvar calls int
+\tclient := &dopplerClient{
+\t\ttoken:  "token",
+\t\tconfig: "prod",
+\t\thttpClient: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
+\t\t\tcalls++
+\t\t\treturn jsonResponse(http.StatusBadRequest, "bad request"), nil
+\t\t})},
+\t}
+
+\tif _, err := client.fetchWithRetry("project"); err == nil {
+\t\tt.Fatal("expected error for non-retryable status")
+\t}
+\tif calls != 1 {
+\t\tt.Fatalf("expected 1 HTTP call, got %d", calls)
+\t}
+}
+
+func TestLoadTenantsFromDopplerUsesCache(t *testing.T) {
+\tt.Setenv("DOPPLER_TOKEN", "")
+
+\tglobalDopplerClient = &dopplerClient{
+\t\tcache: map[string]map[string]string{
+\t\t\t"code_airborne": {"BRAND_TENANTS": "brandA"},
+\t\t\t"brandA": {"AIRBORNE_TENANT_CONFIG": `{"tenant_id":"Acme","providers":{"openai":{"enabled":true,"api_key":"key","model":"gpt-4o"}}}`},
+\t\t},
+\t}
+\tdopplerOnce = sync.Once{}
+
+\tgot, err := LoadTenantsFromDoppler()
+\tif err != nil {
+\t\tt.Fatalf("LoadTenantsFromDoppler error: %v", err)
+\t}
+\tif len(got) != 1 {
+\t\tt.Fatalf("expected 1 tenant, got %d", len(got))
+\t}
+\tcfg, ok := got["acme"]
+\tif !ok {
+\t\tt.Fatalf("expected tenant key acme, got %v", got)
+\t}
+\tif cfg.TenantID != "acme" {
+\t\tt.Fatalf("TenantID = %q, want acme", cfg.TenantID)
+\t}
+}
```

```diff
diff --git a/internal/db/repository_test.go b/internal/db/repository_test.go
new file mode 100644
index 0000000..7e9f6b2
--- /dev/null
+++ b/internal/db/repository_test.go
@@
+package db
+
+import "testing"
+
+func TestNewTenantRepositoryValidation(t *testing.T) {
+\tclient := &Client{}
+
+\tif _, err := NewTenantRepository(client, "invalid"); err == nil {
+\t\tt.Fatal("expected error for invalid tenant ID")
+\t}
+
+\trepo, err := NewTenantRepository(client, "ai8")
+\tif err != nil {
+\t\tt.Fatalf("NewTenantRepository error: %v", err)
+\t}
+\tif repo.TenantID() != "ai8" {
+\t\tt.Fatalf("TenantID = %q, want ai8", repo.TenantID())
+\t}
+\tif repo.tablePrefix != "ai8_airborne" {
+\t\tt.Fatalf("tablePrefix = %q, want ai8_airborne", repo.tablePrefix)
+\t}
+}
+
+func TestRepositoryTableNames(t *testing.T) {
+\tlegacy := &Repository{tablePrefix: ""}
+\tif legacy.threadsTable() != "airborne_threads" {
+\t\tt.Fatalf("threadsTable = %q", legacy.threadsTable())
+\t}
+\tif legacy.messagesTable() != "airborne_messages" {
+\t\tt.Fatalf("messagesTable = %q", legacy.messagesTable())
+\t}
+\tif legacy.filesTable() != "airborne_files" {
+\t\tt.Fatalf("filesTable = %q", legacy.filesTable())
+\t}
+
+\ttenant := &Repository{tablePrefix: "ai8_airborne"}
+\tif tenant.threadsTable() != "ai8_airborne_threads" {
+\t\tt.Fatalf("tenant threadsTable = %q", tenant.threadsTable())
+\t}
+\tif tenant.messagesTable() != "ai8_airborne_messages" {
+\t\tt.Fatalf("tenant messagesTable = %q", tenant.messagesTable())
+\t}
+\tif tenant.filesTable() != "ai8_airborne_files" {
+\t\tt.Fatalf("tenant filesTable = %q", tenant.filesTable())
+\t}
+\tif tenant.fileUploadsTable() != "ai8_airborne_file_provider_uploads" {
+\t\tt.Fatalf("tenant fileUploadsTable = %q", tenant.fileUploadsTable())
+\t}
+\tif tenant.vectorStoresTable() != "ai8_airborne_thread_vector_stores" {
+\t\tt.Fatalf("tenant vectorStoresTable = %q", tenant.vectorStoresTable())
+\t}
+}
+
+func TestClientTenantRepositoryCaching(t *testing.T) {
+\tclient := &Client{tenantRepos: make(map[string]*Repository)}
+
+\trepo1, err := client.TenantRepository("ai8")
+\tif err != nil {
+\t\tt.Fatalf("TenantRepository error: %v", err)
+\t}
+\trepo2, err := client.TenantRepository("ai8")
+\tif err != nil {
+\t\tt.Fatalf("TenantRepository second error: %v", err)
+\t}
+\tif repo1 != repo2 {
+\t\tt.Fatal("expected cached repository instance")
+\t}
+}
```

**Notes / Assumptions**
- Generated code in `gen/go` and `markdown_svc/clients/go` is excluded from unit-test proposals.
- DB repository methods likely need integration tests or a thin interface over `pgxpool.Pool` for mocks; I avoided patch diffs here to keep changes minimal.
- Doppler tests mutate package-level state; keep them non-parallel or reset globals in a test helper if you expect parallel test runs.
