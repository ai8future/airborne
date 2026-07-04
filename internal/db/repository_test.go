package db

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

// TestGetChatByID proves PK lookup: found for an existing chat, found=false
// for an unknown id.
func TestGetChatByID(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	repo, _ := NewTenantRepository(c, "ai8")

	chat := &Chat{ID: newUUID(), TenantID: "ai8", UserID: "u1", Status: "active"}
	if err := repo.CreateChat(ctx, chat); err != nil {
		t.Fatal(err)
	}

	got, found, err := repo.GetChatByID(ctx, chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("GetChatByID(existing) found=false, want true")
	}
	if got.ID != chat.ID || got.UserID != "u1" {
		t.Errorf("got chat (id %s, user %s), want (id %s, user u1)", got.ID, got.UserID, chat.ID)
	}

	if _, found, err = repo.GetChatByID(ctx, newUUID()); err != nil {
		t.Fatal(err)
	} else if found {
		t.Error("GetChatByID(unknown) found=true, want false")
	}
}

// TestPersistTurn proves the composed single-transaction turn write: chat
// get-or-create by PK (external_ref stays empty — reserved for caller
// correlation ids), user + assistant messages with the correct parent chain,
// head advanced to the assistant id, debug row written; a second turn threads
// linearly onto the head, and PersistTurn on the now-existing chat id neither
// errors nor duplicates the chat.
func TestPersistTurn(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	repo, _ := NewTenantRepository(c, "ai8")

	chatID := newUUID()
	chat := &Chat{ID: chatID, TenantID: "ai8", UserID: "u1", Provider: "openai", ModelID: "gpt-4o", Status: "active"}

	u1 := &ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chatID, UserID: "u1", Role: RoleUser, Content: TextContent("hi"), Status: "complete"}
	a1 := &ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chatID, UserID: "u1", Role: RoleAssistant, Content: TextContent("hello"), Status: "complete"}
	debug := &TurnDebug{
		SystemPrompt:    "sys",
		RawRequestJSON:  json.RawMessage(`{"req":1}`),
		RawResponseJSON: json.RawMessage(`{"resp":1}`),
		RenderedHTML:    "<p>hello</p>",
	}
	if err := repo.PersistTurn(ctx, chat, u1, a1, debug); err != nil {
		t.Fatal(err)
	}

	got, found, err := repo.GetChatByID(ctx, chatID)
	if err != nil || !found {
		t.Fatalf("chat after PersistTurn: found=%v err=%v", found, err)
	}
	if got.ExternalRef != "" {
		t.Errorf("ExternalRef = %q, want empty (reserved for caller correlation ids)", got.ExternalRef)
	}
	if got.CurrentMessageID == nil || *got.CurrentMessageID != a1.ID {
		t.Errorf("head = %v, want %s (assistant id)", got.CurrentMessageID, a1.ID)
	}

	branch, err := repo.GetActiveBranch(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 2 || branch[0].ID != u1.ID || branch[1].ID != a1.ID {
		t.Fatalf("branch = %+v, want [user, assistant]", branch)
	}
	if branch[0].ParentID != nil {
		t.Errorf("user ParentID = %v, want nil (root)", branch[0].ParentID)
	}
	if branch[1].ParentID == nil || *branch[1].ParentID != u1.ID {
		t.Errorf("assistant ParentID = %v, want %s (user id)", branch[1].ParentID, u1.ID)
	}

	// Debug row present, read back through the admin inspector join.
	data, err := NewRepository(c).GetDebugDataAllTenants(ctx, uuid.MustParse(a1.ID))
	if err != nil {
		t.Fatal(err)
	}
	if data.SystemPrompt != "sys" || data.RenderedHTML != "<p>hello</p>" {
		t.Errorf("debug = (prompt %q, html %q), want (sys, <p>hello</p>)", data.SystemPrompt, data.RenderedHTML)
	}

	// Second turn on the SAME chat id: get-or-create must be idempotent (no
	// error, no duplicate chat) and the turn must thread onto the head.
	u2 := &ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chatID, UserID: "u1", Role: RoleUser, Content: TextContent("more"), Status: "complete"}
	a2 := &ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chatID, UserID: "u1", Role: RoleAssistant, Content: TextContent("sure"), Status: "complete"}
	if err := repo.PersistTurn(ctx, chat, u2, a2, nil); err != nil {
		t.Fatalf("PersistTurn on existing chat id: %v", err)
	}

	branch, err = repo.GetActiveBranch(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 4 || branch[2].ID != u2.ID || branch[3].ID != a2.ID {
		t.Fatalf("branch after 2nd turn has %d messages, want 4 ending [u2, a2]", len(branch))
	}
	if branch[2].ParentID == nil || *branch[2].ParentID != a1.ID {
		t.Errorf("2nd user ParentID = %v, want %s (previous head)", branch[2].ParentID, a1.ID)
	}

	chats, err := repo.ListChats(ctx, "u1", 10)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ch := range chats {
		if ch.ID == chatID {
			n++
		}
	}
	if n != 1 {
		t.Errorf("chat id appears %d times in ListChats, want exactly 1", n)
	}
}

// TestPersistTurn_ExternalRefUniqueViolationSurfaces proves the behavior the
// service's external_ref lost-create-race retry (addendum A10) depends on:
// persisting a turn onto a NEW chat id carrying an external_ref that another
// chat already owns trips the partial unique index (tenant_id, external_ref),
// and the violation surfaces through PersistTurn's error wrapping as a
// *pgconn.PgError with code 23505 (detectable via errors.As).
func TestPersistTurn_ExternalRefUniqueViolationSurfaces(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	repo, _ := NewTenantRepository(c, "ai8")

	winner := &Chat{ID: newUUID(), TenantID: "ai8", UserID: "u1", Status: "active", ExternalRef: "conv-uv"}
	if err := repo.CreateChat(ctx, winner); err != nil {
		t.Fatal(err)
	}

	loserID := newUUID()
	loser := &Chat{ID: loserID, TenantID: "ai8", UserID: "u1", Status: "active", ExternalRef: "conv-uv"}
	u := &ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: loserID, UserID: "u1", Role: RoleUser, Content: TextContent("hi"), Status: "complete"}
	a := &ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: loserID, UserID: "u1", Role: RoleAssistant, Content: TextContent("hello"), Status: "complete"}

	err := repo.PersistTurn(ctx, loser, u, a, nil)
	if err == nil {
		t.Fatal("PersistTurn with duplicate external_ref succeeded, want unique violation")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("error = %v, want wrapped *pgconn.PgError code 23505", err)
	}

	// Recovery path (what the service retry does): re-resolve the ref and land
	// the turn on the winner's chat — exactly one chat owns the ref.
	got, found, err := repo.GetChatByExternalRef(ctx, "conv-uv")
	if err != nil || !found {
		t.Fatalf("GetChatByExternalRef after violation: found=%v err=%v", found, err)
	}
	if got.ID != winner.ID {
		t.Errorf("ref owner = %s, want winner %s", got.ID, winner.ID)
	}
	if err := repo.PersistTurn(ctx, &Chat{ID: got.ID, TenantID: "ai8", UserID: "u1", Status: "active", ExternalRef: "conv-uv"}, u, a, nil); err != nil {
		t.Fatalf("retry PersistTurn onto winner: %v", err)
	}
	branch, err := repo.GetActiveBranch(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 2 || branch[0].ID != u.ID || branch[1].ID != a.ID {
		t.Fatalf("winner branch = %d messages, want the retried [user, assistant] turn", len(branch))
	}
}

// TestPersistTurn_ConcurrentFirstTurns proves the ON CONFLICT (id) DO NOTHING
// get-or-create: two concurrent first turns on the same new chat id both
// persist — the loser's chat insert no-ops and, because its insert blocks on
// the winner's in-flight row, its turn threads onto the winner's committed
// head. Neither turn is dropped.
func TestPersistTurn_ConcurrentFirstTurns(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	chatID := newUUID()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repo, _ := NewTenantRepository(c, "ai8")
			u := &ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chatID, UserID: "u1", Role: RoleUser, Content: TextContent("q"), Status: "complete"}
			a := &ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chatID, UserID: "u1", Role: RoleAssistant, Content: TextContent("r"), Status: "complete"}
			errs[i] = repo.PersistTurn(ctx,
				&Chat{ID: chatID, TenantID: "ai8", UserID: "u1", Status: "active"}, u, a, nil)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}

	repo, _ := NewTenantRepository(c, "ai8")
	branch, err := repo.GetActiveBranch(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 4 {
		t.Fatalf("active branch has %d messages, want 4 (both turns persisted)", len(branch))
	}
}
