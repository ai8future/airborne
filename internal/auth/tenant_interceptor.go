package auth

import (
	"context"
	"strings"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/tenant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// TenantContextKey is the context key for tenant config
	TenantContextKey contextKey = "aibox_tenant"
)

// TenantInterceptor validates tenant_id and injects tenant config into context.
type TenantInterceptor struct {
	manager     *tenant.Manager
	skipMethods map[string]bool
}

// NewTenantInterceptor creates a new tenant interceptor.
func NewTenantInterceptor(mgr *tenant.Manager) *TenantInterceptor {
	return &TenantInterceptor{
		manager: mgr,
		skipMethods: map[string]bool{
			"/aibox.v1.AdminService/Health":         true,
			"/aibox.v1.AdminService/Ready":          true,
			"/aibox.v1.AdminService/Version":        true,
			"/aibox.v1.FileService/CreateFileStore": true,
			"/aibox.v1.FileService/UploadFile":      true,
			"/aibox.v1.FileService/DeleteFileStore": true,
			"/aibox.v1.FileService/GetFileStore":    true,
			"/aibox.v1.FileService/ListFileStores":  true,
		},
	}
}

// UnaryInterceptor validates tenant and adds config to context.
func (t *TenantInterceptor) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Skip for admin endpoints
		if t.skipMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		// Extract tenant_id from request
		tenantID := extractTenantID(req)

		// Resolve tenant
		tenantCfg, err := t.resolveTenant(tenantID)
		if err != nil {
			return nil, err
		}

		// Add tenant config to context
		ctx = context.WithValue(ctx, TenantContextKey, tenantCfg)

		return handler(ctx, req)
	}
}

// StreamInterceptor validates tenant and adds config to context for streams.
func (t *TenantInterceptor) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Skip for admin endpoints
		if t.skipMethods[info.FullMethod] {
			return handler(srv, ss)
		}

		// For streaming, we need to intercept the first message
		// Since GenerateReplyStream uses the same request as GenerateReply,
		// we'll use a wrapped stream that extracts tenant from first recv

		wrapped := &tenantStream{
			ServerStream: ss,
			interceptor:  t,
			tenantSet:    false,
		}

		return handler(srv, wrapped)
	}
}

// resolveTenant resolves the tenant config from tenant_id.
func (t *TenantInterceptor) resolveTenant(tenantID string) (*tenant.TenantConfig, error) {
	// If tenant_id is empty, check for single-tenant mode
	if tenantID == "" {
		if t.manager.IsSingleTenant() {
			cfg, _ := t.manager.DefaultTenant()
			return &cfg, nil
		}
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	// Normalize tenant_id
	tenantID = strings.ToLower(strings.TrimSpace(tenantID))

	// Validate tenant exists
	cfg, ok := t.manager.Tenant(tenantID)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "tenant %q not found", tenantID)
	}

	return &cfg, nil
}

// extractTenantID extracts tenant_id from various request types.
func extractTenantID(req interface{}) string {
	switch r := req.(type) {
	case *pb.GenerateReplyRequest:
		return r.TenantId
	case *pb.SelectProviderRequest:
		return r.TenantId
	default:
		return ""
	}
}

// TenantFromContext retrieves the tenant config from context.
func TenantFromContext(ctx context.Context) *tenant.TenantConfig {
	if cfg, ok := ctx.Value(TenantContextKey).(*tenant.TenantConfig); ok {
		return cfg
	}
	return nil
}

// TenantIDFromContext extracts tenant ID from context with fallbacks.
// Tries tenant config first, then client ID, then returns "default".
func TenantIDFromContext(ctx context.Context) string {
	if cfg := TenantFromContext(ctx); cfg != nil && cfg.TenantID != "" {
		return cfg.TenantID
	}
	if client := ClientFromContext(ctx); client != nil && client.ClientID != "" {
		return client.ClientID
	}
	return "default"
}

// tenantStream wraps a ServerStream to handle tenant extraction for streaming.
type tenantStream struct {
	grpc.ServerStream
	interceptor *TenantInterceptor
	tenantSet   bool
	tenantCfg   *tenant.TenantConfig
}

func (s *tenantStream) Context() context.Context {
	if s.tenantCfg != nil {
		return context.WithValue(s.ServerStream.Context(), TenantContextKey, s.tenantCfg)
	}
	return s.ServerStream.Context()
}

// RecvMsg intercepts the first message to extract tenant_id.
func (s *tenantStream) RecvMsg(m interface{}) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}

	// Extract tenant from first message if not already set
	if !s.tenantSet {
		tenantID := extractTenantID(m)
		cfg, err := s.interceptor.resolveTenant(tenantID)
		if err != nil {
			return err
		}
		s.tenantCfg = cfg
		s.tenantSet = true
	}

	return nil
}
