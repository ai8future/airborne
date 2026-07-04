package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Repository provides tenant-scoped data access for the relational chat model
// (airborne_chats / airborne_chat_messages / airborne_models). Every tenant
// query runs inside a WithTenant transaction so RLS scopes it to tenantID;
// the cross-tenant admin analytics run inside WithCrossTenant. The zero-value
// tenantID (from NewRepository) is only valid for the *AllTenants methods,
// which never depend on a tenant GUC.
type Repository struct {
	client   *Client
	tenantID string
}

// NewRepository creates a tenant-agnostic repository. It is only valid for the
// cross-tenant admin analytics (GetActivityFeedAllTenants,
// GetDebugDataAllTenants, GetThreadConversationAllTenants), which run under
// WithCrossTenant and never read the tenant GUC. Use NewTenantRepository for
// any tenant-scoped operation.
func NewRepository(client *Client) *Repository {
	return &Repository{client: client}
}

// NewTenantRepository creates a repository scoped to a specific tenant. Tenant
// validity is enforced upstream (see Client.IsValidTenant / RLS), so this
// constructor never returns an error — the signature is retained so the cached
// TenantRepository and its call sites are untouched.
func NewTenantRepository(client *Client, tenantID string) (*Repository, error) {
	return &Repository{client: client, tenantID: tenantID}, nil
}

// TenantID returns the tenant ID this repository is scoped to.
func (r *Repository) TenantID() string {
	return r.tenantID
}

// ---------------------------------------------------------------------------
// Column lists + row scanners (shared so every read stays column-order safe).
// ---------------------------------------------------------------------------

// chatColumns is the canonical SELECT list for a Chat row. external_ref and the
// nullable text columns are COALESCE'd so an unset value scans back as "" (the
// inverse of CreateChat writing SQL NULL for an empty ExternalRef).
const chatColumns = `id, tenant_id, user_id, COALESCE(title,''), COALESCE(model_id,''),
	COALESCE(provider,''), status, current_message_id, pinned, COALESCE(external_ref,''),
	metadata, created_at, updated_at`

func scanChat(row pgx.Row, c *Chat) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.UserID, &c.Title, &c.ModelID, &c.Provider,
		&c.Status, &c.CurrentMessageID, &c.Pinned, &c.ExternalRef,
		&c.Metadata, &c.CreatedAt, &c.UpdatedAt,
	)
}

// chatMessageColumns is the canonical SELECT list for a ChatMessage row. The
// DECIMAL cost columns are cast to float8 so they scan cleanly into *float64.
const chatMessageColumns = `id, tenant_id, chat_id, parent_id, sibling_seq, user_id, role,
	content, model_id, provider, response_id, status, status_history,
	input_tokens, output_tokens, total_tokens, cost_usd::float8,
	grounding_queries, grounding_cost_usd::float8, processing_time_ms,
	usage, sources, embeds, created_at, updated_at`

func scanChatMessage(rows pgx.Rows, m *ChatMessage) error {
	return rows.Scan(
		&m.ID, &m.TenantID, &m.ChatID, &m.ParentID, &m.SiblingSeq, &m.UserID, &m.Role,
		&m.Content, &m.ModelID, &m.Provider, &m.ResponseID, &m.Status, &m.StatusHistory,
		&m.InputTokens, &m.OutputTokens, &m.TotalTokens, &m.CostUSD,
		&m.GroundingQueries, &m.GroundingCostUSD, &m.ProcessingTimeMs,
		&m.Usage, &m.Sources, &m.Embeds, &m.CreatedAt, &m.UpdatedAt,
	)
}

// modelColumns is the canonical SELECT list for a Model row.
const modelColumns = `id, tenant_id, base_model_id, name, provider, params, meta,
	is_active, created_at, updated_at`

func scanModel(row pgx.Row, m *Model) error {
	return row.Scan(
		&m.ID, &m.TenantID, &m.BaseModelID, &m.Name, &m.Provider,
		&m.Params, &m.Meta, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
	)
}

// nullIfEmpty maps "" to a nil *string so an unset value is written as SQL
// NULL. external_ref's unique index is partial (WHERE external_ref IS NOT
// NULL), so an empty value must be NULL, never ”.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------------------
// Chat CRUD (tenant-scoped).
// ---------------------------------------------------------------------------

// CreateChat inserts a new chat. An empty ExternalRef is persisted as SQL NULL
// (never ”) so the partial unique index on external_ref is not tripped by
// callers that do not set a correlation id. An empty Status defaults to
// 'active' (the schema DEFAULT does not apply once a value is passed).
func (r *Repository) CreateChat(ctx context.Context, chat *Chat) error {
	status := chat.Status
	if status == "" {
		status = ChatStatusActive
	}
	return r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO airborne_chats
			  (id, tenant_id, user_id, title, model_id, provider, status,
			   current_message_id, pinned, external_ref, metadata)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			chat.ID, r.tenantID, chat.UserID, nullIfEmpty(chat.Title),
			nullIfEmpty(chat.ModelID), nullIfEmpty(chat.Provider), status,
			chat.CurrentMessageID, chat.Pinned, nullIfEmpty(chat.ExternalRef), chat.Metadata)
		if err != nil {
			return fmt.Errorf("create chat: %w", err)
		}
		return nil
	})
}

// GetChatByExternalRef looks up a chat by its opaque caller correlation id
// (addendum A9). RLS scopes the lookup to this repository's tenant, so the same
// external_ref used by another tenant is invisible. Returns (nil, false, nil)
// when no chat matches.
func (r *Repository) GetChatByExternalRef(ctx context.Context, externalRef string) (*Chat, bool, error) {
	var (
		chat  Chat
		found bool
	)
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT `+chatColumns+` FROM airborne_chats WHERE external_ref = $1`, externalRef)
		if err := scanChat(row, &chat); err != nil {
			if err == pgx.ErrNoRows {
				return nil
			}
			return fmt.Errorf("get chat by external_ref: %w", err)
		}
		found = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	return &chat, true, nil
}

// ListChats returns a user's chats, most-recently-updated first.
func (r *Repository) ListChats(ctx context.Context, userID string, limit int) ([]Chat, error) {
	var out []Chat
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+chatColumns+` FROM airborne_chats
			 WHERE user_id = $1 ORDER BY updated_at DESC LIMIT $2`, userID, limit)
		if err != nil {
			return fmt.Errorf("list chats: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var c Chat
			if err := scanChat(rows, &c); err != nil {
				return fmt.Errorf("scan chat: %w", err)
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Message tree CRUD (tenant-scoped).
// ---------------------------------------------------------------------------

// AppendMessage inserts a message and advances the chat's current_message_id,
// both in a single WithTenant transaction. When parentID is nil the message is
// a linear append onto the chat's current head. sibling_seq is the next value
// among the parent's children (COALESCE(MAX(sibling_seq)+1,0)); the read is
// racy under READ COMMITTED, so reads tie-break by (sibling_seq, created_at,
// id) rather than depending on it being unique. Returns the message id.
func (r *Repository) AppendMessage(ctx context.Context, chatID string, parentID *string, m *ChatMessage) (string, error) {
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		if parentID == nil { // linear append: parent = current head
			var head *string
			if err := tx.QueryRow(ctx,
				`SELECT current_message_id FROM airborne_chats WHERE id=$1`, chatID).Scan(&head); err != nil {
				return fmt.Errorf("read current head: %w", err)
			}
			parentID = head
		}
		// Deterministic sibling ordering: next seq among children of this parent.
		var seq int
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(sibling_seq)+1, 0) FROM airborne_chat_messages
			 WHERE chat_id=$1 AND parent_id IS NOT DISTINCT FROM $2`, chatID, parentID).Scan(&seq); err != nil {
			return fmt.Errorf("compute sibling_seq: %w", err)
		}
		status := m.Status
		if status == "" {
			status = ChatMessageStatusComplete
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO airborne_chat_messages
			  (id, tenant_id, chat_id, parent_id, sibling_seq, user_id, role, content, model_id, provider,
			   response_id, status, status_history, input_tokens, output_tokens, total_tokens, cost_usd,
			   grounding_queries, grounding_cost_usd, processing_time_ms, usage, sources, embeds)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
			m.ID, r.tenantID, chatID, parentID, seq, m.UserID, m.Role, NormalizeJSONB(m.Content), m.ModelID, m.Provider,
			m.ResponseID, status, m.StatusHistory, m.InputTokens, m.OutputTokens, m.TotalTokens, m.CostUSD,
			m.GroundingQueries, m.GroundingCostUSD, m.ProcessingTimeMs, m.Usage, m.Sources, m.Embeds); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE airborne_chats SET current_message_id=$1 WHERE id=$2`, m.ID, chatID); err != nil {
			return fmt.Errorf("advance current_message_id: %w", err)
		}
		return nil
	})
	return m.ID, err
}

// GetActiveBranch returns the chat's active branch, root-first: a recursive-CTE
// walk from current_message_id up through parent_id to the root, ordered by
// created_at ascending.
func (r *Repository) GetActiveBranch(ctx context.Context, chatID string) ([]ChatMessage, error) {
	var out []ChatMessage
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			WITH RECURSIVE branch AS (
			  SELECT m.* FROM airborne_chat_messages m
			  JOIN airborne_chats c ON c.current_message_id = m.id
			  WHERE c.id = $1
			  UNION ALL
			  SELECT p.* FROM airborne_chat_messages p
			  JOIN branch b ON p.id = b.parent_id
			)
			SELECT `+chatMessageColumns+` FROM branch ORDER BY created_at ASC`, chatID)
		if err != nil {
			return fmt.Errorf("get active branch: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var m ChatMessage
			if err := scanChatMessage(rows, &m); err != nil {
				return fmt.Errorf("scan message: %w", err)
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// GetSiblings returns the children of parentID within a chat, ordered by
// (sibling_seq, created_at, id). The trailing keys break ties deterministically
// if two concurrent same-parent appends computed the same sibling_seq. A nil
// parentID selects the chat's root messages.
func (r *Repository) GetSiblings(ctx context.Context, chatID string, parentID *string) ([]ChatMessage, error) {
	var out []ChatMessage
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+chatMessageColumns+` FROM airborne_chat_messages
			 WHERE chat_id=$1 AND parent_id IS NOT DISTINCT FROM $2
			 ORDER BY sibling_seq, created_at, id`, chatID, parentID)
		if err != nil {
			return fmt.Errorf("get siblings: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var m ChatMessage
			if err := scanChatMessage(rows, &m); err != nil {
				return fmt.Errorf("scan message: %w", err)
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// SaveMessageDebug upserts the cold debug blob (system prompt + raw
// request/response + rendered HTML) for a message into
// airborne_chat_message_debug, tenant-scoped. Empty system_prompt/rendered_html
// are stored as SQL NULL; nil raw JSON stays NULL.
func (r *Repository) SaveMessageDebug(ctx context.Context, messageID string, systemPrompt string, rawRequestJSON, rawResponseJSON json.RawMessage, renderedHTML string) error {
	return r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO airborne_chat_message_debug
			  (message_id, tenant_id, system_prompt, raw_request_json, raw_response_json, rendered_html)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (message_id) DO UPDATE SET
			  system_prompt = EXCLUDED.system_prompt,
			  raw_request_json = EXCLUDED.raw_request_json,
			  raw_response_json = EXCLUDED.raw_response_json,
			  rendered_html = EXCLUDED.rendered_html`,
			messageID, r.tenantID, nullIfEmpty(systemPrompt), rawRequestJSON, rawResponseJSON, nullIfEmpty(renderedHTML))
		if err != nil {
			return fmt.Errorf("save message debug: %w", err)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Model registry CRUD (tenant-scoped).
// ---------------------------------------------------------------------------

// UpsertModel inserts or updates a tenant model-registry row. params/meta are
// routed through NormalizeJSONB so a nil json.RawMessage becomes '{}' rather
// than SQL NULL (both columns are NOT NULL and the DEFAULT does not apply once
// a value is passed).
func (r *Repository) UpsertModel(ctx context.Context, m *Model) error {
	return r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO airborne_models
			  (id, tenant_id, base_model_id, name, provider, params, meta, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (tenant_id, id) DO UPDATE SET
			  base_model_id = EXCLUDED.base_model_id,
			  name = EXCLUDED.name,
			  provider = EXCLUDED.provider,
			  params = EXCLUDED.params,
			  meta = EXCLUDED.meta,
			  is_active = EXCLUDED.is_active`,
			m.ID, r.tenantID, m.BaseModelID, m.Name, m.Provider,
			NormalizeJSONB(m.Params), NormalizeJSONB(m.Meta), m.IsActive)
		if err != nil {
			return fmt.Errorf("upsert model: %w", err)
		}
		return nil
	})
}

// ListModels returns all model-registry rows for the tenant, ordered by id.
func (r *Repository) ListModels(ctx context.Context) ([]Model, error) {
	var out []Model
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+modelColumns+` FROM airborne_models ORDER BY id`)
		if err != nil {
			return fmt.Errorf("list models: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var m Model
			if err := scanModel(rows, &m); err != nil {
				return fmt.Errorf("scan model: %w", err)
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// ResolveModel returns the active registry row for id (with its base_model_id
// and params), or nil if the id is not registered or is soft-disabled.
func (r *Repository) ResolveModel(ctx context.Context, id string) (*Model, error) {
	var (
		model Model
		found bool
	)
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT `+modelColumns+` FROM airborne_models WHERE id = $1 AND is_active = true`, id)
		if err := scanModel(row, &model); err != nil {
			if err == pgx.ErrNoRows {
				return nil
			}
			return fmt.Errorf("resolve model: %w", err)
		}
		found = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &model, nil
}

// ---------------------------------------------------------------------------
// Cross-tenant admin analytics (WithCrossTenant / no tenant GUC).
// ---------------------------------------------------------------------------

// GetActivityFeedAllTenants returns the latest assistant messages across all
// tenants for the admin dashboard, newest first. It reads
// airborne_chat_messages directly under WithCrossTenant (a single query, no
// per-tenant UNION); tenant_id comes from each row.
func (r *Repository) GetActivityFeedAllTenants(ctx context.Context, limit int) ([]ActivityEntry, error) {
	var out []ActivityEntry
	err := r.client.WithCrossTenant(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT
				m.id,
				m.chat_id,
				m.tenant_id,
				m.user_id,
				COALESCE(m.content->>'text', m.content::text, '') AS content,
				COALESCE(m.provider, '') AS provider,
				COALESCE(m.model_id, '') AS model_id,
				COALESCE(m.input_tokens, 0) AS input_tokens,
				COALESCE(m.output_tokens, 0) AS output_tokens,
				COALESCE(m.total_tokens, 0) AS total_tokens,
				COALESCE(m.cost_usd, 0)::float8 AS cost_usd,
				COALESCE(m.grounding_queries, 0) AS grounding_queries,
				COALESCE(m.grounding_cost_usd, 0)::float8 AS grounding_cost_usd,
				COALESCE(m.processing_time_ms, 0) AS processing_time_ms,
				CASE WHEN m.status = 'error' THEN 'failed' ELSE 'success' END AS status,
				m.created_at,
				(SELECT COALESCE(SUM(cost_usd), 0)::float8
				 FROM airborne_chat_messages WHERE chat_id = m.chat_id) AS chat_cost_usd
			FROM airborne_chat_messages m
			WHERE m.role = 'assistant'
			ORDER BY m.created_at DESC
			LIMIT $1`, limit)
		if err != nil {
			return fmt.Errorf("activity feed (all tenants): %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var e ActivityEntry
			if err := rows.Scan(
				&e.ID, &e.ChatID, &e.TenantID, &e.UserID, &e.Content,
				&e.Provider, &e.ModelID, &e.InputTokens, &e.OutputTokens, &e.TotalTokens,
				&e.CostUSD, &e.GroundingQueries, &e.GroundingCostUSD, &e.ProcessingTimeMs,
				&e.Status, &e.Timestamp, &e.ThreadCostUSD,
			); err != nil {
				return fmt.Errorf("scan activity entry: %w", err)
			}
			e.FullContent = e.Content
			if len(e.Content) > 100 {
				e.Content = e.Content[:100] + "..."
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// GetDebugDataAllTenants returns the full request/response debug view for a
// message across all tenants (admin inspector). It joins airborne_chat_messages
// to the cold airborne_chat_message_debug blob under WithCrossTenant; tenant_id
// comes from the row. user_input is the nearest preceding user message in the
// same chat.
func (r *Repository) GetDebugDataAllTenants(ctx context.Context, messageID uuid.UUID) (*DebugData, error) {
	var (
		data  DebugData
		found bool
	)
	err := r.client.WithCrossTenant(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT
				m.id,
				m.chat_id,
				m.tenant_id,
				m.user_id,
				m.created_at,
				COALESCE(d.system_prompt, '') AS system_prompt,
				COALESCE(m.provider, '') AS provider,
				COALESCE(m.model_id, '') AS model_id,
				COALESCE(m.content->>'text', m.content::text, '') AS response_text,
				COALESCE(m.input_tokens, 0) AS tokens_in,
				COALESCE(m.output_tokens, 0) AS tokens_out,
				COALESCE(m.cost_usd, 0)::float8 AS cost_usd,
				COALESCE(m.grounding_queries, 0) AS grounding_queries,
				COALESCE(m.grounding_cost_usd, 0)::float8 AS grounding_cost_usd,
				COALESCE(m.processing_time_ms, 0) AS duration_ms,
				COALESCE(m.response_id, '') AS response_id,
				COALESCE(m.sources::text, '') AS citations,
				COALESCE(d.raw_request_json::text, '') AS raw_request_json,
				COALESCE(d.raw_response_json::text, '') AS raw_response_json,
				COALESCE(d.rendered_html, '') AS rendered_html,
				CASE WHEN m.status = 'error' THEN 'failed' ELSE 'success' END AS status,
				COALESCE(m.error::text, '') AS error,
				(SELECT COALESCE(u.content->>'text', u.content::text, '')
				 FROM airborne_chat_messages u
				 WHERE u.chat_id = m.chat_id AND u.role = 'user' AND u.created_at <= m.created_at
				 ORDER BY u.created_at DESC LIMIT 1) AS user_input
			FROM airborne_chat_messages m
			LEFT JOIN airborne_chat_message_debug d ON d.message_id = m.id
			WHERE m.id = $1`, messageID)
		var userInput *string
		if err := row.Scan(
			&data.MessageID, &data.ChatID, &data.TenantID, &data.UserID, &data.Timestamp,
			&data.SystemPrompt, &data.RequestProvider, &data.ResponseModel, &data.ResponseText,
			&data.TokensIn, &data.TokensOut, &data.CostUSD, &data.GroundingQueries, &data.GroundingCostUSD,
			&data.DurationMs, &data.ResponseID, &data.Citations, &data.RawRequestJSON, &data.RawResponseJSON,
			&data.RenderedHTML, &data.Status, &data.Error, &userInput,
		); err != nil {
			if err == pgx.ErrNoRows {
				return nil
			}
			return fmt.Errorf("get debug data (all tenants): %w", err)
		}
		found = true
		data.RequestModel = data.ResponseModel
		data.RequestTimestamp = data.Timestamp.Format("2006-01-02T15:04:05Z07:00")
		if userInput != nil {
			data.UserInput = *userInput
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("message not found")
	}
	return &data, nil
}

// GetThreadConversationAllTenants reconstructs a chat's active-branch
// conversation across all tenants (admin conversation view). It reads the chat
// header, then walks from current_message_id via the recursive CTE, joining the
// cold debug blob for rendered HTML — all under WithCrossTenant.
func (r *Repository) GetThreadConversationAllTenants(ctx context.Context, chatID uuid.UUID) (*ThreadConversation, error) {
	var (
		conv  ThreadConversation
		found bool
	)
	err := r.client.WithCrossTenant(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT id, tenant_id, user_id, COALESCE(provider,''), COALESCE(model_id,''),
			       created_at, updated_at
			FROM airborne_chats WHERE id = $1`, chatID)
		if err := row.Scan(
			&conv.ChatID, &conv.TenantID, &conv.UserID, &conv.Provider, &conv.ModelID,
			&conv.CreatedAt, &conv.UpdatedAt,
		); err != nil {
			if err == pgx.ErrNoRows {
				return nil
			}
			return fmt.Errorf("get chat header: %w", err)
		}
		found = true

		rows, err := tx.Query(ctx, `
			WITH RECURSIVE branch AS (
			  SELECT m.* FROM airborne_chat_messages m
			  JOIN airborne_chats c ON c.current_message_id = m.id
			  WHERE c.id = $1
			  UNION ALL
			  SELECT p.* FROM airborne_chat_messages p
			  JOIN branch b ON p.id = b.parent_id
			)
			SELECT b.id, b.role,
			       COALESCE(b.content->>'text', b.content::text, '') AS content,
			       COALESCE(d.rendered_html, '') AS rendered_html,
			       COALESCE(b.model_id, '') AS model_id,
			       COALESCE(b.provider, '') AS provider,
			       b.created_at
			FROM branch b
			LEFT JOIN airborne_chat_message_debug d ON d.message_id = b.id
			ORDER BY b.created_at ASC`, chatID)
		if err != nil {
			return fmt.Errorf("walk conversation: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var msg ConversationMessage
			if err := rows.Scan(
				&msg.ID, &msg.Role, &msg.Content, &msg.RenderedHTML,
				&msg.Model, &msg.Provider, &msg.Timestamp,
			); err != nil {
				return fmt.Errorf("scan conversation message: %w", err)
			}
			conv.Messages = append(conv.Messages, msg)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		conv.MessageCount = len(conv.Messages)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("thread not found")
	}
	return &conv, nil
}
