package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/db"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/redis"
	"github.com/ai8future/airborne/internal/service/config"
	"github.com/ai8future/airborne/internal/tenant"
	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgconn"
	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// newIdemTestService builds a ChatService with mock providers and a
// miniredis-backed idempotency store. Returns the service, the openai mock
// (whose generateCalls slice is the generation counter), and the miniredis
// handle so tests can simulate Redis outages.
func newIdemTestService(t *testing.T, retention ...time.Duration) (*ChatService, *mockProvider, *miniredis.Miniredis) {
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
	completedRetention := defaultIdemCompletedRetention
	if len(retention) > 0 {
		completedRetention = retention[0]
	}
	svc := &ChatService{
		openaiProvider:    mockOpenAI,
		geminiProvider:    newMockProvider("gemini"),
		anthropicProvider: newMockProvider("anthropic"),
		configBuilder:     config.NewBuilder(),
		idem:              newIdempotencyStore(redisClient, completedRetention),
	}
	return svc, mockOpenAI, mr
}

type fakeIdempotencyClient struct {
	values        map[string]string
	beginErr      error
	readErr       error
	completionErr error
	releaseErr    error
	forceExisting bool
	// replaceBeforeComplete simulates expiry/eviction/failover followed by a
	// replacement acquisition immediately before the stale owner completes.
	replaceBeforeComplete string
}

type deadlineRecordingProvider struct {
	*mockProvider
	deadline    time.Time
	hasDeadline bool
}

func (p *deadlineRecordingProvider) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
	p.deadline, p.hasDeadline = ctx.Deadline()
	return p.mockProvider.GenerateReply(ctx, params)
}

func newFakeIdempotencyClient() *fakeIdempotencyClient {
	return &fakeIdempotencyClient{values: make(map[string]string)}
}

func (f *fakeIdempotencyClient) SetNX(_ context.Context, key string, value interface{}, _ time.Duration) (bool, error) {
	if f.beginErr != nil {
		return false, f.beginErr
	}
	if f.forceExisting {
		return false, nil
	}
	if _, exists := f.values[key]; exists {
		return false, nil
	}
	f.values[key] = fmt.Sprint(value)
	return true, nil
}

func (f *fakeIdempotencyClient) Get(_ context.Context, key string) (string, error) {
	if f.readErr != nil {
		return "", f.readErr
	}
	value, ok := f.values[key]
	if !ok {
		return "", goredis.Nil
	}
	return value, nil
}

func (f *fakeIdempotencyClient) CompareAndSet(_ context.Context, key, expected string, value interface{}, _ time.Duration) (bool, error) {
	if f.completionErr != nil {
		return false, f.completionErr
	}
	if f.replaceBeforeComplete != "" {
		f.values[key] = f.replaceBeforeComplete
		f.replaceBeforeComplete = ""
	}
	if f.values[key] != expected {
		return false, nil
	}
	switch value := value.(type) {
	case []byte:
		f.values[key] = string(value)
	default:
		f.values[key] = fmt.Sprint(value)
	}
	return true, nil
}

func (f *fakeIdempotencyClient) CompareAndDelete(_ context.Context, key, expected string) (bool, error) {
	if f.releaseErr != nil {
		return false, f.releaseErr
	}
	if f.values[key] != expected {
		return false, nil
	}
	delete(f.values, key)
	return true, nil
}

func newIdemServiceWithClient(client idempotencyClient) (*ChatService, *mockProvider) {
	mockOpenAI := newMockProvider("openai")
	return &ChatService{
		openaiProvider:    mockOpenAI,
		geminiProvider:    newMockProvider("gemini"),
		anthropicProvider: newMockProvider("anthropic"),
		configBuilder:     config.NewBuilder(),
		idem: &idempotencyStore{
			client:             client,
			completedRetention: defaultIdemCompletedRetention,
		},
	}, mockOpenAI
}

func errorInfoDetail(err error) *errdetails.ErrorInfo {
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	return nil
}

func errorInfoReason(err error) string {
	if info := errorInfoDetail(err); info != nil {
		return info.Reason
	}
	return ""
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

// A delimiter-joined namespace maps both pairs below to
// "idem:tenant:a:b:c". Tenant and caller key boundaries must remain distinct
// even when both values contain the delimiter.
func TestGenerateReply_TenantAndKeyDelimiterPairsDoNotCollide(t *testing.T) {
	svc, mock, _ := newIdemTestService(t)

	if _, err := svc.GenerateReply(tenantCtx("tenant:a"), &pb.GenerateReplyRequest{UserInput: "first", IdempotencyKey: "b:c"}); err != nil {
		t.Fatalf("first delimiter pair failed: %v", err)
	}
	if _, err := svc.GenerateReply(tenantCtx("tenant:a:b"), &pb.GenerateReplyRequest{UserInput: "second", IdempotencyKey: "c"}); err != nil {
		t.Fatalf("second delimiter pair failed: %v", err)
	}

	if got := len(mock.generateCalls); got != 2 {
		t.Fatalf("provider calls = %d, want 2 for distinct tenant/key pairs", got)
	}
}

func TestIdempotencyRedisNamespaceIsVersionedBoundedAndUnambiguous(t *testing.T) {
	store := &idempotencyStore{}
	first := store.redisKey("tenant:a", "b:c")
	second := store.redisKey("tenant:a:b", "c")
	if first == second {
		t.Fatal("delimiter-bearing tenant/key pairs produced the same Redis key")
	}
	if !strings.HasPrefix(first, idemRedisKeyPrefix) {
		t.Fatalf("Redis key %q lacks versioned namespace %q", first, idemRedisKeyPrefix)
	}
	if got, want := len(first), len(idemRedisKeyPrefix)+sha256.Size*2; got != want {
		t.Fatalf("Redis key length = %d, want fixed length %d", got, want)
	}
	if strings.Contains(first, "tenant:a") || strings.Contains(first, "b:c") {
		t.Fatal("Redis key exposes raw tenant or caller key")
	}
}

func TestGenerateReply_InvalidIdempotencyKeysAreRejectedBeforeDispatch(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		fromMetadata bool
	}{
		{name: "space", key: "eai1_bad key"},
		{name: "control", key: "eai1_bad\nkey"},
		{name: "non ascii", key: "eai1_café"},
		{name: "too long", key: strings.Repeat("a", 256)},
		{name: "metadata control", key: "eai1_bad\tkey", fromMetadata: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, mock, mr := newIdemTestService(t)
			req := &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: tt.key}
			if tt.fromMetadata {
				req.IdempotencyKey = ""
				req.Metadata = map[string]string{"idempotency_key": tt.key}
			}
			_, err := svc.GenerateReply(tenantCtx("test-tenant"), req)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want %v (err=%v)", status.Code(err), codes.InvalidArgument, err)
			}
			if got := len(mock.generateCalls); got != 0 {
				t.Fatalf("provider calls = %d, want 0", got)
			}
			if keys := mr.Keys(); len(keys) != 0 {
				t.Fatalf("Redis keys = %v, want none", keys)
			}
		})
	}
}

func TestGenerateReply_EmailAIKeyContractReplays(t *testing.T) {
	svc, mock, _ := newIdemTestService(t)
	ctx := tenantCtx("test-tenant")
	key := "eai1_01JZX8YQ0PZQH1WQF6V6R7R8S9"

	first, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: key})
	if err != nil {
		t.Fatalf("first eai1_ request failed: %v", err)
	}
	second, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: key})
	if err != nil {
		t.Fatalf("replayed eai1_ request failed: %v", err)
	}
	if got := len(mock.generateCalls); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
	if !bytes.Equal(mustMarshal(t, first), mustMarshal(t, second)) {
		t.Fatal("eai1_ replay was not byte-identical")
	}
}

// TestGenerateReply_InFlightDuplicateAborted: while a key's first request is
// still processing (in-flight marker held), a duplicate is rejected with
// codes.Aborted rather than double-generating — mirroring the admin path's
// 409 Conflict.
func TestGenerateReply_InFlightDuplicateAborted(t *testing.T) {
	svc, mock, _ := newIdemTestService(t)
	ctx := tenantCtx("test-tenant")

	// Simulate an in-flight first request by acquiring and retaining its claim.
	_, state, claim, err := svc.idem.Begin(ctx, "test-tenant", "key-busy")
	if err != nil || state != idemAcquired || claim == nil {
		t.Fatalf("Begin = state %v, claim %v, err %v; want acquired claim", state, claim, err)
	}

	_, err = svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-busy"})
	if status.Code(err) != codes.Aborted {
		t.Fatalf("expected codes.Aborted for in-flight duplicate, got %v (err=%v)", status.Code(err), err)
	}
	if got := len(mock.generateCalls); got != 0 {
		t.Errorf("expected 0 provider generations for in-flight duplicate, got %d", got)
	}
}

// TestGenerateReply_RedisDownFailsClosed: a keyed request must never dispatch
// when its idempotency prerequisite cannot be read.
func TestGenerateReply_RedisDownFailsClosed(t *testing.T) {
	svc, mock, mr := newIdemTestService(t)
	ctx := tenantCtx("test-tenant")
	mr.Close() // Redis goes down before any call

	_, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-1"})
	if status.Code(err) != codes.Unavailable || errorInfoReason(err) != idempotencyUnavailableReason {
		t.Fatalf("Redis-down error = %v, reason = %q", err, errorInfoReason(err))
	}

	if got := len(mock.generateCalls); got != 0 {
		t.Errorf("expected 0 provider generations with Redis down, got %d", got)
	}
}

// TestGenerateReply_NoIdemStoreFailsClosed: keyed generation requires a live
// idempotency store even though unkeyed generation remains supported.
func TestGenerateReply_NoIdemStoreFailsClosed(t *testing.T) {
	mock := newMockProvider("openai")
	svc := &ChatService{
		openaiProvider:    mock,
		geminiProvider:    newMockProvider("gemini"),
		anthropicProvider: newMockProvider("anthropic"),
		configBuilder:     config.NewBuilder(),
		idem:              newIdempotencyStore(nil, defaultIdemCompletedRetention),
	}
	ctx := tenantCtx("test-tenant")

	_, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-1"})
	if status.Code(err) != codes.Unavailable || errorInfoReason(err) != idempotencyUnavailableReason {
		t.Fatalf("missing-store error = %v, reason = %q", err, errorInfoReason(err))
	}
	if got := len(mock.generateCalls); got != 0 {
		t.Errorf("expected 0 provider generations without idem store, got %d", got)
	}

	if _, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "unkeyed"}); err != nil {
		t.Fatalf("unkeyed GenerateReply without idem store failed: %v", err)
	}
	if got := len(mock.generateCalls); got != 1 {
		t.Errorf("expected the unkeyed request alone to generate, got %d calls", got)
	}
}

func TestGenerateReply_IdempotencyBeginFailuresAreTypedPreDispatch(t *testing.T) {
	redisKey := (&idempotencyStore{}).redisKey("test-tenant", "key-1")
	tests := []struct {
		name   string
		client *fakeIdempotencyClient
	}{
		{
			name: "begin failure",
			client: &fakeIdempotencyClient{
				values:   make(map[string]string),
				beginErr: errors.New("setnx failed"),
			},
		},
		{
			name: "read failure",
			client: &fakeIdempotencyClient{
				values:        make(map[string]string),
				forceExisting: true,
				readErr:       errors.New("get failed"),
			},
		},
		{
			name: "marker disappeared",
			client: &fakeIdempotencyClient{
				values:        make(map[string]string),
				forceExisting: true,
			},
		},
		{
			name: "invalid cached marker",
			client: &fakeIdempotencyClient{
				values: map[string]string{redisKey: "\xff"},
			},
		},
		{
			name: "invalid cached payload",
			client: &fakeIdempotencyClient{
				values: map[string]string{redisKey: idemCompletedMarker + "\xff"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, mock := newIdemServiceWithClient(tt.client)
			_, err := svc.GenerateReply(tenantCtx("test-tenant"), &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-1"})
			if status.Code(err) != codes.Unavailable {
				t.Fatalf("code = %v, want %v (err=%v)", status.Code(err), codes.Unavailable, err)
			}
			if reason := errorInfoReason(err); reason != idempotencyUnavailableReason {
				t.Fatalf("ErrorInfo reason = %q, want %q", reason, idempotencyUnavailableReason)
			}
			if info := errorInfoDetail(err); info == nil || info.Metadata["dispatch_phase"] != "pre_dispatch" {
				t.Fatalf("ErrorInfo metadata = %#v, want pre_dispatch", info)
			}
			if got := len(mock.generateCalls); got != 0 {
				t.Fatalf("provider calls = %d, want 0", got)
			}
		})
	}
}

func TestGenerateReply_CanceledBeginIsNotClassifiedAsStorageUnavailable(t *testing.T) {
	client := newFakeIdempotencyClient()
	client.beginErr = context.Canceled
	svc, mock := newIdemServiceWithClient(client)
	ctx, cancel := context.WithCancel(tenantCtx("test-tenant"))
	cancel()

	_, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "eai1_cancelled"})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("code = %v, want %v (err=%v)", status.Code(err), codes.Canceled, err)
	}
	if reason := errorInfoReason(err); reason == idempotencyUnavailableReason {
		t.Fatalf("caller cancellation was marked as a storage failure: %v", err)
	}
	if got := len(mock.generateCalls); got != 0 {
		t.Fatalf("provider calls = %d, want 0", got)
	}
}

func TestGenerateReply_CompletionFailureIsAmbiguousAndRetainsMarker(t *testing.T) {
	client := newFakeIdempotencyClient()
	client.completionErr = errors.New("completion set failed")
	svc, mock := newIdemServiceWithClient(client)
	ctx := tenantCtx("test-tenant")
	req := &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-ambiguous"}

	resp, err := svc.GenerateReply(ctx, req)
	if resp != nil {
		t.Fatalf("completion failure returned successful response: %v", resp)
	}
	if status.Code(err) != codes.DataLoss || errorInfoReason(err) != idempotencyCompletionAmbiguousReason {
		t.Fatalf("completion error = %v, reason = %q", err, errorInfoReason(err))
	}
	if info := errorInfoDetail(err); info == nil || info.Metadata["retry_disposition"] != "quarantine" || info.Metadata["dispatch_phase"] != "post_dispatch" {
		t.Fatalf("completion ErrorInfo metadata = %#v, want post-dispatch quarantine", info)
	}
	redisKey := svc.idem.redisKey("test-tenant", "key-ambiguous")
	if got := client.values[redisKey]; !isIdemInFlightValue(got) {
		t.Fatalf("marker after completion failure is not an owned in-flight marker")
	}

	_, retryErr := svc.GenerateReply(ctx, req)
	if status.Code(retryErr) != codes.Aborted {
		t.Fatalf("automatic retry code = %v, want %v (err=%v)", status.Code(retryErr), codes.Aborted, retryErr)
	}
	if got := len(mock.generateCalls); got != 1 {
		t.Fatalf("provider calls after automatic retry = %d, want 1", got)
	}
}

func TestGenerateReply_CompletionOwnershipLossIsAmbiguousAndDoesNotRegenerate(t *testing.T) {
	replacementToken, err := newIdempotencyOwnerToken()
	if err != nil {
		t.Fatalf("newIdempotencyOwnerToken: %v", err)
	}
	replacementMarker := idemInFlightMarker + replacementToken
	client := newFakeIdempotencyClient()
	client.replaceBeforeComplete = replacementMarker
	svc, mock := newIdemServiceWithClient(client)
	ctx := tenantCtx("test-tenant")
	req := &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-owner-lost"}

	resp, err := svc.GenerateReply(ctx, req)
	if resp != nil {
		t.Fatalf("ownership loss returned successful response: %v", resp)
	}
	if status.Code(err) != codes.DataLoss || errorInfoReason(err) != idempotencyCompletionAmbiguousReason {
		t.Fatalf("completion error = %v, reason = %q", err, errorInfoReason(err))
	}
	redisKey := svc.idem.redisKey("test-tenant", "key-owner-lost")
	if got := client.values[redisKey]; got != replacementMarker {
		t.Fatal("stale completion overwrote the replacement owner marker")
	}

	_, retryErr := svc.GenerateReply(ctx, req)
	if status.Code(retryErr) != codes.Aborted {
		t.Fatalf("automatic retry code = %v, want %v (err=%v)", status.Code(retryErr), codes.Aborted, retryErr)
	}
	if got := len(mock.generateCalls); got != 1 {
		t.Fatalf("provider calls after ownership loss and retry = %d, want 1", got)
	}
}

func TestIdempotencyStaleOwnerCannotReleaseReplacementAfterExpiry(t *testing.T) {
	svc, _, mr := newIdemTestService(t)
	store := svc.idem
	ctx := context.Background()
	const tenantID = "tenant:with:delimiter"
	const key = "eai1_owner-release:test"

	_, state, stale, err := store.Begin(ctx, tenantID, key)
	if err != nil || state != idemAcquired || stale == nil {
		t.Fatalf("first Begin = state %v, claim %v, err %v", state, stale, err)
	}
	mr.FastForward(idemInFlightTTL)
	if mr.Exists(stale.redisKey) {
		t.Fatal("first in-flight marker did not expire")
	}
	_, state, replacement, err := store.Begin(ctx, tenantID, key)
	if err != nil || state != idemAcquired || replacement == nil {
		t.Fatalf("replacement Begin = state %v, claim %v, err %v", state, replacement, err)
	}
	if stale.ownerToken == replacement.ownerToken {
		t.Fatal("separate acquisitions reused an owner token")
	}

	store.Release(ctx, stale)
	got, err := mr.Get(replacement.redisKey)
	if err != nil {
		t.Fatalf("replacement marker missing after stale release: %v", err)
	}
	if got != replacement.marker() {
		t.Fatal("stale release changed the replacement owner marker")
	}
}

func TestIdempotencyStaleOwnerCannotCompleteOverReplacementAfterExpiry(t *testing.T) {
	svc, _, mr := newIdemTestService(t)
	store := svc.idem
	ctx := context.Background()
	const tenantID = "tenant:with:delimiter"
	const key = "eai1_owner-complete:test"

	_, state, stale, err := store.Begin(ctx, tenantID, key)
	if err != nil || state != idemAcquired || stale == nil {
		t.Fatalf("first Begin = state %v, claim %v, err %v", state, stale, err)
	}
	mr.FastForward(idemInFlightTTL)
	_, state, replacement, err := store.Begin(ctx, tenantID, key)
	if err != nil || state != idemAcquired || replacement == nil {
		t.Fatalf("replacement Begin = state %v, claim %v, err %v", state, replacement, err)
	}

	err = store.Put(ctx, stale, &pb.GenerateReplyResponse{Text: "stale response"})
	if err == nil {
		t.Fatal("stale completion unexpectedly proved ownership")
	}
	got, getErr := mr.Get(replacement.redisKey)
	if getErr != nil {
		t.Fatalf("replacement marker missing after stale completion: %v", getErr)
	}
	if got != replacement.marker() {
		t.Fatal("stale completion overwrote the replacement owner marker")
	}
}

func TestIdempotencyRetentionAndLeaseSafety(t *testing.T) {
	retention := 72 * time.Hour
	svc, mock, mr := newIdemTestService(t, retention)
	deadlineProvider := &deadlineRecordingProvider{mockProvider: mock}
	svc.openaiProvider = deadlineProvider
	ctx := tenantCtx("test-tenant")
	started := time.Now()
	if _, err := svc.GenerateReply(ctx, &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "key-retention"}); err != nil {
		t.Fatalf("GenerateReply failed: %v", err)
	}
	if got := mr.TTL(svc.idem.redisKey("test-tenant", "key-retention")); got != retention {
		t.Fatalf("completed response TTL = %s, want %s", got, retention)
	}
	if idemInFlightTTL <= maximumKeyedGenerationDuration {
		t.Fatalf("in-flight lease %s must exceed maximum keyed generation duration %s", idemInFlightTTL, maximumKeyedGenerationDuration)
	}
	if !deadlineProvider.hasDeadline {
		t.Fatal("keyed provider context has no maximum generation deadline")
	}
	if got := deadlineProvider.deadline.Sub(started); got > maximumKeyedGenerationDuration+time.Second || got < maximumKeyedGenerationDuration-time.Second {
		t.Fatalf("keyed generation deadline = %s from start, want approximately %s", got, maximumKeyedGenerationDuration)
	}
}

func TestGenerateReply_ProviderExhaustionIsNotPreDispatchSafe(t *testing.T) {
	svc, mock, _ := newIdemTestService(t)
	mock.generateErr = status.Error(codes.ResourceExhausted, "provider exhausted")
	_, err := svc.GenerateReply(tenantCtx("test-tenant"), &pb.GenerateReplyRequest{UserInput: "hello", IdempotencyKey: "provider-exhausted"})
	if got := len(mock.generateCalls); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
	if reason := errorInfoReason(err); reason == idempotencyUnavailableReason {
		t.Fatalf("post-dispatch provider failure was incorrectly marked safe: %v", err)
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
