// Package retry provides application-level retry utilities for AI provider SDK calls.
//
// This package operates at the SDK/provider level — it retries higher-level operations
// where the provider SDK manages its own HTTP client (e.g., OpenAI, Anthropic, Gemini SDKs).
//
// For HTTP transport-level retries on raw outbound HTTP calls (e.g., Qdrant, Ollama, Docbox),
// use chassis-go/call.Client instead. The two layers are complementary, not competing.
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
