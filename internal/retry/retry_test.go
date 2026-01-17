package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Not retryable - nil
		{"nil error", nil, false},

		// Not retryable - context errors
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},

		// Not retryable - authentication errors
		{"401 unauthorized", errors.New("401 unauthorized"), false},
		{"403 forbidden", errors.New("403 forbidden"), false},
		{"invalid_api_key", errors.New("invalid_api_key"), false},
		{"authentication failed", errors.New("authentication failed"), false},
		{"permission denied", errors.New("permission_denied"), false},
		{"unauthenticated", errors.New("unauthenticated request"), false},
		{"not_found_error", errors.New("not_found_error"), false},

		// Not retryable - invalid request errors
		{"400 bad request", errors.New("400 bad request"), false},
		{"422 unprocessable", errors.New("422 unprocessable entity"), false},
		{"invalid_request", errors.New("invalid_request"), false},
		{"invalid_argument", errors.New("invalid_argument"), false},
		{"malformed json", errors.New("malformed json"), false},
		{"validation error", errors.New("validation failed"), false},

		// Retryable - rate limits
		{"429 rate limit", errors.New("429 too many requests"), true},
		{"rate limit text", errors.New("rate limit exceeded"), true},

		// Retryable - server errors
		{"500 internal", errors.New("500 internal server error"), true},
		{"502 bad gateway", errors.New("502 bad gateway"), true},
		{"503 unavailable", errors.New("503 service unavailable"), true},
		{"504 timeout", errors.New("504 gateway timeout"), true},
		{"529 overloaded", errors.New("529 overloaded"), true},
		{"server_error", errors.New("server_error occurred"), true},

		// Retryable - network/connection errors
		{"connection refused", errors.New("connection refused"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"timeout", errors.New("request timeout"), true},
		{"temporary failure", errors.New("temporary network issue"), true},
		{"eof error", errors.New("unexpected EOF"), true},
		{"tls handshake", errors.New("tls handshake failure"), true},
		{"no such host", errors.New("dial tcp: no such host"), true},
		{"api_connection", errors.New("api_connection error"), true},
		{"overloaded", errors.New("service overloaded"), true},
		{"resource exhausted", errors.New("resource exhausted"), true},

		// Not retryable - unknown errors (default)
		{"unknown error", errors.New("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestSleepWithBackoff_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Attempt 3 would normally sleep for BackoffBase * 4 = 1000ms
	SleepWithBackoff(ctx, 3)

	duration := time.Since(start)
	// Should return quickly after context cancellation (~50ms), not wait full backoff
	if duration > 200*time.Millisecond {
		t.Errorf("SleepWithBackoff took %v, expected ~50ms due to context cancellation", duration)
	}
}

func TestSleepWithBackoff_ExponentialDelay(t *testing.T) {
	ctx := context.Background()

	// Test that attempt 1 sleeps for approximately BackoffBase (250ms)
	start := time.Now()
	SleepWithBackoff(ctx, 1)
	duration := time.Since(start)

	// Allow 50ms tolerance for timing variance
	expected := BackoffBase
	if duration < expected-50*time.Millisecond || duration > expected+100*time.Millisecond {
		t.Errorf("attempt 1: SleepWithBackoff took %v, expected ~%v", duration, expected)
	}
}

func TestDefaults(t *testing.T) {
	// Verify default constants are sensible
	if MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", MaxAttempts)
	}

	if RequestTimeout != 3*time.Minute {
		t.Errorf("RequestTimeout = %v, want 3m", RequestTimeout)
	}

	if BackoffBase != 250*time.Millisecond {
		t.Errorf("BackoffBase = %v, want 250ms", BackoffBase)
	}
}
