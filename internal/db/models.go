package db

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Thread represents a conversation container (tenant isolation is at table level).
type Thread struct {
	ID           uuid.UUID  `json:"id"`
	UserID       string     `json:"user_id"`
	Provider     *string    `json:"provider,omitempty"`
	Model        *string    `json:"model,omitempty"`
	Status       string     `json:"status"`
	MessageCount int        `json:"message_count"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Metadata     *string    `json:"metadata,omitempty"` // JSONB stored as string
}

// ThreadStatus constants
const (
	ThreadStatusActive   = "active"
	ThreadStatusArchived = "archived"
	ThreadStatusDeleted  = "deleted"
)

// Message represents a conversation message (user, assistant, or system).
type Message struct {
	ID               uuid.UUID  `json:"id"`
	ThreadID         uuid.UUID  `json:"thread_id"`
	Role             string     `json:"role"` // user, assistant, system
	Content          string     `json:"content"`
	Provider         *string    `json:"provider,omitempty"`
	Model            *string    `json:"model,omitempty"`
	ResponseID       *string    `json:"response_id,omitempty"` // OpenAI previousResponseID
	InputTokens      *int       `json:"input_tokens,omitempty"`
	OutputTokens     *int       `json:"output_tokens,omitempty"`
	TotalTokens      *int       `json:"total_tokens,omitempty"`
	CostUSD          *float64   `json:"cost_usd,omitempty"`
	GroundingQueries *int       `json:"grounding_queries,omitempty"` // Web search queries for grounding cost
	GroundingCostUSD *float64   `json:"grounding_cost_usd,omitempty"`
	ProcessingTimeMs *int       `json:"processing_time_ms,omitempty"`
	Citations        *string    `json:"citations,omitempty"` // JSONB stored as string
	CreatedAt        time.Time  `json:"created_at"`
	Metadata         *string    `json:"metadata,omitempty"` // JSONB stored as string

	// Debug fields (for request/response inspection)
	SystemPrompt    *string `json:"system_prompt,omitempty"`
	RawRequestJSON  *string `json:"raw_request_json,omitempty"`
	RawResponseJSON *string `json:"raw_response_json,omitempty"`
	RenderedHTML    *string `json:"rendered_html,omitempty"` // HTML from markdown_svc (TOAST-compressed by PostgreSQL)
}

// MessageRole constants
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// ActivityEntry represents a single entry in the activity feed.
// This is the denormalized view for the admin dashboard.
type ActivityEntry struct {
	ID               uuid.UUID `json:"id"`
	ThreadID         uuid.UUID `json:"thread_id"`
	TenantID         string    `json:"tenant"`
	UserID           string    `json:"user_id"`
	Content          string    `json:"content"`
	FullContent      string    `json:"full_content,omitempty"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	InputTokens      int       `json:"input_tokens"`
	OutputTokens     int       `json:"output_tokens"`
	TotalTokens      int       `json:"tokens_used"`
	CostUSD          float64   `json:"cost_usd"`
	GroundingQueries int       `json:"grounding_queries"`
	GroundingCostUSD float64   `json:"grounding_cost_usd"`
	ThreadCostUSD    float64   `json:"thread_cost_usd"`
	ProcessingTimeMs int       `json:"processing_time_ms"`
	Status           string    `json:"status"` // success, failed
	Timestamp        time.Time `json:"timestamp"`
}

// DebugData contains the complete request/response data for a conversation turn.
// Used by the admin dashboard debug inspector modal.
type DebugData struct {
	// Metadata
	MessageID uuid.UUID `json:"message_id"`
	ThreadID  uuid.UUID `json:"thread_id"`
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Timestamp time.Time `json:"timestamp"`

	// Request (what was sent to AI)
	SystemPrompt     string `json:"system_prompt"`
	UserInput        string `json:"user_input"`
	RequestModel     string `json:"request_model"`
	RequestProvider  string `json:"request_provider"`
	RequestTimestamp string `json:"request_timestamp"`

	// Response (what came back from AI)
	ResponseText     string  `json:"response_text"`
	ResponseModel    string  `json:"response_model"`
	TokensIn         int     `json:"tokens_in"`
	TokensOut        int     `json:"tokens_out"`
	CostUSD          float64 `json:"cost_usd"`
	DurationMs       int     `json:"duration_ms"`
	ResponseID       string  `json:"response_id,omitempty"`
	Citations        string  `json:"citations,omitempty"`

	// Raw HTTP payloads (for JSON view)
	RawRequestJSON  string `json:"raw_request_json,omitempty"`
	RawResponseJSON string `json:"raw_response_json,omitempty"`

	// Rendered HTML (from markdown_svc)
	RenderedHTML string `json:"rendered_html,omitempty"`

	// Status
	Status string `json:"status"` // success, failed
	Error  string `json:"error,omitempty"`
}

// Citation represents a web or file search citation.
type Citation struct {
	Type     string `json:"type"` // url, file
	URL      string `json:"url,omitempty"`
	Title    string `json:"title,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	Filename string `json:"filename,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
}

// ParseCitations parses JSONB citations string into Citation slice.
func ParseCitations(citationsJSON *string) ([]Citation, error) {
	if citationsJSON == nil || *citationsJSON == "" {
		return nil, nil
	}
	var citations []Citation
	if err := json.Unmarshal([]byte(*citationsJSON), &citations); err != nil {
		return nil, err
	}
	return citations, nil
}

// CitationsToJSON converts Citation slice to JSONB string.
func CitationsToJSON(citations []Citation) (*string, error) {
	if len(citations) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(citations)
	if err != nil {
		return nil, err
	}
	s := string(data)
	return &s, nil
}

// NewThread creates a new thread with default values.
// Tenant isolation is at the table level, not row level.
func NewThread(userID string) *Thread {
	now := time.Now()
	return &Thread{
		ID:           uuid.New(),
		UserID:       userID,
		Status:       ThreadStatusActive,
		MessageCount: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// NewMessage creates a new message.
func NewMessage(threadID uuid.UUID, role, content string) *Message {
	return &Message{
		ID:        uuid.New(),
		ThreadID:  threadID,
		Role:      role,
		Content:   content,
		CreatedAt: time.Now(),
	}
}

// SetAssistantMetrics sets provider metrics on an assistant message.
func (m *Message) SetAssistantMetrics(provider, model string, inputTokens, outputTokens, processingTimeMs int, costUSD float64, responseID string) {
	m.Provider = &provider
	m.Model = &model
	m.InputTokens = &inputTokens
	m.OutputTokens = &outputTokens
	total := inputTokens + outputTokens
	m.TotalTokens = &total
	m.CostUSD = &costUSD
	m.ProcessingTimeMs = &processingTimeMs
	if responseID != "" {
		m.ResponseID = &responseID
	}
}

// TruncateContent returns truncated content for preview display.
func (m *Message) TruncateContent(maxLen int) string {
	if len(m.Content) <= maxLen {
		return m.Content
	}
	return m.Content[:maxLen] + "..."
}

// ConversationMessage represents a message in the conversation view.
// This is a simplified view for the chat display.
type ConversationMessage struct {
	ID           uuid.UUID `json:"id"`
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	RenderedHTML string    `json:"rendered_html,omitempty"`
	Model        string    `json:"model,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// ThreadConversation contains full thread data with all messages.
type ThreadConversation struct {
	ThreadID     uuid.UUID             `json:"thread_id"`
	TenantID     string                `json:"tenant_id"`
	UserID       string                `json:"user_id"`
	Provider     string                `json:"provider,omitempty"`
	Model        string                `json:"model,omitempty"`
	MessageCount int                   `json:"message_count"`
	Messages     []ConversationMessage `json:"messages"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
}

// File represents an uploaded file for RAG and attachments.
type File struct {
	ID        uuid.UUID  `json:"id"`
	UserID    string     `json:"user_id"`
	Filename  string     `json:"filename"`
	MimeType  *string    `json:"mime_type,omitempty"`
	SizeBytes *int64     `json:"size_bytes,omitempty"`
	StoreID   *string    `json:"store_id,omitempty"`   // Vector store ID for RAG
	FileID    *string    `json:"file_id,omitempty"`    // Provider file ID
	Provider  *string    `json:"provider,omitempty"`   // Provider that owns the file
	Status    string     `json:"status"`               // uploaded, processing, ready, failed
	CreatedAt time.Time  `json:"created_at"`
	Metadata  *string    `json:"metadata,omitempty"`   // JSONB stored as string
}

// FileStatus constants
const (
	FileStatusUploaded   = "uploaded"
	FileStatusProcessing = "processing"
	FileStatusReady      = "ready"
	FileStatusFailed     = "failed"
)

// FileProviderUpload tracks file uploads to different AI providers.
type FileProviderUpload struct {
	ID              uuid.UUID  `json:"id"`
	FileID          uuid.UUID  `json:"file_id"`
	Provider        string     `json:"provider"`           // openai, gemini, etc.
	ProviderFileID  *string    `json:"provider_file_id,omitempty"`
	ProviderStoreID *string    `json:"provider_store_id,omitempty"`
	Status          string     `json:"status"`             // pending, uploading, ready, failed
	CreatedAt       time.Time  `json:"created_at"`
	UploadedAt      *time.Time `json:"uploaded_at,omitempty"`
}

// UploadStatus constants
const (
	UploadStatusPending   = "pending"
	UploadStatusUploading = "uploading"
	UploadStatusReady     = "ready"
	UploadStatusFailed    = "failed"
)

// ThreadVectorStore links threads to vector stores for RAG.
type ThreadVectorStore struct {
	ID        uuid.UUID `json:"id"`
	ThreadID  uuid.UUID `json:"thread_id"`
	StoreID   string    `json:"store_id"`
	Provider  string    `json:"provider"`   // openai, qdrant, etc.
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}
