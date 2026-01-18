// Package markdownsvc provides a Go client for the markdown_svc gRPC service.
package markdownsvc

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/ai8future/markdown_svc/clients/go/markdownsvcv1"
)

// Client wraps the gRPC client with convenient methods.
type Client struct {
	conn   *grpc.ClientConn
	client pb.MarkdownServiceClient
}

// Option configures the client.
type Option func(*clientOptions)

type clientOptions struct {
	timeout time.Duration
}

// WithTimeout sets the default timeout for operations.
func WithTimeout(d time.Duration) Option {
	return func(o *clientOptions) {
		o.timeout = d
	}
}

// NewClient creates a new markdown_svc client.
func NewClient(address string, opts ...Option) (*Client, error) {
	options := &clientOptions{
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(options)
	}

	ctx, cancel := context.WithTimeout(context.Background(), options.timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		conn:   conn,
		client: pb.NewMarkdownServiceClient(conn),
	}, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// ParseOptions configures ParseMarkdown behavior.
type ParseOptions struct {
	Preset             string
	Plugins            []PluginConfig
	IncludeHTML        bool
	SanitizationPreset string
	CustomSchemaJSON   string
	Transforms         []string
}

// ParseOption configures parsing.
type ParseOption func(*ParseOptions)

// WithPreset sets the plugin preset.
func WithPreset(preset string) ParseOption {
	return func(o *ParseOptions) {
		o.Preset = preset
	}
}

// WithPlugins sets explicit plugins.
func WithPlugins(plugins ...PluginConfig) ParseOption {
	return func(o *ParseOptions) {
		o.Plugins = plugins
	}
}

// WithHTML enables HTML rendering.
func WithHTML() ParseOption {
	return func(o *ParseOptions) {
		o.IncludeHTML = true
	}
}

// WithSanitization sets the sanitization preset.
func WithSanitization(preset string) ParseOption {
	return func(o *ParseOptions) {
		o.SanitizationPreset = preset
	}
}

// WithTransforms adds transforms to apply.
func WithTransforms(transforms ...string) ParseOption {
	return func(o *ParseOptions) {
		o.Transforms = transforms
	}
}

// PluginConfig specifies a plugin and its options.
type PluginConfig struct {
	Name    string
	Options string
}

// Plugin creates a plugin config.
func Plugin(name string, options ...string) PluginConfig {
	cfg := PluginConfig{Name: name}
	if len(options) > 0 {
		cfg.Options = options[0]
	}
	return cfg
}

// ParseResult contains parsing results.
type ParseResult struct {
	AstJSON string
	HTML    string
	Results []TransformResult
}

// TransformResult contains transform output.
type TransformResult struct {
	Type      string
	Links     []ExtractedLink
	PlainText string
}

// ExtractedLink represents a link.
type ExtractedLink struct {
	URL   string
	Text  string
	Title string
	Line  int32
}

// ParseMarkdown parses markdown with options.
func (c *Client) ParseMarkdown(ctx context.Context, content string, opts ...ParseOption) (*ParseResult, error) {
	options := &ParseOptions{}
	for _, opt := range opts {
		opt(options)
	}

	req := &pb.ParseMarkdownRequest{
		Content:            content,
		Preset:             options.Preset,
		IncludeHtml:        options.IncludeHTML,
		SanitizationPreset: options.SanitizationPreset,
		CustomSchemaJson:   options.CustomSchemaJSON,
	}

	for _, p := range options.Plugins {
		req.Plugins = append(req.Plugins, &pb.PluginConfig{
			Name:    p.Name,
			Options: p.Options,
		})
	}

	for _, t := range options.Transforms {
		req.Transforms = append(req.Transforms, &pb.Transform{Type: t})
	}

	resp, err := c.client.ParseMarkdown(ctx, req)
	if err != nil {
		return nil, err
	}

	result := &ParseResult{
		AstJSON: resp.AstJson,
		HTML:    resp.Html,
	}

	for _, r := range resp.Results {
		tr := TransformResult{
			Type:      r.TransformType,
			PlainText: r.PlainText,
		}
		for _, l := range r.Links {
			tr.Links = append(tr.Links, ExtractedLink{
				URL:   l.Url,
				Text:  l.Text,
				Title: l.Title,
				Line:  l.Line,
			})
		}
		result.Results = append(result.Results, tr)
	}

	return result, nil
}

// RenderToHTML renders markdown to HTML.
func (c *Client) RenderToHTML(ctx context.Context, content string, opts ...ParseOption) (string, error) {
	options := &ParseOptions{}
	for _, opt := range opts {
		opt(options)
	}

	req := &pb.RenderToHTMLRequest{
		Content:            content,
		Preset:             options.Preset,
		SanitizationPreset: options.SanitizationPreset,
		CustomSchemaJson:   options.CustomSchemaJSON,
	}

	for _, p := range options.Plugins {
		req.Plugins = append(req.Plugins, &pb.PluginConfig{
			Name:    p.Name,
			Options: p.Options,
		})
	}

	resp, err := c.client.RenderToHTML(ctx, req)
	if err != nil {
		return "", err
	}

	return resp.Html, nil
}

// ChunkOptions configures chunking behavior.
type ChunkOptions struct {
	MaxChunkSize       int32
	OverlapSize        int32
	PreserveCodeBlocks bool
	IncludeMetadata    bool
}

// ChunkOption configures chunking.
type ChunkOption func(*ChunkOptions)

// MaxChunkSize sets max characters per chunk.
func MaxChunkSize(size int32) ChunkOption {
	return func(o *ChunkOptions) {
		o.MaxChunkSize = size
	}
}

// OverlapSize sets overlap between chunks.
func OverlapSize(size int32) ChunkOption {
	return func(o *ChunkOptions) {
		o.OverlapSize = size
	}
}

// PreserveCodeBlocks keeps code blocks intact.
func PreserveCodeBlocks() ChunkOption {
	return func(o *ChunkOptions) {
		o.PreserveCodeBlocks = true
	}
}

// IncludeMetadata includes section paths.
func IncludeMetadata() ChunkOption {
	return func(o *ChunkOptions) {
		o.IncludeMetadata = true
	}
}

// Chunk represents a markdown chunk.
type Chunk struct {
	Content        string
	Index          int32
	StartOffset    int32
	EndOffset      int32
	SectionPath    []string
	HeadingContext string
	ChunkType      string
}

// OutlineNode represents a heading.
type OutlineNode struct {
	Depth       int32
	Text        string
	StartOffset int32
	Children    []OutlineNode
}

// ChunkResult contains chunking results.
type ChunkResult struct {
	Chunks  []Chunk
	Outline []OutlineNode
}

// ChunkMarkdown chunks markdown semantically.
func (c *Client) ChunkMarkdown(ctx context.Context, content string, opts ...ChunkOption) (*ChunkResult, error) {
	options := &ChunkOptions{}
	for _, opt := range opts {
		opt(options)
	}

	req := &pb.ChunkMarkdownRequest{
		Content: content,
		Options: &pb.ChunkingOptions{
			MaxChunkSize:       options.MaxChunkSize,
			OverlapSize:        options.OverlapSize,
			PreserveCodeBlocks: options.PreserveCodeBlocks,
			IncludeMetadata:    options.IncludeMetadata,
		},
	}

	resp, err := c.client.ChunkMarkdown(ctx, req)
	if err != nil {
		return nil, err
	}

	result := &ChunkResult{}

	for _, ch := range resp.Chunks {
		chunk := Chunk{
			Content: ch.Content,
		}
		if ch.Metadata != nil {
			chunk.Index = ch.Metadata.Index
			chunk.StartOffset = ch.Metadata.StartOffset
			chunk.EndOffset = ch.Metadata.EndOffset
			chunk.SectionPath = ch.Metadata.SectionPath
			chunk.HeadingContext = ch.Metadata.HeadingContext
			chunk.ChunkType = ch.Metadata.ChunkType
		}
		result.Chunks = append(result.Chunks, chunk)
	}

	if resp.Outline != nil {
		result.Outline = convertOutlineNodes(resp.Outline.Nodes)
	}

	return result, nil
}

func convertOutlineNodes(nodes []*pb.OutlineNode) []OutlineNode {
	var result []OutlineNode
	for _, n := range nodes {
		node := OutlineNode{
			Depth:       n.Depth,
			Text:        n.Text,
			StartOffset: n.StartOffset,
			Children:    convertOutlineNodes(n.Children),
		}
		result = append(result, node)
	}
	return result
}

// ExtractLinks extracts all links from markdown.
func (c *Client) ExtractLinks(ctx context.Context, content string) ([]ExtractedLink, error) {
	result, err := c.ParseMarkdown(ctx, content, WithTransforms("extract_links"))
	if err != nil {
		return nil, err
	}

	if len(result.Results) > 0 {
		return result.Results[0].Links, nil
	}
	return nil, nil
}

// ToPlainText converts markdown to plain text.
func (c *Client) ToPlainText(ctx context.Context, content string) (string, error) {
	result, err := c.ParseMarkdown(ctx, content, WithTransforms("strip_formatting"))
	if err != nil {
		return "", err
	}

	if len(result.Results) > 0 {
		return result.Results[0].PlainText, nil
	}
	return "", nil
}
