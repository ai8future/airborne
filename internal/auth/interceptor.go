package auth

import (
	"context"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// contextKey is a custom type for context keys
type contextKey string

const (
	// ClientContextKey is the context key for client info
	ClientContextKey contextKey = "aibox_client"
)

// Authenticator handles API key authentication
type Authenticator struct {
	keyStore    *KeyStore
	rateLimiter *RateLimiter
	skipMethods map[string]bool
}

// NewAuthenticator creates a new authenticator
func NewAuthenticator(keyStore *KeyStore, rateLimiter *RateLimiter) *Authenticator {
	return &Authenticator{
		keyStore:    keyStore,
		rateLimiter: rateLimiter,
		skipMethods: map[string]bool{
			"/aibox.v1.AdminService/Health":  true,
			"/aibox.v1.AdminService/Version": true,
		},
	}
}

// UnaryInterceptor returns a unary server interceptor for authentication
func (a *Authenticator) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Skip auth for health endpoints
		if a.skipMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		// Authenticate
		client, err := a.authenticate(ctx)
		if err != nil {
			return nil, err
		}

		// Check rate limits
		if a.rateLimiter != nil {
			if err := a.rateLimiter.Allow(ctx, client); err != nil {
				return nil, status.Error(codes.ResourceExhausted, err.Error())
			}
		}

		// Add client to context
		ctx = context.WithValue(ctx, ClientContextKey, client)

		return handler(ctx, req)
	}
}

// StreamInterceptor returns a stream server interceptor for authentication
func (a *Authenticator) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Skip auth for health endpoints
		if a.skipMethods[info.FullMethod] {
			return handler(srv, ss)
		}

		// Authenticate
		client, err := a.authenticate(ss.Context())
		if err != nil {
			return err
		}

		// Check rate limits
		if a.rateLimiter != nil {
			if err := a.rateLimiter.Allow(ss.Context(), client); err != nil {
				return status.Error(codes.ResourceExhausted, err.Error())
			}
		}

		// Wrap stream with authenticated context
		wrapped := &authenticatedStream{
			ServerStream: ss,
			ctx:          context.WithValue(ss.Context(), ClientContextKey, client),
		}

		return handler(srv, wrapped)
	}
}

// authenticate extracts and validates the API key from metadata
func (a *Authenticator) authenticate(ctx context.Context) (*ClientKey, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	// Extract API key from authorization header
	apiKey := extractAPIKey(md)
	if apiKey == "" {
		return nil, status.Error(codes.Unauthenticated, "missing API key")
	}

	// Validate key
	client, err := a.keyStore.ValidateKey(ctx, apiKey)
	if err != nil {
		slog.Debug("authentication failed", "error", err)
		switch err {
		case ErrKeyNotFound, ErrInvalidKey:
			return nil, status.Error(codes.Unauthenticated, "invalid API key")
		case ErrKeyExpired:
			return nil, status.Error(codes.Unauthenticated, "API key expired")
		default:
			return nil, status.Error(codes.Internal, "authentication error")
		}
	}

	return client, nil
}

// extractAPIKey extracts the API key from gRPC metadata
func extractAPIKey(md metadata.MD) string {
	// Try authorization header first
	if auths := md.Get("authorization"); len(auths) > 0 {
		auth := auths[0]
		// Support "Bearer <token>" format
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
		return auth
	}

	// Try x-api-key header
	if keys := md.Get("x-api-key"); len(keys) > 0 {
		return keys[0]
	}

	return ""
}

// authenticatedStream wraps a ServerStream with an authenticated context
type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedStream) Context() context.Context {
	return s.ctx
}

// ClientFromContext retrieves the authenticated client from context
func ClientFromContext(ctx context.Context) *ClientKey {
	if client, ok := ctx.Value(ClientContextKey).(*ClientKey); ok {
		return client
	}
	return nil
}

// RequirePermission checks if the client has the required permission
func RequirePermission(ctx context.Context, perm Permission) error {
	client := ClientFromContext(ctx)
	if client == nil {
		return status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !client.HasPermission(perm) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}
