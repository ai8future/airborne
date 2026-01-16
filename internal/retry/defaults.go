package retry

import "time"

// Default retry and timeout constants shared across providers.
const (
	// MaxAttempts is the default number of retry attempts.
	MaxAttempts = 3

	// RequestTimeout is the default request timeout.
	RequestTimeout = 3 * time.Minute

	// BackoffBase is the base duration for exponential backoff.
	BackoffBase = 250 * time.Millisecond
)
