package provider

import (
	"context"
	"time"
)

// Provider defines the interface for AI providers
type Provider interface {
	// Name returns the provider identifier (e.g., "openai", "gemini", "anthropic")
	Name() string

	// GenerateReply generates a reply (blocking/unary)
	GenerateReply(ctx context.Context, params GenerateParams) (GenerateResult, error)

	// GenerateReplyStream generates a streaming reply
	GenerateReplyStream(ctx context.Context, params GenerateParams) (<-chan StreamChunk, error)

	// SupportsFileSearch returns true if provider supports RAG/file search
	SupportsFileSearch() bool

	// SupportsWebSearch returns true if provider supports web search grounding
	SupportsWebSearch() bool

	// SupportsNativeContinuity returns true if provider has native conversation continuity
	// (like OpenAI's response_id). If false, full history must be passed each time.
	SupportsNativeContinuity() bool

	// SupportsStreaming returns true if provider supports streaming responses
	SupportsStreaming() bool
}

// GenerateParams contains all parameters for generating a reply
type GenerateParams struct {
	// Instructions is the system prompt
	Instructions string

	// UserInput is the user's message
	UserInput string

	// ConversationHistory contains previous messages for context
	ConversationHistory []Message

	// FileStoreID is the vector store or file search store ID
	FileStoreID string

	// PreviousResponseID is for OpenAI conversation continuity
	PreviousResponseID string

	// OverrideModel overrides the default model
	OverrideModel string

	// EnableWebSearch enables web search grounding
	EnableWebSearch bool

	// EnableFileSearch enables RAG with file search
	EnableFileSearch bool

	// FileIDToFilename maps file IDs to original filenames
	FileIDToFilename map[string]string

	// InlineImages contains images to include directly in the prompt
	InlineImages []InlineImage

	// Config contains provider-specific configuration
	Config ProviderConfig

	// RequestID for tracing
	RequestID string

	// ClientID identifies the calling client
	ClientID string
}

// Message represents a conversation message
type Message struct {
	Role      string
	Content   string
	Timestamp time.Time
}

// InlineImage represents an image to include directly in the prompt
type InlineImage struct {
	URI      string
	MIMEType string
	Filename string
}

// ProviderConfig contains provider-specific configuration
type ProviderConfig struct {
	APIKey          string
	Model           string
	Temperature     *float64
	TopP            *float64
	MaxOutputTokens *int
	BaseURL         string
	ExtraOptions    map[string]string
}

// GenerateResult contains the generated reply
type GenerateResult struct {
	// Text is the generated response
	Text string

	// ResponseID is for conversation continuity (OpenAI)
	ResponseID string

	// Usage contains token usage metrics
	Usage *Usage

	// Citations contains source citations
	Citations []Citation

	// Model is the actual model used
	Model string

	// RequestJSON contains the raw API request for debugging
	RequestJSON []byte

	// ResponseJSON contains the raw API response for debugging
	ResponseJSON []byte
}

// Usage contains token usage metrics
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

// Citation represents a source citation
type Citation struct {
	Type       CitationType
	Provider   string
	URL        string
	Title      string
	FileID     string
	Filename   string
	Snippet    string
	StartIndex int
	EndIndex   int
	BrokenLink bool
}

// CitationType indicates the citation source type
type CitationType int

const (
	CitationTypeUnknown CitationType = iota
	CitationTypeURL
	CitationTypeFile
)

// StreamChunk represents a chunk in a streaming response
type StreamChunk struct {
	Type       ChunkType
	Text       string
	Index      int
	Usage      *Usage
	Citation   *Citation
	ResponseID string
	Model      string
	Error      error
	ErrorCode  string
	Retryable  bool
}

// ChunkType indicates the type of stream chunk
type ChunkType int

const (
	ChunkTypeText ChunkType = iota
	ChunkTypeUsage
	ChunkTypeCitation
	ChunkTypeComplete
	ChunkTypeError
)
