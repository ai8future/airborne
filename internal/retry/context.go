package retry

import (
	"context"
	"time"
)

// EnsureTimeout returns a context with the given timeout if none exists.
// If the context already has a deadline, it returns the original context unchanged.
// The returned cancel function should always be called (safe to call on nil cancel).
func EnsureTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {} // noop cancel
	}
	return context.WithTimeout(ctx, timeout)
}
