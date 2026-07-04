package db

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// newUUID returns a fresh canonical UUID string for use as a chat/message id.
func newUUID() string { return uuid.NewString() }

func strp(s string) *string { return &s }

func TestAppendMessage_LinearBranchRoundTrip(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	repo, _ := NewTenantRepository(c, "ai8")
	chat := &Chat{ID: newUUID(), TenantID: "ai8", UserID: "u1", Status: "active"}
	if err := repo.CreateChat(ctx, chat); err != nil {
		t.Fatal(err)
	}

	m1, err := repo.AppendMessage(ctx, chat.ID, nil,
		&ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chat.ID, UserID: "u1", Role: "user", Content: TextContent("hi"), Status: "complete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendMessage(ctx, chat.ID, &m1,
		&ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chat.ID, UserID: "u1", Role: "assistant", Content: TextContent("hello"), Status: "complete"}); err != nil {
		t.Fatal(err)
	}

	branch, err := repo.GetActiveBranch(ctx, chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 2 || branch[0].Role != "user" || branch[1].Role != "assistant" {
		t.Fatalf("branch = %+v", branch)
	}
}

// TestSiblings_SeqOrdering proves two same-parent appends get sibling_seq 0
// then 1, and GetSiblings returns them in that order.
func TestSiblings_SeqOrdering(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	repo, _ := NewTenantRepository(c, "ai8")

	chat := &Chat{ID: newUUID(), TenantID: "ai8", UserID: "u1", Status: "active"}
	if err := repo.CreateChat(ctx, chat); err != nil {
		t.Fatal(err)
	}
	root, err := repo.AppendMessage(ctx, chat.ID, nil,
		&ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chat.ID, UserID: "u1", Role: "user", Content: TextContent("root"), Status: "complete"})
	if err != nil {
		t.Fatal(err)
	}

	// Two children of the same parent (root): expect sibling_seq 0 then 1.
	childA, err := repo.AppendMessage(ctx, chat.ID, &root,
		&ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chat.ID, UserID: "u1", Role: "assistant", Content: TextContent("A"), Status: "complete"})
	if err != nil {
		t.Fatal(err)
	}
	childB, err := repo.AppendMessage(ctx, chat.ID, &root,
		&ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chat.ID, UserID: "u1", Role: "assistant", Content: TextContent("B"), Status: "complete"})
	if err != nil {
		t.Fatal(err)
	}

	sibs, err := repo.GetSiblings(ctx, chat.ID, &root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sibs) != 2 {
		t.Fatalf("GetSiblings returned %d children, want 2: %+v", len(sibs), sibs)
	}
	if sibs[0].ID != childA || sibs[0].SiblingSeq != 0 {
		t.Errorf("first sibling = (id %s, seq %d), want (id %s, seq 0)", sibs[0].ID, sibs[0].SiblingSeq, childA)
	}
	if sibs[1].ID != childB || sibs[1].SiblingSeq != 1 {
		t.Errorf("second sibling = (id %s, seq %d), want (id %s, seq 1)", sibs[1].ID, sibs[1].SiblingSeq, childB)
	}
}

// TestResolveModel proves ResolveModel returns a registered alias's
// base_model_id/params and nil for an unregistered id.
func TestResolveModel(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	repo, _ := NewTenantRepository(c, "ai8")

	if err := repo.UpsertModel(ctx, &Model{
		ID:          "fast",
		TenantID:    "ai8",
		BaseModelID: strp("gpt-4o"),
		Name:        strp("Fast"),
		Provider:    strp("openai"),
		Params:      json.RawMessage(`{"temperature":0.2}`),
		IsActive:    true,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ResolveModel(ctx, "fast")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("ResolveModel(fast) = nil, want registered row")
	}
	if got.BaseModelID == nil || *got.BaseModelID != "gpt-4o" {
		t.Errorf("BaseModelID = %v, want gpt-4o", got.BaseModelID)
	}
	var params struct {
		Temperature float64 `json:"temperature"`
	}
	if err := json.Unmarshal(got.Params, &params); err != nil {
		t.Fatalf("params not valid JSON: %v (raw=%s)", err, got.Params)
	}
	if params.Temperature != 0.2 {
		t.Errorf("params.temperature = %v, want 0.2", params.Temperature)
	}

	missing, err := repo.ResolveModel(ctx, "not-registered")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Errorf("ResolveModel(not-registered) = %+v, want nil", missing)
	}
}

// TestExternalRef proves addendum A9: a chat created under ai8 with an
// external_ref is found by that tenant, and the same ref is invisible to
// another tenant (RLS isolation).
func TestExternalRef(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	ai8, _ := NewTenantRepository(c, "ai8")
	chat := &Chat{ID: newUUID(), TenantID: "ai8", UserID: "u1", Status: "active", ExternalRef: "conv-1"}
	if err := ai8.CreateChat(ctx, chat); err != nil {
		t.Fatal(err)
	}

	got, found, err := ai8.GetChatByExternalRef(ctx, "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("GetChatByExternalRef(conv-1) as ai8 found=false, want true")
	}
	if got.ID != chat.ID || got.ExternalRef != "conv-1" {
		t.Errorf("got chat (id %s, ref %q), want (id %s, ref conv-1)", got.ID, got.ExternalRef, chat.ID)
	}

	// RLS isolation: email4ai must not see ai8's external_ref.
	other, _ := NewTenantRepository(c, "email4ai")
	_, found, err = other.GetChatByExternalRef(ctx, "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("GetChatByExternalRef(conv-1) as email4ai found=true, want false (RLS isolation)")
	}
}
