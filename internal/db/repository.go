package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ValidTenantIDs contains the list of valid tenant IDs.
var ValidTenantIDs = map[string]bool{
	"ai8":      true,
	"email4ai": true,
	"zztest":   true,
}

// ErrInvalidTenant is returned when an invalid tenant ID is provided.
var ErrInvalidTenant = errors.New("invalid tenant ID: must be 'ai8', 'email4ai', or 'zztest'")

// Repository provides data access operations for threads and messages.
// Each repository instance is scoped to a specific tenant's tables.
type Repository struct {
	client      *Client
	tablePrefix string // "ai8_airborne" or "email4ai_airborne"
	tenantID    string // "ai8", "email4ai", "zztest"
}

// NewRepository creates a new repository backed by the given client.
// Deprecated: Use NewTenantRepository for tenant-specific operations.
func NewRepository(client *Client) *Repository {
	return &Repository{client: client, tablePrefix: "", tenantID: ""}
}

// NewTenantRepository creates a new repository scoped to a specific tenant's tables.
// Returns an error if the tenantID is not valid.
func NewTenantRepository(client *Client, tenantID string) (*Repository, error) {
	if !ValidTenantIDs[tenantID] {
		return nil, fmt.Errorf("%w: got %q", ErrInvalidTenant, tenantID)
	}
	return &Repository{
		client:      client,
		tablePrefix: tenantID + "_airborne",
		tenantID:    tenantID,
	}, nil
}

// TenantID returns the tenant ID this repository is scoped to.
func (r *Repository) TenantID() string {
	return r.tenantID
}

// threadsTable returns the tenant-specific threads table name.
func (r *Repository) threadsTable() string {
	if r.tablePrefix == "" {
		return "airborne_threads" // Legacy table
	}
	return r.tablePrefix + "_threads"
}

// messagesTable returns the tenant-specific messages table name.
func (r *Repository) messagesTable() string {
	if r.tablePrefix == "" {
		return "airborne_messages" // Legacy table
	}
	return r.tablePrefix + "_messages"
}

// filesTable returns the tenant-specific files table name.
func (r *Repository) filesTable() string {
	if r.tablePrefix == "" {
		return "airborne_files" // Legacy table
	}
	return r.tablePrefix + "_files"
}

// fileUploadsTable returns the tenant-specific file uploads table name.
func (r *Repository) fileUploadsTable() string {
	if r.tablePrefix == "" {
		return "airborne_file_provider_uploads" // Legacy table
	}
	return r.tablePrefix + "_file_provider_uploads"
}

// vectorStoresTable returns the tenant-specific vector stores table name.
func (r *Repository) vectorStoresTable() string {
	if r.tablePrefix == "" {
		return "airborne_thread_vector_stores" // Legacy table
	}
	return r.tablePrefix + "_thread_vector_stores"
}

// CreateThread inserts a new thread into the database.
func (r *Repository) CreateThread(ctx context.Context, thread *Thread) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (id, user_id, provider, model, status, message_count, created_at, updated_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, r.threadsTable())
	r.client.logQuery(query, thread.ID, thread.UserID)

	_, err := r.client.pool.Exec(ctx, query,
		thread.ID,
		thread.UserID,
		thread.Provider,
		thread.Model,
		thread.Status,
		thread.MessageCount,
		thread.CreatedAt,
		thread.UpdatedAt,
		thread.Metadata,
	)
	if err != nil {
		return fmt.Errorf("failed to create thread: %w", err)
	}
	return nil
}

// GetThread retrieves a thread by ID.
func (r *Repository) GetThread(ctx context.Context, id uuid.UUID) (*Thread, error) {
	query := fmt.Sprintf(`
		SELECT id, user_id, provider, model, status, message_count, created_at, updated_at, metadata
		FROM %s
		WHERE id = $1
	`, r.threadsTable())
	r.client.logQuery(query, id)

	var thread Thread
	err := r.client.pool.QueryRow(ctx, query, id).Scan(
		&thread.ID,
		&thread.UserID,
		&thread.Provider,
		&thread.Model,
		&thread.Status,
		&thread.MessageCount,
		&thread.CreatedAt,
		&thread.UpdatedAt,
		&thread.Metadata,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get thread: %w", err)
	}
	return &thread, nil
}

// UpdateThreadProvider updates the last-used provider and model for a thread.
func (r *Repository) UpdateThreadProvider(ctx context.Context, threadID uuid.UUID, provider, model string) error {
	query := fmt.Sprintf(`
		UPDATE %s
		SET provider = $2, model = $3, updated_at = NOW()
		WHERE id = $1
	`, r.threadsTable())
	r.client.logQuery(query, threadID, provider, model)

	_, err := r.client.pool.Exec(ctx, query, threadID, provider, model)
	if err != nil {
		return fmt.Errorf("failed to update thread provider: %w", err)
	}
	return nil
}

// CreateMessage inserts a new message into the database.
func (r *Repository) CreateMessage(ctx context.Context, msg *Message) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (
			id, thread_id, role, content, provider, model, response_id,
			input_tokens, output_tokens, total_tokens, cost_usd,
			processing_time_ms, citations, created_at, metadata,
			system_prompt, raw_request_json, raw_response_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`, r.messagesTable())
	r.client.logQuery(query, msg.ID, msg.ThreadID, msg.Role)

	_, err := r.client.pool.Exec(ctx, query,
		msg.ID,
		msg.ThreadID,
		msg.Role,
		msg.Content,
		msg.Provider,
		msg.Model,
		msg.ResponseID,
		msg.InputTokens,
		msg.OutputTokens,
		msg.TotalTokens,
		msg.CostUSD,
		msg.ProcessingTimeMs,
		msg.Citations,
		msg.CreatedAt,
		msg.Metadata,
		msg.SystemPrompt,
		msg.RawRequestJSON,
		msg.RawResponseJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}
	return nil
}

// GetMessages retrieves messages for a thread, ordered chronologically.
func (r *Repository) GetMessages(ctx context.Context, threadID uuid.UUID, limit int) ([]Message, error) {
	query := fmt.Sprintf(`
		SELECT id, thread_id, role, content, provider, model, response_id,
		       input_tokens, output_tokens, total_tokens, cost_usd,
		       processing_time_ms, citations, created_at, metadata
		FROM %s
		WHERE thread_id = $1
		ORDER BY created_at ASC
		LIMIT $2
	`, r.messagesTable())
	r.client.logQuery(query, threadID, limit)

	rows, err := r.client.pool.Query(ctx, query, threadID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		err := rows.Scan(
			&msg.ID,
			&msg.ThreadID,
			&msg.Role,
			&msg.Content,
			&msg.Provider,
			&msg.Model,
			&msg.ResponseID,
			&msg.InputTokens,
			&msg.OutputTokens,
			&msg.TotalTokens,
			&msg.CostUSD,
			&msg.ProcessingTimeMs,
			&msg.Citations,
			&msg.CreatedAt,
			&msg.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// GetActivityFeed retrieves the latest assistant messages for the activity dashboard.
// This queries the tenant-specific tables.
func (r *Repository) GetActivityFeed(ctx context.Context, limit int) ([]ActivityEntry, error) {
	query := fmt.Sprintf(`
		SELECT
			m.id,
			m.thread_id,
			t.user_id,
			m.content,
			COALESCE(m.provider, '') as provider,
			COALESCE(m.model, '') as model,
			COALESCE(m.input_tokens, 0) as input_tokens,
			COALESCE(m.output_tokens, 0) as output_tokens,
			COALESCE(m.total_tokens, 0) as total_tokens,
			COALESCE(m.cost_usd, 0) as cost_usd,
			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
			m.created_at,
			(
				SELECT COALESCE(SUM(cost_usd), 0)
				FROM %s
				WHERE thread_id = m.thread_id
			) AS thread_cost_usd
		FROM %s m
		JOIN %s t ON m.thread_id = t.id
		WHERE m.role = 'assistant'
		ORDER BY m.created_at DESC
		LIMIT $1
	`, r.messagesTable(), r.messagesTable(), r.threadsTable())
	r.client.logQuery(query, limit)

	rows, err := r.client.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get activity feed: %w", err)
	}
	defer rows.Close()

	var entries []ActivityEntry
	for rows.Next() {
		var entry ActivityEntry
		err := rows.Scan(
			&entry.ID,
			&entry.ThreadID,
			&entry.UserID,
			&entry.Content,
			&entry.Provider,
			&entry.Model,
			&entry.InputTokens,
			&entry.OutputTokens,
			&entry.TotalTokens,
			&entry.CostUSD,
			&entry.ProcessingTimeMs,
			&entry.Timestamp,
			&entry.ThreadCostUSD,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan activity entry: %w", err)
		}
		// Set tenant ID from repository context
		entry.TenantID = r.tenantID
		// Set status based on provider presence (if we got a response, it's success)
		entry.Status = "success"
		// Truncate content for preview, keep full content
		entry.FullContent = entry.Content
		if len(entry.Content) > 100 {
			entry.Content = entry.Content[:100] + "..."
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// GetActivityFeedAllTenants retrieves activity from all tenant tables combined.
// This is used by the admin dashboard to show a unified activity feed.
func (r *Repository) GetActivityFeedAllTenants(ctx context.Context, limit int) ([]ActivityEntry, error) {
	query := `
		SELECT
			m.id,
			m.thread_id,
			'ai8' as tenant_id,
			t.user_id,
			m.content,
			COALESCE(m.provider, '') as provider,
			COALESCE(m.model, '') as model,
			COALESCE(m.input_tokens, 0) as input_tokens,
			COALESCE(m.output_tokens, 0) as output_tokens,
			COALESCE(m.total_tokens, 0) as total_tokens,
			COALESCE(m.cost_usd, 0) as cost_usd,
			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
			m.created_at,
			(
				SELECT COALESCE(SUM(cost_usd), 0)
				FROM ai8_airborne_messages
				WHERE thread_id = m.thread_id
			) AS thread_cost_usd
		FROM ai8_airborne_messages m
		JOIN ai8_airborne_threads t ON m.thread_id = t.id
		WHERE m.role = 'assistant'

		UNION ALL

		SELECT
			m.id,
			m.thread_id,
			'email4ai' as tenant_id,
			t.user_id,
			m.content,
			COALESCE(m.provider, '') as provider,
			COALESCE(m.model, '') as model,
			COALESCE(m.input_tokens, 0) as input_tokens,
			COALESCE(m.output_tokens, 0) as output_tokens,
			COALESCE(m.total_tokens, 0) as total_tokens,
			COALESCE(m.cost_usd, 0) as cost_usd,
			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
			m.created_at,
			(
				SELECT COALESCE(SUM(cost_usd), 0)
				FROM email4ai_airborne_messages
				WHERE thread_id = m.thread_id
			) AS thread_cost_usd
		FROM email4ai_airborne_messages m
		JOIN email4ai_airborne_threads t ON m.thread_id = t.id
		WHERE m.role = 'assistant'

		UNION ALL

		SELECT
			m.id,
			m.thread_id,
			'zztest' as tenant_id,
			t.user_id,
			m.content,
			COALESCE(m.provider, '') as provider,
			COALESCE(m.model, '') as model,
			COALESCE(m.input_tokens, 0) as input_tokens,
			COALESCE(m.output_tokens, 0) as output_tokens,
			COALESCE(m.total_tokens, 0) as total_tokens,
			COALESCE(m.cost_usd, 0) as cost_usd,
			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
			m.created_at,
			(
				SELECT COALESCE(SUM(cost_usd), 0)
				FROM zztest_airborne_messages
				WHERE thread_id = m.thread_id
			) AS thread_cost_usd
		FROM zztest_airborne_messages m
		JOIN zztest_airborne_threads t ON m.thread_id = t.id
		WHERE m.role = 'assistant'

		ORDER BY created_at DESC
		LIMIT $1
	`
	r.client.logQuery(query, limit)

	rows, err := r.client.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get activity feed (all tenants): %w", err)
	}
	defer rows.Close()

	var entries []ActivityEntry
	for rows.Next() {
		var entry ActivityEntry
		err := rows.Scan(
			&entry.ID,
			&entry.ThreadID,
			&entry.TenantID,
			&entry.UserID,
			&entry.Content,
			&entry.Provider,
			&entry.Model,
			&entry.InputTokens,
			&entry.OutputTokens,
			&entry.TotalTokens,
			&entry.CostUSD,
			&entry.ProcessingTimeMs,
			&entry.Timestamp,
			&entry.ThreadCostUSD,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan activity entry: %w", err)
		}
		entry.Status = "success"
		entry.FullContent = entry.Content
		if len(entry.Content) > 100 {
			entry.Content = entry.Content[:100] + "..."
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// GetActivityFeedByTenant retrieves activity for a specific tenant.
// This creates a tenant-specific repository and queries that tenant's tables.
func (r *Repository) GetActivityFeedByTenant(ctx context.Context, tenantID string, limit int) ([]ActivityEntry, error) {
	// Validate tenant ID
	if !ValidTenantIDs[tenantID] {
		return nil, fmt.Errorf("%w: got %q", ErrInvalidTenant, tenantID)
	}

	// Create a tenant-specific repository
	tenantRepo, err := NewTenantRepository(r.client, tenantID)
	if err != nil {
		return nil, err
	}

	return tenantRepo.GetActivityFeed(ctx, limit)
}

// DebugInfo contains debug data to store alongside messages.
type DebugInfo struct {
	SystemPrompt    string
	RawRequestJSON  string
	RawResponseJSON string
	RenderedHTML    string
}

// PersistConversationTurn saves both user and assistant messages in a transaction.
// This is the main entry point for chat service persistence.
// Note: tenantID parameter is no longer needed - the repository is already scoped to a tenant.
func (r *Repository) PersistConversationTurn(ctx context.Context, threadID uuid.UUID, userID string, userContent, assistantContent, provider, model, responseID string, inputTokens, outputTokens, processingTimeMs int, costUSD float64) error {
	return r.PersistConversationTurnWithDebug(ctx, threadID, userID, userContent, assistantContent, provider, model, responseID, inputTokens, outputTokens, processingTimeMs, costUSD, nil, nil)
}

// PersistConversationTurnWithDebug saves both user and assistant messages with optional debug data and citations.
func (r *Repository) PersistConversationTurnWithDebug(ctx context.Context, threadID uuid.UUID, userID string, userContent, assistantContent, provider, model, responseID string, inputTokens, outputTokens, processingTimeMs int, costUSD float64, debug *DebugInfo, citations []Citation) error {
	tx, err := r.client.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Check if thread exists, create if not
	var threadExists bool
	checkQuery := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1)", r.threadsTable())
	err = tx.QueryRow(ctx, checkQuery, threadID).Scan(&threadExists)
	if err != nil {
		return fmt.Errorf("failed to check thread existence: %w", err)
	}

	if !threadExists {
		// Create new thread (no tenant_id column needed - table is tenant-specific)
		createQuery := fmt.Sprintf(`
			INSERT INTO %s (id, user_id, provider, model, status, message_count, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'active', 0, NOW(), NOW())
		`, r.threadsTable())
		_, err = tx.Exec(ctx, createQuery, threadID, userID, provider, model)
		if err != nil {
			return fmt.Errorf("failed to create thread: %w", err)
		}
		slog.Debug("created new thread", "thread_id", threadID, "tenant", r.tenantID)
	}

	// Insert user message
	userMsgID := uuid.New()
	userInsertQuery := fmt.Sprintf(`
		INSERT INTO %s (id, thread_id, role, content, created_at)
		VALUES ($1, $2, 'user', $3, NOW())
	`, r.messagesTable())
	_, err = tx.Exec(ctx, userInsertQuery, userMsgID, threadID, userContent)
	if err != nil {
		return fmt.Errorf("failed to insert user message: %w", err)
	}

	// Insert assistant message with full metrics and optional debug data
	assistantMsgID := uuid.New()
	totalTokens := inputTokens + outputTokens

	var systemPrompt, rawReqJSON, rawRespJSON, renderedHTML *string
	if debug != nil {
		if debug.SystemPrompt != "" {
			systemPrompt = &debug.SystemPrompt
		}
		if debug.RawRequestJSON != "" {
			rawReqJSON = &debug.RawRequestJSON
		}
		if debug.RawResponseJSON != "" {
			rawRespJSON = &debug.RawResponseJSON
		}
		if debug.RenderedHTML != "" {
			renderedHTML = &debug.RenderedHTML
		}
	}

	// Serialize citations to JSON
	citationsJSON, err := CitationsToJSON(citations)
	if err != nil {
		slog.Warn("failed to serialize citations", "error", err)
		// Continue without citations rather than failing the entire persist
	}

	assistantInsertQuery := fmt.Sprintf(`
		INSERT INTO %s (
			id, thread_id, role, content, provider, model, response_id,
			input_tokens, output_tokens, total_tokens, cost_usd, processing_time_ms, created_at,
			system_prompt, raw_request_json, raw_response_json, rendered_html, citations
		) VALUES ($1, $2, 'assistant', $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), $12, $13, $14, $15, $16)
	`, r.messagesTable())
	_, err = tx.Exec(ctx, assistantInsertQuery, assistantMsgID, threadID, assistantContent, provider, model, responseID,
		inputTokens, outputTokens, totalTokens, costUSD, processingTimeMs,
		systemPrompt, rawReqJSON, rawRespJSON, renderedHTML, citationsJSON)
	if err != nil {
		return fmt.Errorf("failed to insert assistant message: %w", err)
	}

	// Update thread's last-used provider (trigger updates message_count and updated_at)
	updateQuery := fmt.Sprintf(`
		UPDATE %s
		SET provider = $2, model = $3, updated_at = NOW()
		WHERE id = $1
	`, r.threadsTable())
	_, err = tx.Exec(ctx, updateQuery, threadID, provider, model)
	if err != nil {
		return fmt.Errorf("failed to update thread provider: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.Debug("persisted conversation turn",
		"thread_id", threadID,
		"tenant", r.tenantID,
		"provider", provider,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"cost_usd", costUSD,
	)
	return nil
}

// GetDebugData retrieves the full request/response debug data for a message.
func (r *Repository) GetDebugData(ctx context.Context, messageID uuid.UUID) (*DebugData, error) {
	query := fmt.Sprintf(`
		SELECT
			m.id,
			m.thread_id,
			t.user_id,
			m.created_at,
			COALESCE(m.system_prompt, '') as system_prompt,
			COALESCE(m.provider, '') as provider,
			COALESCE(m.model, '') as model,
			m.content as response_text,
			COALESCE(m.input_tokens, 0) as tokens_in,
			COALESCE(m.output_tokens, 0) as tokens_out,
			COALESCE(m.cost_usd, 0) as cost_usd,
			COALESCE(m.processing_time_ms, 0) as duration_ms,
			COALESCE(m.response_id, '') as response_id,
			COALESCE(m.citations::text, '') as citations,
			COALESCE(m.raw_request_json::text, '') as raw_request_json,
			COALESCE(m.raw_response_json::text, '') as raw_response_json,
			COALESCE(m.rendered_html, '') as rendered_html,
			(
				SELECT COALESCE(content, '')
				FROM %s
				WHERE thread_id = m.thread_id
					AND role = 'user'
					AND created_at <= m.created_at
				ORDER BY created_at DESC
				LIMIT 1
			) as user_input
		FROM %s m
		JOIN %s t ON m.thread_id = t.id
		WHERE m.id = $1 AND m.role = 'assistant'
	`, r.messagesTable(), r.messagesTable(), r.threadsTable())
	r.client.logQuery(query, messageID)

	var data DebugData
	var userInput *string
	err := r.client.pool.QueryRow(ctx, query, messageID).Scan(
		&data.MessageID,
		&data.ThreadID,
		&data.UserID,
		&data.Timestamp,
		&data.SystemPrompt,
		&data.RequestProvider,
		&data.ResponseModel,
		&data.ResponseText,
		&data.TokensIn,
		&data.TokensOut,
		&data.CostUSD,
		&data.DurationMs,
		&data.ResponseID,
		&data.Citations,
		&data.RawRequestJSON,
		&data.RawResponseJSON,
		&data.RenderedHTML,
		&userInput,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("message not found")
		}
		return nil, fmt.Errorf("failed to get debug data: %w", err)
	}

	// Set tenant ID from repository context
	data.TenantID = r.tenantID

	// Set derived fields
	data.RequestModel = data.ResponseModel // Model requested = model used for now
	data.RequestTimestamp = data.Timestamp.Format("2006-01-02T15:04:05Z07:00")
	data.Status = "success"
	if userInput != nil {
		data.UserInput = *userInput
	}

	return &data, nil
}

// GetDebugDataAllTenants searches for debug data across all tenant tables.
// Used by admin dashboard when the tenant is unknown.
func (r *Repository) GetDebugDataAllTenants(ctx context.Context, messageID uuid.UUID) (*DebugData, error) {
	// Try each tenant in order
	for _, tenantID := range []string{"ai8", "email4ai", "zztest"} {
		repo, err := NewTenantRepository(r.client, tenantID)
		if err != nil {
			continue
		}
		data, err := repo.GetDebugData(ctx, messageID)
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("message not found in any tenant")
}

// GetOrCreateThread ensures a thread exists for the given user.
func (r *Repository) GetOrCreateThread(ctx context.Context, threadID uuid.UUID, userID string) (*Thread, error) {
	// Try to get existing thread
	thread, err := r.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if thread != nil {
		return thread, nil
	}

	// Create new thread
	thread = NewThread(userID)
	thread.ID = threadID // Use the provided ID
	if err := r.CreateThread(ctx, thread); err != nil {
		return nil, err
	}
	return thread, nil
}

// GetThreadConversation retrieves complete thread data with all messages for conversation view.
func (r *Repository) GetThreadConversation(ctx context.Context, threadID uuid.UUID) (*ThreadConversation, error) {
	// First get thread info
	threadQuery := fmt.Sprintf(`
		SELECT id, user_id, COALESCE(provider, '') as provider, COALESCE(model, '') as model,
		       message_count, created_at, updated_at
		FROM %s
		WHERE id = $1
	`, r.threadsTable())
	r.client.logQuery(threadQuery, threadID)

	var conv ThreadConversation
	err := r.client.pool.QueryRow(ctx, threadQuery, threadID).Scan(
		&conv.ThreadID,
		&conv.UserID,
		&conv.Provider,
		&conv.Model,
		&conv.MessageCount,
		&conv.CreatedAt,
		&conv.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("thread not found")
		}
		return nil, fmt.Errorf("failed to get thread: %w", err)
	}

	// Set tenant ID from repository context
	conv.TenantID = r.tenantID

	// Get all messages in chronological order
	messagesQuery := fmt.Sprintf(`
		SELECT id, role, content, COALESCE(rendered_html, '') as rendered_html,
		       COALESCE(model, '') as model, COALESCE(provider, '') as provider, created_at
		FROM %s
		WHERE thread_id = $1
		ORDER BY created_at ASC
	`, r.messagesTable())
	r.client.logQuery(messagesQuery, threadID)

	rows, err := r.client.pool.Query(ctx, messagesQuery, threadID)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msg ConversationMessage
		err := rows.Scan(
			&msg.ID,
			&msg.Role,
			&msg.Content,
			&msg.RenderedHTML,
			&msg.Model,
			&msg.Provider,
			&msg.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		conv.Messages = append(conv.Messages, msg)
	}

	return &conv, nil
}

// GetThreadConversationAllTenants searches for a thread conversation across all tenant tables.
// Used by admin dashboard when the tenant is unknown.
func (r *Repository) GetThreadConversationAllTenants(ctx context.Context, threadID uuid.UUID) (*ThreadConversation, error) {
	// Try each tenant in order
	for _, tenantID := range []string{"ai8", "email4ai", "zztest"} {
		repo, err := NewTenantRepository(r.client, tenantID)
		if err != nil {
			continue
		}
		conv, err := repo.GetThreadConversation(ctx, threadID)
		if err == nil {
			return conv, nil
		}
	}
	return nil, fmt.Errorf("thread not found in any tenant")
}
