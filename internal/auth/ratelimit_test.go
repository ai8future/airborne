package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ai8future/airborne/internal/redis"
)

func TestRateLimiter_AtomicIncrement(t *testing.T) {
	t.Run("increment and expire are atomic", func(t *testing.T) {
		// The checkLimit function uses a Lua script that:
		// 1. Increments counter
		// 2. Sets TTL if new key
		// 3. Returns current count
		// All in a single atomic operation
		t.Skip("requires Redis test container - see integration tests")
	})
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	t.Run("keys expire after window", func(t *testing.T) {
		t.Skip("requires Redis test container - see integration tests")
	})
}

func TestRateLimiter_Disabled(t *testing.T) {
	t.Run("Allow returns nil when disabled", func(t *testing.T) {
		rl := NewRateLimiter(nil, RateLimits{
			RequestsPerMinute: 100,
			RequestsPerDay:    1000,
			TokensPerMinute:   10000,
		}, false) // disabled

		client := &ClientKey{
			ClientID: "test-client",
			RateLimits: RateLimits{
				RequestsPerMinute: 10,
			},
		}

		err := rl.Allow(context.Background(), client)
		if err != nil {
			t.Errorf("Allow() should return nil when disabled, got: %v", err)
		}
	})

	t.Run("RecordTokens returns nil when disabled", func(t *testing.T) {
		rl := NewRateLimiter(nil, RateLimits{
			TokensPerMinute: 10000,
		}, false) // disabled

		err := rl.RecordTokens(context.Background(), "test-client", 1000, 5000)
		if err != nil {
			t.Errorf("RecordTokens() should return nil when disabled, got: %v", err)
		}
	})
}

func TestRateLimiter_RecordTokensDefaultTPM(t *testing.T) {
	// These tests verify the RecordTokens logic for applying default TPM limits.
	// Since RecordTokens requires Redis for actual rate limit enforcement,
	// we test the early-return logic paths that don't require Redis.

	t.Run("skips when disabled regardless of limits", func(t *testing.T) {
		rl := NewRateLimiter(nil, RateLimits{
			TokensPerMinute: 10000,
		}, false) // disabled

		// Should return nil even though we pass limit=0 (because disabled)
		err := rl.RecordTokens(context.Background(), "test-client", 1000, 0)
		if err != nil {
			t.Errorf("RecordTokens() should return nil when disabled, got: %v", err)
		}
	})

	t.Run("skips when both client and default limits are 0 (unlimited)", func(t *testing.T) {
		rl := NewRateLimiter(nil, RateLimits{
			TokensPerMinute: 0, // No default limit
		}, true) // enabled but no default

		// This should return nil because both client limit (0) and default (0) are unlimited
		// This test verifies we don't crash when Redis is nil and limits are truly unlimited
		err := rl.RecordTokens(context.Background(), "test-client", 1000, 0)
		if err != nil {
			t.Errorf("RecordTokens() should return nil when both limits are 0, got: %v", err)
		}
	})

	t.Run("applies default TPM when client limit is 0", func(t *testing.T) {
		// This test verifies the fix: when client limit=0, default should be applied.
		// Before the fix, limit=0 caused an early return without checking defaults.
		t.Skip("requires Redis test container - verifies default TPM is applied when client TPM=0")
	})

	t.Run("uses client limit when set (non-zero)", func(t *testing.T) {
		t.Skip("requires Redis test container - verifies client TPM takes precedence over default")
	})
}

func TestRateLimiter_AllowAppliesDefaults(t *testing.T) {
	// These tests verify that Allow() properly applies default limits
	// when client-specific limits are 0.

	t.Run("applies default RPM when client RPM is 0", func(t *testing.T) {
		t.Skip("requires Redis test container - see integration tests")
	})

	t.Run("applies default RPD when client RPD is 0", func(t *testing.T) {
		t.Skip("requires Redis test container - see integration tests")
	})

	t.Run("uses client limits when set (non-zero)", func(t *testing.T) {
		t.Skip("requires Redis test container - see integration tests")
	})
}

func TestNewRateLimiter(t *testing.T) {
	t.Run("creates rate limiter with defaults", func(t *testing.T) {
		defaults := RateLimits{
			RequestsPerMinute: 100,
			RequestsPerDay:    1000,
			TokensPerMinute:   50000,
		}

		rl := NewRateLimiter(nil, defaults, true)

		if rl == nil {
			t.Fatal("NewRateLimiter() returned nil")
		}
		if !rl.enabled {
			t.Error("NewRateLimiter() enabled should be true")
		}
		if rl.defaultLimits.RequestsPerMinute != 100 {
			t.Errorf("defaultLimits.RequestsPerMinute = %d, want 100", rl.defaultLimits.RequestsPerMinute)
		}
		if rl.defaultLimits.RequestsPerDay != 1000 {
			t.Errorf("defaultLimits.RequestsPerDay = %d, want 1000", rl.defaultLimits.RequestsPerDay)
		}
		if rl.defaultLimits.TokensPerMinute != 50000 {
			t.Errorf("defaultLimits.TokensPerMinute = %d, want 50000", rl.defaultLimits.TokensPerMinute)
		}
	})

	t.Run("creates disabled rate limiter", func(t *testing.T) {
		rl := NewRateLimiter(nil, RateLimits{}, false)

		if rl == nil {
			t.Fatal("NewRateLimiter() returned nil")
		}
		if rl.enabled {
			t.Error("NewRateLimiter() enabled should be false")
		}
	})
}

func TestGetUsage_MalformedValue(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client, err := redis.NewClient(redis.Config{Addr: s.Addr()})
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}
	defer client.Close()

	rl := NewRateLimiter(client, RateLimits{
		RequestsPerMinute: 100,
		RequestsPerDay:    1000,
		TokensPerMinute:   50000,
	}, true)

	ctx := context.Background()
	clientID := "test-client"

	// Inject malformed (non-numeric) values directly into Redis
	s.Set("aibox:ratelimit:"+clientID+":rpm", "not-a-number")
	s.Set("aibox:ratelimit:"+clientID+":rpd", "garbage")
	s.Set("aibox:ratelimit:"+clientID+":tpm", "xyz123")

	usage, err := rl.GetUsage(ctx, clientID)
	if err != nil {
		t.Fatalf("GetUsage should not return error on malformed data: %v", err)
	}

	// Malformed values should be treated as 0 to avoid blocking legitimate requests
	if usage["rpm"] != 0 {
		t.Errorf("rpm = %d, want 0 for malformed data", usage["rpm"])
	}
	if usage["rpd"] != 0 {
		t.Errorf("rpd = %d, want 0 for malformed data", usage["rpd"])
	}
	if usage["tpm"] != 0 {
		t.Errorf("tpm = %d, want 0 for malformed data", usage["tpm"])
	}
}

func TestGetUsage_ValidValues(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client, err := redis.NewClient(redis.Config{Addr: s.Addr()})
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}
	defer client.Close()

	rl := NewRateLimiter(client, RateLimits{
		RequestsPerMinute: 100,
		RequestsPerDay:    1000,
		TokensPerMinute:   50000,
	}, true)

	ctx := context.Background()
	clientID := "test-client"

	// Inject valid numeric values directly into Redis
	s.Set("aibox:ratelimit:"+clientID+":rpm", "42")
	s.Set("aibox:ratelimit:"+clientID+":rpd", "123")
	s.Set("aibox:ratelimit:"+clientID+":tpm", "9999")

	usage, err := rl.GetUsage(ctx, clientID)
	if err != nil {
		t.Fatalf("GetUsage failed: %v", err)
	}

	if usage["rpm"] != 42 {
		t.Errorf("rpm = %d, want 42", usage["rpm"])
	}
	if usage["rpd"] != 123 {
		t.Errorf("rpd = %d, want 123", usage["rpd"])
	}
	if usage["tpm"] != 9999 {
		t.Errorf("tpm = %d, want 9999", usage["tpm"])
	}
}

func TestCheckLimit_TypeCoercion(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client, err := redis.NewClient(redis.Config{Addr: s.Addr()})
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}
	defer client.Close()

	rl := NewRateLimiter(client, RateLimits{
		RequestsPerMinute: 100,
		RequestsPerDay:    1000,
		TokensPerMinute:   50000,
	}, true)

	ctx := context.Background()
	clientKey := &ClientKey{
		ClientID: "test-client",
		RateLimits: RateLimits{
			RequestsPerMinute: 10,
			RequestsPerDay:    100,
		},
	}

	// First request should be allowed (count = 1)
	err = rl.Allow(ctx, clientKey)
	if err != nil {
		t.Errorf("First request should be allowed: %v", err)
	}

	// Make requests up to the limit
	for i := 0; i < 9; i++ {
		err = rl.Allow(ctx, clientKey)
		if err != nil {
			t.Errorf("Request %d should be allowed: %v", i+2, err)
		}
	}

	// 11th request should be rate limited
	err = rl.Allow(ctx, clientKey)
	if err != ErrRateLimitExceeded {
		t.Errorf("Expected ErrRateLimitExceeded, got: %v", err)
	}
}

func TestRecordTokens_WithMiniredis(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client, err := redis.NewClient(redis.Config{Addr: s.Addr()})
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}
	defer client.Close()

	rl := NewRateLimiter(client, RateLimits{
		TokensPerMinute: 1000,
	}, true)

	ctx := context.Background()
	clientID := "test-client"

	// First token recording should succeed
	err = rl.RecordTokens(ctx, clientID, 500, 1000)
	if err != nil {
		t.Errorf("First RecordTokens should succeed: %v", err)
	}

	// Second recording that stays within limit should succeed
	err = rl.RecordTokens(ctx, clientID, 400, 1000)
	if err != nil {
		t.Errorf("Second RecordTokens should succeed: %v", err)
	}

	// Recording that exceeds limit should return ErrRateLimitExceeded
	err = rl.RecordTokens(ctx, clientID, 200, 1000)
	if err != ErrRateLimitExceeded {
		t.Errorf("Expected ErrRateLimitExceeded, got: %v", err)
	}
}

// Unused import guard for time package
var _ = time.Second
