# Small Code Test Report
Date Created: 2026-01-07 23:03:36 +0100

## Coverage Gaps Addressed
- Chat service helper logic (tenant-aware provider selection, config merging, RAG prompt formatting)
- Admin service authorization/health responses without Redis
- Tenant interceptor behavior (tenant resolution + FileService skip behavior)
- Config loader env overrides + ${VAR} expansion

Note: The tenant interceptor skip test below assumes the audit fix that skips FileService methods.

## Patch-ready diffs

### internal/service/chat_test.go
```diff
diff --git a/internal/service/chat_test.go b/internal/service/chat_test.go
new file mode 100644
index 0000000..0000000
--- /dev/null
+++ b/internal/service/chat_test.go
@@
+package service
+
+import (
+	"context"
+	"strings"
+	"testing"
+
+	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
+	"github.com/cliffpyles/aibox/internal/auth"
+	"github.com/cliffpyles/aibox/internal/rag"
+	"github.com/cliffpyles/aibox/internal/tenant"
+)
+
+func float64Ptr(v float64) *float64 { return &v }
+func int32Ptr(v int32) *int32 { return &v }
+
+func TestHasCustomBaseURL(t *testing.T) {
+	req := &pb.GenerateReplyRequest{
+		ProviderConfigs: map[string]*pb.ProviderConfig{
+			"openai": {BaseUrl: "https://example.com"},
+		},
+	}
+	if !hasCustomBaseURL(req) {
+		t.Fatal("expected custom base URL to be detected")
+	}
+
+	req = &pb.GenerateReplyRequest{
+		ProviderConfigs: map[string]*pb.ProviderConfig{
+			"openai": {BaseUrl: "   "},
+		},
+	}
+	if hasCustomBaseURL(req) {
+		t.Fatal("expected whitespace base URL to be ignored")
+	}
+
+	if hasCustomBaseURL(&pb.GenerateReplyRequest{}) {
+		t.Fatal("expected empty provider config to be false")
+	}
+}
+
+func TestBuildProviderConfig_Overrides(t *testing.T) {
+	tenantCfg := &tenant.TenantConfig{
+		TenantID: "tenant1",
+		Providers: map[string]tenant.ProviderConfig{
+			"openai": {
+				Enabled:      true,
+				APIKey:       "tenant-key",
+				Model:        "tenant-model",
+				BaseURL:      "https://tenant.example.com",
+				ExtraOptions: map[string]string{"tenant": "true"},
+			},
+		},
+	}
+
+	ctx := context.WithValue(context.Background(), auth.TenantContextKey, tenantCfg)
+	req := &pb.GenerateReplyRequest{
+		ProviderConfigs: map[string]*pb.ProviderConfig{
+			"openai": {
+				ApiKey:          "request-key",
+				Model:           "request-model",
+				Temperature:     float64Ptr(0.7),
+				TopP:            float64Ptr(0.9),
+				MaxOutputTokens: int32Ptr(2048),
+				BaseUrl:         "https://request.example.com",
+				ExtraOptions:    map[string]string{"req": "yes"},
+			},
+		},
+	}
+
+	svc := NewChatService(nil, nil)
+	cfg := svc.buildProviderConfig(ctx, req, "openai")
+
+	if cfg.APIKey != "tenant-key" {
+		t.Fatalf("APIKey = %q, want tenant-key (request override must be ignored)", cfg.APIKey)
+	}
+	if cfg.Model != "request-model" {
+		t.Fatalf("Model = %q, want request-model", cfg.Model)
+	}
+	if cfg.BaseURL != "https://request.example.com" {
+		t.Fatalf("BaseURL = %q, want request override", cfg.BaseURL)
+	}
+	if cfg.Temperature == nil || *cfg.Temperature != 0.7 {
+		t.Fatalf("Temperature = %v, want 0.7", cfg.Temperature)
+	}
+	if cfg.TopP == nil || *cfg.TopP != 0.9 {
+		t.Fatalf("TopP = %v, want 0.9", cfg.TopP)
+	}
+	if cfg.MaxOutputTokens == nil || *cfg.MaxOutputTokens != 2048 {
+		t.Fatalf("MaxOutputTokens = %v, want 2048", cfg.MaxOutputTokens)
+	}
+	if cfg.ExtraOptions["tenant"] != "true" || cfg.ExtraOptions["req"] != "yes" {
+		t.Fatalf("ExtraOptions = %#v, want merged tenant+req keys", cfg.ExtraOptions)
+	}
+}
+
+func TestSelectProviderWithTenant_DefaultProvider(t *testing.T) {
+	tenantCfg := &tenant.TenantConfig{
+		TenantID: "tenant1",
+		Providers: map[string]tenant.ProviderConfig{
+			"gemini": {Enabled: true, APIKey: "key", Model: "gemini-2.0-flash"},
+		},
+		Failover: tenant.FailoverConfig{Enabled: true, Order: []string{"gemini"}},
+	}
+
+	ctx := context.WithValue(context.Background(), auth.TenantContextKey, tenantCfg)
+	svc := NewChatService(nil, nil)
+
+	provider, err := svc.selectProviderWithTenant(ctx, &pb.GenerateReplyRequest{
+		PreferredProvider: pb.Provider_PROVIDER_UNSPECIFIED,
+	})
+	if err != nil {
+		t.Fatalf("selectProviderWithTenant error: %v", err)
+	}
+	if provider.Name() != "gemini" {
+		t.Fatalf("provider = %q, want gemini", provider.Name())
+	}
+}
+
+func TestFormatRAGContext(t *testing.T) {
+	chunks := []rag.RetrieveResult{
+		{Filename: "doc.txt", Text: "hello world"},
+		{Filename: "notes.md", Text: "second chunk"},
+	}
+
+	got := formatRAGContext(chunks)
+	if !strings.Contains(got, "Relevant context") {
+		t.Fatalf("expected RAG header in output")
+	}
+	if !strings.Contains(got, "doc.txt") || !strings.Contains(got, "notes.md") {
+		t.Fatalf("expected filenames in output: %q", got)
+	}
+}
```

### internal/service/admin_test.go
```diff
diff --git a/internal/service/admin_test.go b/internal/service/admin_test.go
new file mode 100644
index 0000000..0000000
--- /dev/null
+++ b/internal/service/admin_test.go
@@
+package service
+
+import (
+	"context"
+	"testing"
+
+	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
+	"github.com/cliffpyles/aibox/internal/auth"
+	"google.golang.org/grpc/codes"
+	"google.golang.org/grpc/status"
+)
+
+func adminCtx() context.Context {
+	return context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
+		ClientID:    "admin",
+		Permissions: []auth.Permission{auth.PermissionAdmin},
+	})
+}
+
+func TestAdminService_Health(t *testing.T) {
+	svc := NewAdminService(nil, AdminServiceConfig{
+		Version:   "v1",
+		GitCommit: "abc",
+		BuildTime: "now",
+		GoVersion: "go1",
+	})
+
+	resp, err := svc.Health(context.Background(), &pb.HealthRequest{})
+	if err != nil {
+		t.Fatalf("Health() error: %v", err)
+	}
+	if resp.Status != "healthy" {
+		t.Fatalf("Status = %q, want healthy", resp.Status)
+	}
+	if resp.Version != "v1" {
+		t.Fatalf("Version = %q, want v1", resp.Version)
+	}
+}
+
+func TestAdminService_Ready_NoRedis(t *testing.T) {
+	svc := NewAdminService(nil, AdminServiceConfig{Version: "v1"})
+
+	resp, err := svc.Ready(adminCtx(), &pb.ReadyRequest{})
+	if err != nil {
+		t.Fatalf("Ready() error: %v", err)
+	}
+	if resp.Ready {
+		t.Fatal("Ready should be false when Redis is not configured")
+	}
+	dep := resp.Dependencies["redis"]
+	if dep == nil || dep.Healthy {
+		t.Fatalf("expected redis dependency to be unhealthy")
+	}
+}
+
+func TestAdminService_Version_PermissionDenied(t *testing.T) {
+	ctx := context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
+		ClientID:    "client",
+		Permissions: []auth.Permission{auth.PermissionChat},
+	})
+
+	_, err := NewAdminService(nil, AdminServiceConfig{}).Version(ctx, &pb.VersionRequest{})
+	if status.Code(err) != codes.PermissionDenied {
+		t.Fatalf("expected PermissionDenied, got %v", status.Code(err))
+	}
+}
+
+func TestAdminService_Version_Admin(t *testing.T) {
+	svc := NewAdminService(nil, AdminServiceConfig{
+		Version:   "v1",
+		GitCommit: "abc",
+		BuildTime: "now",
+		GoVersion: "go1",
+	})
+
+	resp, err := svc.Version(adminCtx(), &pb.VersionRequest{})
+	if err != nil {
+		t.Fatalf("Version() error: %v", err)
+	}
+	if resp.Version != "v1" || resp.GitCommit != "abc" {
+		t.Fatalf("unexpected version response: %#v", resp)
+	}
+}
```

### internal/auth/tenant_interceptor_test.go
```diff
diff --git a/internal/auth/tenant_interceptor_test.go b/internal/auth/tenant_interceptor_test.go
new file mode 100644
index 0000000..0000000
--- /dev/null
+++ b/internal/auth/tenant_interceptor_test.go
@@
+package auth
+
+import (
+	"context"
+	"testing"
+
+	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
+	"github.com/cliffpyles/aibox/internal/tenant"
+	"google.golang.org/grpc"
+	"google.golang.org/grpc/codes"
+	"google.golang.org/grpc/status"
+)
+
+func newManager(ids ...string) *tenant.Manager {
+	tenants := make(map[string]tenant.TenantConfig, len(ids))
+	for _, id := range ids {
+		tenants[id] = tenant.TenantConfig{TenantID: id}
+	}
+	return &tenant.Manager{Tenants: tenants}
+}
+
+func TestTenantInterceptor_ResolveTenant_SingleTenantDefault(t *testing.T) {
+	ti := NewTenantInterceptor(newManager("tenant1"))
+
+	cfg, err := ti.resolveTenant("")
+	if err != nil {
+		t.Fatalf("resolveTenant error: %v", err)
+	}
+	if cfg.TenantID != "tenant1" {
+		t.Fatalf("TenantID = %q, want tenant1", cfg.TenantID)
+	}
+}
+
+func TestTenantInterceptor_ResolveTenant_RequiresTenantID(t *testing.T) {
+	ti := NewTenantInterceptor(newManager("t1", "t2"))
+
+	_, err := ti.resolveTenant("")
+	if status.Code(err) != codes.InvalidArgument {
+		t.Fatalf("expected InvalidArgument, got %v", status.Code(err))
+	}
+}
+
+func TestTenantInterceptor_ExtractTenantID(t *testing.T) {
+	if got := extractTenantID(&pb.GenerateReplyRequest{TenantId: "t1"}); got != "t1" {
+		t.Fatalf("GenerateReplyRequest tenant = %q, want t1", got)
+	}
+	if got := extractTenantID(&pb.SelectProviderRequest{TenantId: "t2"}); got != "t2" {
+		t.Fatalf("SelectProviderRequest tenant = %q, want t2", got)
+	}
+	if got := extractTenantID(&pb.DeleteFileStoreRequest{StoreId: "store"}); got != "" {
+		t.Fatalf("unexpected tenant from file request: %q", got)
+	}
+}
+
+func TestTenantInterceptor_SkipsFileServiceMethods(t *testing.T) {
+	ti := NewTenantInterceptor(newManager("t1", "t2"))
+	interceptor := ti.UnaryInterceptor()
+
+	called := false
+	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
+		called = true
+		return "ok", nil
+	}
+
+	_, err := interceptor(
+		context.Background(),
+		&pb.DeleteFileStoreRequest{StoreId: "store"},
+		&grpc.UnaryServerInfo{FullMethod: "/aibox.v1.FileService/DeleteFileStore"},
+		handler,
+	)
+	if err != nil {
+		t.Fatalf("unexpected error: %v", err)
+	}
+	if !called {
+		t.Fatal("handler was not invoked")
+	}
+}
```

### internal/config/config_test.go
```diff
diff --git a/internal/config/config_test.go b/internal/config/config_test.go
new file mode 100644
index 0000000..0000000
--- /dev/null
+++ b/internal/config/config_test.go
@@
+package config
+
+import (
+	"os"
+	"path/filepath"
+	"testing"
+)
+
+func TestLoad_EnvOverridesAndExpansion(t *testing.T) {
+	dir := t.TempDir()
+	configPath := filepath.Join(dir, "aibox.yaml")
+
+	data := `server:
+  grpc_port: 50051
+  host: "0.0.0.0"
+redis:
+  addr: "localhost:6379"
+  password: "${REDIS_PASSWORD}"
+  db: 0
+logging:
+  level: "info"
+  format: "json"
+rag:
+  enabled: false
+`
+
+	if err := os.WriteFile(configPath, []byte(data), 0o600); err != nil {
+		t.Fatalf("write config: %v", err)
+	}
+
+	t.Setenv("AIBOX_CONFIG", configPath)
+	t.Setenv("AIBOX_GRPC_PORT", "60001")
+	t.Setenv("AIBOX_HOST", "127.0.0.1")
+	t.Setenv("REDIS_PASSWORD", "secret")
+	t.Setenv("RAG_ENABLED", "true")
+	t.Setenv("RAG_RETRIEVAL_TOP_K", "9")
+
+	cfg, err := Load()
+	if err != nil {
+		t.Fatalf("Load() error: %v", err)
+	}
+	if cfg.Server.GRPCPort != 60001 {
+		t.Fatalf("GRPCPort = %d, want 60001", cfg.Server.GRPCPort)
+	}
+	if cfg.Server.Host != "127.0.0.1" {
+		t.Fatalf("Host = %q, want 127.0.0.1", cfg.Server.Host)
+	}
+	if cfg.Redis.Password != "secret" {
+		t.Fatalf("Redis.Password = %q, want secret", cfg.Redis.Password)
+	}
+	if !cfg.RAG.Enabled || cfg.RAG.RetrievalTopK != 9 {
+		t.Fatalf("RAG overrides not applied: %+v", cfg.RAG)
+	}
+}
```
