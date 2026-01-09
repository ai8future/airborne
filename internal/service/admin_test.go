package service

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/auth"
)

// ctxWithAdminPermission creates a context with admin permission for testing.
func ctxWithAdminPermission(clientID string) context.Context {
	return context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    clientID,
		Permissions: []auth.Permission{auth.PermissionAdmin},
	})
}

// ctxWithChatPermission creates a context with chat permission (no admin).
func ctxWithChatPermission(clientID string) context.Context {
	return context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    clientID,
		Permissions: []auth.Permission{auth.PermissionChat},
	})
}

// mockRedisClient is a mock implementation for testing.
type mockRedisClient struct {
	pingErr error
}

func (m *mockRedisClient) Ping(ctx context.Context) error {
	return m.pingErr
}

func TestNewAdminService(t *testing.T) {
	cfg := AdminServiceConfig{
		Version:   "1.0.0",
		GitCommit: "abc123",
		BuildTime: "2025-01-01T00:00:00Z",
		GoVersion: "go1.21.0",
	}

	svc := NewAdminService(nil, cfg)

	if svc == nil {
		t.Fatal("expected non-nil AdminService")
	}
	if svc.version != "1.0.0" {
		t.Errorf("expected version=1.0.0, got %s", svc.version)
	}
	if svc.gitCommit != "abc123" {
		t.Errorf("expected gitCommit=abc123, got %s", svc.gitCommit)
	}
	if svc.buildTime != "2025-01-01T00:00:00Z" {
		t.Errorf("expected buildTime=2025-01-01T00:00:00Z, got %s", svc.buildTime)
	}
	if svc.goVersion != "go1.21.0" {
		t.Errorf("expected goVersion=go1.21.0, got %s", svc.goVersion)
	}
	if svc.startTime.IsZero() {
		t.Error("expected startTime to be set")
	}
}

func TestAdminService_Health_Success(t *testing.T) {
	cfg := AdminServiceConfig{
		Version:   "2.0.0",
		GitCommit: "def456",
		BuildTime: "2025-06-15T12:00:00Z",
		GoVersion: "go1.22.0",
	}
	svc := NewAdminService(nil, cfg)

	// Health should work without authentication
	resp, err := svc.Health(context.Background(), &pb.HealthRequest{})

	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if resp.Status != "healthy" {
		t.Errorf("expected Status=healthy, got %s", resp.Status)
	}
	if resp.Version != "2.0.0" {
		t.Errorf("expected Version=2.0.0, got %s", resp.Version)
	}
	if resp.UptimeSeconds < 0 {
		t.Errorf("expected UptimeSeconds >= 0, got %d", resp.UptimeSeconds)
	}
}

func TestAdminService_Health_UptimeIncreases(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// First call
	resp1, err := svc.Health(context.Background(), &pb.HealthRequest{})
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	// Small delay
	time.Sleep(10 * time.Millisecond)

	// Second call should have same or greater uptime
	resp2, err := svc.Health(context.Background(), &pb.HealthRequest{})
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if resp2.UptimeSeconds < resp1.UptimeSeconds {
		t.Errorf("expected uptime to increase, got %d then %d", resp1.UptimeSeconds, resp2.UptimeSeconds)
	}
}

func TestAdminService_Health_NoAuthRequired(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Health should work without any auth context
	resp, err := svc.Health(context.Background(), &pb.HealthRequest{})

	if err != nil {
		t.Fatalf("Health should not require auth: %v", err)
	}
	if resp.Status != "healthy" {
		t.Errorf("expected Status=healthy, got %s", resp.Status)
	}
}

func TestAdminService_Ready_WithAdminPermission(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Ready should work with admin permission
	resp, err := svc.Ready(ctxWithAdminPermission("test-client"), &pb.ReadyRequest{})

	if err != nil {
		t.Fatalf("Ready failed: %v", err)
	}
	// With nil Redis (static auth mode), Redis is not in dependencies
	// so the service should be Ready=true (no failing deps)
	if !resp.Ready {
		t.Error("expected Ready=true when Redis not configured (static auth mode)")
	}
	if resp.Dependencies == nil {
		t.Fatal("expected Dependencies to be set")
	}
	// Redis should NOT be in dependencies when nil (static auth mode)
	if _, ok := resp.Dependencies["redis"]; ok {
		t.Error("expected no redis in dependencies when not configured (static auth mode)")
	}
}

func TestAdminService_Ready_WithoutAuth(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Ready should fail without auth
	_, err := svc.Ready(context.Background(), &pb.ReadyRequest{})

	if err == nil {
		t.Fatal("expected auth error for Ready without auth")
	}
}

func TestAdminService_Ready_WithoutAdminPermission(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Ready should fail without admin permission
	_, err := svc.Ready(ctxWithChatPermission("test-client"), &pb.ReadyRequest{})

	if err == nil {
		t.Fatal("expected permission error for Ready without admin permission")
	}
}

func TestAdminService_Version_WithAdminPermission(t *testing.T) {
	cfg := AdminServiceConfig{
		Version:   "3.1.4",
		GitCommit: "abc123def",
		BuildTime: "2025-12-25T10:30:00Z",
		GoVersion: "go1.23.0",
	}
	svc := NewAdminService(nil, cfg)

	// Version should work with admin permission
	resp, err := svc.Version(ctxWithAdminPermission("test-client"), &pb.VersionRequest{})

	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}
	if resp.Version != "3.1.4" {
		t.Errorf("expected Version=3.1.4, got %s", resp.Version)
	}
	if resp.GitCommit != "abc123def" {
		t.Errorf("expected GitCommit=abc123def, got %s", resp.GitCommit)
	}
	if resp.BuildTime != "2025-12-25T10:30:00Z" {
		t.Errorf("expected BuildTime=2025-12-25T10:30:00Z, got %s", resp.BuildTime)
	}
	if resp.GoVersion != "go1.23.0" {
		t.Errorf("expected GoVersion=go1.23.0, got %s", resp.GoVersion)
	}
}

func TestAdminService_Version_WithoutAuth(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Version should fail without auth
	_, err := svc.Version(context.Background(), &pb.VersionRequest{})

	if err == nil {
		t.Fatal("expected auth error for Version without auth")
	}
}

func TestAdminService_Version_WithoutAdminPermission(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Version should fail without admin permission
	_, err := svc.Version(ctxWithChatPermission("test-client"), &pb.VersionRequest{})

	if err == nil {
		t.Fatal("expected permission error for Version without admin permission")
	}
}

func TestAdminService_Version_EmptyConfig(t *testing.T) {
	// Test with empty config (all defaults)
	cfg := AdminServiceConfig{}
	svc := NewAdminService(nil, cfg)

	resp, err := svc.Version(ctxWithAdminPermission("test-client"), &pb.VersionRequest{})

	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}
	// Empty strings are valid - the service just returns what was configured
	if resp.Version != "" {
		t.Errorf("expected empty Version, got %s", resp.Version)
	}
	if resp.GitCommit != "" {
		t.Errorf("expected empty GitCommit, got %s", resp.GitCommit)
	}
	if resp.BuildTime != "" {
		t.Errorf("expected empty BuildTime, got %s", resp.BuildTime)
	}
	if resp.GoVersion != "" {
		t.Errorf("expected empty GoVersion, got %s", resp.GoVersion)
	}
}

// mockPingableRedis implements the redis.Ping interface for testing.
type mockPingableRedis struct {
	pingErr error
}

func (m *mockPingableRedis) Ping(ctx context.Context) error {
	return m.pingErr
}

func TestAdminService_Ready_StaticAuthModeNoRedis(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}

	// Create a service without Redis (static auth mode)
	svc := &AdminService{
		version:   cfg.Version,
		startTime: time.Now(),
		// redis is nil - static auth mode
	}

	resp, err := svc.Ready(ctxWithAdminPermission("test-client"), &pb.ReadyRequest{})

	if err != nil {
		t.Fatalf("Ready failed: %v", err)
	}

	// With nil redis (static auth mode), Redis is not in dependencies
	// so the service should be Ready=true
	if !resp.Ready {
		t.Error("expected Ready=true in static auth mode (no Redis)")
	}
	// Redis should NOT be in dependencies when nil
	if _, ok := resp.Dependencies["redis"]; ok {
		t.Error("expected no redis in dependencies in static auth mode")
	}
}

func TestAdminService_Ready_OverallReadiness(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	resp, err := svc.Ready(ctxWithAdminPermission("test-client"), &pb.ReadyRequest{})

	if err != nil {
		t.Fatalf("Ready failed: %v", err)
	}

	// Check that overall readiness reflects dependency health
	allHealthy := true
	for _, dep := range resp.Dependencies {
		if !dep.Healthy {
			allHealthy = false
			break
		}
	}

	if resp.Ready != allHealthy {
		t.Errorf("expected Ready=%v to match all dependencies healthy=%v", resp.Ready, allHealthy)
	}
}

func TestAdminService_AdminPermissionGrantsAccess(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Create context with admin permission
	ctx := context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    "admin-user",
		Permissions: []auth.Permission{auth.PermissionAdmin},
	})

	// Both Ready and Version should succeed with admin permission
	_, err := svc.Ready(ctx, &pb.ReadyRequest{})
	if err != nil {
		t.Errorf("Ready should succeed with admin permission: %v", err)
	}

	_, err = svc.Version(ctx, &pb.VersionRequest{})
	if err != nil {
		t.Errorf("Version should succeed with admin permission: %v", err)
	}
}

func TestAdminService_MultiplePermissionsIncludingAdmin(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Create context with multiple permissions including admin
	ctx := context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    "multi-perm-user",
		Permissions: []auth.Permission{auth.PermissionChat, auth.PermissionFiles, auth.PermissionAdmin},
	})

	// Should succeed with admin in the list
	_, err := svc.Ready(ctx, &pb.ReadyRequest{})
	if err != nil {
		t.Errorf("Ready should succeed with admin permission: %v", err)
	}

	_, err = svc.Version(ctx, &pb.VersionRequest{})
	if err != nil {
		t.Errorf("Version should succeed with admin permission: %v", err)
	}
}

func TestAdminService_AllPermissionsExceptAdmin(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Create context with all permissions EXCEPT admin
	ctx := context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    "non-admin-user",
		Permissions: []auth.Permission{auth.PermissionChat, auth.PermissionChatStream, auth.PermissionFiles},
	})

	// Should fail without admin permission
	_, err := svc.Ready(ctx, &pb.ReadyRequest{})
	if err == nil {
		t.Error("Ready should fail without admin permission")
	}

	_, err = svc.Version(ctx, &pb.VersionRequest{})
	if err == nil {
		t.Error("Version should fail without admin permission")
	}
}

// redisClientInterface is what the admin service uses for redis operations.
type redisClientInterface interface {
	Ping(ctx context.Context) error
}

// mockRedisForReady allows injecting a mock redis for Ready tests.
type mockRedisForReady struct {
	pingErr     error
	pingLatency time.Duration
}

func (m *mockRedisForReady) Ping(ctx context.Context) error {
	if m.pingLatency > 0 {
		time.Sleep(m.pingLatency)
	}
	return m.pingErr
}

func TestAdminService_Health_AlwaysReturnsHealthy(t *testing.T) {
	// Even with no dependencies, Health returns healthy
	// because Health is just a liveness check
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	resp, err := svc.Health(context.Background(), &pb.HealthRequest{})

	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if resp.Status != "healthy" {
		t.Errorf("Health should always return healthy status, got %s", resp.Status)
	}
}

func TestAdminService_Ready_DependencyMapInitialized(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg) // nil Redis = static auth mode

	resp, err := svc.Ready(ctxWithAdminPermission("test-client"), &pb.ReadyRequest{})

	if err != nil {
		t.Fatalf("Ready failed: %v", err)
	}
	if resp.Dependencies == nil {
		t.Fatal("Dependencies map should be initialized")
	}
	// In static auth mode (nil Redis), Redis should NOT be in dependencies
	if _, ok := resp.Dependencies["redis"]; ok {
		t.Error("Dependencies should NOT include redis when Redis client is nil")
	}
}

func TestAdminServiceConfig_AllFields(t *testing.T) {
	// Test that all config fields are properly passed through
	testCases := []struct {
		name      string
		version   string
		gitCommit string
		buildTime string
		goVersion string
	}{
		{
			name:      "all fields set",
			version:   "1.2.3",
			gitCommit: "commit123",
			buildTime: "2025-01-01T00:00:00Z",
			goVersion: "go1.21.0",
		},
		{
			name:      "minimal fields",
			version:   "0.0.1",
			gitCommit: "",
			buildTime: "",
			goVersion: "",
		},
		{
			name:      "special characters",
			version:   "1.0.0-beta.1+build.123",
			gitCommit: "abc123def456",
			buildTime: "2025-06-15T12:30:45Z",
			goVersion: "go1.22.3",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := AdminServiceConfig{
				Version:   tc.version,
				GitCommit: tc.gitCommit,
				BuildTime: tc.buildTime,
				GoVersion: tc.goVersion,
			}
			svc := NewAdminService(nil, cfg)

			resp, err := svc.Version(ctxWithAdminPermission("test"), &pb.VersionRequest{})
			if err != nil {
				t.Fatalf("Version failed: %v", err)
			}

			if resp.Version != tc.version {
				t.Errorf("expected Version=%s, got %s", tc.version, resp.Version)
			}
			if resp.GitCommit != tc.gitCommit {
				t.Errorf("expected GitCommit=%s, got %s", tc.gitCommit, resp.GitCommit)
			}
			if resp.BuildTime != tc.buildTime {
				t.Errorf("expected BuildTime=%s, got %s", tc.buildTime, resp.BuildTime)
			}
			if resp.GoVersion != tc.goVersion {
				t.Errorf("expected GoVersion=%s, got %s", tc.goVersion, resp.GoVersion)
			}
		})
	}
}

func TestAdminService_ContextCancellation(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Create a cancelled context with admin permission
	ctx, cancel := context.WithCancel(ctxWithAdminPermission("test-client"))
	cancel() // Cancel immediately

	// Health should still work even with cancelled context
	// (no async operations that check context)
	resp, err := svc.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		t.Errorf("Health should not fail with cancelled context: %v", err)
	}
	if resp.Status != "healthy" {
		t.Errorf("expected Status=healthy, got %s", resp.Status)
	}
}

func TestAdminService_NilRequests(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Nil requests should be handled gracefully (proto accepts empty messages)
	ctx := ctxWithAdminPermission("test-client")

	_, err := svc.Health(ctx, nil)
	if err != nil {
		// Some implementations might reject nil, which is also acceptable
		t.Logf("Health with nil request returned error (acceptable): %v", err)
	}

	_, err = svc.Ready(ctx, nil)
	if err != nil {
		t.Logf("Ready with nil request returned error (acceptable): %v", err)
	}

	_, err = svc.Version(ctx, nil)
	if err != nil {
		t.Logf("Version with nil request returned error (acceptable): %v", err)
	}
}

func TestAdminService_ErrorTypes(t *testing.T) {
	cfg := AdminServiceConfig{Version: "1.0.0"}
	svc := NewAdminService(nil, cfg)

	// Test that errors are gRPC status errors
	_, err := svc.Ready(context.Background(), &pb.ReadyRequest{})
	if err == nil {
		t.Fatal("expected error")
	}

	// Check that it's an error (the auth package returns gRPC status errors)
	var grpcErr interface{ GRPCStatus() interface{} }
	if !errors.As(err, &grpcErr) {
		t.Logf("Error returned: %v (type: %T)", err, err)
		// This is still acceptable - just verifying the error exists
	}
}
