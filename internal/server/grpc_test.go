package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ai8future/airborne/internal/auth"
	"github.com/ai8future/airborne/internal/config"
	"github.com/alicebob/miniredis/v2"
	"google.golang.org/grpc"
)

func staticServerConfig(redisAddr string) *config.Config {
	return &config.Config{
		Auth:  config.AuthConfig{AuthMode: "static", AdminToken: "test-token-12345"},
		Redis: config.RedisConfig{Addr: redisAddr},
	}
}

func waitForRedisConnections(t *testing.T, mr *miniredis.Miniredis, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if mr.CurrentConnectionCount() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Redis connections = %d, want %d", mr.CurrentConnectionCount(), want)
}

func TestNewGRPCServer_FailsWithoutRedisInRedisAuthMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 50051,
			Host:     "127.0.0.1",
		},
		Auth: config.AuthConfig{
			AuthMode: "redis", // Redis auth mode requires Redis
		},
		Redis: config.RedisConfig{
			Addr: "invalid:6379", // Will fail to connect
		},
	}

	_, _, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err == nil {
		t.Fatal("expected error when Redis unavailable in redis auth mode")
	}
}

func TestNewGRPCServer_WorksWithStaticAuthMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 50051,
			Host:     "127.0.0.1",
		},
		Auth: config.AuthConfig{
			AuthMode:   "static",
			AdminToken: "test-token-12345",
		},
	}

	server, _, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("static auth mode should not require Redis: %v", err)
	}
	if server == nil {
		t.Fatal("server should not be nil")
	}
	server.Stop()
}

func TestNewGRPCServer_StaticAuthInitializesConfiguredRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	server, components, err := NewGRPCServer(staticServerConfig(mr.Addr()), VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("NewGRPCServer() error = %v", err)
	}
	t.Cleanup(server.Stop)
	if components.RedisClient == nil {
		t.Fatal("configured reachable Redis was not initialized for static auth")
	}
	if components.KeyStore != nil || components.RateLimiter != nil {
		t.Fatal("static auth must not enable Redis authentication components")
	}
	components.Close()
}

func TestNewGRPCServer_StaticAuthContinuesWithoutRedis(t *testing.T) {
	for _, tc := range []struct {
		name string
		addr string
	}{
		{name: "missing", addr: ""},
		{name: "unavailable", addr: "127.0.0.1:1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, components, err := NewGRPCServer(staticServerConfig(tc.addr), VersionInfo{Version: "test"})
			if err != nil {
				t.Fatalf("static auth should retain optional unkeyed startup: %v", err)
			}
			t.Cleanup(server.Stop)
			if components.RedisClient != nil {
				t.Fatal("missing or unavailable Redis should leave static auth unkeyed")
			}
			components.Close()
		})
	}
}

func TestNewGRPCServer_RedisAuthFailsWhenRedisMissing(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{AuthMode: "redis"}}
	server, components, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err == nil || server != nil || components != nil {
		t.Fatalf("NewGRPCServer() = (%v, %v, %v), want missing Redis failure", server, components, err)
	}
	if !strings.Contains(err.Error(), "redis required for auth_mode=redis") {
		t.Fatalf("NewGRPCServer() error = %q, want required Redis context", err)
	}
}

func TestNewGRPCServer_GRPCHealthIncludesInitializedRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	server, components, err := NewGRPCServer(staticServerConfig(mr.Addr()), VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("NewGRPCServer() error = %v", err)
	}
	t.Cleanup(server.Stop)
	t.Cleanup(components.Close)
	if err := checkServerDependencies(context.Background(), components.DBClient, components.RedisClient); err != nil {
		t.Fatalf("healthy dependency check error = %v", err)
	}

	mr.Close()
	if err := checkServerDependencies(context.Background(), components.DBClient, components.RedisClient); err == nil {
		t.Fatal("dependency check succeeded after Redis failure")
	}
}

func TestServerComponentsCloseClosesRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	server, components, err := NewGRPCServer(staticServerConfig(mr.Addr()), VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("NewGRPCServer() error = %v", err)
	}
	t.Cleanup(server.Stop)
	waitForRedisConnections(t, mr, 1)

	components.Close()
	waitForRedisConnections(t, mr, 0)
	if err := components.RedisClient.Ping(context.Background()); err == nil {
		t.Fatal("Ping() after ServerComponents.Close() succeeded")
	}
}

func TestNewGRPCServer_ClosesRedisWhenLaterInitializationFails(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := staticServerConfig(mr.Addr())
	cfg.TLS = config.TLSConfig{Enabled: true, CertFile: "missing-cert.pem", KeyFile: "missing-key.pem"}

	server, components, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err == nil || server != nil || components != nil {
		t.Fatalf("NewGRPCServer() = (%v, %v, %v), want TLS setup failure", server, components, err)
	}
	waitForRedisConnections(t, mr, 0)
}

func TestNewGRPCServer_FailsWithoutTokenInStaticAuthMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 50051,
			Host:     "127.0.0.1",
		},
		Auth: config.AuthConfig{
			AuthMode:   "static",
			AdminToken: "", // No token
		},
	}

	_, _, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err == nil {
		t.Fatal("expected error when AdminToken missing in static auth mode")
	}
}

func TestNewGRPCServer_InitializesOptionalRAGLifecycle(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AuthMode: "static", AdminToken: "test-token"},
		RAG: config.RAGConfig{
			Enabled:        true,
			OllamaURL:      "http://127.0.0.1:1",
			EmbeddingModel: "test-embedding",
			QdrantURL:      "http://127.0.0.1:1",
			DocboxURL:      "http://127.0.0.1:1",
			ChunkSize:      32,
			ChunkOverlap:   4,
			RetrievalTopK:  2,
		},
	}

	server, components, err := NewGRPCServer(cfg, VersionInfo{Version: "test-rag"})
	if err != nil {
		t.Fatalf("NewGRPCServer() error = %v", err)
	}
	t.Cleanup(server.Stop)
	if components.ChatService == nil {
		t.Fatal("ChatService was not initialized")
	}
	if len(components.HealthChecks) != 2 || components.HealthChecks["qdrant"] == nil || components.HealthChecks["ollama"] == nil {
		t.Fatalf("RAG health checks = %#v, want qdrant and ollama", components.HealthChecks)
	}
	components.Close()
}

func TestNewGRPCServer_RejectsUnreadableTLSFiles(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AuthMode: "static", AdminToken: "test-token"},
		TLS:  config.TLSConfig{Enabled: true, CertFile: "missing-cert.pem", KeyFile: "missing-key.pem"},
	}
	server, components, err := NewGRPCServer(cfg, VersionInfo{Version: "test-tls"})
	if err == nil || server != nil || components != nil {
		t.Fatalf("NewGRPCServer() = (%v, %v, %v), want TLS setup failure", server, components, err)
	}
}

func TestNewGRPCServer_ContinuesWhenOptionalDatabaseIsUnavailable(t *testing.T) {
	cfg := &config.Config{
		Auth:     config.AuthConfig{AuthMode: "static", AdminToken: "test-token"},
		Database: config.DatabaseConfig{Enabled: true, URL: "postgres://127.0.0.1:1/unavailable", MaxConnections: 1},
	}
	server, components, err := NewGRPCServer(cfg, VersionInfo{Version: "test-db"})
	if err != nil {
		t.Fatalf("NewGRPCServer() error = %v", err)
	}
	t.Cleanup(server.Stop)
	if components.DBClient != nil {
		t.Fatal("unreachable optional database should not produce a client")
	}
}

func TestDevelopmentAuthInterceptor_NoAdminPermission(t *testing.T) {
	interceptor := developmentAuthInterceptor()

	// Create a mock handler that captures the context
	var capturedCtx context.Context
	mockHandler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedCtx = ctx
		return nil, nil
	}

	// Call the interceptor with a mock request
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/test"}, mockHandler)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	// Extract the client from context
	client, ok := capturedCtx.Value(auth.ClientContextKey).(*auth.ClientKey)
	if !ok {
		t.Fatal("expected ClientKey in context")
	}

	// Verify PermissionAdmin is NOT granted
	if client.HasPermission(auth.PermissionAdmin) {
		t.Error("development interceptor should NOT grant PermissionAdmin")
	}

	// Verify PermissionChat IS granted
	if !client.HasPermission(auth.PermissionChat) {
		t.Error("development interceptor should grant PermissionChat")
	}

	// Verify PermissionChatStream IS granted
	if !client.HasPermission(auth.PermissionChatStream) {
		t.Error("development interceptor should grant PermissionChatStream")
	}

	// Verify PermissionFiles IS granted
	if !client.HasPermission(auth.PermissionFiles) {
		t.Error("development interceptor should grant PermissionFiles")
	}
}

func TestDevelopmentAuthStreamInterceptor_NoAdminPermission(t *testing.T) {
	interceptor := developmentAuthStreamInterceptor()

	// Create a mock stream with a context
	mockStream := &mockServerStream{ctx: context.Background()}

	// Create a mock handler that captures the stream context
	var capturedCtx context.Context
	mockHandler := func(srv interface{}, stream grpc.ServerStream) error {
		capturedCtx = stream.Context()
		return nil
	}

	// Call the interceptor
	err := interceptor(nil, mockStream, &grpc.StreamServerInfo{FullMethod: "/test"}, mockHandler)
	if err != nil {
		t.Fatalf("stream interceptor returned error: %v", err)
	}

	// Extract the client from context
	client, ok := capturedCtx.Value(auth.ClientContextKey).(*auth.ClientKey)
	if !ok {
		t.Fatal("expected ClientKey in context")
	}

	// Verify PermissionAdmin is NOT granted
	if client.HasPermission(auth.PermissionAdmin) {
		t.Error("development stream interceptor should NOT grant PermissionAdmin")
	}

	// Verify PermissionChat IS granted
	if !client.HasPermission(auth.PermissionChat) {
		t.Error("development stream interceptor should grant PermissionChat")
	}
}

// mockServerStream is a minimal mock for grpc.ServerStream
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func TestServerComponentsCloseAndHealthLogging(t *testing.T) {
	// Nil components are a valid lifecycle state when optional dependencies are disabled.
	(&ServerComponents{}).Close()

	called := false
	inner := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		called = true
		return handler(ctx, req)
	}
	wrapped := skipHealthLogging(inner)
	for _, tc := range []struct {
		method    string
		wantInner bool
	}{
		{"/grpc.health.v1.Health/Check", false},
		{"/airborne.v1.Airborne/GenerateReply", true},
	} {
		called = false
		_, err := wrapped(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: tc.method}, func(context.Context, any) (any, error) { return "ok", nil })
		if err != nil || called != tc.wantInner {
			t.Fatalf("wrapped %s = called=%v err=%v", tc.method, called, err)
		}
	}
}
