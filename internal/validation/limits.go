package validation

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
)

const (
	// MaxUserInputBytes is the maximum size of user input (100KB)
	MaxUserInputBytes = 100 * 1024

	// MaxInstructionsBytes is the maximum size of system instructions (50KB)
	MaxInstructionsBytes = 50 * 1024

	// MaxHistoryCount is the maximum number of conversation history messages
	MaxHistoryCount = 100

	// MaxMetadataEntries is the maximum number of metadata key-value pairs
	MaxMetadataEntries = 50

	// MaxMetadataKeyBytes is the maximum size of a single metadata key (1KB)
	MaxMetadataKeyBytes = 1024

	// MaxMetadataValueBytes is the maximum size of a single metadata value (10KB)
	MaxMetadataValueBytes = 10 * 1024

	// MaxRequestIDLength is the maximum length of a request ID
	MaxRequestIDLength = 128
)

var (
	ErrUserInputTooLarge     = errors.New("user_input exceeds maximum size")
	ErrInstructionsTooLarge  = errors.New("instructions exceed maximum size")
	ErrHistoryTooLong        = errors.New("conversation_history exceeds maximum length")
	ErrMetadataTooLarge      = errors.New("metadata exceeds maximum entries")
	ErrMetadataKeyTooLarge   = errors.New("metadata key exceeds maximum size")
	ErrMetadataValueTooLarge = errors.New("metadata value exceeds maximum size")
	ErrInvalidRequestID      = errors.New("invalid request_id format")
)

// ValidateGenerateRequest validates size limits for a generate request
func ValidateGenerateRequest(userInput, instructions string, historyCount int) error {
	if len(userInput) > MaxUserInputBytes {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrUserInputTooLarge, len(userInput), MaxUserInputBytes)
	}

	if len(instructions) > MaxInstructionsBytes {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrInstructionsTooLarge, len(instructions), MaxInstructionsBytes)
	}

	if historyCount > MaxHistoryCount {
		return fmt.Errorf("%w: %d messages (max %d)", ErrHistoryTooLong, historyCount, MaxHistoryCount)
	}

	return nil
}

// ValidateMetadata checks that metadata doesn't exceed limits.
func ValidateMetadata(metadata map[string]string) error {
	if len(metadata) > MaxMetadataEntries {
		return fmt.Errorf("%w: %d entries (max %d)", ErrMetadataTooLarge, len(metadata), MaxMetadataEntries)
	}
	for k, v := range metadata {
		if len(k) > MaxMetadataKeyBytes {
			return fmt.Errorf("%w: key length %d (max %d)", ErrMetadataKeyTooLarge, len(k), MaxMetadataKeyBytes)
		}
		if len(v) > MaxMetadataValueBytes {
			return fmt.Errorf("%w: value length %d (max %d)", ErrMetadataValueTooLarge, len(v), MaxMetadataValueBytes)
		}
	}
	return nil
}

// requestIDPattern allows alphanumeric, hyphens, underscores
var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// ValidateOrGenerateRequestID validates an existing request ID or generates a new one
func ValidateOrGenerateRequestID(requestID string) (string, error) {
	if requestID == "" {
		return generateRequestID()
	}

	if len(requestID) > MaxRequestIDLength {
		return "", fmt.Errorf("%w: exceeds %d characters", ErrInvalidRequestID, MaxRequestIDLength)
	}

	if !requestIDPattern.MatchString(requestID) {
		return "", fmt.Errorf("%w: contains invalid characters", ErrInvalidRequestID)
	}

	return requestID, nil
}

// generateRequestID generates a new random request ID
func generateRequestID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate request ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
