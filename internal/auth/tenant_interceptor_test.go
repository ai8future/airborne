package auth

import (
	"context"
	"testing"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/tenant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newTestManager creates a Manager with the given tenant configs for testing.
func newTestManager(tenants map[string]tenant.TenantConfig) *tenant.Manager {
	return &tenant.Manager{
		Tenants: tenants,
	}
}

// mockUnaryHandler is a mock gRPC unary handler for testing.
func mockUnaryHandler(ctx context.Context, req interface{}) (interface{}, error) {
	// Return the tenant from context to verify it was set
	tenantCfg := TenantFromContext(ctx)
	return tenantCfg, nil
}

// mockStreamHandler is a mock gRPC stream handler for testing.
func mockStreamHandler(srv interface{}, ss grpc.ServerStream) error {
	return nil
}

// mockServerStream implements grpc.ServerStream for testing.
type mockServerStream struct {
	grpc.ServerStream
	ctx      context.Context
	recvMsg  interface{}
	recvErr  error
	recvOnce bool
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func (m *mockServerStream) RecvMsg(msg interface{}) error {
	if m.recvErr != nil {
		return m.recvErr
	}
	if m.recvOnce {
		return nil
	}
	m.recvOnce = true
	// Copy the prepared message
	if req, ok := msg.(*pb.GenerateReplyRequest); ok {
		if srcReq, ok := m.recvMsg.(*pb.GenerateReplyRequest); ok {
			req.TenantId = srcReq.TenantId
			req.UserInput = srcReq.UserInput
		}
	}
	return nil
}

func (m *mockServerStream) SendMsg(msg interface{}) error {
	return nil
}

func TestNewTenantInterceptor(t *testing.T) {
	mgr := newTestManager(map[string]tenant.TenantConfig{
		"test": {TenantID: "test"},
	})

	interceptor := NewTenantInterceptor(mgr)

	if interceptor == nil {
		t.Fatal("NewTenantInterceptor() returned nil")
	}

	if interceptor.manager != mgr {
		t.Error("NewTenantInterceptor() manager not set correctly")
	}

	// Verify skip methods are populated
	expectedSkips := []string{
		"/aibox.v1.AdminService/Health",
		"/aibox.v1.AdminService/Ready",
		"/aibox.v1.AdminService/Version",
		"/aibox.v1.FileService/CreateFileStore",
		"/aibox.v1.FileService/UploadFile",
		"/aibox.v1.FileService/DeleteFileStore",
		"/aibox.v1.FileService/GetFileStore",
		"/aibox.v1.FileService/ListFileStores",
	}

	for _, method := range expectedSkips {
		if !interceptor.skipMethods[method] {
			t.Errorf("Expected skip method %q to be true", method)
		}
	}
}

func TestExtractTenantID(t *testing.T) {
	tests := []struct {
		name     string
		req      interface{}
		expected string
	}{
		{
			name:     "GenerateReplyRequest with tenant_id",
			req:      &pb.GenerateReplyRequest{TenantId: "tenant-123"},
			expected: "tenant-123",
		},
		{
			name:     "GenerateReplyRequest empty tenant_id",
			req:      &pb.GenerateReplyRequest{},
			expected: "",
		},
		{
			name:     "SelectProviderRequest with tenant_id",
			req:      &pb.SelectProviderRequest{TenantId: "tenant-456"},
			expected: "tenant-456",
		},
		{
			name:     "SelectProviderRequest empty tenant_id",
			req:      &pb.SelectProviderRequest{},
			expected: "",
		},
		{
			name:     "Unknown request type",
			req:      struct{}{},
			expected: "",
		},
		{
			name:     "nil request",
			req:      nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTenantID(tt.req)
			if result != tt.expected {
				t.Errorf("extractTenantID() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestUnaryInterceptor_SkipMethods(t *testing.T) {
	mgr := newTestManager(map[string]tenant.TenantConfig{})
	interceptor := NewTenantInterceptor(mgr)
	unary := interceptor.UnaryInterceptor()

	skipMethods := []string{
		"/aibox.v1.AdminService/Health",
		"/aibox.v1.AdminService/Ready",
		"/aibox.v1.AdminService/Version",
		"/aibox.v1.FileService/CreateFileStore",
		"/aibox.v1.FileService/UploadFile",
		"/aibox.v1.FileService/DeleteFileStore",
		"/aibox.v1.FileService/GetFileStore",
		"/aibox.v1.FileService/ListFileStores",
	}

	for _, method := range skipMethods {
		t.Run(method, func(t *testing.T) {
			ctx := context.Background()
			info := &grpc.UnaryServerInfo{FullMethod: method}

			handlerCalled := false
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return nil, nil
			}

			_, err := unary(ctx, nil, info, handler)
			if err != nil {
				t.Errorf("UnaryInterceptor() error = %v for skip method", err)
			}
			if !handlerCalled {
				t.Error("Handler not called for skip method")
			}
		})
	}
}

func TestUnaryInterceptor_TenantExtraction(t *testing.T) {
	tests := []struct {
		name         string
		tenants      map[string]tenant.TenantConfig
		req          interface{}
		wantErr      bool
		wantCode     codes.Code
		wantTenantID string
	}{
		{
			name: "valid tenant from GenerateReplyRequest",
			tenants: map[string]tenant.TenantConfig{
				"tenant-123": {TenantID: "tenant-123", DisplayName: "Test Tenant"},
			},
			req:          &pb.GenerateReplyRequest{TenantId: "tenant-123"},
			wantErr:      false,
			wantTenantID: "tenant-123",
		},
		{
			name: "valid tenant from SelectProviderRequest",
			tenants: map[string]tenant.TenantConfig{
				"tenant-456": {TenantID: "tenant-456", DisplayName: "Another Tenant"},
			},
			req:          &pb.SelectProviderRequest{TenantId: "tenant-456"},
			wantErr:      false,
			wantTenantID: "tenant-456",
		},
		{
			name: "tenant_id normalized (uppercase to lowercase)",
			tenants: map[string]tenant.TenantConfig{
				"tenant-abc": {TenantID: "tenant-abc"},
			},
			req:          &pb.GenerateReplyRequest{TenantId: "TENANT-ABC"},
			wantErr:      false,
			wantTenantID: "tenant-abc",
		},
		{
			name: "tenant_id normalized (whitespace trimmed)",
			tenants: map[string]tenant.TenantConfig{
				"tenant-xyz": {TenantID: "tenant-xyz"},
			},
			req:          &pb.GenerateReplyRequest{TenantId: "  tenant-xyz  "},
			wantErr:      false,
			wantTenantID: "tenant-xyz",
		},
		{
			name: "single tenant mode - empty tenant_id uses default",
			tenants: map[string]tenant.TenantConfig{
				"default-tenant": {TenantID: "default-tenant"},
			},
			req:          &pb.GenerateReplyRequest{TenantId: ""},
			wantErr:      false,
			wantTenantID: "default-tenant",
		},
		{
			name: "multi-tenant mode - empty tenant_id returns error",
			tenants: map[string]tenant.TenantConfig{
				"tenant-a": {TenantID: "tenant-a"},
				"tenant-b": {TenantID: "tenant-b"},
			},
			req:      &pb.GenerateReplyRequest{TenantId: ""},
			wantErr:  true,
			wantCode: codes.InvalidArgument,
		},
		{
			name: "tenant not found",
			tenants: map[string]tenant.TenantConfig{
				"existing-tenant": {TenantID: "existing-tenant"},
			},
			req:      &pb.GenerateReplyRequest{TenantId: "nonexistent"},
			wantErr:  true,
			wantCode: codes.NotFound,
		},
		{
			name:     "no tenants configured - empty tenant_id",
			tenants:  map[string]tenant.TenantConfig{},
			req:      &pb.GenerateReplyRequest{TenantId: ""},
			wantErr:  true,
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newTestManager(tt.tenants)
			interceptor := NewTenantInterceptor(mgr)
			unary := interceptor.UnaryInterceptor()

			ctx := context.Background()
			info := &grpc.UnaryServerInfo{FullMethod: "/aibox.v1.ChatService/GenerateReply"}

			resp, err := unary(ctx, tt.req, info, mockUnaryHandler)

			if tt.wantErr {
				if err == nil {
					t.Fatal("UnaryInterceptor() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("UnaryInterceptor() error is not a gRPC status: %v", err)
				}
				if st.Code() != tt.wantCode {
					t.Errorf("UnaryInterceptor() code = %v, want %v", st.Code(), tt.wantCode)
				}
				return
			}

			if err != nil {
				t.Fatalf("UnaryInterceptor() unexpected error: %v", err)
			}

			// Verify tenant was set in context
			tenantCfg, ok := resp.(*tenant.TenantConfig)
			if !ok || tenantCfg == nil {
				t.Fatal("Handler did not receive tenant config in context")
			}
			if tenantCfg.TenantID != tt.wantTenantID {
				t.Errorf("TenantID = %v, want %v", tenantCfg.TenantID, tt.wantTenantID)
			}
		})
	}
}

func TestStreamInterceptor_SkipMethods(t *testing.T) {
	mgr := newTestManager(map[string]tenant.TenantConfig{})
	interceptor := NewTenantInterceptor(mgr)
	stream := interceptor.StreamInterceptor()

	skipMethods := []string{
		"/aibox.v1.AdminService/Health",
		"/aibox.v1.AdminService/Ready",
		"/aibox.v1.AdminService/Version",
		"/aibox.v1.FileService/CreateFileStore",
		"/aibox.v1.FileService/UploadFile",
	}

	for _, method := range skipMethods {
		t.Run(method, func(t *testing.T) {
			info := &grpc.StreamServerInfo{FullMethod: method}
			ss := &mockServerStream{ctx: context.Background()}

			handlerCalled := false
			handler := func(srv interface{}, ss grpc.ServerStream) error {
				handlerCalled = true
				return nil
			}

			err := stream(nil, ss, info, handler)
			if err != nil {
				t.Errorf("StreamInterceptor() error = %v for skip method", err)
			}
			if !handlerCalled {
				t.Error("Handler not called for skip method")
			}
		})
	}
}

func TestStreamInterceptor_TenantExtraction(t *testing.T) {
	tests := []struct {
		name         string
		tenants      map[string]tenant.TenantConfig
		recvMsg      *pb.GenerateReplyRequest
		wantErr      bool
		wantCode     codes.Code
		wantTenantID string
	}{
		{
			name: "valid tenant from stream message",
			tenants: map[string]tenant.TenantConfig{
				"stream-tenant": {TenantID: "stream-tenant"},
			},
			recvMsg:      &pb.GenerateReplyRequest{TenantId: "stream-tenant"},
			wantErr:      false,
			wantTenantID: "stream-tenant",
		},
		{
			name: "single tenant mode - empty tenant_id",
			tenants: map[string]tenant.TenantConfig{
				"single-tenant": {TenantID: "single-tenant"},
			},
			recvMsg:      &pb.GenerateReplyRequest{TenantId: ""},
			wantErr:      false,
			wantTenantID: "single-tenant",
		},
		{
			name: "multi-tenant mode - empty tenant_id",
			tenants: map[string]tenant.TenantConfig{
				"tenant-1": {TenantID: "tenant-1"},
				"tenant-2": {TenantID: "tenant-2"},
			},
			recvMsg:  &pb.GenerateReplyRequest{TenantId: ""},
			wantErr:  true,
			wantCode: codes.InvalidArgument,
		},
		{
			name: "tenant not found",
			tenants: map[string]tenant.TenantConfig{
				"existing": {TenantID: "existing"},
			},
			recvMsg:  &pb.GenerateReplyRequest{TenantId: "nonexistent"},
			wantErr:  true,
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newTestManager(tt.tenants)
			interceptor := NewTenantInterceptor(mgr)
			streamInterceptor := interceptor.StreamInterceptor()

			info := &grpc.StreamServerInfo{FullMethod: "/aibox.v1.ChatService/GenerateReplyStream"}
			ss := &mockServerStream{
				ctx:     context.Background(),
				recvMsg: tt.recvMsg,
			}

			var handlerStream grpc.ServerStream
			handler := func(srv interface{}, stream grpc.ServerStream) error {
				handlerStream = stream
				// Simulate receiving a message to trigger tenant extraction
				msg := &pb.GenerateReplyRequest{}
				if err := stream.RecvMsg(msg); err != nil {
					return err
				}
				return nil
			}

			err := streamInterceptor(nil, ss, info, handler)

			if tt.wantErr {
				if err == nil {
					t.Fatal("StreamInterceptor() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("StreamInterceptor() error is not a gRPC status: %v", err)
				}
				if st.Code() != tt.wantCode {
					t.Errorf("StreamInterceptor() code = %v, want %v", st.Code(), tt.wantCode)
				}
				return
			}

			if err != nil {
				t.Fatalf("StreamInterceptor() unexpected error: %v", err)
			}

			// Verify tenant config is accessible through wrapped stream's context
			if handlerStream != nil {
				tenantCfg := TenantFromContext(handlerStream.Context())
				if tenantCfg == nil {
					t.Fatal("Tenant config not found in stream context")
				}
				if tenantCfg.TenantID != tt.wantTenantID {
					t.Errorf("TenantID = %v, want %v", tenantCfg.TenantID, tt.wantTenantID)
				}
			}
		})
	}
}

func TestTenantStream_Context(t *testing.T) {
	t.Run("returns base context when tenant not set", func(t *testing.T) {
		baseCtx := context.WithValue(context.Background(), "test-key", "test-value")
		ss := &mockServerStream{ctx: baseCtx}

		wrapped := &tenantStream{
			ServerStream: ss,
			tenantSet:    false,
			tenantCfg:    nil,
		}

		ctx := wrapped.Context()
		if ctx.Value("test-key") != "test-value" {
			t.Error("Base context values not preserved")
		}
		if TenantFromContext(ctx) != nil {
			t.Error("Expected no tenant in context")
		}
	})

	t.Run("returns context with tenant when set", func(t *testing.T) {
		baseCtx := context.Background()
		ss := &mockServerStream{ctx: baseCtx}
		expectedTenant := &tenant.TenantConfig{TenantID: "ctx-tenant"}

		wrapped := &tenantStream{
			ServerStream: ss,
			tenantSet:    true,
			tenantCfg:    expectedTenant,
		}

		ctx := wrapped.Context()
		tenantCfg := TenantFromContext(ctx)
		if tenantCfg == nil {
			t.Fatal("Expected tenant in context")
		}
		if tenantCfg.TenantID != "ctx-tenant" {
			t.Errorf("TenantID = %v, want ctx-tenant", tenantCfg.TenantID)
		}
	})
}

func TestTenantFromContext(t *testing.T) {
	t.Run("no tenant in context", func(t *testing.T) {
		ctx := context.Background()
		cfg := TenantFromContext(ctx)
		if cfg != nil {
			t.Errorf("TenantFromContext() = %v, want nil", cfg)
		}
	})

	t.Run("tenant in context", func(t *testing.T) {
		expected := &tenant.TenantConfig{
			TenantID:    "test-tenant",
			DisplayName: "Test Tenant",
		}
		ctx := context.WithValue(context.Background(), TenantContextKey, expected)
		cfg := TenantFromContext(ctx)
		if cfg != expected {
			t.Errorf("TenantFromContext() = %v, want %v", cfg, expected)
		}
	})

	t.Run("wrong type in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), TenantContextKey, "not a tenant config")
		cfg := TenantFromContext(ctx)
		if cfg != nil {
			t.Errorf("TenantFromContext() = %v, want nil for wrong type", cfg)
		}
	})
}

func TestResolveTenant(t *testing.T) {
	tests := []struct {
		name         string
		tenants      map[string]tenant.TenantConfig
		tenantID     string
		wantErr      bool
		wantCode     codes.Code
		wantTenantID string
	}{
		{
			name: "valid tenant ID",
			tenants: map[string]tenant.TenantConfig{
				"valid-tenant": {TenantID: "valid-tenant"},
			},
			tenantID:     "valid-tenant",
			wantErr:      false,
			wantTenantID: "valid-tenant",
		},
		{
			name: "normalized tenant ID",
			tenants: map[string]tenant.TenantConfig{
				"normalized": {TenantID: "normalized"},
			},
			tenantID:     "  NORMALIZED  ",
			wantErr:      false,
			wantTenantID: "normalized",
		},
		{
			name: "empty tenant ID - single tenant mode",
			tenants: map[string]tenant.TenantConfig{
				"default": {TenantID: "default"},
			},
			tenantID:     "",
			wantErr:      false,
			wantTenantID: "default",
		},
		{
			name: "empty tenant ID - multi-tenant mode",
			tenants: map[string]tenant.TenantConfig{
				"a": {TenantID: "a"},
				"b": {TenantID: "b"},
			},
			tenantID: "",
			wantErr:  true,
			wantCode: codes.InvalidArgument,
		},
		{
			name: "tenant not found",
			tenants: map[string]tenant.TenantConfig{
				"exists": {TenantID: "exists"},
			},
			tenantID: "does-not-exist",
			wantErr:  true,
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newTestManager(tt.tenants)
			interceptor := NewTenantInterceptor(mgr)

			cfg, err := interceptor.resolveTenant(tt.tenantID)

			if tt.wantErr {
				if err == nil {
					t.Fatal("resolveTenant() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("resolveTenant() error is not a gRPC status: %v", err)
				}
				if st.Code() != tt.wantCode {
					t.Errorf("resolveTenant() code = %v, want %v", st.Code(), tt.wantCode)
				}
				return
			}

			if err != nil {
				t.Fatalf("resolveTenant() unexpected error: %v", err)
			}

			if cfg == nil {
				t.Fatal("resolveTenant() returned nil config")
			}
			if cfg.TenantID != tt.wantTenantID {
				t.Errorf("TenantID = %v, want %v", cfg.TenantID, tt.wantTenantID)
			}
		})
	}
}

func TestUnaryInterceptor_NonSkippedMethodRequiresTenant(t *testing.T) {
	// Test that non-skipped methods require tenant resolution
	tenants := map[string]tenant.TenantConfig{
		"tenant-a": {TenantID: "tenant-a"},
		"tenant-b": {TenantID: "tenant-b"},
	}
	mgr := newTestManager(tenants)
	interceptor := NewTenantInterceptor(mgr)
	unary := interceptor.UnaryInterceptor()

	nonSkippedMethods := []string{
		"/aibox.v1.ChatService/GenerateReply",
		"/aibox.v1.ChatService/SelectProvider",
		"/aibox.v1.SomeOtherService/SomeMethod",
	}

	for _, method := range nonSkippedMethods {
		t.Run(method, func(t *testing.T) {
			ctx := context.Background()
			info := &grpc.UnaryServerInfo{FullMethod: method}
			// Request without tenant_id in multi-tenant mode
			req := &pb.GenerateReplyRequest{}

			_, err := unary(ctx, req, info, mockUnaryHandler)

			if err == nil {
				t.Error("Expected error for non-skipped method without tenant_id in multi-tenant mode")
			}

			st, ok := status.FromError(err)
			if !ok {
				t.Fatalf("Error is not a gRPC status: %v", err)
			}
			if st.Code() != codes.InvalidArgument {
				t.Errorf("Code = %v, want %v", st.Code(), codes.InvalidArgument)
			}
		})
	}
}

func TestTenantStream_RecvMsg_OnlyExtractsOnce(t *testing.T) {
	// Verify that tenant extraction only happens on the first RecvMsg call
	mgr := newTestManager(map[string]tenant.TenantConfig{
		"first-tenant": {TenantID: "first-tenant"},
	})
	interceptor := NewTenantInterceptor(mgr)

	recvCount := 0
	ss := &mockServerStream{
		ctx: context.Background(),
		recvMsg: &pb.GenerateReplyRequest{TenantId: "first-tenant"},
	}

	wrapped := &tenantStream{
		ServerStream: ss,
		interceptor:  interceptor,
		tenantSet:    false,
	}

	// First RecvMsg should extract tenant
	msg1 := &pb.GenerateReplyRequest{}
	err := wrapped.RecvMsg(msg1)
	if err != nil {
		t.Fatalf("First RecvMsg() error: %v", err)
	}
	recvCount++

	if !wrapped.tenantSet {
		t.Error("tenantSet should be true after first RecvMsg")
	}
	if wrapped.tenantCfg == nil || wrapped.tenantCfg.TenantID != "first-tenant" {
		t.Error("Tenant config not set correctly after first RecvMsg")
	}

	// Second RecvMsg should not re-extract (tenantSet is already true)
	msg2 := &pb.GenerateReplyRequest{}
	err = wrapped.RecvMsg(msg2)
	if err != nil {
		t.Fatalf("Second RecvMsg() error: %v", err)
	}

	// Tenant config should remain the same
	if wrapped.tenantCfg.TenantID != "first-tenant" {
		t.Error("Tenant config changed after second RecvMsg")
	}
}
