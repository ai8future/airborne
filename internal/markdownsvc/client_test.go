package markdownsvc

import (
	"context"
	"errors"
	"net"
	"testing"

	pb "github.com/ai8future/airborne/markdown_svc/clients/go/markdownsvcv1"
	"google.golang.org/grpc"
)

type markdownTestServer struct {
	pb.UnimplementedMarkdownServiceServer
}

func (markdownTestServer) RenderToHTML(context.Context, *pb.RenderToHTMLRequest) (*pb.RenderToHTMLResponse, error) {
	return &pb.RenderToHTMLResponse{Html: "<h1>Hello</h1>"}, nil
}

func TestInitialize_Empty(t *testing.T) {
	// Reset global state
	_ = Close()

	err := Initialize("")
	if err != nil {
		t.Fatalf("Initialize(\"\") error: %v", err)
	}
	if IsEnabled() {
		t.Fatal("IsEnabled() should be false when address is empty")
	}

	// Cleanup
	_ = Close()
}

func TestInitialize_InvalidAddress(t *testing.T) {
	// Reset global state
	_ = Close()

	// Invalid address should fail gracefully (logs warning, disables service)
	err := Initialize("invalid-addr-no-colon")
	// Should not return error - falls back to disabled
	if err != nil {
		t.Fatalf("Initialize() with invalid address should not error: %v", err)
	}
	if IsEnabled() {
		t.Fatal("IsEnabled() should be false after failed connection")
	}

	_ = Close()
}

func TestIsEnabled_NotInitialized(t *testing.T) {
	// Reset global state
	_ = Close()

	if IsEnabled() {
		t.Fatal("IsEnabled() should be false when not initialized")
	}
}

func TestRenderHTML_NotEnabled(t *testing.T) {
	// Reset global state
	_ = Close()

	_, err := RenderHTML(context.Background(), "# Hello")
	if !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("RenderHTML() error = %v, want ErrNotEnabled", err)
	}
}

func TestClose_SafeWhenNotInitialized(t *testing.T) {
	// Reset global state
	_ = Close()

	// Should not panic or error when calling Close() multiple times
	err := Close()
	if err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	// Can call multiple times safely
	err = Close()
	if err != nil {
		t.Fatalf("Close() second call error: %v", err)
	}
}

func TestErrNotEnabled_Message(t *testing.T) {
	err := ErrNotEnabled
	expected := "markdown_svc not enabled"
	if err.Error() != expected {
		t.Fatalf("ErrNotEnabled.Error() = %q, want %q", err.Error(), expected)
	}
}

func TestRenderHTML_ContextCancellation(t *testing.T) {
	// Reset and don't initialize - tests the nil client path
	_ = Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := RenderHTML(ctx, "# Hello")
	// Should return ErrNotEnabled since client is nil (cancelled context doesn't matter if client is nil)
	if !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("RenderHTML() with cancelled context and nil client error = %v, want ErrNotEnabled", err)
	}
}

func TestRenderHTMLWithInProcessGRPCServer(t *testing.T) {
	_ = Close()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterMarkdownServiceServer(server, markdownTestServer{})
	go server.Serve(lis)
	t.Cleanup(func() { server.Stop(); _ = lis.Close(); _ = Close() })
	if err := Initialize(lis.Addr().String()); err != nil {
		t.Fatal(err)
	}
	html, err := RenderHTML(context.Background(), "# Hello")
	if err != nil || html != "<h1>Hello</h1>" {
		t.Fatalf("RenderHTML() = %q, %v", html, err)
	}
}
