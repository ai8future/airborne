package auth

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestExtractStaticToken(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.MD
		want string
	}{
		{
			name: "bearer token",
			md: metadata.MD{
				"authorization": []string{"Bearer static123"},
			},
			want: "static123",
		},
		{
			name: "authorization without bearer",
			md: metadata.MD{
				"authorization": []string{"rawtoken"},
			},
			want: "rawtoken",
		},
		{
			name: "x-api-key header",
			md: metadata.MD{
				"x-api-key": []string{"apikey"},
			},
			want: "apikey",
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
			name: "no auth headers",
			md:   metadata.MD{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStaticToken(tt.md)
			if got != tt.want {
				t.Errorf("extractStaticToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStaticAuthenticateSuccess(t *testing.T) {
	auth := NewStaticAuthenticator("secret")
	md := metadata.Pairs("authorization", "Bearer secret")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	gotCtx, err := auth.authenticate(ctx)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	client, ok := gotCtx.Value(ClientContextKey).(*ClientKey)
	if !ok || client == nil {
		t.Fatal("expected ClientKey in context")
	}
	if client.ClientID != "admin" {
		t.Errorf("ClientID = %q, want %q", client.ClientID, "admin")
	}
	if client.ClientName != "static-admin" {
		t.Errorf("ClientName = %q, want %q", client.ClientName, "static-admin")
	}
	if len(client.Permissions) != 4 {
		t.Errorf("Permissions length = %d, want 4", len(client.Permissions))
	}
}

func TestStaticAuthenticateMissingMetadata(t *testing.T) {
	auth := NewStaticAuthenticator("secret")

	_, err := auth.authenticate(context.Background())
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated error, got %v", err)
	}
}

func TestStaticAuthenticateInvalidToken(t *testing.T) {
	auth := NewStaticAuthenticator("secret")
	md := metadata.Pairs("x-api-key", "wrong")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := auth.authenticate(ctx)
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated error, got %v", err)
	}
}

func TestStaticAuthenticateMissingToken(t *testing.T) {
	auth := NewStaticAuthenticator("secret")
	md := metadata.MD{}
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := auth.authenticate(ctx)
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated error, got %v", err)
	}
}

func TestStaticUnaryInterceptorSkipMethod(t *testing.T) {
	auth := NewStaticAuthenticator("secret")
	interceptor := auth.UnaryInterceptor()
	called := false

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/aibox.v1.AdminService/Health"}, handler)
	if err != nil {
		t.Fatalf("interceptor error: %v", err)
	}
	if !called {
		t.Fatal("expected handler to be called for skip method")
	}
}

func TestStaticUnaryInterceptorRequiresAuth(t *testing.T) {
	auth := NewStaticAuthenticator("secret")
	interceptor := auth.UnaryInterceptor()
	called := false

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}

	// No auth context - should fail
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/aibox.v1.ChatService/Chat"}, handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated error, got %v", err)
	}
	if called {
		t.Fatal("handler should not be called without auth")
	}
}

func TestStaticUnaryInterceptorWithValidAuth(t *testing.T) {
	auth := NewStaticAuthenticator("secret")
	interceptor := auth.UnaryInterceptor()
	called := false
	var gotClient *ClientKey

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		gotClient = ctx.Value(ClientContextKey).(*ClientKey)
		return "ok", nil
	}

	md := metadata.Pairs("authorization", "Bearer secret")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/aibox.v1.ChatService/Chat"}, handler)
	if err != nil {
		t.Fatalf("interceptor error: %v", err)
	}
	if !called {
		t.Fatal("expected handler to be called")
	}
	if gotClient == nil || gotClient.ClientID != "admin" {
		t.Fatal("expected ClientKey with admin ID in context")
	}
}
