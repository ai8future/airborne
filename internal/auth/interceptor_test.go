package auth

import (
	"context"
	"testing"

	airborneredis "github.com/ai8future/airborne/internal/redis"
	"github.com/alicebob/miniredis/v2"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		md      metadata.MD
		want    string
		wantNil bool
	}{
		{
			name: "bearer token",
			md: metadata.MD{
				"authorization": []string{"Bearer myapikey123"},
			},
			want: "myapikey123",
		},
		{
			name: "authorization without bearer",
			md: metadata.MD{
				"authorization": []string{"rawtoken456"},
			},
			want: "rawtoken456",
		},
		{
			name: "x-api-key header",
			md: metadata.MD{
				"x-api-key": []string{"xapikey789"},
			},
			want: "xapikey789",
		},
		{
			name: "authorization takes precedence over x-api-key",
			md: metadata.MD{
				"authorization": []string{"Bearer authtoken"},
				"x-api-key":     []string{"xapitoken"},
			},
			want: "authtoken",
		},
		{
			name:    "no auth headers",
			md:      metadata.MD{},
			wantNil: true,
		},
		{
			name: "empty authorization",
			md: metadata.MD{
				"authorization": []string{""},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAPIKey(tt.md)
			if tt.wantNil {
				if got != "" {
					t.Errorf("extractAPIKey() = %v, want empty", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("extractAPIKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthenticatorRateLimitHasTypedPreDispatchDetail(t *testing.T) {
	mr := miniredis.RunT(t)
	redisClient, err := airborneredis.NewClient(airborneredis.Config{Addr: mr.Addr()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	keyStore := NewKeyStore(redisClient)
	rawKey, _, err := keyStore.GenerateAPIKey(context.Background(), "rate-limited-client", "test", []Permission{PermissionChat}, RateLimits{RequestsPerMinute: 1})
	if err != nil {
		t.Fatal(err)
	}
	authenticator := NewAuthenticator(keyStore, NewRateLimiter(redisClient, RateLimits{}, true))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+rawKey))
	handlerCalls := 0
	handler := func(context.Context, any) (any, error) {
		handlerCalls++
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/airborne.v1.AirborneService/GenerateReply"}

	if _, err := authenticator.UnaryInterceptor()(ctx, nil, info, handler); err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	_, err = authenticator.UnaryInterceptor()(ctx, nil, info, handler)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("rate-limit code = %v, want %v (err=%v)", status.Code(err), codes.ResourceExhausted, err)
	}
	var errorInfo *errdetails.ErrorInfo
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			errorInfo = info
		}
	}
	if errorInfo == nil || errorInfo.Reason != authRateLimitPreDispatchReason {
		t.Fatalf("rate-limit ErrorInfo = %#v, want reason %q", errorInfo, authRateLimitPreDispatchReason)
	}
	if errorInfo.Metadata["dispatch_phase"] != "pre_dispatch" {
		t.Fatalf("rate-limit dispatch phase = %q, want pre_dispatch", errorInfo.Metadata["dispatch_phase"])
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
}

func TestRequirePermission(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		perm     Permission
		wantCode codes.Code
		wantErr  bool
	}{
		{
			name:     "no client in context",
			ctx:      context.Background(),
			perm:     PermissionChat,
			wantCode: codes.Unauthenticated,
			wantErr:  true,
		},
		{
			name: "client without permission",
			ctx: context.WithValue(context.Background(), ClientContextKey, &ClientKey{
				ClientID:    "client1",
				Permissions: []Permission{PermissionFiles},
			}),
			perm:     PermissionChat,
			wantCode: codes.PermissionDenied,
			wantErr:  true,
		},
		{
			name: "client with permission",
			ctx: context.WithValue(context.Background(), ClientContextKey, &ClientKey{
				ClientID:    "client2",
				Permissions: []Permission{PermissionChat},
			}),
			perm:    PermissionChat,
			wantErr: false,
		},
		{
			name: "admin has all permissions",
			ctx: context.WithValue(context.Background(), ClientContextKey, &ClientKey{
				ClientID:    "admin",
				Permissions: []Permission{PermissionAdmin},
			}),
			perm:    PermissionFiles,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequirePermission(tt.ctx, tt.perm)
			if tt.wantErr {
				if err == nil {
					t.Errorf("RequirePermission() expected error, got nil")
					return
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Errorf("RequirePermission() error is not a gRPC status")
					return
				}
				if st.Code() != tt.wantCode {
					t.Errorf("RequirePermission() code = %v, want %v", st.Code(), tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Errorf("RequirePermission() unexpected error: %v", err)
			}
		})
	}
}

func TestTenantIDFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "empty context returns default",
			ctx:      context.Background(),
			expected: "default",
		},
		{
			name: "client ID fallback",
			ctx: context.WithValue(context.Background(), ClientContextKey, &ClientKey{
				ClientID: "client-123",
			}),
			expected: "client-123",
		},
		{
			name:     "default when all empty",
			ctx:      context.Background(),
			expected: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TenantIDFromContext(tt.ctx)
			if result != tt.expected {
				t.Errorf("TenantIDFromContext() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestClientFromContext(t *testing.T) {
	t.Run("no client", func(t *testing.T) {
		ctx := context.Background()
		client := ClientFromContext(ctx)
		if client != nil {
			t.Errorf("ClientFromContext() = %v, want nil", client)
		}
	})

	t.Run("has client", func(t *testing.T) {
		expected := &ClientKey{
			ClientID:   "test-client",
			ClientName: "Test Client",
		}
		ctx := context.WithValue(context.Background(), ClientContextKey, expected)
		client := ClientFromContext(ctx)
		if client != expected {
			t.Errorf("ClientFromContext() = %v, want %v", client, expected)
		}
	})

	t.Run("wrong type in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ClientContextKey, "not a client key")
		client := ClientFromContext(ctx)
		if client != nil {
			t.Errorf("ClientFromContext() = %v, want nil for wrong type", client)
		}
	})
}

func TestAuthenticatedStreamContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), ClientContextKey, &ClientKey{ClientID: "stream"})
	stream := &authenticatedStream{ServerStream: &mockServerStream{ctx: context.Background()}, ctx: ctx}
	if got := ClientFromContext(stream.Context()); got == nil || got.ClientID != "stream" {
		t.Fatalf("stream context client = %#v", got)
	}
}

func TestAuthenticatorInterceptorBoundaries(t *testing.T) {
	a := NewAuthenticator(nil, nil)
	called := false
	_, err := a.UnaryInterceptor()(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/airborne.v1.AdminService/Health"}, func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil })
	if err != nil || !called {
		t.Fatalf("health must bypass auth: %v", err)
	}
	_, err = a.UnaryInterceptor()(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/private"}, func(context.Context, any) (any, error) { return nil, nil })
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("private missing metadata code = %v", status.Code(err))
	}
}
