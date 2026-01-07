package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/cliffpyles/aibox/internal/redis"
)

const (
	rateLimitPrefix = "aibox:ratelimit:"
)

// rateLimitScript is a Lua script for atomic rate limiting
// It increments the counter and sets TTL atomically, returning the new count
const rateLimitScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])

local current = redis.call('INCR', key)
if current == 1 then
    redis.call('EXPIRE', key, window)
end

return current
`

// RateLimiter implements Redis-backed rate limiting
type RateLimiter struct {
	redis          *redis.Client
	defaultLimits  RateLimits
	enabled        bool
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(redis *redis.Client, defaultLimits RateLimits, enabled bool) *RateLimiter {
	return &RateLimiter{
		redis:         redis,
		defaultLimits: defaultLimits,
		enabled:       enabled,
	}
}

// Allow checks if a request is allowed under rate limits
func (r *RateLimiter) Allow(ctx context.Context, client *ClientKey) error {
	if !r.enabled {
		return nil
	}

	limits := client.RateLimits
	if limits.RequestsPerMinute == 0 {
		limits.RequestsPerMinute = r.defaultLimits.RequestsPerMinute
	}
	if limits.RequestsPerDay == 0 {
		limits.RequestsPerDay = r.defaultLimits.RequestsPerDay
	}

	// Check per-minute limit
	if limits.RequestsPerMinute > 0 {
		if err := r.checkLimit(ctx, client.ClientID, "rpm", limits.RequestsPerMinute, time.Minute); err != nil {
			return err
		}
	}

	// Check per-day limit
	if limits.RequestsPerDay > 0 {
		if err := r.checkLimit(ctx, client.ClientID, "rpd", limits.RequestsPerDay, 24*time.Hour); err != nil {
			return err
		}
	}

	return nil
}

// RecordTokens records token usage for TPM limiting
func (r *RateLimiter) RecordTokens(ctx context.Context, clientID string, tokens int64, limit int) error {
	if !r.enabled {
		return nil
	}

	// Apply default TPM limit if client-specific limit is 0
	if limit == 0 {
		limit = r.defaultLimits.TokensPerMinute
	}

	// Only skip if both client limit and default are 0 (unlimited)
	if limit == 0 {
		return nil
	}

	key := fmt.Sprintf("%s%s:tpm", rateLimitPrefix, clientID)

	// Get current count
	count, err := r.redis.IncrBy(ctx, key, tokens)
	if err != nil {
		return fmt.Errorf("failed to record tokens: %w", err)
	}

	// Set expiry if this is first increment
	if count == tokens {
		_ = r.redis.Expire(ctx, key, time.Minute)
	}

	// Check if over limit (return error but don't block - already processed)
	if int(count) > limit {
		return ErrRateLimitExceeded
	}

	return nil
}

// checkLimit checks and increments a rate limit counter atomically
func (r *RateLimiter) checkLimit(ctx context.Context, clientID, limitType string, limit int, window time.Duration) error {
	key := fmt.Sprintf("%s%s:%s", rateLimitPrefix, clientID, limitType)
	windowSeconds := int(window.Seconds())

	result, err := r.redis.Eval(ctx, rateLimitScript, []string{key}, limit, windowSeconds)
	if err != nil {
		return fmt.Errorf("failed to check rate limit: %w", err)
	}

	count, ok := result.(int64)
	if !ok {
		return fmt.Errorf("unexpected result type from rate limit script")
	}

	if int(count) > limit {
		return ErrRateLimitExceeded
	}

	return nil
}

// GetUsage returns current usage for a client
func (r *RateLimiter) GetUsage(ctx context.Context, clientID string) (map[string]int64, error) {
	usage := make(map[string]int64)

	for _, limitType := range []string{"rpm", "rpd", "tpm"} {
		key := fmt.Sprintf("%s%s:%s", rateLimitPrefix, clientID, limitType)
		val, err := r.redis.Get(ctx, key)
		if err != nil && !redis.IsNil(err) {
			return nil, err
		}
		if val != "" {
			var count int64
			fmt.Sscanf(val, "%d", &count)
			usage[limitType] = count
		}
	}

	return usage, nil
}

// Reset resets rate limit counters for a client
func (r *RateLimiter) Reset(ctx context.Context, clientID string) error {
	for _, limitType := range []string{"rpm", "rpd", "tpm"} {
		key := fmt.Sprintf("%s%s:%s", rateLimitPrefix, clientID, limitType)
		if err := r.redis.Del(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
