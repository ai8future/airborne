Date Created: 2026-01-24 23:58:16 +0100
Date Updated: 2026-01-28
TOTAL_SCORE: 70/100

Overview
- Reviewed Go packages outside generated directories (notably `gen/` and `markdown_svc/clients/`).
- Existing tests cover many core paths, but key admin/db/cli/pricing logic is untested.
- Score reflects moderate coverage with several critical flows lacking unit tests.

Untested Hotspots (by package)
- `internal/admin/server.go`: request validation, error responses, history compression, MIME detection, tenant API key lookup, idempotency paths.
- `internal/cli/output.go` and `internal/cli/client.go`: formatting helpers and HTTP error handling.
- `cmd/airborne/main.go`: logger configuration and health-check control flow.

*`internal/db/models.go` - TESTED in v1.7.15 (models_test.go)*
*`internal/pricing/pricing.go` - TESTED in v1.7.15 (pricing_test.go)*

High-Value Unit Test Proposals
- Admin server
  - `buildCompressedHistory`: truncation rules, assistant message dropping after limit, previousResponseID behavior.
  - `detectMIMEType`: extension mapping, case-insensitivity, unknown extension fallback.
  - `getGeminiAPIKey`: tenant not found, provider disabled, API key missing, happy path.
  - Handlers with `httptest`: method validation and nil-db fallback behavior (`/admin/health`, `/admin/activity`, `/admin/thread`, `/admin/debug`).
- DB
  - `ParseCitations`/`CitationsToJSON`: nil/empty handling, invalid JSON errors, round-trip fidelity.
  - `NewThread`/`NewMessage`/`SetAssistantMetrics`/`TruncateContent`: defaults, token totals, response ID handling.
  - `Client.TenantRepository`: caching and invalid tenant error semantics without needing a live DB.
  - Repository CRUD paths should be covered with an integration test DB (pgxmock or dockerized Postgres).
- CLI
  - Output formatting helpers for token/cost/duration/timestamps.
  - HTTP client methods (`Health`, `Activity`, `Test`, `Debug`, `Thread`) using `httptest` to validate status and decode failures.
  - Cobra commands: JSON output branch and empty/no-activity branch.
- Pricing
  - `Cost.Format` for unknown and normal cases; `CalculateCost` should return 0 for unknown models.
- Cmd
  - `configureLogger` sets level/format correctly for config values (table-driven).
  - `runHealthCheck` host selection when host is empty or 0.0.0.0 (use a stub gRPC health server).

Patch-Ready Diffs

1) `internal/admin/server_test.go`
```diff
diff --git a/internal/admin/server_test.go b/internal/admin/server_test.go
new file mode 100644
--- /dev/null
+++ b/internal/admin/server_test.go
@@
+package admin
+
+import (
+    "strings"
+    "testing"
+    "time"
+
+    "github.com/ai8future/airborne/internal/db"
+    "github.com/ai8future/airborne/internal/tenant"
+)
+
+func TestBuildCompressedHistory_TruncatesOlderAssistantMessages(t *testing.T) {
+    long := strings.Repeat("a", 600)
+    respIDs := []string{"r1", "r2", "r3", "r4", "r5"}
+    messages := []db.Message{
+        {Role: db.RoleAssistant, Content: long, ResponseID: &respIDs[0], CreatedAt: time.Unix(1, 0)},
+        {Role: db.RoleAssistant, Content: long, ResponseID: &respIDs[1], CreatedAt: time.Unix(2, 0)},
+        {Role: db.RoleAssistant, Content: long, ResponseID: &respIDs[2], CreatedAt: time.Unix(3, 0)},
+        {Role: db.RoleAssistant, Content: long, ResponseID: &respIDs[3], CreatedAt: time.Unix(4, 0)},
+        {Role: db.RoleAssistant, Content: long, ResponseID: &respIDs[4], CreatedAt: time.Unix(5, 0)},
+    }
+
+    var prev string
+    got := buildCompressedHistory(messages, &prev)
+
+    if len(got) != 5 {
+        t.Fatalf("expected 5 messages, got %d", len(got))
+    }
+    if len(got[0].Content) != len(long) {
+        t.Fatalf("expected full content for first message, got %d chars", len(got[0].Content))
+    }
+    if len(got[3].Content) != 503 || !strings.HasSuffix(got[3].Content, "...") {
+        t.Fatalf("expected truncated content at index 3, got %d chars", len(got[3].Content))
+    }
+    if prev != "r5" {
+        t.Fatalf("expected previousResponseID r5, got %q", prev)
+    }
+}
+
+func TestBuildCompressedHistory_DropsAssistantWhenOverLimit(t *testing.T) {
+    respIDs := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7"}
+    messages := []db.Message{
+        {Role: db.RoleAssistant, Content: "a", ResponseID: &respIDs[0], CreatedAt: time.Unix(1, 0)},
+        {Role: db.RoleAssistant, Content: "b", ResponseID: &respIDs[1], CreatedAt: time.Unix(2, 0)},
+        {Role: db.RoleAssistant, Content: "c", ResponseID: &respIDs[2], CreatedAt: time.Unix(3, 0)},
+        {Role: db.RoleAssistant, Content: "d", ResponseID: &respIDs[3], CreatedAt: time.Unix(4, 0)},
+        {Role: db.RoleAssistant, Content: "e", ResponseID: &respIDs[4], CreatedAt: time.Unix(5, 0)},
+        {Role: db.RoleAssistant, Content: "f", ResponseID: &respIDs[5], CreatedAt: time.Unix(6, 0)},
+        {Role: db.RoleAssistant, Content: "g", ResponseID: &respIDs[6], CreatedAt: time.Unix(7, 0)},
+        {Role: db.RoleUser, Content: "hello", CreatedAt: time.Unix(8, 0)},
+    }
+
+    var prev string
+    got := buildCompressedHistory(messages, &prev)
+
+    if len(got) != 1 {
+        t.Fatalf("expected 1 message, got %d", len(got))
+    }
+    if got[0].Role != db.RoleUser {
+        t.Fatalf("expected user message only, got role %q", got[0].Role)
+    }
+    if prev != "r7" {
+        t.Fatalf("expected previousResponseID r7, got %q", prev)
+    }
+}
+
+func TestDetectMIMEType(t *testing.T) {
+    tests := map[string]string{
+        "report.PDF":  "application/pdf",
+        "note.txt":    "text/plain",
+        "README.MD":   "text/markdown",
+        "image.JPEG":  "image/jpeg",
+        "archive.bin": "application/octet-stream",
+        "noext":       "application/octet-stream",
+    }
+
+    for name, want := range tests {
+        if got := detectMIMEType(name); got != want {
+            t.Fatalf("detectMIMEType(%q) = %q, want %q", name, got, want)
+        }
+    }
+}
+
+func TestGetGeminiAPIKey(t *testing.T) {
+    mgrWith := func(cfg tenant.TenantConfig) *tenant.Manager {
+        return &tenant.Manager{Tenants: map[string]tenant.TenantConfig{"t1": cfg}}
+    }
+
+    tests := []struct {
+        name     string
+        mgr      *tenant.Manager
+        tenantID string
+        wantKey  string
+        wantErr  string
+    }{
+        {"nil manager", nil, "t1", "", "tenant manager not configured"},
+        {"missing tenant", mgrWith(tenant.TenantConfig{}), "missing", "", "tenant not found"},
+        {"provider disabled", mgrWith(tenant.TenantConfig{TenantID: "t1"}), "t1", "", "gemini provider not enabled"},
+        {"missing api key", mgrWith(tenant.TenantConfig{TenantID: "t1", Providers: map[string]tenant.ProviderConfig{"gemini": {Enabled: true}}}), "t1", "", "gemini API key not configured"},
+        {"ok", mgrWith(tenant.TenantConfig{TenantID: "t1", Providers: map[string]tenant.ProviderConfig{"gemini": {Enabled: true, APIKey: "secret"}}}), "t1", "secret", ""},
+    }
+
+    for _, tt := range tests {
+        t.Run(tt.name, func(t *testing.T) {
+            s := &Server{tenantMgr: tt.mgr}
+            key, err := s.getGeminiAPIKey(tt.tenantID)
+            if tt.wantErr != "" {
+                if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
+                    t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
+                }
+                return
+            }
+            if err != nil {
+                t.Fatalf("unexpected error: %v", err)
+            }
+            if key != tt.wantKey {
+                t.Fatalf("expected key %q, got %q", tt.wantKey, key)
+            }
+        })
+    }
+}
```

2) `internal/cli/output_test.go`
```diff
diff --git a/internal/cli/output_test.go b/internal/cli/output_test.go
new file mode 100644
--- /dev/null
+++ b/internal/cli/output_test.go
@@
+package cli
+
+import (
+    "testing"
+    "time"
+
+    "github.com/fatih/color"
+)
+
+func TestFormatTokens(t *testing.T) {
+    if got := FormatTokens(999); got != "999" {
+        t.Fatalf("FormatTokens(999) = %q", got)
+    }
+    if got := FormatTokens(1000); got != "1.0K" {
+        t.Fatalf("FormatTokens(1000) = %q", got)
+    }
+    if got := FormatTokens(1500); got != "1.5K" {
+        t.Fatalf("FormatTokens(1500) = %q", got)
+    }
+}
+
+func TestFormatCost(t *testing.T) {
+    if got := FormatCost(0); got != "$0.000" {
+        t.Fatalf("FormatCost(0) = %q", got)
+    }
+    if got := FormatCost(0.1234); got != "$0.123" {
+        t.Fatalf("FormatCost(0.1234) = %q", got)
+    }
+}
+
+func TestFormatDuration(t *testing.T) {
+    if got := FormatDuration(999); got != "999ms" {
+        t.Fatalf("FormatDuration(999) = %q", got)
+    }
+    if got := FormatDuration(1000); got != "1.0s" {
+        t.Fatalf("FormatDuration(1000) = %q", got)
+    }
+    if got := FormatDuration(1500); got != "1.5s" {
+        t.Fatalf("FormatDuration(1500) = %q", got)
+    }
+}
+
+func TestFormatStatus(t *testing.T) {
+    oldNoColor := color.NoColor
+    color.NoColor = true
+    defer func() { color.NoColor = oldNoColor }()
+
+    if got := FormatStatus("success"); got != "\u2713" {
+        t.Fatalf("FormatStatus(success) = %q", got)
+    }
+    if got := FormatStatus("error"); got != "\u2717" {
+        t.Fatalf("FormatStatus(error) = %q", got)
+    }
+}
+
+func TestFormatTimestamp(t *testing.T) {
+    oldLoc := time.Local
+    time.Local = time.UTC
+    defer func() { time.Local = oldLoc }()
+
+    if got := FormatTimestamp("2024-01-02T03:04:05Z"); got != "2024-01-02 03:04:05" {
+        t.Fatalf("FormatTimestamp RFC3339 = %q", got)
+    }
+    if got := FormatTimestamp("not-a-time"); got != "not-a-time" {
+        t.Fatalf("FormatTimestamp invalid = %q", got)
+    }
+}
+
+func TestTruncateString(t *testing.T) {
+    if got := TruncateString("hello\nworld", 8); got != "hello..." {
+        t.Fatalf("TruncateString = %q", got)
+    }
+    if got := TruncateString("hi\n", 10); got != "hi " {
+        t.Fatalf("TruncateString short = %q", got)
+    }
+}
```

3) `internal/db/models_test.go`
```diff
diff --git a/internal/db/models_test.go b/internal/db/models_test.go
new file mode 100644
--- /dev/null
+++ b/internal/db/models_test.go
@@
+package db
+
+import (
+    "testing"
+    "time"
+
+    "github.com/google/uuid"
+)
+
+func TestParseCitations(t *testing.T) {
+    if got, err := ParseCitations(nil); err != nil || got != nil {
+        t.Fatalf("ParseCitations(nil) = %v, %v", got, err)
+    }
+
+    empty := ""
+    if got, err := ParseCitations(&empty); err != nil || got != nil {
+        t.Fatalf("ParseCitations(empty) = %v, %v", got, err)
+    }
+
+    bad := "{"
+    if _, err := ParseCitations(&bad); err == nil {
+        t.Fatal("expected error for invalid JSON")
+    }
+
+    good := `[{"type":"url","url":"https://example.com","title":"Example"}]`
+    got, err := ParseCitations(&good)
+    if err != nil {
+        t.Fatalf("unexpected error: %v", err)
+    }
+    if len(got) != 1 || got[0].URL != "https://example.com" {
+        t.Fatalf("unexpected citations: %+v", got)
+    }
+}
+
+func TestCitationsToJSON_RoundTrip(t *testing.T) {
+    if got, err := CitationsToJSON(nil); err != nil || got != nil {
+        t.Fatalf("CitationsToJSON(nil) = %v, %v", got, err)
+    }
+
+    citations := []Citation{{Type: "url", URL: "https://example.com", Title: "Example"}}
+    jsonStr, err := CitationsToJSON(citations)
+    if err != nil || jsonStr == nil {
+        t.Fatalf("CitationsToJSON error: %v", err)
+    }
+
+    parsed, err := ParseCitations(jsonStr)
+    if err != nil {
+        t.Fatalf("ParseCitations error: %v", err)
+    }
+    if len(parsed) != 1 || parsed[0].URL != "https://example.com" {
+        t.Fatalf("round-trip mismatch: %+v", parsed)
+    }
+}
+
+func TestNewThread(t *testing.T) {
+    thread := NewThread("user-1")
+    if thread.UserID != "user-1" {
+        t.Fatalf("UserID = %q", thread.UserID)
+    }
+    if thread.Status != ThreadStatusActive {
+        t.Fatalf("Status = %q", thread.Status)
+    }
+    if thread.MessageCount != 0 {
+        t.Fatalf("MessageCount = %d", thread.MessageCount)
+    }
+    if thread.ID == uuid.Nil {
+        t.Fatal("expected non-nil ID")
+    }
+    if !thread.CreatedAt.Equal(thread.UpdatedAt) {
+        t.Fatalf("created/updated mismatch: %v vs %v", thread.CreatedAt, thread.UpdatedAt)
+    }
+}
+
+func TestNewMessage(t *testing.T) {
+    tid := uuid.New()
+    msg := NewMessage(tid, RoleUser, "hello")
+    if msg.ThreadID != tid {
+        t.Fatalf("ThreadID = %v", msg.ThreadID)
+    }
+    if msg.Role != RoleUser {
+        t.Fatalf("Role = %q", msg.Role)
+    }
+    if msg.Content != "hello" {
+        t.Fatalf("Content = %q", msg.Content)
+    }
+    if msg.CreatedAt.IsZero() {
+        t.Fatal("expected CreatedAt to be set")
+    }
+}
+
+func TestSetAssistantMetrics(t *testing.T) {
+    msg := &Message{}
+    msg.SetAssistantMetrics("openai", "gpt", 10, 5, 250, 0.12, "resp-1")
+
+    if msg.Provider == nil || *msg.Provider != "openai" {
+        t.Fatalf("Provider = %v", msg.Provider)
+    }
+    if msg.Model == nil || *msg.Model != "gpt" {
+        t.Fatalf("Model = %v", msg.Model)
+    }
+    if msg.InputTokens == nil || *msg.InputTokens != 10 {
+        t.Fatalf("InputTokens = %v", msg.InputTokens)
+    }
+    if msg.OutputTokens == nil || *msg.OutputTokens != 5 {
+        t.Fatalf("OutputTokens = %v", msg.OutputTokens)
+    }
+    if msg.TotalTokens == nil || *msg.TotalTokens != 15 {
+        t.Fatalf("TotalTokens = %v", msg.TotalTokens)
+    }
+    if msg.CostUSD == nil || *msg.CostUSD != 0.12 {
+        t.Fatalf("CostUSD = %v", msg.CostUSD)
+    }
+    if msg.ProcessingTimeMs == nil || *msg.ProcessingTimeMs != 250 {
+        t.Fatalf("ProcessingTimeMs = %v", msg.ProcessingTimeMs)
+    }
+    if msg.ResponseID == nil || *msg.ResponseID != "resp-1" {
+        t.Fatalf("ResponseID = %v", msg.ResponseID)
+    }
+}
+
+func TestTruncateContent(t *testing.T) {
+    msg := &Message{Content: "hello"}
+    if got := msg.TruncateContent(10); got != "hello" {
+        t.Fatalf("TruncateContent short = %q", got)
+    }
+
+    msg.Content = "0123456789ABC"
+    if got := msg.TruncateContent(10); got != "0123456789..." {
+        t.Fatalf("TruncateContent long = %q", got)
+    }
+}
+
+func TestNewMessage_Timestamps(t *testing.T) {
+    tid := uuid.New()
+    msg := NewMessage(tid, RoleAssistant, "hi")
+    if time.Since(msg.CreatedAt) > time.Second {
+        t.Fatalf("CreatedAt too old: %v", msg.CreatedAt)
+    }
+}
```

4) `internal/db/postgres_test.go`
```diff
diff --git a/internal/db/postgres_test.go b/internal/db/postgres_test.go
new file mode 100644
--- /dev/null
+++ b/internal/db/postgres_test.go
@@
+package db
+
+import (
+    "errors"
+    "testing"
+)
+
+func TestTenantRepository_Caches(t *testing.T) {
+    client := &Client{tenantRepos: make(map[string]*Repository)}
+
+    repo1, err := client.TenantRepository("ai8")
+    if err != nil {
+        t.Fatalf("unexpected error: %v", err)
+    }
+    repo2, err := client.TenantRepository("ai8")
+    if err != nil {
+        t.Fatalf("unexpected error: %v", err)
+    }
+    if repo1 != repo2 {
+        t.Fatal("expected cached repository instance")
+    }
+    if repo1.TenantID() != "ai8" {
+        t.Fatalf("TenantID = %q", repo1.TenantID())
+    }
+}
+
+func TestTenantRepository_InvalidTenant(t *testing.T) {
+    client := &Client{tenantRepos: make(map[string]*Repository)}
+    if _, err := client.TenantRepository("nope"); !errors.Is(err, ErrInvalidTenant) {
+        t.Fatalf("expected ErrInvalidTenant, got %v", err)
+    }
+}
```

5) `internal/pricing/pricing_test.go`
```diff
diff --git a/internal/pricing/pricing_test.go b/internal/pricing/pricing_test.go
new file mode 100644
--- /dev/null
+++ b/internal/pricing/pricing_test.go
@@
+package pricing
+
+import "testing"
+
+func TestCostFormatUnknown(t *testing.T) {
+    c := Cost{Model: "mystery", Unknown: true}
+    got := c.Format()
+    want := "Cost: unknown (model \"mystery\" not in pricing data)"
+    if got != want {
+        t.Fatalf("Format = %q", got)
+    }
+}
+
+func TestCostFormatKnown(t *testing.T) {
+    c := Cost{Model: "test", InputTokens: 10, OutputTokens: 5, InputCost: 0.1, OutputCost: 0.2, TotalCost: 0.3}
+    got := c.Format()
+    want := "Input: $0.1000 (10 tokens) | Output: $0.2000 (5 tokens) | Total: $0.3000"
+    if got != want {
+        t.Fatalf("Format = %q", got)
+    }
+}
+
+func TestCalculateCost_UnknownModel(t *testing.T) {
+    if got := CalculateCost("unknown-model", 10, 5); got != 0 {
+        t.Fatalf("CalculateCost unknown = %f", got)
+    }
+}
```

Additional Suggestions (no diff)
- `internal/cli/client.go`: use `httptest.NewServer` to validate URL/query composition and error surfaces on non-200 status codes.
- `internal/admin/server.go`: add `httptest` coverage for method validation, nil-db responses, and error paths without requiring gRPC/Redis.
- `internal/db/repository.go`: run integration tests with a lightweight Postgres (docker-compose or testcontainer) to verify CRUD and activity feeds.
