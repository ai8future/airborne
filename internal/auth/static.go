package auth

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// StaticAuthenticator validates requests against a static admin token.
// This is a simpler alternative to Redis-backed KeyStore authentication,
// suitable for internal deployments with a single static token.
type StaticAuthenticator struct {
	adminToken  string
	skipMethods map[string]bool
}

// NewStaticAuthenticator creates a new static token authenticator.
func NewStaticAuthenticator(adminToken string) *StaticAuthenticator {
	return &StaticAuthenticator{
		adminToken: adminToken,
		skipMethods: map[string]bool{
			"/airborne.v1.AdminService/Health": true,
		},
	}
}

// UnaryInterceptor returns a unary server interceptor for static token authentication.
func (a *StaticAuthenticator) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if a.skipMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		ctx, err := a.authenticate(ctx)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamInterceptor returns a stream server interceptor for static token authentication.
func (a *StaticAuthenticator) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if a.skipMethods[info.FullMethod] {
			return handler(srv, ss)
		}
		ctx, err := a.authenticate(ss.Context())
		if err != nil {
			return err
		}
		wrapped := &staticWrappedStream{ServerStream: ss, ctx: ctx}
		return handler(srv, wrapped)
	}
}

func (a *StaticAuthenticator) authenticate(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	token := extractStaticToken(md)
	if token == "" {
		return nil, status.Error(codes.Unauthenticated, "missing API key")
	}

	if subtle.ConstantTimeCompare([]byte(token), []byte(a.adminToken)) != 1 {
		return nil, status.Error(codes.Unauthenticated, "invalid API key")
	}

	// Inject a static client for compatibility with code that expects ClientKey
	client := &ClientKey{
		ClientID:    "admin",
		ClientName:  "static-admin",
		Permissions: []Permission{PermissionChat, PermissionChatStream, PermissionFiles, PermissionAdmin},
	}
	return context.WithValue(ctx, ClientContextKey, client), nil
}

// extractStaticToken extracts the token from gRPC metadata.
func extractStaticToken(md metadata.MD) string {
	// Try authorization header first
	if auths := md.Get("authorization"); len(auths) > 0 {
		if token := normalizeAuthHeader(auths[0]); token != "" {
			return token
		}
	}
	// Try x-api-key header
	if keys := md.Get("x-api-key"); len(keys) > 0 {
		return strings.TrimSpace(keys[0])
	}
	return ""
}

type staticWrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *staticWrappedStream) Context() context.Context {
	return s.ctx
}
