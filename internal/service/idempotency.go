package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/db"
	"github.com/ai8future/airborne/internal/redis"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Idempotent GenerateReply (addendum A10): a duplicate call carrying the same
// idempotency key must replay the byte-identical prior response instead of
// regenerating — email_ai_svc's downstream send fingerprints SHA256(body), so
// a re-generated (different) body would be rejected. This mirrors the admin
// path's Redis idempotency pattern (internal/admin/server.go handleChat): an
// atomic SetNX in-flight marker, a durably acknowledged completed response,
// and failures never cached.

const (
	defaultIdemCompletedRetention = 48 * time.Hour
	maxIdempotencyKeyBytes        = 255
	idemOwnerTokenBytes           = 32

	// Redis keys are fixed-size, versioned hashes over length-prefixed tenant
	// and caller-key components. The length prefixes preserve component
	// boundaries before hashing, so delimiter-bearing pairs cannot alias.
	idemRedisKeyPrefix = "airborne:idem:v1:"
	idemRedisKeyDomain = "airborne/idempotency/v1\x00"

	// maximumKeyedGenerationDuration is enforced around the entire keyed path.
	// Provider attempts are individually capped at 3m, except Anthropic
	// extended thinking at 15m; one sequential failover can therefore consume
	// at most 18m. The outer deadline also bounds validation/RAG overhead.
	maximumKeyedGenerationDuration = 18 * time.Minute

	// idemInFlightTTL bounds how long an abandoned in-flight marker can block
	// retries (e.g. a crash between acquire and Put/Release). It MUST exceed
	// the worst-case generation time: if the marker expires mid-generation, a
	// concurrent duplicate SetNX-acquires it and regenerates — reopening the
	// double-generate window this feature closes. The codebase's per-attempt
	// ceilings (imposed by retry.EnsureTimeout when the caller set no gRPC
	// deadline) are retry.RequestTimeout = 3m (internal/retry/defaults.go) for
	// all providers, except Anthropic extended-thinking attempts at
	// thinkingTimeout = 15m (internal/provider/anthropic/client.go); failover
	// adds one sequential fallback attempt (+3m), so worst case is 18m.
	// 20m exceeds the enforced outer ceiling and leaves two minutes for the
	// completion-store write before the marker can expire.
	idemInFlightTTL = 20 * time.Minute

	// idemInFlightMarker distinguishes "processing" from a cached response.
	// Completed values use a separate marker before their protobuf payload so
	// arbitrary Redis bytes are never mistaken for a valid cached response.
	idemInFlightMarker  = "\x00processing:"
	idemCompletedMarker = "\x00completed:"

	// idemOpTimeout bounds the post-generation Put/Release writes, which run
	// on a context detached from the request (the client may already be gone).
	idemOpTimeout = 5 * time.Second

	idempotencyUnavailableReason         = "idempotency_unavailable"
	idempotencyCompletionAmbiguousReason = "idempotency_completion_ambiguous"
	idempotencyErrorDomain               = "airborne.ai8future.com"
)

var errIdempotencyOwnerTokenGeneration = errors.New("idempotency owner token generation failed")

// idemState is the outcome of idempotencyStore.Begin.
type idemState int

const (
	idemUnknown idemState = iota
	// idemHit: a completed response is cached — replay it.
	idemHit
	// idemAcquired: first request for this key — the caller owns the in-flight
	// marker and must Put (success) or Release (any error).
	idemAcquired
	// idemInFlight: another request with this key is currently processing.
	idemInFlight
)

type idempotencyClient interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)
	Get(ctx context.Context, key string) (string, error)
	CompareAndSet(ctx context.Context, key, expected string, value interface{}, expiration time.Duration) (bool, error)
	CompareAndDelete(ctx context.Context, key, expected string) (bool, error)
}

// idempotencyStore is the tenant-namespaced Redis idempotency cache for the
// gRPC generate path.
type idempotencyStore struct {
	client             idempotencyClient
	completedRetention time.Duration
}

// idempotencyClaim proves ownership of one acquired in-flight marker. It is
// carried across generation and required for both completion and release.
type idempotencyClaim struct {
	redisKey   string
	tenantID   string
	ownerToken string
}

func (c *idempotencyClaim) marker() string {
	return idemInFlightMarker + c.ownerToken
}

// newIdempotencyStore wraps the Redis client. A nil client yields a nil store;
// keyed handlers fail closed while unkeyed handlers remain unaffected.
func newIdempotencyStore(client *redis.Client, completedRetention time.Duration) *idempotencyStore {
	if client == nil {
		return nil
	}
	if completedRetention <= 0 {
		completedRetention = defaultIdemCompletedRetention
	}
	return &idempotencyStore{client: client, completedRetention: completedRetention}
}

func (s *idempotencyStore) redisKey(tenantID, key string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(idemRedisKeyDomain))
	writeLengthPrefixed(h, tenantID)
	writeLengthPrefixed(h, key)
	return fmt.Sprintf("%s%x", idemRedisKeyPrefix, h.Sum(nil))
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeLengthPrefixed(dst byteWriter, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = dst.Write(size[:])
	_, _ = dst.Write([]byte(value))
}

func newIdempotencyOwnerToken() (string, error) {
	raw := make([]byte, idemOwnerTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("%w: %v", errIdempotencyOwnerTokenGeneration, err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func isIdemInFlightValue(value string) bool {
	if !strings.HasPrefix(value, idemInFlightMarker) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, idemInFlightMarker))
	return err == nil && len(raw) == idemOwnerTokenBytes
}

// Begin runs the pre-dispatch idempotency check. It atomically acquires the
// in-flight marker (SetNX); if the key already exists it is either a completed
// response (replayed) or a concurrent request (in-flight). Any storage/read or
// decode uncertainty is returned so the handler can fail closed pre-dispatch.
func (s *idempotencyStore) Begin(ctx context.Context, tenantID, key string) (*pb.GenerateReplyResponse, idemState, *idempotencyClaim, error) {
	rkey := s.redisKey(tenantID, key)
	ownerToken, err := newIdempotencyOwnerToken()
	if err != nil {
		return nil, idemUnknown, nil, err
	}
	claim := &idempotencyClaim{redisKey: rkey, tenantID: tenantID, ownerToken: ownerToken}

	acquired, err := s.client.SetNX(ctx, rkey, claim.marker(), idemInFlightTTL)
	if err != nil {
		return nil, idemUnknown, nil, fmt.Errorf("acquire idempotency marker: %w", err)
	}
	if acquired {
		return nil, idemAcquired, claim, nil
	}

	val, err := s.client.Get(ctx, rkey)
	if err != nil {
		if redis.IsNil(err) {
			return nil, idemUnknown, nil, fmt.Errorf("idempotency marker disappeared during begin: %w", err)
		}
		return nil, idemUnknown, nil, fmt.Errorf("read idempotency record: %w", err)
	}
	if isIdemInFlightValue(val) {
		return nil, idemInFlight, nil, nil
	}
	if !strings.HasPrefix(val, idemCompletedMarker) {
		return nil, idemUnknown, nil, fmt.Errorf("invalid cached idempotency marker")
	}

	var resp pb.GenerateReplyResponse
	if err := proto.Unmarshal([]byte(strings.TrimPrefix(val, idemCompletedMarker)), &resp); err != nil {
		return nil, idemUnknown, nil, fmt.Errorf("decode cached idempotent response: %w", err)
	}
	return &resp, idemHit, nil, nil
}

// Put caches the serialized successful response for the configured retention.
// A successful return requires Redis to acknowledge the completed bytes.
// Failures never mutate the record; an owned marker remains quarantined, and
// a replacement owner's marker remains untouched.
func (s *idempotencyStore) Put(ctx context.Context, claim *idempotencyClaim, resp *pb.GenerateReplyResponse) error {
	if claim == nil {
		return fmt.Errorf("complete idempotency claim: missing ownership")
	}
	data, err := proto.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal idempotent response: %w", err)
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), idemOpTimeout)
	defer cancel()
	completedValue := append([]byte(idemCompletedMarker), data...)
	completed, err := s.client.CompareAndSet(opCtx, claim.redisKey, claim.marker(), completedValue, s.completedRetention)
	if err != nil {
		return fmt.Errorf("store completed idempotent response: %w", err)
	}
	if !completed {
		return fmt.Errorf("store completed idempotent response: ownership not proven")
	}
	return nil
}

// Release drops the in-flight marker after a failed generation so the retry
// regenerates — provider errors are never cached. Runs on a detached context
// (the failure that got us here may be a cancelled request context).
func (s *idempotencyStore) Release(ctx context.Context, claim *idempotencyClaim) {
	if claim == nil {
		slog.Warn("failed to release idempotency marker: missing ownership")
		return
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), idemOpTimeout)
	defer cancel()
	released, err := s.client.CompareAndDelete(opCtx, claim.redisKey, claim.marker())
	if err != nil {
		slog.Warn("failed to release idempotency marker", "error", err, "tenant_id", claim.tenantID)
		return
	}
	if !released {
		slog.Warn("idempotency marker ownership lost before release", "tenant_id", claim.tenantID)
	}
}

func validateIdempotencyKey(key string) error {
	if len(key) > maxIdempotencyKeyBytes {
		return fmt.Errorf("idempotency_key must be at most %d bytes", maxIdempotencyKeyBytes)
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return fmt.Errorf("idempotency_key must contain only visible ASCII characters")
		}
	}
	return nil
}

func idempotencyUnavailableError() error {
	return idempotencyDetailedError(
		codes.Unavailable,
		idempotencyUnavailableReason,
		"idempotency storage unavailable before provider dispatch",
		map[string]string{
			"dispatch_phase":    "pre_dispatch",
			"retry_disposition": "retry_after_storage_recovery",
		},
	)
}

func idempotencyCompletionAmbiguousError() error {
	return idempotencyDetailedError(
		codes.DataLoss,
		idempotencyCompletionAmbiguousReason,
		"generation completed but its idempotent response could not be cached; quarantine this key",
		map[string]string{
			"dispatch_phase":    "post_dispatch",
			"retry_disposition": "quarantine",
		},
	)
}

func idempotencyDetailedError(code codes.Code, reason, message string, metadata map[string]string) error {
	base := status.New(code, message)
	detailed, err := base.WithDetails(&errdetails.ErrorInfo{
		Reason:   reason,
		Domain:   idempotencyErrorDomain,
		Metadata: metadata,
	})
	if err != nil {
		slog.Error("failed to attach idempotency gRPC error detail", "reason", reason, "error", err)
		return base.Err()
	}
	return detailed.Err()
}

// idempotencyKeyFromRequest resolves the dedup key: first-class field first,
// metadata["idempotency_key"] fallback (zero-client-change path).
func idempotencyKeyFromRequest(req *pb.GenerateReplyRequest) string {
	if key := req.GetIdempotencyKey(); key != "" {
		return key
	}
	return req.GetMetadata()["idempotency_key"]
}

// externalRefFromRequest resolves the opaque chat correlation id: first-class
// field first, metadata["external_ref"] fallback.
func externalRefFromRequest(req *pb.GenerateReplyRequest) string {
	if ref := req.GetExternalRef(); ref != "" {
		return ref
	}
	return req.GetMetadata()["external_ref"]
}

// ---------------------------------------------------------------------------
// external_ref-aware turn persistence (A10 Step 3 tail, on top of A9).
// ---------------------------------------------------------------------------

// turnPersister is the slice of *db.Repository that persistTurnWithRef needs;
// an interface so the resolution logic is unit-testable with a focused fake.
type turnPersister interface {
	GetChatByExternalRef(ctx context.Context, externalRef string) (*db.Chat, bool, error)
	PersistTurn(ctx context.Context, chat *db.Chat, userMsg, assistantMsg *db.ChatMessage, debug *db.TurnDebug) error
}

// persistTurnWithRef persists one turn, landing it on the chat identified by
// the caller's external_ref when one is supplied:
//
//   - ref == "": today's behavior, unchanged — PersistTurn onto chat.ID
//     (PK-keyed thread id), external_ref stays NULL.
//   - ref != "", chat exists: the turn threads onto the FOUND chat's id.
//     PersistTurn's INSERT ... ON CONFLICT (id) DO NOTHING no-ops, so no
//     second chat row is ever created for a ref that already exists.
//   - ref != "", no chat: chat keeps its fresh id and the INSERT records
//     external_ref, creating the correlated chat.
//
// Two concurrent first turns can both miss the lookup; the loser's INSERT then
// trips the partial unique index on (tenant_id, external_ref). That path is
// handled deliberately: on unique violation the ref is re-resolved and the
// turn retried once onto the winner's chat instead of being dropped.
func persistTurnWithRef(ctx context.Context, repo turnPersister, externalRef string, chat *db.Chat, userMsg, asstMsg *db.ChatMessage, debug *db.TurnDebug) error {
	if externalRef == "" {
		return repo.PersistTurn(ctx, chat, userMsg, asstMsg, debug)
	}

	chat.ExternalRef = externalRef
	existing, found, err := repo.GetChatByExternalRef(ctx, externalRef)
	if err != nil {
		return fmt.Errorf("resolve external_ref: %w", err)
	}
	if found {
		retargetTurn(chat, userMsg, asstMsg, existing.ID)
	}

	err = repo.PersistTurn(ctx, chat, userMsg, asstMsg, debug)
	if err == nil || !isUniqueViolation(err) {
		return err
	}

	// Lost the create race: another request committed this external_ref first.
	existing, found, lookupErr := repo.GetChatByExternalRef(ctx, externalRef)
	if lookupErr != nil || !found {
		return err // surface the original violation; the retry path is unavailable
	}
	retargetTurn(chat, userMsg, asstMsg, existing.ID)
	return repo.PersistTurn(ctx, chat, userMsg, asstMsg, debug)
}

// retargetTurn points a prepared turn at an existing chat id (the external_ref
// owner) instead of the fresh PK-keyed id it was built with.
func retargetTurn(chat *db.Chat, userMsg, asstMsg *db.ChatMessage, chatID string) {
	chat.ID = chatID
	userMsg.ChatID = chatID
	asstMsg.ChatID = chatID
}

// isUniqueViolation reports whether err is a Postgres unique_violation (23505),
// e.g. the partial unique index on airborne_chats(tenant_id, external_ref).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
