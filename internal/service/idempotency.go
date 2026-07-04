package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/db"
	"github.com/ai8future/airborne/internal/redis"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
)

// Idempotent GenerateReply (addendum A10): a duplicate call carrying the same
// idempotency key must replay the byte-identical prior response instead of
// regenerating — email_ai_svc's downstream send fingerprints SHA256(body), so
// a re-generated (different) body would be rejected. This mirrors the admin
// path's Redis idempotency pattern (internal/admin/server.go handleChat): an
// atomic SetNX in-flight marker, the successful response cached for 24h, and
// failures never cached.

const (
	// idemTTL is how long a cached successful response replays (admin parity).
	idemTTL = 24 * time.Hour

	// idemInFlightTTL bounds how long an abandoned in-flight marker can block
	// retries (e.g. a crash between acquire and Put/Release).
	idemInFlightTTL = 5 * time.Minute

	// idemInFlightMarker distinguishes "processing" from a cached response.
	// The leading 0x00 byte can never appear at the start of proto.Marshal
	// output (field numbers start at 1), so the marker is unambiguous.
	idemInFlightMarker = "\x00processing"

	// idemOpTimeout bounds the post-generation Put/Release writes, which run
	// on a context detached from the request (the client may already be gone).
	idemOpTimeout = 5 * time.Second
)

// idemState is the outcome of idempotencyStore.Begin.
type idemState int

const (
	// idemBypass: no usable store (nil client or Redis unavailable) — proceed
	// uncached. The idempotency store must never fail a request.
	idemBypass idemState = iota
	// idemHit: a completed response is cached — replay it.
	idemHit
	// idemAcquired: first request for this key — the caller owns the in-flight
	// marker and must Put (success) or Release (any error).
	idemAcquired
	// idemInFlight: another request with this key is currently processing.
	idemInFlight
)

// idempotencyStore is the tenant-namespaced Redis idempotency cache for the
// gRPC generate path. Keys are "idem:{tenant}:{key}" so one tenant can never
// replay another tenant's response.
type idempotencyStore struct {
	client *redis.Client
}

// newIdempotencyStore wraps the Redis client; a nil client yields a nil store,
// which the handler treats as "idempotency disabled" (today's behavior).
func newIdempotencyStore(client *redis.Client) *idempotencyStore {
	if client == nil {
		return nil
	}
	return &idempotencyStore{client: client}
}

func (s *idempotencyStore) redisKey(tenantID, key string) string {
	return fmt.Sprintf("idem:%s:%s", tenantID, key)
}

// Begin runs the pre-dispatch idempotency check. It atomically acquires the
// in-flight marker (SetNX); if the key already exists it is either a completed
// response (replayed) or a concurrent request (in-flight). Redis errors always
// degrade to idemBypass — never fail the request because the store is down.
func (s *idempotencyStore) Begin(ctx context.Context, tenantID, key string) (*pb.GenerateReplyResponse, idemState) {
	rkey := s.redisKey(tenantID, key)

	acquired, err := s.client.SetNX(ctx, rkey, idemInFlightMarker, idemInFlightTTL)
	if err != nil {
		slog.Warn("idempotency store unavailable, proceeding uncached", "error", err, "tenant_id", tenantID)
		return nil, idemBypass
	}
	if acquired {
		return nil, idemAcquired
	}

	val, err := s.client.Get(ctx, rkey)
	if err != nil {
		if redis.IsNil(err) {
			// Marker expired/released between SetNX and Get; treat as uncached
			// rather than looping on acquire.
			return nil, idemBypass
		}
		slog.Warn("idempotency lookup failed, proceeding uncached", "error", err, "tenant_id", tenantID)
		return nil, idemBypass
	}
	if val == idemInFlightMarker {
		return nil, idemInFlight
	}

	var resp pb.GenerateReplyResponse
	if err := proto.Unmarshal([]byte(val), &resp); err != nil {
		slog.Warn("cached idempotent response unreadable, regenerating", "error", err, "tenant_id", tenantID)
		return nil, idemBypass
	}
	return &resp, idemHit
}

// Put caches the serialized successful response for 24h, overwriting the
// in-flight marker. Replay unmarshals these exact bytes, so the replayed
// response re-serializes byte-identically. Best-effort: on failure the marker
// is released so retries are not locked out for idemInFlightTTL.
func (s *idempotencyStore) Put(ctx context.Context, tenantID, key string, resp *pb.GenerateReplyResponse) {
	data, err := proto.Marshal(resp)
	if err != nil {
		slog.Warn("failed to marshal response for idempotency cache", "error", err, "tenant_id", tenantID)
		s.Release(ctx, tenantID, key)
		return
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), idemOpTimeout)
	defer cancel()
	if err := s.client.Set(opCtx, s.redisKey(tenantID, key), data, idemTTL); err != nil {
		slog.Warn("failed to cache idempotent response", "error", err, "tenant_id", tenantID)
		s.Release(ctx, tenantID, key)
	}
}

// Release drops the in-flight marker after a failed generation so the retry
// regenerates — provider errors are never cached. Runs on a detached context
// (the failure that got us here may be a cancelled request context).
func (s *idempotencyStore) Release(ctx context.Context, tenantID, key string) {
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), idemOpTimeout)
	defer cancel()
	if err := s.client.Del(opCtx, s.redisKey(tenantID, key)); err != nil {
		slog.Warn("failed to release idempotency marker", "error", err, "tenant_id", tenantID)
	}
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
