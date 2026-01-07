package auth

import (
	"context"
	"testing"
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
