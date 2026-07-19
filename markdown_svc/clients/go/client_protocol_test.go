package markdownsvc

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ai8future/airborne/markdown_svc/clients/go/markdownsvcv1"
)

type protocolServer struct {
	pb.UnimplementedMarkdownServiceServer

	mu        sync.Mutex
	parseReq  *pb.ParseMarkdownRequest
	parseTTL  time.Duration
	renderReq *pb.RenderToHTMLRequest
	chunkReq  *pb.ChunkMarkdownRequest
}

func (s *protocolServer) ParseMarkdown(ctx context.Context, req *pb.ParseMarkdownRequest) (*pb.ParseMarkdownResponse, error) {
	s.mu.Lock()
	s.parseReq = req
	if deadline, ok := ctx.Deadline(); ok {
		s.parseTTL = time.Until(deadline)
	}
	s.mu.Unlock()

	switch req.Content {
	case "parse-error", "extract-error", "plain-error":
		return nil, status.Error(codes.InvalidArgument, "invalid markdown")
	case "empty":
		return &pb.ParseMarkdownResponse{}, nil
	case "links":
		return &pb.ParseMarkdownResponse{Results: []*pb.TransformResult{{
			TransformType: "extract_links",
			Links: []*pb.ExtractedLink{{
				Url:   "https://example.com",
				Text:  "example",
				Title: "Example",
				Line:  7,
			}},
		}}}, nil
	case "plain":
		return &pb.ParseMarkdownResponse{Results: []*pb.TransformResult{{
			TransformType: "strip_formatting",
			PlainText:     "plain text",
		}}}, nil
	default:
		return &pb.ParseMarkdownResponse{
			AstJson: `{"type":"root"}`,
			Html:    "<h1>Hello</h1>",
			Results: []*pb.TransformResult{{
				TransformType: "extract_links",
				PlainText:     "Hello",
				Links: []*pb.ExtractedLink{{
					Url:   "https://example.com/docs",
					Text:  "docs",
					Title: "Documentation",
					Line:  3,
				}},
			}},
		}, nil
	}
}

func (s *protocolServer) RenderToHTML(_ context.Context, req *pb.RenderToHTMLRequest) (*pb.RenderToHTMLResponse, error) {
	s.mu.Lock()
	s.renderReq = req
	s.mu.Unlock()

	if req.Content == "render-error" {
		return nil, status.Error(codes.Internal, "renderer failed")
	}
	return &pb.RenderToHTMLResponse{Html: "<p>rendered</p>"}, nil
}

func (s *protocolServer) ChunkMarkdown(_ context.Context, req *pb.ChunkMarkdownRequest) (*pb.ChunkMarkdownResponse, error) {
	s.mu.Lock()
	s.chunkReq = req
	s.mu.Unlock()

	if req.Content == "chunk-error" {
		return nil, status.Error(codes.ResourceExhausted, "too many chunks")
	}
	return &pb.ChunkMarkdownResponse{
		Chunks: []*pb.MarkdownChunk{
			{
				Content: "first",
				Metadata: &pb.ChunkMetadata{
					Index:          1,
					StartOffset:    10,
					EndOffset:      20,
					SectionPath:    []string{"Guide", "Install"},
					HeadingContext: "Install",
					ChunkType:      "section",
				},
			},
			{Content: "without metadata"},
		},
		Outline: &pb.DocumentOutline{Nodes: []*pb.OutlineNode{{
			Depth:       1,
			Text:        "Guide",
			StartOffset: 0,
			Children: []*pb.OutlineNode{{
				Depth:       2,
				Text:        "Install",
				StartOffset: 10,
			}},
		}}},
	}, nil
}

func (s *protocolServer) lastParseRequest() *pb.ParseMarkdownRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.parseReq
}

func (s *protocolServer) lastParseTTL() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.parseTTL
}

func (s *protocolServer) lastRenderRequest() *pb.RenderToHTMLRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.renderReq
}

func (s *protocolServer) lastChunkRequest() *pb.ChunkMarkdownRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chunkReq
}

func startProtocolServer(t *testing.T) (string, *protocolServer, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := grpc.NewServer()
	service := &protocolServer{}
	pb.RegisterMarkdownServiceServer(server, service)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	cleanup := func() {
		server.Stop()
		_ = listener.Close()
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("serve: %v", err)
		}
	}
	return listener.Addr().String(), service, cleanup
}

func newProtocolClient(t *testing.T) (*Client, *protocolServer) {
	t.Helper()
	address, service, stop := startProtocolServer(t)
	client, err := NewClient(address)
	if err != nil {
		stop()
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil && !errors.Is(err, grpc.ErrClientConnClosing) {
			t.Errorf("Close: %v", err)
		}
		stop()
	})
	return client, service
}

func TestNewClientAndConnectionLifecycle(t *testing.T) {
	address, _, stop := startProtocolServer(t)
	client, err := NewClient(address, WithTimeout(time.Second))
	if err != nil {
		stop()
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Close(); err != nil {
		stop()
		t.Fatalf("Close: %v", err)
	}

	_, err = client.ParseMarkdown(context.Background(), "after close")
	if status.Code(err) != codes.Canceled {
		stop()
		t.Fatalf("ParseMarkdown after Close code = %v, want %v (err=%v)", status.Code(err), codes.Canceled, err)
	}
	stop()
}

func TestNewClientHonorsDialTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	started := time.Now()
	client, err := NewClient(address, WithTimeout(25*time.Millisecond))
	if err == nil {
		_ = client.Close()
		t.Fatal("NewClient unexpectedly connected to closed listener")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("NewClient exceeded bounded dial timeout: %s", elapsed)
	}
}

func TestOperationContextSelectsEarliestDeadline(t *testing.T) {
	client := &Client{timeout: 200 * time.Millisecond}

	ctx, cancel := client.operationContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("operationContext did not apply a default deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > client.timeout {
		t.Fatalf("default deadline remaining = %s, want within (0, %s]", remaining, client.timeout)
	}

	laterParent, cancelLater := context.WithTimeout(context.Background(), time.Hour)
	defer cancelLater()
	laterCtx, cancel := client.operationContext(laterParent)
	defer cancel()
	laterDeadline, ok := laterCtx.Deadline()
	if !ok {
		t.Fatal("operationContext did not cap a later caller deadline")
	}
	if remaining := time.Until(laterDeadline); remaining <= 0 || remaining > client.timeout {
		t.Fatalf("capped deadline remaining = %s, want within (0, %s]", remaining, client.timeout)
	}

	earlierDeadline := time.Now().Add(50 * time.Millisecond)
	earlierParent, cancelEarlier := context.WithDeadline(context.Background(), earlierDeadline)
	defer cancelEarlier()
	earlierCtx, cancel := client.operationContext(earlierParent)
	defer cancel()
	gotEarlierDeadline, ok := earlierCtx.Deadline()
	if !ok || !gotEarlierDeadline.Equal(earlierDeadline) {
		t.Fatalf("earlier deadline = %v, %v; want %v, true", gotEarlierDeadline, ok, earlierDeadline)
	}

	canceledParent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	canceledCtx, cancel := client.operationContext(canceledParent)
	defer cancel()
	if !errors.Is(canceledCtx.Err(), context.Canceled) {
		t.Fatalf("operationContext cancellation = %v, want context.Canceled", canceledCtx.Err())
	}
}

func TestParseMarkdownProtocolAndConversion(t *testing.T) {
	client, service := newProtocolClient(t)

	result, err := client.ParseMarkdown(
		context.Background(),
		"# Hello",
		WithPreset("github"),
		WithPlugins(Plugin("gfm", `{"tables":true}`), Plugin("frontmatter")),
		WithHTML(),
		WithSanitization("strict"),
		func(options *ParseOptions) { options.CustomSchemaJSON = `{"tagNames":["mark"]}` },
		WithTransforms("extract_links", "strip_formatting"),
	)
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}

	want := &ParseResult{
		AstJSON: `{"type":"root"}`,
		HTML:    "<h1>Hello</h1>",
		Results: []TransformResult{{
			Type:      "extract_links",
			PlainText: "Hello",
			Links: []ExtractedLink{{
				URL: "https://example.com/docs", Text: "docs", Title: "Documentation", Line: 3,
			}},
		}},
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("ParseMarkdown result = %#v, want %#v", result, want)
	}
	if ttl := service.lastParseTTL(); ttl < 118*time.Second || ttl > defaultOperationTimeout {
		t.Fatalf("ParseMarkdown server deadline = %s, want approximately %s", ttl, defaultOperationTimeout)
	}

	req := service.lastParseRequest()
	if req.GetContent() != "# Hello" || req.GetPreset() != "github" || !req.GetIncludeHtml() || req.GetSanitizationPreset() != "strict" || req.GetCustomSchemaJson() != `{"tagNames":["mark"]}` {
		t.Fatalf("ParseMarkdown request scalars = %#v", req)
	}
	if len(req.GetPlugins()) != 2 || req.GetPlugins()[0].GetName() != "gfm" || req.GetPlugins()[0].GetOptions() != `{"tables":true}` || req.GetPlugins()[1].GetOptions() != "" {
		t.Fatalf("ParseMarkdown plugins = %#v", req.GetPlugins())
	}
	if got := []string{req.GetTransforms()[0].GetType(), req.GetTransforms()[1].GetType()}; !reflect.DeepEqual(got, []string{"extract_links", "strip_formatting"}) {
		t.Fatalf("ParseMarkdown transforms = %v", got)
	}

	_, err = client.ParseMarkdown(context.Background(), "parse-error")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ParseMarkdown error code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
}

func TestRenderToHTMLProtocolAndError(t *testing.T) {
	client, service := newProtocolClient(t)

	html, err := client.RenderToHTML(
		context.Background(),
		"render",
		WithPreset("commonmark"),
		WithPlugins(Plugin("gfm", "options")),
		WithSanitization("relaxed"),
		func(options *ParseOptions) { options.CustomSchemaJSON = "schema" },
	)
	if err != nil {
		t.Fatalf("RenderToHTML: %v", err)
	}
	if html != "<p>rendered</p>" {
		t.Fatalf("RenderToHTML = %q", html)
	}
	req := service.lastRenderRequest()
	if req.GetContent() != "render" || req.GetPreset() != "commonmark" || req.GetSanitizationPreset() != "relaxed" || req.GetCustomSchemaJson() != "schema" || len(req.GetPlugins()) != 1 || req.GetPlugins()[0].GetOptions() != "options" {
		t.Fatalf("RenderToHTML request = %#v", req)
	}

	_, err = client.RenderToHTML(context.Background(), "render-error")
	if status.Code(err) != codes.Internal {
		t.Fatalf("RenderToHTML error code = %v, want %v", status.Code(err), codes.Internal)
	}
}

func TestChunkMarkdownProtocolAndConversion(t *testing.T) {
	client, service := newProtocolClient(t)

	result, err := client.ChunkMarkdown(
		context.Background(),
		"chunk",
		MaxChunkSize(500),
		OverlapSize(25),
		PreserveCodeBlocks(),
		IncludeMetadata(),
	)
	if err != nil {
		t.Fatalf("ChunkMarkdown: %v", err)
	}
	want := &ChunkResult{
		Chunks: []Chunk{
			{
				Content: "first", Index: 1, StartOffset: 10, EndOffset: 20,
				SectionPath: []string{"Guide", "Install"}, HeadingContext: "Install", ChunkType: "section",
			},
			{Content: "without metadata"},
		},
		Outline: []OutlineNode{{
			Depth: 1, Text: "Guide", StartOffset: 0,
			Children: []OutlineNode{{Depth: 2, Text: "Install", StartOffset: 10}},
		}},
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("ChunkMarkdown result = %#v, want %#v", result, want)
	}
	req := service.lastChunkRequest()
	if req.GetContent() != "chunk" || req.GetOptions().GetMaxChunkSize() != 500 || req.GetOptions().GetOverlapSize() != 25 || !req.GetOptions().GetPreserveCodeBlocks() || !req.GetOptions().GetIncludeMetadata() {
		t.Fatalf("ChunkMarkdown request = %#v", req)
	}

	_, err = client.ChunkMarkdown(context.Background(), "chunk-error")
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("ChunkMarkdown error code = %v, want %v", status.Code(err), codes.ResourceExhausted)
	}
}

func TestConvenienceTransforms(t *testing.T) {
	client, _ := newProtocolClient(t)

	links, err := client.ExtractLinks(context.Background(), "links")
	if err != nil {
		t.Fatalf("ExtractLinks: %v", err)
	}
	wantLinks := []ExtractedLink{{URL: "https://example.com", Text: "example", Title: "Example", Line: 7}}
	if !reflect.DeepEqual(links, wantLinks) {
		t.Fatalf("ExtractLinks = %#v, want %#v", links, wantLinks)
	}
	if links, err := client.ExtractLinks(context.Background(), "empty"); err != nil || links != nil {
		t.Fatalf("ExtractLinks empty = %#v, %v", links, err)
	}
	if _, err := client.ExtractLinks(context.Background(), "extract-error"); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ExtractLinks error = %v", err)
	}

	plain, err := client.ToPlainText(context.Background(), "plain")
	if err != nil {
		t.Fatalf("ToPlainText: %v", err)
	}
	if plain != "plain text" {
		t.Fatalf("ToPlainText = %q", plain)
	}
	if plain, err := client.ToPlainText(context.Background(), "empty"); err != nil || plain != "" {
		t.Fatalf("ToPlainText empty = %q, %v", plain, err)
	}
	if _, err := client.ToPlainText(context.Background(), "plain-error"); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ToPlainText error = %v", err)
	}
}
