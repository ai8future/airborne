package db

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MessageRole constants. These role strings are shared by the relational
// ChatMessage.Role column (valid_role CHECK) and by admin/service callers.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
	RoleTool      = "tool"
)

// ============================================================================
// Relational chat/chat_message models (migrations/001_baseline.sql).
//
// Chat is the conversation container (airborne_chats); ChatMessage is the
// sole source of truth for conversation content and forms a branchable tree
// via ParentID (airborne_chat_messages). Model is the per-tenant model
// registry row (airborne_models). These are additive alongside the legacy
// Thread/Message structs above, which repository.go still depends on until
// Task 5's rewrite.
// ============================================================================

// ChatStatus constants (airborne_chats.status CHECK).
const (
	ChatStatusActive   = "active"
	ChatStatusArchived = "archived"
	ChatStatusDeleted  = "deleted"
)

// ChatMessageStatus constants (airborne_chat_messages.status CHECK).
const (
	ChatMessageStatusPending   = "pending"
	ChatMessageStatusStreaming = "streaming"
	ChatMessageStatusComplete  = "complete"
	ChatMessageStatusError     = "error"
)

// Chat represents a conversation container. Tenant isolation is enforced at
// the row level via RLS (see migrations/001_baseline.sql), keyed by TenantID.
type Chat struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	UserID           string          `json:"user_id"`
	Title            string          `json:"title,omitempty"`
	ModelID          string          `json:"model_id,omitempty"` // last-used model
	Provider         string          `json:"provider,omitempty"` // last-used provider
	Status           string          `json:"status"`
	CurrentMessageID *string         `json:"current_message_id,omitempty"` // head of the active branch (leaf)
	Pinned           bool            `json:"pinned"`
	ExternalRef      string          `json:"external_ref,omitempty"` // opaque caller correlation id (addendum A9); empty = not set
	Metadata         json.RawMessage `json:"metadata,omitempty"`     // nullable JSONB; nil is fine (column has no NOT NULL constraint)
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// ChatMessage represents a single node in a chat's branchable message tree.
// It is the sole source of truth for conversation content
// (airborne_chat_messages) — sibling branches share a ParentID and are
// ordered by (SiblingSeq, CreatedAt, ID).
type ChatMessage struct {
	ID       string  `json:"id"`
	TenantID string  `json:"tenant_id"`
	ChatID   string  `json:"chat_id"`
	ParentID *string `json:"parent_id,omitempty"` // nil = root message (tree edge; omitted, not null, when unset)

	SiblingSeq int    `json:"sibling_seq"`
	UserID     string `json:"user_id"`
	Role       string `json:"role"` // user, assistant, system, tool

	// Content is NOT NULL in the schema (string or list of content blocks).
	// Always populate via TextContent(s) for plain-text legacy callers, or
	// with a pre-built content-block JSON value — never leave this nil.
	Content json.RawMessage `json:"content"`

	ModelID    *string `json:"model_id,omitempty"`
	Provider   *string `json:"provider,omitempty"`
	ResponseID *string `json:"response_id,omitempty"` // provider continuity id

	Status        string          `json:"status"`
	StatusHistory json.RawMessage `json:"status_history,omitempty"` // ordered generation-event timeline; nullable

	InputTokens      *int     `json:"input_tokens,omitempty"`
	OutputTokens     *int     `json:"output_tokens,omitempty"`
	TotalTokens      *int     `json:"total_tokens,omitempty"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
	GroundingQueries *int     `json:"grounding_queries,omitempty"`
	GroundingCostUSD *float64 `json:"grounding_cost_usd,omitempty"`
	ProcessingTimeMs *int     `json:"processing_time_ms,omitempty"`

	Usage   json.RawMessage `json:"usage,omitempty"`   // raw provider usage (catch-all)
	Sources json.RawMessage `json:"sources,omitempty"` // citations: [{source:{id,name,type}, document:[...], metadata:[...]}]
	Embeds  json.RawMessage `json:"embeds,omitempty"`  // inline artifacts/media rendered in the message

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Model is a per-tenant model-registry row (airborne_models): named
// presets/aliases with default params and soft-disable, per Open WebUI's
// base_model_id + params + is_active pattern.
type Model struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`

	BaseModelID *string `json:"base_model_id,omitempty"` // upstream real model for an alias; nil = itself
	Name        *string `json:"name,omitempty"`
	Provider    *string `json:"provider,omitempty"`

	// Params and Meta are NOT NULL in the schema. Route both through
	// NormalizeJSONB before an INSERT/UPDATE so a zero-value json.RawMessage
	// (nil) never marshals to SQL NULL — a column DEFAULT does not apply
	// when NULL is explicitly passed.
	Params json.RawMessage `json:"params"`
	Meta   json.RawMessage `json:"meta"`

	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TextContent wraps a plain string as the JSON content form stored in
// airborne_chat_messages.content, e.g. TextContent("hi") -> {"text":"hi"}.
// content is NOT NULL, so callers with a legacy plain-string message body
// must go through this (or an equivalent non-nil content-block value) —
// never assign a nil json.RawMessage to ChatMessage.Content.
func TextContent(s string) json.RawMessage {
	data, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: s})
	if err != nil {
		// json.Marshal of a plain string field cannot fail in practice
		// (invalid UTF-8 is replaced, not rejected); this is a defensive
		// fallback so we still never return nil for a NOT NULL column.
		return json.RawMessage(`{"text":""}`)
	}
	return json.RawMessage(data)
}

// NormalizeJSONB returns b unchanged if it holds any bytes, otherwise a
// non-nil empty JSON object. Use this to guard NOT NULL JSONB columns
// (airborne_models.params, airborne_models.meta) before INSERT/UPDATE: a nil
// or zero-length json.RawMessage marshals to SQL NULL, and a column DEFAULT
// does not apply when NULL is explicitly passed in the statement.
func NormalizeJSONB(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	return b
}

// ActivityEntry represents a single entry in the activity feed.
// This is the denormalized view for the admin dashboard.
//
// NOTE: ChatID and ModelID are renamed from the pre-Task-4 ThreadID/Model
// Go field names to match the new Chat/ChatMessage model naming, but every
// json tag is preserved byte-for-byte (ChatID keeps `json:"thread_id"`,
// ModelID keeps `json:"model"`) — the admin dashboard's JSON wire contract
// does not change.
type ActivityEntry struct {
	ID               uuid.UUID `json:"id"`
	ChatID           uuid.UUID `json:"thread_id"`
	TenantID         string    `json:"tenant"`
	UserID           string    `json:"user_id"`
	Content          string    `json:"content"`
	FullContent      string    `json:"full_content,omitempty"`
	Provider         string    `json:"provider"`
	ModelID          string    `json:"model"`
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
//
// NOTE: ChatID is renamed from the pre-Task-4 ThreadID Go field name; the
// json tag is preserved byte-for-byte (`json:"thread_id"`) — the debug
// inspector modal's JSON wire contract does not change. This struct still
// mixes fields sourced from airborne_chat_messages (tokens, cost, etc.) and
// airborne_chat_message_debug (system_prompt, raw_request_json,
// raw_response_json, rendered_html) — Tasks 5/7 join both to populate it.
type DebugData struct {
	// Metadata
	MessageID uuid.UUID `json:"message_id"`
	ChatID    uuid.UUID `json:"thread_id"`
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
	GroundingQueries int     `json:"grounding_queries"`
	GroundingCostUSD float64 `json:"grounding_cost_usd"`
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

// DebugInfo carries the cold request/response debug blob a caller wants stored
// alongside a message (persisted via Repository.SaveMessageDebug into
// airborne_chat_message_debug).
type DebugInfo struct {
	SystemPrompt    string
	RawRequestJSON  string
	RawResponseJSON string
	RenderedHTML    string
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
//
// NOTE: ChatID and ModelID are renamed from the pre-Task-4 ThreadID/Model Go
// field names; every json tag is preserved byte-for-byte (ChatID keeps
// `json:"thread_id"`, ModelID keeps `json:"model,omitempty"`) — the
// conversation view's JSON wire contract does not change.
type ThreadConversation struct {
	ChatID       uuid.UUID             `json:"thread_id"`
	TenantID     string                `json:"tenant_id"`
	UserID       string                `json:"user_id"`
	Provider     string                `json:"provider,omitempty"`
	ModelID      string                `json:"model,omitempty"`
	MessageCount int                   `json:"message_count"`
	Messages     []ConversationMessage `json:"messages"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
}

// File represents an uploaded file for RAG and attachments.
type File struct {
	ID        uuid.UUID `json:"id"`
	UserID    string    `json:"user_id"`
	Filename  string    `json:"filename"`
	MimeType  *string   `json:"mime_type,omitempty"`
	SizeBytes *int64    `json:"size_bytes,omitempty"`
	StoreID   *string   `json:"store_id,omitempty"` // Vector store ID for RAG
	FileID    *string   `json:"file_id,omitempty"`  // Provider file ID
	Provider  *string   `json:"provider,omitempty"` // Provider that owns the file
	Status    string    `json:"status"`             // uploaded, processing, ready, failed
	Hash      *string   `json:"hash,omitempty"`     // content hash for dedup (OWUI file.hash)
	CreatedAt time.Time `json:"created_at"`
	Metadata  *string   `json:"metadata,omitempty"` // JSONB stored as string
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
	Provider        string     `json:"provider"` // openai, gemini, etc.
	ProviderFileID  *string    `json:"provider_file_id,omitempty"`
	ProviderStoreID *string    `json:"provider_store_id,omitempty"`
	Status          string     `json:"status"` // pending, uploading, ready, failed
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
	Provider  string    `json:"provider"` // openai, qdrant, etc.
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}
