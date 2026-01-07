package auth

import (
	"context"
	"testing"

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

func TestRequirePermission(t *testing.T) {
	tests := []struct {
		name       string
		ctx        context.Context
		perm       Permission
		wantCode   codes.Code
		wantErr    bool
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
