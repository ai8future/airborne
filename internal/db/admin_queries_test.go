package db

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// seedTurn persists one user+assistant turn (with a debug blob) for a tenant and
// returns the chat id and the user/assistant message ids. asstStatus lets a
// caller seed a failed assistant turn (status "error") to exercise the
// activity/debug status mapping.
func seedTurn(t *testing.T, c *Client, tenantID, userText, asstText, asstStatus string) (chatID, userID, asstID string) {
	t.Helper()
	repo, _ := NewTenantRepository(c, tenantID)
	ctx := context.Background()

	chatID = uuid.NewString()
	userID = uuid.NewString()
	asstID = uuid.NewString()

	userMsg := &ChatMessage{
		ID: userID, TenantID: tenantID, ChatID: chatID, UserID: "u1",
		Role: RoleUser, Content: TextContent(userText), Status: ChatMessageStatusComplete,
	}
	inTok, outTok, tot := 11, 22, 33
	cost := 0.0042
	asstMsg := &ChatMessage{
		ID: asstID, TenantID: tenantID, ChatID: chatID, UserID: "u1",
		Role: RoleAssistant, Content: TextContent(asstText), Status: asstStatus,
		ModelID: strp("gpt-4o"), Provider: strp("openai"),
		InputTokens: &inTok, OutputTokens: &outTok, TotalTokens: &tot, CostUSD: &cost,
	}
	debug := &TurnDebug{
		SystemPrompt:    "you are helpful",
		RawRequestJSON:  json.RawMessage(`{"req":"` + userText + `"}`),
		RawResponseJSON: json.RawMessage(`{"resp":"` + asstText + `"}`),
		RenderedHTML:    "<p>" + asstText + "</p>",
	}
	if err := repo.PersistTurn(ctx, &Chat{
		ID: chatID, TenantID: tenantID, UserID: "u1",
		Provider: "openai", ModelID: "gpt-4o", Status: ChatStatusActive,
	}, userMsg, asstMsg, debug); err != nil {
		t.Fatalf("seed turn for %s: %v", tenantID, err)
	}
	return chatID, userID, asstID
}

// TestGetActivityFeedAllTenants_CrossTenant proves the cross-tenant admin feed
// returns assistant rows from more than one tenant in a single query, with
// tenant_id filled from the row and the status mapped to success/failed.
func TestGetActivityFeedAllTenants_CrossTenant(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	_, _, ai8Asst := seedTurn(t, c, "ai8", "hi from ai8", "reply ai8", ChatMessageStatusComplete)
	_, _, e4aAsst := seedTurn(t, c, "email4ai", "hi from e4a", "reply e4a", ChatMessageStatusError)

	entries, err := NewRepository(c).GetActivityFeedAllTenants(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("activity feed returned %d entries, want >= 2 (one per tenant)", len(entries))
	}

	byID := map[string]ActivityEntry{}
	tenants := map[string]bool{}
	for _, e := range entries {
		byID[e.ID.String()] = e
		if e.TenantID == "" {
			t.Errorf("entry %s has empty tenant_id (cross-tenant read must fill it from the row)", e.ID)
		}
		tenants[e.TenantID] = true
	}
	if !tenants["ai8"] || !tenants["email4ai"] {
		t.Fatalf("feed missing a tenant: saw %v, want both ai8 and email4ai", tenants)
	}

	ok, found := byID[ai8Asst]
	if !found {
		t.Fatalf("ai8 assistant message %s not in feed", ai8Asst)
	}
	if ok.Status != "success" {
		t.Errorf("ai8 (complete) status = %q, want success", ok.Status)
	}
	if ok.ModelID != "gpt-4o" || ok.Provider != "openai" {
		t.Errorf("ai8 entry model/provider = %q/%q, want gpt-4o/openai", ok.ModelID, ok.Provider)
	}
	if ok.FullContent != "reply ai8" {
		t.Errorf("ai8 full_content = %q, want %q", ok.FullContent, "reply ai8")
	}

	bad, found := byID[e4aAsst]
	if !found {
		t.Fatalf("email4ai assistant message %s not in feed", e4aAsst)
	}
	if bad.Status != "failed" {
		t.Errorf("email4ai (error) status = %q, want failed", bad.Status)
	}
}

// TestGetDebugDataAllTenants_CrossTenantAndRoleFilter proves the debug inspector
// join populates the cold blob fields across tenants and that the lookup is
// scoped to assistant messages (a user message id resolves as "message not
// found", the pre-migration contract / carried finding #1).
func TestGetDebugDataAllTenants_CrossTenantAndRoleFilter(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	_, ai8User, ai8Asst := seedTurn(t, c, "ai8", "debug me", "debugged", ChatMessageStatusComplete)

	data, err := NewRepository(c).GetDebugDataAllTenants(ctx, uuid.MustParse(ai8Asst))
	if err != nil {
		t.Fatalf("debug data for assistant id: %v", err)
	}
	if data.TenantID != "ai8" {
		t.Errorf("tenant_id = %q, want ai8 (from row)", data.TenantID)
	}
	if data.SystemPrompt != "you are helpful" {
		t.Errorf("system_prompt = %q, want %q", data.SystemPrompt, "you are helpful")
	}
	if data.ResponseText != "debugged" {
		t.Errorf("response_text = %q, want %q", data.ResponseText, "debugged")
	}
	if data.UserInput != "debug me" {
		t.Errorf("user_input = %q, want %q (nearest preceding user message)", data.UserInput, "debug me")
	}
	if data.RawRequestJSON == "" || data.RawResponseJSON == "" {
		t.Errorf("raw request/response JSON not populated from debug blob: req=%q resp=%q",
			data.RawRequestJSON, data.RawResponseJSON)
	}
	if data.RenderedHTML != "<p>debugged</p>" {
		t.Errorf("rendered_html = %q, want %q", data.RenderedHTML, "<p>debugged</p>")
	}
	if data.Status != "success" {
		t.Errorf("status = %q, want success", data.Status)
	}

	// Role filter: a user message id must resolve as not found (only assistant
	// turns are debuggable). Restored in carried finding #1.
	if _, err := NewRepository(c).GetDebugDataAllTenants(ctx, uuid.MustParse(ai8User)); err == nil {
		t.Fatal("GetDebugDataAllTenants(user message id) returned no error, want 'message not found'")
	}
}

// TestGetThreadConversationAllTenants_RootFirst proves the conversation view
// reconstructs the active branch root-first across tenants, with the chat header
// and per-message rows populated.
func TestGetThreadConversationAllTenants_RootFirst(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	chatID, _, _ := seedTurn(t, c, "email4ai", "first", "second", ChatMessageStatusComplete)

	conv, err := NewRepository(c).GetThreadConversationAllTenants(ctx, uuid.MustParse(chatID))
	if err != nil {
		t.Fatal(err)
	}
	if conv.TenantID != "email4ai" {
		t.Errorf("tenant_id = %q, want email4ai (from chat header)", conv.TenantID)
	}
	if conv.MessageCount != 2 || len(conv.Messages) != 2 {
		t.Fatalf("message_count = %d / len = %d, want 2", conv.MessageCount, len(conv.Messages))
	}
	// Root first: the user turn is the root, the assistant its child.
	if conv.Messages[0].Role != RoleUser || conv.Messages[0].Content != "first" {
		t.Errorf("messages[0] = (%s, %q), want (user, first) — root must be first",
			conv.Messages[0].Role, conv.Messages[0].Content)
	}
	if conv.Messages[1].Role != RoleAssistant || conv.Messages[1].Content != "second" {
		t.Errorf("messages[1] = (%s, %q), want (assistant, second)",
			conv.Messages[1].Role, conv.Messages[1].Content)
	}
	if conv.Messages[1].RenderedHTML != "<p>second</p>" {
		t.Errorf("assistant rendered_html = %q, want %q", conv.Messages[1].RenderedHTML, "<p>second</p>")
	}
}
