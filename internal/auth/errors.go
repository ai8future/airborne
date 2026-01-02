package auth

import "errors"

var (
	// ErrInvalidKey indicates the API key format is invalid or secret doesn't match
	ErrInvalidKey = errors.New("invalid API key")

	// ErrKeyNotFound indicates the API key doesn't exist
	ErrKeyNotFound = errors.New("API key not found")

	// ErrKeyExpired indicates the API key has expired
	ErrKeyExpired = errors.New("API key expired")

	// ErrPermissionDenied indicates the key lacks required permission
	ErrPermissionDenied = errors.New("permission denied")

	// ErrRateLimitExceeded indicates rate limit was exceeded
	ErrRateLimitExceeded = errors.New("rate limit exceeded")

	// ErrMissingAPIKey indicates no API key was provided
	ErrMissingAPIKey = errors.New("missing API key")
)
