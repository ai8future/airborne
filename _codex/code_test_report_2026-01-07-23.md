# AIBox Unit Test Coverage Report
Date Created: 2026-01-07 23:38:09 +0100

## Untested Areas Identified
- cmd/aibox/main.go: runHealthCheck behavior (healthy/unhealthy) has no coverage.
- internal/config/config.go: Load/applyEnvOverrides/expandEnv/validate logic has no coverage.
- internal/tenant/env.go: loadEnv defaults, overrides, and validation lack tests.
- internal/auth/tenant_interceptor.go: resolveTenant, unary/stream interceptor behavior, and extractTenantID lack tests.
- internal/redis/client.go: wrapper methods around go-redis are untested.
- internal/service/admin.go: Health/Ready/Version (permission gates + dependency checks) are untested.
- internal/service/chat.go: base_url security gate, provider selection, failover logic, stream chunk mapping, and RAG helpers are untested.
- internal/provider/provider.go, internal/rag/embedder/extractor interfaces, internal/rag/testutil/mocks, and gen/go/*: interface-only or generated code where unit tests add little value.

## Proposed Unit Tests (High Priority)
- internal/config/config_test.go: Load with file + env overrides, env expansion, validate errors, and invalid env override behavior.
- internal/tenant/env_test.go: default values, env overrides, invalid port/db, TLS cert/key validation.
- internal/auth/tenant_interceptor_test.go: resolveTenant (single/multi-tenant, normalization), unary skip/inject, stream intercept extraction, and extractTenantID.
- internal/redis/client_test.go: basic operations (Set/Get/Del/Exists/Incr/Expire/TTL, hash ops) using miniredis.
- internal/service/admin_test.go: Health response fields, Ready permission checks, Ready redis not configured, Ready redis healthy (miniredis), Version permission gating.
- internal/service/chat_test.go: custom base_url security gate, provider config merging, tenant-based provider selection, failover response fields, stream chunk mapping, and RAG formatting helpers.
- cmd/aibox/main_test.go: runHealthCheck success/unhealthy using a test gRPC AdminService and temp config file.

## Dependency Notes
- miniredis is suggested for reliable Redis unit tests without external services. Diffs below add it plus required go.sum entries.

## Patch-Ready Diffs

```diff
diff --git a/go.mod b/go.mod
index 8b5c8c0..0e7d4af 100644
--- a/go.mod
+++ b/go.mod
@@ -3,6 +3,7 @@ module github.com/cliffpyles/aibox
 go 1.25.5
 
 require (
+	github.com/alicebob/miniredis/v2 v2.33.0
 	github.com/anthropics/anthropic-sdk-go v1.19.0
 	github.com/openai/openai-go v1.12.0
 	github.com/redis/go-redis/v9 v9.17.2
@@ -12,6 +13,7 @@ require (
 )
 
 require (
+	github.com/alicebob/gopher-json v0.0.0-20200520072559-a9ecdc9d1d3a // indirect
 	cloud.google.com/go v0.116.0 // indirect
 	cloud.google.com/go/auth v0.9.3 // indirect
 	cloud.google.com/go/compute/metadata v0.9.0 // indirect
@@ -28,6 +30,7 @@ require (
 	github.com/gorilla/websocket v1.5.3 // indirect
 	github.com/tidwall/gjson v1.18.0 // indirect
 	github.com/tidwall/match v1.1.1 // indirect
 	github.com/tidwall/pretty v1.2.1 // indirect
 	github.com/tidwall/sjson v1.2.5 // indirect
+	github.com/yuin/gopher-lua v1.1.1 // indirect
 	go.opencensus.io v0.24.0 // indirect
 	golang.org/x/net v0.47.0 // indirect
 	golang.org/x/sys v0.39.0 // indirect
```

```diff
diff --git a/go.sum b/go.sum
index c6dc4a9..c6329e7 100644
--- a/go.sum
+++ b/go.sum
@@ -6,6 +6,10 @@ cloud.google.com/go/compute/metadata v0.9.0 h1:pDUj4QMoPejqq20dK0Pg2N4yG9zIkYGdB
 cloud.google.com/go/compute/metadata v0.9.0/go.mod h1:E0bWwX5wTnLPedCKqk3pJmVgCBSM6qQI1yTBdEb3C10=
 github.com/BurntSushi/toml v0.3.1/go.mod h1:xHWCNGjB5oqiDr8zfno3MHue2Ht5sIBksp03qcyfWMU=
+github.com/alicebob/gopher-json v0.0.0-20200520072559-a9ecdc9d1d3a h1:HbKu58rmZpUGpz5+4FfNmIU+FmZg2P3Xaj2v2bfNWmk=
+github.com/alicebob/gopher-json v0.0.0-20200520072559-a9ecdc9d1d3a/go.mod h1:SGnFV6hVsYE877CKEZ6tDNTjaSXYUk6QqoIK6PrAtcc=
+github.com/alicebob/miniredis/v2 v2.33.0 h1:uvTF0EDeu9RLnUEG27Db5I68ESoIxTiXbNUiji6lZrA=
+github.com/alicebob/miniredis/v2 v2.33.0/go.mod h1:MhP4a3EU7aENRi9aO+tHfTBZicLqQevyi/DJpoj6mi0=
 github.com/anthropics/anthropic-sdk-go v1.19.0 h1:mO6E+ffSzLRvR/YUH9KJC0uGw0uV8GjISIuzem//3KE=
 github.com/anthropics/anthropic-sdk-go v1.19.0/go.mod h1:WTz31rIUHUHqai2UslPpw5CwXrQP3geYBioRV4WOLvE=
@@ -88,6 +92,8 @@ github.com/tidwall/pretty v1.2.1 h1:qjsOFOWWQl+N3RsoF5/ssm1pHmJJwhjlSbZ51I6wMl4=
 github.com/tidwall/pretty v1.2.1/go.mod h1:ITEVvHYasfjBbM0u2Pg8T2nJnzm8xPwvNhhsoaGGjNU=
 github.com/tidwall/sjson v1.2.5 h1:kLy8mja+1c9jlljvWTlSazM7cKDRfJuR/bOJhcY5NcY=
 github.com/tidwall/sjson v1.2.5/go.mod h1:Fvgq9kS/6ociJEDnK0Fk1cpYF4FIW6ZF7LAe+6jwd28=
+github.com/yuin/gopher-lua v1.1.1 h1:kYKnWBjvbNP4XLT3+bPEwAXJx262OhaHDWDVOPjL46M=
+github.com/yuin/gopher-lua v1.1.1/go.mod h1:GBR0iDaNXjAgGg9zfCvksxSRnQx76gclCIb7kdAd1Pw=
 go.opencensus.io v0.24.0 h1:y73uSU6J157QMP2kn2r30vwW1A2W2WFwSCGnAVxeaD0=
 go.opencensus.io v0.24.0/go.mod h1:vNK8G9p7aAivkbmorf4v+7Hgx+Zs0yY+0fOtgBfjQKo=
```

```diff
diff --git a/internal/config/config_test.go b/internal/config/config_test.go
new file mode 100644
index 0000000..b3e6b6b
--- /dev/null
+++ b/internal/config/config_test.go
@@ -0,0 +1,88 @@
+package config
+
+import (
+	"os"
+	"path/filepath"
+	"testing"
+)
+
+func TestLoad_ConfigAndEnvOverrides(t *testing.T) {
+	dir := t.TempDir()
+	configPath := filepath.Join(dir, "aibox.yaml")
+
+	yaml := `server:
+  grpc_port: 50052
+  host: "0.0.0.0"
+redis:
+  addr: "localhost:6380"
+  password: "${REDIS_PASS}"
+auth:
+  admin_token: "${ADMIN_TOKEN}"
+tls:
+  enabled: false
+logging:
+  level: "debug"
+`
+	if err := os.WriteFile(configPath, []byte(yaml), 0o600); err != nil {
+		t.Fatalf("write config: %v", err)
+	}
+
+	t.Setenv("AIBOX_CONFIG", configPath)
+	t.Setenv("AIBOX_GRPC_PORT", "60000")
+	t.Setenv("AIBOX_HOST", "127.0.0.1")
+	t.Setenv("REDIS_PASS", "secret")
+	t.Setenv("ADMIN_TOKEN", "admintoken")
+
+	cfg, err := Load()
+	if err != nil {
+		t.Fatalf("Load() error: %v", err)
+	}
+
+	if cfg.Server.GRPCPort != 60000 {
+		t.Fatalf("GRPCPort = %d, want %d", cfg.Server.GRPCPort, 60000)
+	}
+	if cfg.Server.Host != "127.0.0.1" {
+		t.Fatalf("Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
+	}
+	if cfg.Redis.Addr != "localhost:6380" {
+		t.Fatalf("Redis.Addr = %q, want %q", cfg.Redis.Addr, "localhost:6380")
+	}
+	if cfg.Redis.Password != "secret" {
+		t.Fatalf("Redis.Password = %q, want %q", cfg.Redis.Password, "secret")
+	}
+	if cfg.Auth.AdminToken != "admintoken" {
+		t.Fatalf("Auth.AdminToken = %q, want %q", cfg.Auth.AdminToken, "admintoken")
+	}
+}
+
+func TestConfigValidate_InvalidPort(t *testing.T) {
+	cfg := defaultConfig()
+	cfg.Server.GRPCPort = 70000
+
+	if err := cfg.validate(); err == nil {
+		t.Fatal("expected error for invalid grpc_port")
+	}
+}
+
+func TestConfigValidate_TLSRequiresCertAndKey(t *testing.T) {
+	cfg := defaultConfig()
+	cfg.TLS.Enabled = true
+	cfg.TLS.CertFile = ""
+	cfg.TLS.KeyFile = ""
+
+	if err := cfg.validate(); err == nil {
+		t.Fatal("expected error for missing TLS cert/key")
+	}
+
+	cfg.TLS.CertFile = "cert.pem"
+	if err := cfg.validate(); err == nil {
+		t.Fatal("expected error for missing TLS key")
+	}
+}
+
+func TestApplyEnvOverrides_InvalidPortIgnored(t *testing.T) {
+	cfg := defaultConfig()
+	t.Setenv("AIBOX_GRPC_PORT", "not-a-number")
+
+	cfg.applyEnvOverrides()
+
+	if cfg.Server.GRPCPort != 50051 {
+		t.Fatalf("GRPCPort = %d, want %d", cfg.Server.GRPCPort, 50051)
+	}
+}
```

```diff
diff --git a/internal/tenant/env_test.go b/internal/tenant/env_test.go
new file mode 100644
index 0000000..7d2db2f
--- /dev/null
+++ b/internal/tenant/env_test.go
@@ -0,0 +1,92 @@
+package tenant
+
+import "testing"
+
+func clearEnv(t *testing.T, keys ...string) {
+	t.Helper()
+	for _, key := range keys {
+		t.Setenv(key, "")
+	}
+}
+
+func TestLoadEnv_Defaults(t *testing.T) {
+	clearEnv(t,
+		"AIBOX_CONFIGS_DIR",
+		"AIBOX_GRPC_PORT",
+		"AIBOX_HOST",
+		"AIBOX_TLS_ENABLED",
+		"AIBOX_TLS_CERT_FILE",
+		"AIBOX_TLS_KEY_FILE",
+		"REDIS_ADDR",
+		"REDIS_PASSWORD",
+		"REDIS_DB",
+		"AIBOX_LOG_LEVEL",
+		"AIBOX_LOG_FORMAT",
+		"AIBOX_ADMIN_TOKEN",
+	)
+
+	cfg, err := loadEnv()
+	if err != nil {
+		t.Fatalf("loadEnv() error: %v", err)
+	}
+
+	if cfg.ConfigsDir != "configs" {
+		t.Fatalf("ConfigsDir = %q, want %q", cfg.ConfigsDir, "configs")
+	}
+	if cfg.GRPCPort != 50051 {
+		t.Fatalf("GRPCPort = %d, want %d", cfg.GRPCPort, 50051)
+	}
+	if cfg.Host != "0.0.0.0" {
+		t.Fatalf("Host = %q, want %q", cfg.Host, "0.0.0.0")
+	}
+	if cfg.RedisAddr != "localhost:6379" {
+		t.Fatalf("RedisAddr = %q, want %q", cfg.RedisAddr, "localhost:6379")
+	}
+	if cfg.RedisDB != 0 {
+		t.Fatalf("RedisDB = %d, want %d", cfg.RedisDB, 0)
+	}
+	if cfg.LogLevel != "info" {
+		t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "info")
+	}
+	if cfg.LogFormat != "json" {
+		t.Fatalf("LogFormat = %q, want %q", cfg.LogFormat, "json")
+	}
+}
+
+func TestLoadEnv_Overrides(t *testing.T) {
+	t.Setenv("AIBOX_CONFIGS_DIR", "configs/custom")
+	t.Setenv("AIBOX_GRPC_PORT", "1234")
+	t.Setenv("AIBOX_HOST", "127.0.0.1")
+	t.Setenv("AIBOX_TLS_ENABLED", "true")
+	t.Setenv("AIBOX_TLS_CERT_FILE", "cert.pem")
+	t.Setenv("AIBOX_TLS_KEY_FILE", "key.pem")
+	t.Setenv("REDIS_ADDR", "redis:6379")
+	t.Setenv("REDIS_PASSWORD", "secret")
+	t.Setenv("REDIS_DB", "5")
+	t.Setenv("AIBOX_LOG_LEVEL", "debug")
+	t.Setenv("AIBOX_LOG_FORMAT", "text")
+	t.Setenv("AIBOX_ADMIN_TOKEN", "admin")
+
+	cfg, err := loadEnv()
+	if err != nil {
+		t.Fatalf("loadEnv() error: %v", err)
+	}
+
+	if cfg.ConfigsDir != "configs/custom" {
+		t.Fatalf("ConfigsDir = %q, want %q", cfg.ConfigsDir, "configs/custom")
+	}
+	if cfg.GRPCPort != 1234 {
+		t.Fatalf("GRPCPort = %d, want %d", cfg.GRPCPort, 1234)
+	}
+	if cfg.Host != "127.0.0.1" {
+		t.Fatalf("Host = %q, want %q", cfg.Host, "127.0.0.1")
+	}
+	if !cfg.TLSEnabled {
+		t.Fatal("TLSEnabled = false, want true")
+	}
+	if cfg.TLSCertFile != "cert.pem" || cfg.TLSKeyFile != "key.pem" {
+		t.Fatalf("TLS cert/key = %q/%q, want %q/%q", cfg.TLSCertFile, cfg.TLSKeyFile, "cert.pem", "key.pem")
+	}
+	if cfg.RedisAddr != "redis:6379" {
+		t.Fatalf("RedisAddr = %q, want %q", cfg.RedisAddr, "redis:6379")
+	}
+	if cfg.RedisPassword != "secret" {
+		t.Fatalf("RedisPassword = %q, want %q", cfg.RedisPassword, "secret")
+	}
+	if cfg.RedisDB != 5 {
+		t.Fatalf("RedisDB = %d, want %d", cfg.RedisDB, 5)
+	}
+	if cfg.LogLevel != "debug" {
+		t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
+	}
+	if cfg.LogFormat != "text" {
+		t.Fatalf("LogFormat = %q, want %q", cfg.LogFormat, "text")
+	}
+	if cfg.AdminToken != "admin" {
+		t.Fatalf("AdminToken = %q, want %q", cfg.AdminToken, "admin")
+	}
+}
+
+func TestLoadEnv_InvalidPort(t *testing.T) {
+	t.Setenv("AIBOX_GRPC_PORT", "not-a-number")
+
+	_, err := loadEnv()
+	if err == nil {
+		t.Fatal("expected error for invalid AIBOX_GRPC_PORT")
+	}
+}
+
+func TestLoadEnv_TLSMissingCertOrKey(t *testing.T) {
+	t.Setenv("AIBOX_TLS_ENABLED", "true")
+	t.Setenv("AIBOX_TLS_CERT_FILE", "")
+	t.Setenv("AIBOX_TLS_KEY_FILE", "")
+
+	_, err := loadEnv()
+	if err == nil {
+		t.Fatal("expected error for missing TLS cert/key")
+	}
+}
```

```diff
diff --git a/internal/auth/tenant_interceptor_test.go b/internal/auth/tenant_interceptor_test.go
new file mode 100644
index 0000000..4cb5f2f
--- /dev/null
+++ b/internal/auth/tenant_interceptor_test.go
@@ -0,0 +1,218 @@
+package auth
+
+import (
+	"context"
+	"fmt"
+	"io"
+	"testing"
+
+	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
+	"github.com/cliffpyles/aibox/internal/tenant"
+	"google.golang.org/grpc"
+	"google.golang.org/grpc/codes"
+	"google.golang.org/grpc/metadata"
+	"google.golang.org/grpc/status"
+)
+
+type mockServerStream struct {
+	grpc.ServerStream
+	ctx  context.Context
+	msgs []interface{}
+	idx  int
+}
+
+func (m *mockServerStream) Context() context.Context {
+	return m.ctx
+}
+
+func (m *mockServerStream) RecvMsg(msg interface{}) error {
+	if m.idx >= len(m.msgs) {
+		return io.EOF
+	}
+
+	next := m.msgs[m.idx]
+	m.idx++
+
+	switch dst := msg.(type) {
+	case *pb.GenerateReplyRequest:
+		*dst = *next.(*pb.GenerateReplyRequest)
+	case *pb.SelectProviderRequest:
+		*dst = *next.(*pb.SelectProviderRequest)
+	default:
+		return fmt.Errorf("unsupported message type %T", msg)
+	}
+
+	return nil
+}
+
+func (m *mockServerStream) SendMsg(interface{}) error { return nil }
+func (m *mockServerStream) SetHeader(metadata.MD) error { return nil }
+func (m *mockServerStream) SendHeader(metadata.MD) error { return nil }
+func (m *mockServerStream) SetTrailer(metadata.MD) {}
+
+func TestResolveTenant_SingleTenantDefault(t *testing.T) {
+	mgr := &tenant.Manager{
+		Tenants: map[string]tenant.TenantConfig{
+			"tenant1": {TenantID: "tenant1"},
+		},
+	}
+
+	interceptor := NewTenantInterceptor(mgr)
+	cfg, err := interceptor.resolveTenant("")
+	if err != nil {
+		t.Fatalf("resolveTenant() error: %v", err)
+	}
+	if cfg.TenantID != "tenant1" {
+		t.Fatalf("TenantID = %q, want %q", cfg.TenantID, "tenant1")
+	}
+}
+
+func TestResolveTenant_MultiTenantMissing(t *testing.T) {
+	mgr := &tenant.Manager{
+		Tenants: map[string]tenant.TenantConfig{
+			"tenant1": {TenantID: "tenant1"},
+			"tenant2": {TenantID: "tenant2"},
+		},
+	}
+
+	interceptor := NewTenantInterceptor(mgr)
+	_, err := interceptor.resolveTenant("")
+	if err == nil {
+		t.Fatal("expected error for missing tenant_id")
+	}
+	if status.Code(err) != codes.InvalidArgument {
+		t.Fatalf("error code = %v, want %v", status.Code(err), codes.InvalidArgument)
+	}
+}
+
+func TestResolveTenant_NotFound(t *testing.T) {
+	mgr := &tenant.Manager{
+		Tenants: map[string]tenant.TenantConfig{
+			"tenant1": {TenantID: "tenant1"},
+		},
+	}
+
+	interceptor := NewTenantInterceptor(mgr)
+	_, err := interceptor.resolveTenant("missing")
+	if err == nil {
+		t.Fatal("expected error for missing tenant")
+	}
+	if status.Code(err) != codes.NotFound {
+		t.Fatalf("error code = %v, want %v", status.Code(err), codes.NotFound)
+	}
+}
+
+func TestResolveTenant_Normalizes(t *testing.T) {
+	mgr := &tenant.Manager{
+		Tenants: map[string]tenant.TenantConfig{
+			"tenant1": {TenantID: "tenant1"},
+		},
+	}
+
+	interceptor := NewTenantInterceptor(mgr)
+	cfg, err := interceptor.resolveTenant("  TENANT1  ")
+	if err != nil {
+		t.Fatalf("resolveTenant() error: %v", err)
+	}
+	if cfg.TenantID != "tenant1" {
+		t.Fatalf("TenantID = %q, want %q", cfg.TenantID, "tenant1")
+	}
+}
+
+func TestExtractTenantID(t *testing.T) {
+	if got := extractTenantID(&pb.GenerateReplyRequest{TenantId: "tenant1"}); got != "tenant1" {
+		t.Fatalf("GenerateReplyRequest tenant = %q, want %q", got, "tenant1")
+	}
+	if got := extractTenantID(&pb.SelectProviderRequest{TenantId: "tenant2"}); got != "tenant2" {
+		t.Fatalf("SelectProviderRequest tenant = %q, want %q", got, "tenant2")
+	}
+	if got := extractTenantID(&pb.HealthRequest{}); got != "" {
+		t.Fatalf("unknown request tenant = %q, want empty", got)
+	}
+}
+
+func TestUnaryInterceptor_SkipMethod(t *testing.T) {
+	mgr := &tenant.Manager{Tenants: map[string]tenant.TenantConfig{"tenant1": {TenantID: "tenant1"}}}
+	interceptor := NewTenantInterceptor(mgr)
+	called := false
+
+	info := &grpc.UnaryServerInfo{FullMethod: "/aibox.v1.AdminService/Health"}
+	_, err := interceptor.UnaryInterceptor()(context.Background(), &pb.HealthRequest{}, info, func(ctx context.Context, req interface{}) (interface{}, error) {
+		called = true
+		if TenantFromContext(ctx) != nil {
+			return nil, fmt.Errorf("tenant should not be injected for health")
+		}
+		return "ok", nil
+	})
+
+	if err != nil {
+		t.Fatalf("unexpected error: %v", err)
+	}
+	if !called {
+		t.Fatal("handler not called")
+	}
+}
+
+func TestUnaryInterceptor_InsertsTenant(t *testing.T) {
+	mgr := &tenant.Manager{Tenants: map[string]tenant.TenantConfig{"tenant1": {TenantID: "tenant1"}}}
+	interceptor := NewTenantInterceptor(mgr)
+
+	info := &grpc.UnaryServerInfo{FullMethod: "/aibox.v1.AIBoxService/GenerateReply"}
+	_, err := interceptor.UnaryInterceptor()(context.Background(), &pb.GenerateReplyRequest{TenantId: "tenant1"}, info, func(ctx context.Context, req interface{}) (interface{}, error) {
+		cfg := TenantFromContext(ctx)
+		if cfg == nil || cfg.TenantID != "tenant1" {
+			return nil, fmt.Errorf("expected tenant to be injected")
+		}
+		return "ok", nil
+	})
+
+	if err != nil {
+		t.Fatalf("unexpected error: %v", err)
+	}
+}
+
+func TestStreamInterceptor_InsertsTenant(t *testing.T) {
+	mgr := &tenant.Manager{Tenants: map[string]tenant.TenantConfig{"tenant1": {TenantID: "tenant1"}}}
+	interceptor := NewTenantInterceptor(mgr)
+
+	stream := &mockServerStream{
+		ctx: context.Background(),
+		msgs: []interface{}{
+			&pb.GenerateReplyRequest{TenantId: "tenant1"},
+		},
+	}
+
+	info := &grpc.StreamServerInfo{FullMethod: "/aibox.v1.AIBoxService/GenerateReplyStream"}
+	err := interceptor.StreamInterceptor()(nil, stream, info, func(srv interface{}, ss grpc.ServerStream) error {
+		var req pb.GenerateReplyRequest
+		if err := ss.RecvMsg(&req); err != nil {
+			return err
+		}
+		cfg := TenantFromContext(ss.Context())
+		if cfg == nil || cfg.TenantID != "tenant1" {
+			return status.Error(codes.Internal, "tenant not injected")
+		}
+		return nil
+	})
+
+	if err != nil {
+		t.Fatalf("unexpected error: %v", err)
+	}
+}
+
+func TestStreamInterceptor_MissingTenant(t *testing.T) {
+	mgr := &tenant.Manager{Tenants: map[string]tenant.TenantConfig{"tenant1": {TenantID: "tenant1"}, "tenant2": {TenantID: "tenant2"}}}
+	interceptor := NewTenantInterceptor(mgr)
+
+	stream := &mockServerStream{
+		ctx:  context.Background(),
+		msgs: []interface{}{&pb.GenerateReplyRequest{}},
+	}
+
+	info := &grpc.StreamServerInfo{FullMethod: "/aibox.v1.AIBoxService/GenerateReplyStream"}
+	err := interceptor.StreamInterceptor()(nil, stream, info, func(srv interface{}, ss grpc.ServerStream) error {
+		var req pb.GenerateReplyRequest
+		return ss.RecvMsg(&req)
+	})
+
+	if err == nil {
+		t.Fatal("expected error for missing tenant_id")
+	}
+	if status.Code(err) != codes.InvalidArgument {
+		t.Fatalf("error code = %v, want %v", status.Code(err), codes.InvalidArgument)
+	}
+}
```

```diff
diff --git a/internal/redis/client_test.go b/internal/redis/client_test.go
new file mode 100644
index 0000000..d4e5bcd
--- /dev/null
+++ b/internal/redis/client_test.go
@@ -0,0 +1,132 @@
+package redis
+
+import (
+	"context"
+	"errors"
+	"testing"
+	"time"
+
+	miniredis "github.com/alicebob/miniredis/v2"
+	goredis "github.com/redis/go-redis/v9"
+)
+
+func TestNewClient_Success(t *testing.T) {
+	s := miniredis.RunT(t)
+	client, err := NewClient(Config{Addr: s.Addr()})
+	if err != nil {
+		t.Fatalf("NewClient() error: %v", err)
+	}
+	defer client.Close()
+}
+
+func TestClient_SetGetDelExists(t *testing.T) {
+	s := miniredis.RunT(t)
+	client, err := NewClient(Config{Addr: s.Addr()})
+	if err != nil {
+		t.Fatalf("NewClient() error: %v", err)
+	}
+	defer client.Close()
+
+	ctx := context.Background()
+
+	if err := client.Set(ctx, "key", "value", 0); err != nil {
+		t.Fatalf("Set() error: %v", err)
+	}
+
+	val, err := client.Get(ctx, "key")
+	if err != nil {
+		t.Fatalf("Get() error: %v", err)
+	}
+	if val != "value" {
+		t.Fatalf("Get() = %q, want %q", val, "value")
+	}
+
+	exists, err := client.Exists(ctx, "key")
+	if err != nil {
+		t.Fatalf("Exists() error: %v", err)
+	}
+	if exists != 1 {
+		t.Fatalf("Exists() = %d, want %d", exists, 1)
+	}
+
+	if err := client.Del(ctx, "key"); err != nil {
+		t.Fatalf("Del() error: %v", err)
+	}
+
+	_, err = client.Get(ctx, "key")
+	if !IsNil(err) {
+		t.Fatalf("expected redis.Nil, got %v", err)
+	}
+}
+
+func TestClient_IncrExpireTTL(t *testing.T) {
+	s := miniredis.RunT(t)
+	client, err := NewClient(Config{Addr: s.Addr()})
+	if err != nil {
+		t.Fatalf("NewClient() error: %v", err)
+	}
+	defer client.Close()
+
+	ctx := context.Background()
+
+	count, err := client.Incr(ctx, "counter")
+	if err != nil {
+		t.Fatalf("Incr() error: %v", err)
+	}
+	if count != 1 {
+		t.Fatalf("Incr() = %d, want %d", count, 1)
+	}
+
+	count, err = client.IncrBy(ctx, "counter", 4)
+	if err != nil {
+		t.Fatalf("IncrBy() error: %v", err)
+	}
+	if count != 5 {
+		t.Fatalf("IncrBy() = %d, want %d", count, 5)
+	}
+
+	if err := client.Expire(ctx, "counter", time.Minute); err != nil {
+		t.Fatalf("Expire() error: %v", err)
+	}
+
+	ttl, err := client.TTL(ctx, "counter")
+	if err != nil {
+		t.Fatalf("TTL() error: %v", err)
+	}
+	if ttl <= 0 {
+		t.Fatalf("TTL() = %v, want > 0", ttl)
+	}
+}
+
+func TestClient_HashOps(t *testing.T) {
+	s := miniredis.RunT(t)
+	client, err := NewClient(Config{Addr: s.Addr()})
+	if err != nil {
+		t.Fatalf("NewClient() error: %v", err)
+	}
+	defer client.Close()
+
+	ctx := context.Background()
+
+	if err := client.HSet(ctx, "hash", "field1", "value1", "field2", "value2"); err != nil {
+		t.Fatalf("HSet() error: %v", err)
+	}
+
+	val, err := client.HGet(ctx, "hash", "field1")
+	if err != nil {
+		t.Fatalf("HGet() error: %v", err)
+	}
+	if val != "value1" {
+		t.Fatalf("HGet() = %q, want %q", val, "value1")
+	}
+
+	all, err := client.HGetAll(ctx, "hash")
+	if err != nil {
+		t.Fatalf("HGetAll() error: %v", err)
+	}
+	if len(all) != 2 {
+		t.Fatalf("HGetAll() len = %d, want %d", len(all), 2)
+	}
+
+	if err := client.HDel(ctx, "hash", "field1"); err != nil {
+		t.Fatalf("HDel() error: %v", err)
+	}
+
+	_, err = client.HGet(ctx, "hash", "field1")
+	if !IsNil(err) {
+		t.Fatalf("expected redis.Nil, got %v", err)
+	}
+}
+
+func TestIsNil(t *testing.T) {
+	if !IsNil(goredis.Nil) {
+		t.Fatal("expected IsNil(redis.Nil) to be true")
+	}
+	if IsNil(errors.New("boom")) {
+		t.Fatal("expected IsNil(error) to be false")
+	}
+}
```

```diff
diff --git a/internal/service/admin_test.go b/internal/service/admin_test.go
new file mode 100644
index 0000000..884e74b
--- /dev/null
+++ b/internal/service/admin_test.go
@@ -0,0 +1,120 @@
+package service
+
+import (
+	"context"
+	"testing"
+
+	miniredis "github.com/alicebob/miniredis/v2"
+	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
+	"github.com/cliffpyles/aibox/internal/auth"
+	redisclient "github.com/cliffpyles/aibox/internal/redis"
+	"google.golang.org/grpc/codes"
+	"google.golang.org/grpc/status"
+)
+
+func ctxWithAdminPermission() context.Context {
+	return context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
+		ClientID:    "admin",
+		Permissions: []auth.Permission{auth.PermissionAdmin},
+	})
+}
+
+func TestAdminService_Health(t *testing.T) {
+	svc := NewAdminService(nil, AdminServiceConfig{Version: "1.2.3"})
+
+	resp, err := svc.Health(context.Background(), &pb.HealthRequest{})
+	if err != nil {
+		t.Fatalf("Health() error: %v", err)
+	}
+	if resp.Status != "healthy" {
+		t.Fatalf("Status = %q, want %q", resp.Status, "healthy")
+	}
+	if resp.Version != "1.2.3" {
+		t.Fatalf("Version = %q, want %q", resp.Version, "1.2.3")
+	}
+}
+
+func TestAdminService_Ready_PermissionDenied(t *testing.T) {
+	svc := NewAdminService(nil, AdminServiceConfig{})
+
+	_, err := svc.Ready(context.Background(), &pb.ReadyRequest{})
+	if err == nil {
+		t.Fatal("expected permission error")
+	}
+	if status.Code(err) != codes.PermissionDenied {
+		t.Fatalf("error code = %v, want %v", status.Code(err), codes.PermissionDenied)
+	}
+}
+
+func TestAdminService_Ready_NoRedis(t *testing.T) {
+	svc := NewAdminService(nil, AdminServiceConfig{})
+
+	resp, err := svc.Ready(ctxWithAdminPermission(), &pb.ReadyRequest{})
+	if err != nil {
+		t.Fatalf("Ready() error: %v", err)
+	}
+	if resp.Ready {
+		t.Fatal("Ready = true, want false")
+	}
+	redisStatus := resp.Dependencies["redis"]
+	if redisStatus == nil || redisStatus.Healthy {
+		t.Fatal("redis dependency should be unhealthy when not configured")
+	}
+	if redisStatus.Message != "not configured" {
+		t.Fatalf("redis message = %q, want %q", redisStatus.Message, "not configured")
+	}
+}
+
+func TestAdminService_Ready_RedisHealthy(t *testing.T) {
+	s := miniredis.RunT(t)
+	client, err := redisclient.NewClient(redisclient.Config{Addr: s.Addr()})
+	if err != nil {
+		t.Fatalf("NewClient() error: %v", err)
+	}
+	defer client.Close()
+
+	svc := NewAdminService(client, AdminServiceConfig{})
+	resp, err := svc.Ready(ctxWithAdminPermission(), &pb.ReadyRequest{})
+	if err != nil {
+		t.Fatalf("Ready() error: %v", err)
+	}
+	if !resp.Ready {
+		t.Fatal("Ready = false, want true")
+	}
+	redisStatus := resp.Dependencies["redis"]
+	if redisStatus == nil || !redisStatus.Healthy {
+		t.Fatal("redis dependency should be healthy")
+	}
+}
+
+func TestAdminService_Version_PermissionDenied(t *testing.T) {
+	svc := NewAdminService(nil, AdminServiceConfig{})
+
+	_, err := svc.Version(context.Background(), &pb.VersionRequest{})
+	if err == nil {
+		t.Fatal("expected permission error")
+	}
+	if status.Code(err) != codes.PermissionDenied {
+		t.Fatalf("error code = %v, want %v", status.Code(err), codes.PermissionDenied)
+	}
+}
+
+func TestAdminService_Version_Success(t *testing.T) {
+	svc := NewAdminService(nil, AdminServiceConfig{
+		Version:   "1.0.0",
+		GitCommit: "abc123",
+		BuildTime: "2026-01-01",
+		GoVersion: "go1.25",
+	})
+
+	resp, err := svc.Version(ctxWithAdminPermission(), &pb.VersionRequest{})
+	if err != nil {
+		t.Fatalf("Version() error: %v", err)
+	}
+	if resp.Version != "1.0.0" || resp.GitCommit != "abc123" || resp.BuildTime != "2026-01-01" || resp.GoVersion != "go1.25" {
+		t.Fatalf("unexpected version response: %+v", resp)
+	}
+}
```

```diff
diff --git a/internal/service/chat_test.go b/internal/service/chat_test.go
new file mode 100644
index 0000000..27a3ae5
--- /dev/null
+++ b/internal/service/chat_test.go
@@ -0,0 +1,262 @@
+package service
+
+import (
+	"context"
+	"errors"
+	"strings"
+	"testing"
+
+	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
+	"github.com/cliffpyles/aibox/internal/auth"
+	"github.com/cliffpyles/aibox/internal/provider"
+	"github.com/cliffpyles/aibox/internal/rag"
+	"github.com/cliffpyles/aibox/internal/tenant"
+	"google.golang.org/grpc/codes"
+	"google.golang.org/grpc/metadata"
+	"google.golang.org/grpc/status"
+)
+
+type fakeProvider struct {
+	name           string
+	generateResult provider.GenerateResult
+	generateErr    error
+	streamChunks   []provider.StreamChunk
+	streamErr      error
+}
+
+func (f *fakeProvider) Name() string { return f.name }
+
+func (f *fakeProvider) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
+	if f.generateErr != nil {
+		return provider.GenerateResult{}, f.generateErr
+	}
+	return f.generateResult, nil
+}
+
+func (f *fakeProvider) GenerateReplyStream(ctx context.Context, params provider.GenerateParams) (<-chan provider.StreamChunk, error) {
+	if f.streamErr != nil {
+		return nil, f.streamErr
+	}
+	ch := make(chan provider.StreamChunk, len(f.streamChunks))
+	for _, chunk := range f.streamChunks {
+		ch <- chunk
+	}
+	close(ch)
+	return ch, nil
+}
+
+func (f *fakeProvider) SupportsFileSearch() bool { return true }
+func (f *fakeProvider) SupportsWebSearch() bool { return true }
+func (f *fakeProvider) SupportsNativeContinuity() bool { return true }
+func (f *fakeProvider) SupportsStreaming() bool { return true }
+
+type mockGenerateReplyStream struct {
+	pb.AIBoxService_GenerateReplyStreamServer
+	ctx  context.Context
+	sent []*pb.GenerateReplyChunk
+}
+
+func (m *mockGenerateReplyStream) Context() context.Context { return m.ctx }
+func (m *mockGenerateReplyStream) Send(chunk *pb.GenerateReplyChunk) error {
+	m.sent = append(m.sent, chunk)
+	return nil
+}
+func (m *mockGenerateReplyStream) SetHeader(metadata.MD) error { return nil }
+func (m *mockGenerateReplyStream) SendHeader(metadata.MD) error { return nil }
+func (m *mockGenerateReplyStream) SetTrailer(metadata.MD) {}
+func (m *mockGenerateReplyStream) RecvMsg(interface{}) error { return nil }
+func (m *mockGenerateReplyStream) SendMsg(interface{}) error { return nil }
+
+func ctxWithPermissions(perms ...auth.Permission) context.Context {
+	return context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
+		ClientID:    "client",
+		Permissions: perms,
+	})
+}
+
+func float64Ptr(v float64) *float64 { return &v }
+func int32Ptr(v int32) *int32       { return &v }
+
+func TestHasCustomBaseURL(t *testing.T) {
+	withURL := &pb.GenerateReplyRequest{
+		ProviderConfigs: map[string]*pb.ProviderConfig{
+			"openai": {BaseUrl: " https://example.com "},
+		},
+	}
+	if !hasCustomBaseURL(withURL) {
+		t.Fatal("expected base_url detection to be true")
+	}
+
+	withoutURL := &pb.GenerateReplyRequest{
+		ProviderConfigs: map[string]*pb.ProviderConfig{
+			"openai": {BaseUrl: "   "},
+		},
+	}
+	if hasCustomBaseURL(withoutURL) {
+		t.Fatal("expected base_url detection to be false")
+	}
+}
+
+func TestBuildProviderConfig_MergesTenantAndOverrides(t *testing.T) {
+	tenantTemp := 0.2
+	tenantTopP := 0.3
+	tenantMax := 1000
+
+	tenantCfg := &tenant.TenantConfig{
+		TenantID: "tenant1",
+		Providers: map[string]tenant.ProviderConfig{
+			"openai": {
+				Enabled:         true,
+				APIKey:          "tenant-key",
+				Model:           "tenant-model",
+				Temperature:     &tenantTemp,
+				TopP:            &tenantTopP,
+				MaxOutputTokens: &tenantMax,
+				BaseURL:         "https://tenant",
+				ExtraOptions:    map[string]string{"foo": "bar"},
+			},
+		},
+	}
+
+	req := &pb.GenerateReplyRequest{
+		ProviderConfigs: map[string]*pb.ProviderConfig{
+			"openai": {
+				ApiKey:          "evil",
+				Model:           "override-model",
+				Temperature:     float64Ptr(0.7),
+				TopP:            float64Ptr(0.8),
+				MaxOutputTokens: int32Ptr(2000),
+				BaseUrl:         "https://override",
+				ExtraOptions:    map[string]string{"foo": "override", "baz": "qux"},
+			},
+		},
+	}
+
+	ctx := context.WithValue(context.Background(), auth.TenantContextKey, tenantCfg)
+	svc := &ChatService{}
+
+	cfg := svc.buildProviderConfig(ctx, req, "openai")
+	if cfg.APIKey != "tenant-key" {
+		t.Fatalf("APIKey = %q, want %q", cfg.APIKey, "tenant-key")
+	}
+	if cfg.Model != "override-model" {
+		t.Fatalf("Model = %q, want %q", cfg.Model, "override-model")
+	}
+	if cfg.Temperature == nil || *cfg.Temperature != 0.7 {
+		t.Fatalf("Temperature = %v, want %v", cfg.Temperature, 0.7)
+	}
+	if cfg.TopP == nil || *cfg.TopP != 0.8 {
+		t.Fatalf("TopP = %v, want %v", cfg.TopP, 0.8)
+	}
+	if cfg.MaxOutputTokens == nil || *cfg.MaxOutputTokens != 2000 {
+		t.Fatalf("MaxOutputTokens = %v, want %v", cfg.MaxOutputTokens, 2000)
+	}
+	if cfg.BaseURL != "https://override" {
+		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, "https://override")
+	}
+	if cfg.ExtraOptions["foo"] != "override" || cfg.ExtraOptions["baz"] != "qux" {
+		t.Fatalf("ExtraOptions = %#v", cfg.ExtraOptions)
+	}
+}
+
+func TestSelectProviderWithTenant_DefaultFromTenant(t *testing.T) {
+	openai := &fakeProvider{name: "openai"}
+	gemini := &fakeProvider{name: "gemini"}
+	anthropic := &fakeProvider{name: "anthropic"}
+	service := &ChatService{openaiProvider: openai, geminiProvider: gemini, anthropicProvider: anthropic}
+
+	tenantCfg := &tenant.TenantConfig{
+		TenantID: "tenant1",
+		Failover: tenant.FailoverConfig{Enabled: true, Order: []string{"gemini", "openai"}},
+		Providers: map[string]tenant.ProviderConfig{
+			"openai": {Enabled: true},
+			"gemini": {Enabled: true},
+		},
+	}
+	ctx := context.WithValue(context.Background(), auth.TenantContextKey, tenantCfg)
+
+	provider, err := service.selectProviderWithTenant(ctx, &pb.GenerateReplyRequest{PreferredProvider: pb.Provider_PROVIDER_UNSPECIFIED})
+	if err != nil {
+		t.Fatalf("selectProviderWithTenant() error: %v", err)
+	}
+	if provider.Name() != "gemini" {
+		t.Fatalf("provider = %q, want %q", provider.Name(), "gemini")
+	}
+}
+
+func TestSelectProviderWithTenant_DisabledProvider(t *testing.T) {
+	service := &ChatService{openaiProvider: &fakeProvider{name: "openai"}, geminiProvider: &fakeProvider{name: "gemini"}, anthropicProvider: &fakeProvider{name: "anthropic"}}
+
+	tenantCfg := &tenant.TenantConfig{
+		TenantID: "tenant1",
+		Providers: map[string]tenant.ProviderConfig{
+			"openai":  {Enabled: true},
+			"gemini":  {Enabled: false},
+			"anthropic": {Enabled: false},
+		},
+	}
+	ctx := context.WithValue(context.Background(), auth.TenantContextKey, tenantCfg)
+
+	_, err := service.selectProviderWithTenant(ctx, &pb.GenerateReplyRequest{PreferredProvider: pb.Provider_PROVIDER_GEMINI})
+	if err == nil {
+		t.Fatal("expected error for disabled provider")
+	}
+}
+
+func TestGenerateReply_CustomBaseURLRequiresAdmin(t *testing.T) {
+	service := &ChatService{openaiProvider: &fakeProvider{name: "openai"}, geminiProvider: &fakeProvider{name: "gemini"}, anthropicProvider: &fakeProvider{name: "anthropic"}}
+	req := &pb.GenerateReplyRequest{
+		UserInput: "hello",
+		ProviderConfigs: map[string]*pb.ProviderConfig{
+			"openai": {BaseUrl: "http://example.com"},
+		},
+	}
+
+	_, err := service.GenerateReply(ctxWithPermissions(auth.PermissionChat), req)
+	if err == nil {
+		t.Fatal("expected permission error")
+	}
+	if status.Code(err) != codes.PermissionDenied {
+		t.Fatalf("error code = %v, want %v", status.Code(err), codes.PermissionDenied)
+	}
+}
+
+func TestGenerateReply_Failover(t *testing.T) {
+	openai := &fakeProvider{name: "openai", generateErr: errors.New("primary failed")}
+	gemini := &fakeProvider{name: "gemini", generateResult: provider.GenerateResult{Text: "fallback", Model: "g1"}}
+	service := &ChatService{openaiProvider: openai, geminiProvider: gemini, anthropicProvider: &fakeProvider{name: "anthropic"}}
+
+	req := &pb.GenerateReplyRequest{
+		UserInput:       "hello",
+		PreferredProvider: pb.Provider_PROVIDER_OPENAI,
+		EnableFailover:  true,
+	}
+
+	resp, err := service.GenerateReply(ctxWithPermissions(auth.PermissionChat), req)
+	if err != nil {
+		t.Fatalf("GenerateReply() error: %v", err)
+	}
+	if !resp.FailedOver {
+		t.Fatal("FailedOver = false, want true")
+	}
+	if resp.Provider != pb.Provider_PROVIDER_GEMINI {
+		t.Fatalf("Provider = %v, want %v", resp.Provider, pb.Provider_PROVIDER_GEMINI)
+	}
+	if resp.OriginalProvider != pb.Provider_PROVIDER_OPENAI {
+		t.Fatalf("OriginalProvider = %v, want %v", resp.OriginalProvider, pb.Provider_PROVIDER_OPENAI)
+	}
+	if resp.OriginalError != "primary failed" {
+		t.Fatalf("OriginalError = %q, want %q", resp.OriginalError, "primary failed")
+	}
+}
+
+func TestGenerateReplyStream_MapsChunks(t *testing.T) {
+	openai := &fakeProvider{name: "openai", streamChunks: []provider.StreamChunk{
+		{Type: provider.ChunkTypeText, Text: "hello", Index: 0},
+		{Type: provider.ChunkTypeUsage, Usage: &provider.Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}},
+		{Type: provider.ChunkTypeCitation, Citation: &provider.Citation{Type: provider.CitationTypeURL, URL: "https://example.com"}},
+		{Type: provider.ChunkTypeComplete, ResponseID: "resp-1", Model: "gpt-test", Usage: &provider.Usage{TotalTokens: 3}},
+	}}
+
+	service := &ChatService{openaiProvider: openai, geminiProvider: &fakeProvider{name: "gemini"}, anthropicProvider: &fakeProvider{name: "anthropic"}}
+	stream := &mockGenerateReplyStream{ctx: ctxWithPermissions(auth.PermissionChatStream)}
+	req := &pb.GenerateReplyRequest{UserInput: "hello"}
+
+	if err := service.GenerateReplyStream(req, stream); err != nil {
+		t.Fatalf("GenerateReplyStream() error: %v", err)
+	}
+	if len(stream.sent) != 4 {
+		t.Fatalf("sent chunks = %d, want %d", len(stream.sent), 4)
+	}
+	if delta := stream.sent[0].GetTextDelta(); delta == nil || delta.Text != "hello" {
+		t.Fatalf("text delta = %#v", delta)
+	}
+	if usage := stream.sent[1].GetUsageUpdate(); usage == nil || usage.Usage.TotalTokens != 3 {
+		t.Fatalf("usage update = %#v", usage)
+	}
+	if citation := stream.sent[2].GetCitationUpdate(); citation == nil || citation.Citation.GetType() != pb.Citation_TYPE_URL {
+		t.Fatalf("citation update = %#v", citation)
+	}
+	if complete := stream.sent[3].GetComplete(); complete == nil || complete.Provider != pb.Provider_PROVIDER_OPENAI {
+		t.Fatalf("complete = %#v", complete)
+	}
+}
+
+func TestFormatRAGContext(t *testing.T) {
+	chunks := []rag.RetrieveResult{
+		{Filename: "file1.txt", Text: "text one"},
+		{Filename: "file2.txt", Text: "text two"},
+	}
+
+	got := formatRAGContext(chunks)
+	if !strings.Contains(got, "Relevant context") {
+		t.Fatalf("missing heading: %q", got)
+	}
+	if !strings.Contains(got, "file1.txt") || !strings.Contains(got, "file2.txt") {
+		t.Fatalf("missing filenames: %q", got)
+	}
+}
+
+func TestRagChunksToCitations_TruncatesSnippet(t *testing.T) {
+	text := strings.Repeat("a", 250)
+	chunks := []rag.RetrieveResult{{Filename: "file.txt", Text: text}}
+
+	citations := ragChunksToCitations(chunks)
+	if len(citations) != 1 {
+		t.Fatalf("citations len = %d, want %d", len(citations), 1)
+	}
+	if !strings.HasSuffix(citations[0].Snippet, "...") {
+		t.Fatalf("snippet should be truncated: %q", citations[0].Snippet)
+	}
+	if citations[0].Type != provider.CitationTypeFile {
+		t.Fatalf("citation type = %v, want %v", citations[0].Type, provider.CitationTypeFile)
+	}
+}
```

```diff
diff --git a/cmd/aibox/main_test.go b/cmd/aibox/main_test.go
new file mode 100644
index 0000000..79b2c65
--- /dev/null
+++ b/cmd/aibox/main_test.go
@@ -0,0 +1,104 @@
+package main
+
+import (
+	"context"
+	"fmt"
+	"net"
+	"os"
+	"path/filepath"
+	"strings"
+	"testing"
+
+	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
+	"google.golang.org/grpc"
+)
+
+type testAdminServer struct {
+	pb.UnimplementedAdminServiceServer
+	status string
+}
+
+func (s *testAdminServer) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
+	return &pb.HealthResponse{Status: s.status}, nil
+}
+
+func startTestAdminServer(t *testing.T, status string) (string, int, func()) {
+	t.Helper()
+
+	lis, err := net.Listen("tcp", "127.0.0.1:0")
+	if err != nil {
+		t.Fatalf("listen: %v", err)
+	}
+
+	grpcServer := grpc.NewServer()
+	pb.RegisterAdminServiceServer(grpcServer, &testAdminServer{status: status})
+
+	go func() {
+		_ = grpcServer.Serve(lis)
+	}()
+
+	addr := lis.Addr().(*net.TCPAddr)
+	stop := func() {
+		grpcServer.Stop()
+		_ = lis.Close()
+	}
+
+	return addr.IP.String(), addr.Port, stop
+}
+
+func writeTestConfig(t *testing.T, host string, port int) string {
+	t.Helper()
+
+	path := filepath.Join(t.TempDir(), "aibox.yaml")
+	content := fmt.Sprintf("server:\n  host: %q\n  grpc_port: %d\ntls:\n  enabled: false\n", host, port)
+	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
+		t.Fatalf("write config: %v", err)
+	}
+
+	return path
+}
+
+func TestRunHealthCheck_Success(t *testing.T) {
+	host, port, stop := startTestAdminServer(t, "healthy")
+	defer stop()
+
+	configPath := writeTestConfig(t, host, port)
+	t.Setenv("AIBOX_CONFIG", configPath)
+
+	if err := runHealthCheck(); err != nil {
+		t.Fatalf("runHealthCheck() error: %v", err)
+	}
+}
+
+func TestRunHealthCheck_Unhealthy(t *testing.T) {
+	host, port, stop := startTestAdminServer(t, "unhealthy")
+	defer stop()
+
+	configPath := writeTestConfig(t, host, port)
+	t.Setenv("AIBOX_CONFIG", configPath)
+
+	err := runHealthCheck()
+	if err == nil {
+		t.Fatal("expected error for unhealthy status")
+	}
+	if !strings.Contains(err.Error(), "server unhealthy") {
+		t.Fatalf("unexpected error: %v", err)
+	}
+}
```
