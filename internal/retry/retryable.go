package retry

import (
	"context"
	"errors"
	"strings"
)

// IsRetryable checks if an error should trigger a retry attempt.
// It handles common patterns across AI providers including:
// - Context cancellation (not retryable)
// - Authentication errors (not retryable)
// - Invalid request errors (not retryable)
// - Rate limits, server errors, network issues (retryable)
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context errors are not retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Authentication/authorization errors - not retryable
	authPatterns := []string{
		"401", "403",
		"invalid_api_key", "authentication", "permission",
		"unauthorized", "unauthenticated", "not_found_error", "permission_denied",
	}
	for _, p := range authPatterns {
		if strings.Contains(errStr, p) {
			return false
		}
	}

	// Invalid request errors - not retryable
	invalidPatterns := []string{
		"400", "422",
		"invalid_request", "invalid_argument", "malformed", "validation",
	}
	for _, p := range invalidPatterns {
		if strings.Contains(errStr, p) {
			return false
		}
	}

	// Retryable errors: rate limits, server errors, network issues
	// 499 = Gemini cancels our request (client closed request)
	retryablePatterns := []string{
		"429", "499", "500", "502", "503", "504", "529",
		"rate", "overloaded", "resource", "server_error",
		"connection", "timeout", "temporary", "eof",
		"tls handshake", "no such host", "api_connection",
	}
	for _, p := range retryablePatterns {
		if strings.Contains(errStr, p) {
			return true
		}
	}

	return false
}
