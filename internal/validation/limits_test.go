package validation

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidateGenerateRequest_RejectsOversizedInput(t *testing.T) {
	tests := []struct {
		name        string
		userInput   string
		instruction string
		historyLen  int
		wantErr     bool
	}{
		{
			name:      "normal input passes",
			userInput: "Hello, how are you?",
			wantErr:   false,
		},
		{
			name:      "oversized user input rejected",
			userInput: strings.Repeat("x", MaxUserInputBytes+1),
			wantErr:   true,
		},
		{
			name:        "oversized instructions rejected",
			userInput:   "test",
			instruction: strings.Repeat("x", MaxInstructionsBytes+1),
			wantErr:     true,
		},
		{
			name:       "too many history items rejected",
			userInput:  "test",
			historyLen: MaxHistoryCount + 1,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGenerateRequest(tt.userInput, tt.instruction, tt.historyLen)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGenerateRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateGenerateRequest_BoundaryValues(t *testing.T) {
	tests := []struct {
		name        string
		userInput   string
		instruction string
		historyLen  int
		wantErr     error
	}{
		{
			name:      "user input at exact limit passes",
			userInput: strings.Repeat("x", MaxUserInputBytes),
			wantErr:   nil,
		},
		{
			name:        "instructions at exact limit passes",
			userInput:   "test",
			instruction: strings.Repeat("x", MaxInstructionsBytes),
			wantErr:     nil,
		},
		{
			name:       "history at exact limit passes",
			userInput:  "test",
			historyLen: MaxHistoryCount,
			wantErr:    nil,
		},
		{
			name:      "user input over limit returns correct error",
			userInput: strings.Repeat("x", MaxUserInputBytes+1),
			wantErr:   ErrUserInputTooLarge,
		},
		{
			name:        "instructions over limit returns correct error",
			userInput:   "test",
			instruction: strings.Repeat("x", MaxInstructionsBytes+1),
			wantErr:     ErrInstructionsTooLarge,
		},
		{
			name:       "history over limit returns correct error",
			userInput:  "test",
			historyLen: MaxHistoryCount + 1,
			wantErr:    ErrHistoryTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGenerateRequest(tt.userInput, tt.instruction, tt.historyLen)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			}
		})
	}
}

func TestValidateMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		wantErr  error
	}{
		{
			name:     "nil metadata passes",
			metadata: nil,
			wantErr:  nil,
		},
		{
			name:     "empty metadata passes",
			metadata: map[string]string{},
			wantErr:  nil,
		},
		{
			name:     "metadata at exact limit passes",
			metadata: makeMetadata(MaxMetadataEntries),
			wantErr:  nil,
		},
		{
			name:     "metadata over limit returns correct error",
			metadata: makeMetadata(MaxMetadataEntries + 1),
			wantErr:  ErrMetadataTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMetadata(tt.metadata)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			}
		})
	}
}

// makeMetadata creates a metadata map with n entries
func makeMetadata(n int) map[string]string {
	m := make(map[string]string, n)
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("key%d", i)] = fmt.Sprintf("value%d", i)
	}
	return m
}

func TestValidateMetadata_KeyTooLarge(t *testing.T) {
	metadata := map[string]string{
		strings.Repeat("k", MaxMetadataKeyBytes+1): "value",
	}
	err := ValidateMetadata(metadata)
	if !errors.Is(err, ErrMetadataKeyTooLarge) {
		t.Errorf("expected ErrMetadataKeyTooLarge, got %v", err)
	}
}

func TestValidateMetadata_ValueTooLarge(t *testing.T) {
	metadata := map[string]string{
		"key": strings.Repeat("v", MaxMetadataValueBytes+1),
	}
	err := ValidateMetadata(metadata)
	if !errors.Is(err, ErrMetadataValueTooLarge) {
		t.Errorf("expected ErrMetadataValueTooLarge, got %v", err)
	}
}

func TestValidateMetadata_MaxSizesValid(t *testing.T) {
	metadata := map[string]string{
		strings.Repeat("k", MaxMetadataKeyBytes): strings.Repeat("v", MaxMetadataValueBytes),
	}
	err := ValidateMetadata(metadata)
	if err != nil {
		t.Errorf("expected nil error for max-size values, got %v", err)
	}
}

func TestValidateOrGenerateRequestID(t *testing.T) {
	tests := []struct {
		name        string
		requestID   string
		wantValid   bool
		wantChanged bool
	}{
		{
			name:        "empty generates new ID",
			requestID:   "",
			wantValid:   true,
			wantChanged: true,
		},
		{
			name:        "valid UUID passes through",
			requestID:   "550e8400-e29b-41d4-a716-446655440000",
			wantValid:   true,
			wantChanged: false,
		},
		{
			name:        "valid alphanumeric passes through",
			requestID:   "req-12345-abcdef",
			wantValid:   true,
			wantChanged: false,
		},
		{
			name:        "invalid characters rejected",
			requestID:   "request<script>alert(1)</script>",
			wantValid:   false,
			wantChanged: false,
		},
		{
			name:        "too long rejected",
			requestID:   strings.Repeat("a", 129),
			wantValid:   false,
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidateOrGenerateRequestID(tt.requestID)
			if tt.wantValid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.wantValid && err == nil {
				t.Errorf("expected error, got valid result: %s", result)
			}
			if tt.wantChanged && result == tt.requestID {
				t.Error("expected ID to be changed/generated")
			}
			if !tt.wantChanged && tt.wantValid && result != tt.requestID {
				t.Errorf("expected ID to pass through, got %s", result)
			}
		})
	}
}
