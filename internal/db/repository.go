package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Repository provides data access operations for threads and messages.
type Repository struct {
	client *Client
}

// NewRepository creates a new repository backed by the given client.
func NewRepository(client *Client) *Repository {
	return &Repository{client: client}
}

// CreateThread inserts a new thread into the database.
func (r *Repository) CreateThread(ctx context.Context, thread *Thread) error {
	query := `
		INSERT INTO airborne_threads (id, tenant_id, user_id, provider, model, status, message_count, created_at, updated_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	r.client.logQuery(query, thread.ID, thread.TenantID, thread.UserID)

	_, err := r.client.pool.Exec(ctx, query,
		thread.ID,
		thread.TenantID,
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
	query := `
		SELECT id, tenant_id, user_id, provider, model, status, message_count, created_at, updated_at, metadata
		FROM airborne_threads
		WHERE id = $1
	`
	r.client.logQuery(query, id)

	var thread Thread
	err := r.client.pool.QueryRow(ctx, query, id).Scan(
		&thread.ID,
		&thread.TenantID,
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
	query := `
		UPDATE airborne_threads
		SET provider = $2, model = $3, updated_at = NOW()
		WHERE id = $1
	`
	r.client.logQuery(query, threadID, provider, model)

	_, err := r.client.pool.Exec(ctx, query, threadID, provider, model)
	if err != nil {
		return fmt.Errorf("failed to update thread provider: %w", err)
	}
	return nil
}

// CreateMessage inserts a new message into the database.
func (r *Repository) CreateMessage(ctx context.Context, msg *Message) error {
	query := `
		INSERT INTO airborne_messages (
			id, thread_id, role, content, provider, model, response_id,
			input_tokens, output_tokens, total_tokens, cost_usd,
			processing_time_ms, citations, created_at, metadata,
			system_prompt, raw_request_json, raw_response_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`
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
	query := `
		SELECT id, thread_id, role, content, provider, model, response_id,
		       input_tokens, output_tokens, total_tokens, cost_usd,
		       processing_time_ms, citations, created_at, metadata
		FROM airborne_messages
		WHERE thread_id = $1
		ORDER BY created_at ASC
		LIMIT $2
	`
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
func (r *Repository) GetActivityFeed(ctx context.Context, limit int) ([]ActivityEntry, error) {
	query := `
		SELECT
			m.id,
			m.thread_id,
			t.tenant_id,
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
				FROM airborne_messages
				WHERE thread_id = m.thread_id
			) AS thread_cost_usd
		FROM airborne_messages m
		JOIN airborne_threads t ON m.thread_id = t.id
		WHERE m.role = 'assistant'
		ORDER BY m.created_at DESC
		LIMIT $1
	`
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

// GetActivityFeedByTenant retrieves activity for a specific tenant.
func (r *Repository) GetActivityFeedByTenant(ctx context.Context, tenantID string, limit int) ([]ActivityEntry, error) {
	query := `
		SELECT
			m.id,
			m.thread_id,
			t.tenant_id,
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
				FROM airborne_messages
				WHERE thread_id = m.thread_id
			) AS thread_cost_usd
		FROM airborne_messages m
		JOIN airborne_threads t ON m.thread_id = t.id
		WHERE m.role = 'assistant' AND t.tenant_id = $1
		ORDER BY m.created_at DESC
		LIMIT $2
	`
	r.client.logQuery(query, tenantID, limit)

	rows, err := r.client.pool.Query(ctx, query, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get activity feed by tenant: %w", err)
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

// DebugInfo contains debug data to store alongside messages.
type DebugInfo struct {
	SystemPrompt    string
	RawRequestJSON  string
	RawResponseJSON string
}

// PersistConversationTurn saves both user and assistant messages in a transaction.
// This is the main entry point for chat service persistence.
func (r *Repository) PersistConversationTurn(ctx context.Context, threadID uuid.UUID, tenantID, userID string, userContent, assistantContent, provider, model, responseID string, inputTokens, outputTokens, processingTimeMs int, costUSD float64) error {
	return r.PersistConversationTurnWithDebug(ctx, threadID, tenantID, userID, userContent, assistantContent, provider, model, responseID, inputTokens, outputTokens, processingTimeMs, costUSD, nil)
}

// PersistConversationTurnWithDebug saves both user and assistant messages with optional debug data.
func (r *Repository) PersistConversationTurnWithDebug(ctx context.Context, threadID uuid.UUID, tenantID, userID string, userContent, assistantContent, provider, model, responseID string, inputTokens, outputTokens, processingTimeMs int, costUSD float64, debug *DebugInfo) error {
	tx, err := r.client.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Check if thread exists, create if not
	var threadExists bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM airborne_threads WHERE id = $1)", threadID).Scan(&threadExists)
	if err != nil {
		return fmt.Errorf("failed to check thread existence: %w", err)
	}

	if !threadExists {
		// Create new thread
		_, err = tx.Exec(ctx, `
			INSERT INTO airborne_threads (id, tenant_id, user_id, provider, model, status, message_count, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, 'active', 0, NOW(), NOW())
		`, threadID, tenantID, userID, provider, model)
		if err != nil {
			return fmt.Errorf("failed to create thread: %w", err)
		}
		slog.Debug("created new thread", "thread_id", threadID, "tenant_id", tenantID)
	}

	// Insert user message
	userMsgID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO airborne_messages (id, thread_id, role, content, created_at)
		VALUES ($1, $2, 'user', $3, NOW())
	`, userMsgID, threadID, userContent)
	if err != nil {
		return fmt.Errorf("failed to insert user message: %w", err)
	}

	// Insert assistant message with full metrics and optional debug data
	assistantMsgID := uuid.New()
	totalTokens := inputTokens + outputTokens

	var systemPrompt, rawReqJSON, rawRespJSON *string
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
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO airborne_messages (
			id, thread_id, role, content, provider, model, response_id,
			input_tokens, output_tokens, total_tokens, cost_usd, processing_time_ms, created_at,
			system_prompt, raw_request_json, raw_response_json
		) VALUES ($1, $2, 'assistant', $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), $12, $13, $14)
	`, assistantMsgID, threadID, assistantContent, provider, model, responseID,
		inputTokens, outputTokens, totalTokens, costUSD, processingTimeMs,
		systemPrompt, rawReqJSON, rawRespJSON)
	if err != nil {
		return fmt.Errorf("failed to insert assistant message: %w", err)
	}

	// Update thread's last-used provider (trigger updates message_count and updated_at)
	_, err = tx.Exec(ctx, `
		UPDATE airborne_threads
		SET provider = $2, model = $3, updated_at = NOW()
		WHERE id = $1
	`, threadID, provider, model)
	if err != nil {
		return fmt.Errorf("failed to update thread provider: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.Debug("persisted conversation turn",
		"thread_id", threadID,
		"provider", provider,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"cost_usd", costUSD,
	)
	return nil
}

// GetDebugData retrieves the full request/response debug data for a message.
func (r *Repository) GetDebugData(ctx context.Context, messageID uuid.UUID) (*DebugData, error) {
	query := `
		SELECT
			m.id,
			m.thread_id,
			t.tenant_id,
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
			COALESCE(m.citations, '') as citations,
			COALESCE(m.raw_request_json::text, '') as raw_request_json,
			COALESCE(m.raw_response_json::text, '') as raw_response_json,
			(
				SELECT COALESCE(content, '')
				FROM airborne_messages
				WHERE thread_id = m.thread_id
					AND role = 'user'
					AND created_at < m.created_at
				ORDER BY created_at DESC
				LIMIT 1
			) as user_input
		FROM airborne_messages m
		JOIN airborne_threads t ON m.thread_id = t.id
		WHERE m.id = $1 AND m.role = 'assistant'
	`
	r.client.logQuery(query, messageID)

	var data DebugData
	var userInput *string
	err := r.client.pool.QueryRow(ctx, query, messageID).Scan(
		&data.MessageID,
		&data.ThreadID,
		&data.TenantID,
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
		&userInput,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("message not found")
		}
		return nil, fmt.Errorf("failed to get debug data: %w", err)
	}

	// Set derived fields
	data.RequestModel = data.ResponseModel // Model requested = model used for now
	data.RequestTimestamp = data.Timestamp.Format("2006-01-02T15:04:05Z07:00")
	data.Status = "success"
	if userInput != nil {
		data.UserInput = *userInput
	}

	return &data, nil
}

// GetOrCreateThread ensures a thread exists for the given tenant/user.
func (r *Repository) GetOrCreateThread(ctx context.Context, threadID uuid.UUID, tenantID, userID string) (*Thread, error) {
	// Try to get existing thread
	thread, err := r.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if thread != nil {
		return thread, nil
	}

	// Create new thread
	thread = NewThread(tenantID, userID)
	thread.ID = threadID // Use the provided ID
	if err := r.CreateThread(ctx, thread); err != nil {
		return nil, err
	}
	return thread, nil
}
