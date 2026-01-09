package server

import (
	"context"
	"testing"

	"github.com/cliffpyles/aibox/internal/auth"
	"github.com/cliffpyles/aibox/internal/config"
	"google.golang.org/grpc"
)

func TestNewGRPCServer_FailsWithoutRedisInProductionMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 50051,
			Host:     "127.0.0.1",
		},
		StartupMode: config.StartupModeProduction,
		Redis: config.RedisConfig{
			Addr: "invalid:6379", // Will fail to connect
		},
	}

	_, _, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err == nil {
		t.Fatal("expected error when Redis unavailable in production mode")
	}
}

func TestNewGRPCServer_AllowsNoRedisInDevelopmentMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 50051,
			Host:     "127.0.0.1",
		},
		StartupMode: config.StartupModeDevelopment,
		Redis: config.RedisConfig{
			Addr: "invalid:6379", // Will fail to connect
		},
	}

	server, _, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("development mode should allow missing Redis: %v", err)
	}
	if server == nil {
		t.Fatal("server should not be nil")
	}
	server.Stop()
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
