// Package retry provides shared retry utilities for provider implementations.
package retry

import (
	"context"
	"time"
)

// SleepWithBackoff sleeps with exponential backoff.
// The delay is calculated as BackoffBase * 2^(attempt-1).
func SleepWithBackoff(ctx context.Context, attempt int) {
	delay := BackoffBase * time.Duration(1<<uint(attempt-1))
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}
