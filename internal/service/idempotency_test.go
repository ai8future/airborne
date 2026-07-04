package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/db"
	"github.com/ai8future/airborne/internal/redis"
	"github.com/ai8future/airborne/internal/service/config"
	"github.com/ai8future/airborne/internal/tenant"
	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// newIdemTestService builds a ChatService with mock providers and a
// miniredis-backed idempotency store. Returns the service, the openai mock
// (whose generateCalls slice is the generation counter), and the miniredis
// handle so tests can simulate Redis outages.
func newIdemTestService(t *testing.T) (*ChatService, *mockProvider, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	redisClient, err := redis.NewClient(redis.Config{Addr: mr.Addr()})
	if err != nil {
		t.Fatalf("failed to create redis client: %v", err)
	}
	t.Cleanup(func() { redisClient.Close() })

	mockOpenAI := newMockProvider("openai")
	svc := &ChatService{
		openaiProvider:    mockOpenAI,
		geminiProvider:    newMockProvider("gemini"),
		anthropicProvider: newMockProvider("anthropic"),
		configBuilder:     config.NewBuilder(),
		idem:              newIdempotencyStore(redisClient),
	}
	return svc, mockOpenAI, mr
}

// tenantCtx builds a chat-permission context scoped to an explicit tenant id.
func tenantCtx(tenantID string) context.Context {
	cfg := &tenant.TenantConfig{
		TenantID:    tenantID,
		DisplayName: tenantID,
		Providers: map[string]tenant.ProviderConfig{
			"openai": {Enabled: true, APIKey: "test-key", Model: "test-model"},
		},
	}
	return ctxWithChatPermissionAndTenant("client-1", cfg)
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	data, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("proto.Marshal failed: %v", err)
	}
	return data
}

// TestGenerateReply_Idempotent is the core A10 contract: a duplicate call with
// the same idempotency_key replays the byte-identical prior response and the
// provider runs exactly once.
func TestGenerateReply_Idempotent(t *testing.T) {
	svc, mock, _ := newIdemTestService(t)
	ctx := tenantCtx("test-tenant")

	req1 := &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-1"}
	resp1, err := svc.GenerateReply(ctx, req1)
	if err != nil {
		t.Fatalf("first GenerateReply failed: %v", err)
	}

	req2 := &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-1"}
	resp2, err := svc.GenerateReply(ctx, req2)
	if err != nil {
		t.Fatalf("second GenerateReply failed: %v", err)
	}

	// The replay short-circuits in GenerateReply (idemHit) and never enters
	// generateReply, so BOTH regeneration AND persistence — which lives inside
	// generateReply/persistConversation — are skipped on the duplicate. The
	// counter staying at 1 after both calls complete is the observable proof of
	// that short-circuit: a second generation (or a second persisted turn) would
	// have required a second provider call.
	if got := len(mock.generateCalls); got != 1 {
		t.Errorf("expected exactly 1 provider generation, got %d", got)
	}
	if !proto.Equal(resp1, resp2) {
		t.Errorf("replayed response is not proto.Equal to the original:\n first: %v\nsecond: %v", resp1, resp2)
	}
	// Byte-identical replay: email_ai_svc's downstream send fingerprints
	// SHA256(body), so serialized bytes must match — proto.Equal is not enough.
	if !bytes.Equal(mustMarshal(t, resp1), mustMarshal(t, resp2)) {
		t.Error("replayed response serialized bytes differ from the original")
	}
}

// TestGenerateReply_IdempotencyKeyFromMetadata: the key is honored from
// metadata["idempotency_key"] with no first-class field set (zero-client-change
// path for email_ai_svc).
func TestGenerateReply_IdempotencyKeyFromMetadata(t *testing.T) {
	svc, mock, _ := newIdemTestService(t)
	ctx := tenantCtx("test-tenant")

	mkReq := func() *pb.GenerateReplyRequest {
		return &pb.GenerateReplyRequest{
			UserInput: "hello",
			Metadata:  map[string]string{"idempotency_key": "meta-key-1"},
		}
	}

	resp1, err := svc.GenerateReply(ctx, mkReq())
	if err != nil {
		t.Fatalf("first GenerateReply failed: %v", err)
	}
	resp2, err := svc.GenerateReply(ctx, mkReq())
	if err != nil {
		t.Fatalf("second GenerateReply failed: %v", err)
	}

	if got := len(mock.generateCalls); got != 1 {
		t.Errorf("expected exactly 1 provider generation via metadata key, got %d", got)
	}
	if !bytes.Equal(mustMarshal(t, resp1), mustMarshal(t, resp2)) {
		t.Error("metadata-keyed replay is not byte-identical")
	}
}

// TestGenerateReply_DifferentKeysGenerate: distinct keys are distinct requests.
func TestGenerateReply_DifferentKeysGenerate(t *testing.T) {
	svc, mock, _ := newIdemTestService(t)
	ctx := tenantCtx("test-tenant")

	if _, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "a", IdempotencyKey: "key-a"}); err != nil {
		t.Fatalf("first GenerateReply failed: %v", err)
	}
	if _, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "b", IdempotencyKey: "key-b"}); err != nil {
		t.Fatalf("second GenerateReply failed: %v", err)
	}

	if got := len(mock.generateCalls); got != 2 {
		t.Errorf("expected 2 provider generations for 2 distinct keys, got %d", got)
	}
}

// TestGenerateReply_ProviderErrorNotCached: a provider failure must never be
// cached — the retry with the same key regenerates.
func TestGenerateReply_ProviderErrorNotCached(t *testing.T) {
	svc, mock, _ := newIdemTestService(t)
	ctx := tenantCtx("test-tenant")

	mock.generateErr = errors.New("provider exploded")
	if _, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-err"}); err == nil {
		t.Fatal("expected first GenerateReply to fail")
	}

	mock.generateErr = nil
	resp, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-err"})
	if err != nil {
		t.Fatalf("retry after provider error failed: %v", err)
	}
	if resp.Text != "Mock response" {
		t.Errorf("retry returned unexpected text %q", resp.Text)
	}

	// Both calls must reach the provider: 1 failed + 1 successful.
	if got := len(mock.generateCalls); got != 2 {
		t.Errorf("expected 2 provider generations (error not cached), got %d", got)
	}
}

// TestGenerateReply_TenantNamespacedKeys: the same idempotency key under two
// tenants must never replay across tenants.
func TestGenerateReply_TenantNamespacedKeys(t *testing.T) {
	svc, mock, _ := newIdemTestService(t)

	if _, err := svc.GenerateReply(tenantCtx("tenant-a"), &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "shared-key"}); err != nil {
		t.Fatalf("tenant-a GenerateReply failed: %v", err)
	}
	if _, err := svc.GenerateReply(tenantCtx("tenant-b"), &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "shared-key"}); err != nil {
		t.Fatalf("tenant-b GenerateReply failed: %v", err)
	}

	if got := len(mock.generateCalls); got != 2 {
		t.Errorf("expected 2 provider generations (no cross-tenant replay), got %d", got)
	}
}

// TestGenerateReply_InFlightDuplicateAborted: while a key's first request is
// still processing (in-flight marker held), a duplicate is rejected with
// codes.Aborted rather than double-generating — mirroring the admin path's
// 409 Conflict.
func TestGenerateReply_InFlightDuplicateAborted(t *testing.T) {
	svc, mock, mr := newIdemTestService(t)
	ctx := tenantCtx("test-tenant")

	// Simulate an in-flight first request by planting the marker directly.
	if err := mr.Set("idem:test-tenant:key-busy", idemInFlightMarker); err != nil {
		t.Fatalf("failed to plant in-flight marker: %v", err)
	}

	_, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-busy"})
	if status.Code(err) != codes.Aborted {
		t.Fatalf("expected codes.Aborted for in-flight duplicate, got %v (err=%v)", status.Code(err), err)
	}
	if got := len(mock.generateCalls); got != 0 {
		t.Errorf("expected 0 provider generations for in-flight duplicate, got %d", got)
	}
}

// TestGenerateReply_RedisDownDegradesUncached: an unreachable idempotency
// store must never fail the request — both calls generate.
func TestGenerateReply_RedisDownDegradesUncached(t *testing.T) {
	svc, mock, mr := newIdemTestService(t)
	ctx := tenantCtx("test-tenant")
	mr.Close() // Redis goes down before any call

	if _, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-1"}); err != nil {
		t.Fatalf("GenerateReply with Redis down failed: %v", err)
	}
	if _, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-1"}); err != nil {
		t.Fatalf("second GenerateReply with Redis down failed: %v", err)
	}

	if got := len(mock.generateCalls); got != 2 {
		t.Errorf("expected 2 provider generations with Redis down (uncached degrade), got %d", got)
	}
}

// TestGenerateReply_NoIdemStoreUncached: a ChatService constructed without a
// Redis client (idem == nil) behaves exactly as today.
func TestGenerateReply_NoIdemStoreUncached(t *testing.T) {
	mock := newMockProvider("openai")
	svc := &ChatService{
		openaiProvider:    mock,
		geminiProvider:    newMockProvider("gemini"),
		anthropicProvider: newMockProvider("anthropic"),
		configBuilder:     config.NewBuilder(),
		idem:              newIdempotencyStore(nil),
	}
	ctx := tenantCtx("test-tenant")

	for i := 0; i < 2; i++ {
		if _, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-1"}); err != nil {
			t.Fatalf("GenerateReply without idem store failed: %v", err)
		}
	}
	if got := len(mock.generateCalls); got != 2 {
		t.Errorf("expected 2 provider generations without idem store, got %d", got)
	}
}

// ==================== external_ref persistence (A10 Step 3 tail) ====================

// fakeTurnPersister is a focused fake for persistTurnWithRef: it records which
// chat each turn landed on and mimics the (tenant_id, external_ref) partial
// unique index, including an optional injected lost-create-race.
type fakeTurnPersister struct {
	chatsByRef map[string]*db.Chat
	chatsByID  map[string]*db.Chat
	turnChats  []string // chat IDs each persisted turn landed on
	// raceChat, when set, simulates a concurrent winner: the first PersistTurn
	// that would create raceChat.ExternalRef fails with a 23505 unique
	// violation and the chat becomes visible to subsequent lookups.
	raceChat *db.Chat
}

func newFakeTurnPersister() *fakeTurnPersister {
	return &fakeTurnPersister{
		chatsByRef: make(map[string]*db.Chat),
		chatsByID:  make(map[string]*db.Chat),
	}
}

func (f *fakeTurnPersister) GetChatByExternalRef(_ context.Context, ref string) (*db.Chat, bool, error) {
	if c, ok := f.chatsByRef[ref]; ok {
		return c, true, nil
	}
	return nil, false, nil
}

func (f *fakeTurnPersister) PersistTurn(_ context.Context, chat *db.Chat, _, _ *db.ChatMessage, _ *db.TurnDebug) error {
	if existing, ok := f.chatsByID[chat.ID]; ok {
		// ON CONFLICT (id) DO NOTHING: turn threads onto the existing chat.
		f.turnChats = append(f.turnChats, existing.ID)
		return nil
	}
	if chat.ExternalRef != "" {
		if f.raceChat != nil && f.raceChat.ExternalRef == chat.ExternalRef {
			// Concurrent winner committed first: surface the index violation
			// and make the winner's chat visible, as Postgres would.
			winner := f.raceChat
			f.raceChat = nil
			f.chatsByRef[winner.ExternalRef] = winner
			f.chatsByID[winner.ID] = winner
			return fmt.Errorf("get-or-create chat: %w", &pgconn.PgError{Code: "23505", ConstraintName: "idx_chats_external"})
		}
		if _, taken := f.chatsByRef[chat.ExternalRef]; taken {
			return fmt.Errorf("get-or-create chat: %w", &pgconn.PgError{Code: "23505", ConstraintName: "idx_chats_external"})
		}
		f.chatsByRef[chat.ExternalRef] = chat
	}
	f.chatsByID[chat.ID] = chat
	f.turnChats = append(f.turnChats, chat.ID)
	return nil
}

func refTurn(id string) (*db.Chat, *db.ChatMessage, *db.ChatMessage) {
	chat := &db.Chat{ID: id, TenantID: "test-tenant", UserID: "u1", Status: db.ChatStatusActive}
	userMsg := &db.ChatMessage{ID: id + "-u", TenantID: "test-tenant", ChatID: id, Role: db.RoleUser}
	asstMsg := &db.ChatMessage{ID: id + "-a", TenantID: "test-tenant", ChatID: id, Role: db.RoleAssistant}
	return chat, userMsg, asstMsg
}

// TestPersistTurnWithRef_ReusesChatAcrossTurns: two turns with the same
// external_ref (e.g. two idempotency keys within one email conversation) land
// on ONE chat — no duplicate chat row.
func TestPersistTurnWithRef_ReusesChatAcrossTurns(t *testing.T) {
	fake := newFakeTurnPersister()
	ctx := context.Background()

	chat1, u1, a1 := refTurn("chat-1")
	if err := persistTurnWithRef(ctx, fake, "conv-42", chat1, u1, a1, nil); err != nil {
		t.Fatalf("first persistTurnWithRef failed: %v", err)
	}
	chat2, u2, a2 := refTurn("chat-2")
	if err := persistTurnWithRef(ctx, fake, "conv-42", chat2, u2, a2, nil); err != nil {
		t.Fatalf("second persistTurnWithRef failed: %v", err)
	}

	if len(fake.turnChats) != 2 {
		t.Fatalf("expected 2 persisted turns, got %d", len(fake.turnChats))
	}
	if fake.turnChats[0] != fake.turnChats[1] {
		t.Errorf("turns landed on different chats: %q vs %q", fake.turnChats[0], fake.turnChats[1])
	}
	if len(fake.chatsByID) != 1 {
		t.Errorf("expected exactly 1 chat row, got %d", len(fake.chatsByID))
	}
	if got := fake.chatsByID[fake.turnChats[0]].ExternalRef; got != "conv-42" {
		t.Errorf("chat created without external_ref recorded: %q", got)
	}
	// Second turn's messages must have been retargeted to the found chat.
	if u2.ChatID != fake.turnChats[0] || a2.ChatID != fake.turnChats[0] {
		t.Errorf("second turn's messages not retargeted: user=%q asst=%q want %q", u2.ChatID, a2.ChatID, fake.turnChats[0])
	}
}

// TestPersistTurnWithRef_LostCreateRaceRetries: a concurrent create of the
// same external_ref trips the partial unique index; the loser re-resolves the
// ref and retries onto the winner's chat instead of dropping the turn.
func TestPersistTurnWithRef_LostCreateRaceRetries(t *testing.T) {
	fake := newFakeTurnPersister()
	fake.raceChat = &db.Chat{ID: "winner-chat", TenantID: "test-tenant", UserID: "u0", ExternalRef: "conv-race", Status: db.ChatStatusActive}
	ctx := context.Background()

	chat, u, a := refTurn("loser-chat")
	if err := persistTurnWithRef(ctx, fake, "conv-race", chat, u, a, nil); err != nil {
		t.Fatalf("persistTurnWithRef after lost race failed: %v", err)
	}

	if len(fake.turnChats) != 1 || fake.turnChats[0] != "winner-chat" {
		t.Fatalf("expected turn to land on winner-chat, got %v", fake.turnChats)
	}
	if u.ChatID != "winner-chat" || a.ChatID != "winner-chat" {
		t.Errorf("messages not retargeted to winner: user=%q asst=%q", u.ChatID, a.ChatID)
	}
}

// TestPersistTurnWithRef_EmptyRefUnchanged: without an external_ref, behavior
// is exactly today's — PK-keyed chat id, no lookup, no ref recorded.
func TestPersistTurnWithRef_EmptyRefUnchanged(t *testing.T) {
	fake := newFakeTurnPersister()
	ctx := context.Background()

	chat, u, a := refTurn("chat-plain")
	if err := persistTurnWithRef(ctx, fake, "", chat, u, a, nil); err != nil {
		t.Fatalf("persistTurnWithRef with empty ref failed: %v", err)
	}

	if len(fake.turnChats) != 1 || fake.turnChats[0] != "chat-plain" {
		t.Fatalf("expected turn on chat-plain, got %v", fake.turnChats)
	}
	if got := fake.chatsByID["chat-plain"].ExternalRef; got != "" {
		t.Errorf("empty ref must stay empty (NULL), got %q", got)
	}
}

// ==================== request field resolution ====================

func TestIdempotencyKeyFromRequest_FirstClassWins(t *testing.T) {
	req := &pb.GenerateReplyRequest{
		IdempotencyKey: "field-key",
		Metadata:       map[string]string{"idempotency_key": "meta-key"},
	}
	if got := idempotencyKeyFromRequest(req); got != "field-key" {
		t.Errorf("expected first-class field to win, got %q", got)
	}
}

func TestExternalRefFromRequest_MetadataFallback(t *testing.T) {
	req := &pb.GenerateReplyRequest{
		Metadata: map[string]string{"external_ref": "conv-7"},
	}
	if got := externalRefFromRequest(req); got != "conv-7" {
		t.Errorf("expected metadata fallback, got %q", got)
	}
}
