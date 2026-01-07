package auth

import (
	"testing"
)

func TestParseAPIKey(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		wantKeyID string
		wantSec   string
		wantErr   bool
	}{
		{
			name:      "valid key",
			apiKey:    "aibox_sk_12345678_secretsecret123",
			wantKeyID: "12345678",
			wantSec:   "secretsecret123",
			wantErr:   false,
		},
		{
			name:      "valid key with long secret",
			apiKey:    "aibox_sk_abcdefgh_verylongsecretvalue1234567890",
			wantKeyID: "abcdefgh",
			wantSec:   "verylongsecretvalue1234567890",
			wantErr:   false,
		},
		{
			name:    "too short",
			apiKey:  "aibox_sk_123",
			wantErr: true,
		},
		{
			name:    "wrong prefix",
			apiKey:  "wrong_sk_12345678_secretsecret123",
			wantErr: true,
		},
		{
			name:    "missing underscore after keyID",
			apiKey:  "aibox_sk_12345678secretsecret123",
			wantErr: true,
		},
		{
			name:    "empty string",
			apiKey:  "",
			wantErr: true,
		},
		{
			name:    "key ID too short",
			apiKey:  "aibox_sk_1234567_secret",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyID, secret, err := parseAPIKey(tt.apiKey)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseAPIKey() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("parseAPIKey() unexpected error: %v", err)
				return
			}
			if keyID != tt.wantKeyID {
				t.Errorf("parseAPIKey() keyID = %v, want %v", keyID, tt.wantKeyID)
			}
			if secret != tt.wantSec {
				t.Errorf("parseAPIKey() secret = %v, want %v", secret, tt.wantSec)
			}
		})
	}
}

func TestGenerateRandomString(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"8 chars", 8},
		{"16 chars", 16},
		{"32 chars", 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generateRandomString(tt.length)
			if err != nil {
				t.Errorf("generateRandomString() unexpected error: %v", err)
				return
			}
			if len(result) != tt.length {
				t.Errorf("generateRandomString() length = %v, want %v", len(result), tt.length)
			}
			// Verify it's hex (all chars are 0-9 or a-f)
			for _, c := range result {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("generateRandomString() contains non-hex char: %c", c)
				}
			}
		})
	}

	// Test uniqueness
	t.Run("unique values", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			result, _ := generateRandomString(16)
			if seen[result] {
				t.Errorf("generateRandomString() produced duplicate: %s", result)
			}
			seen[result] = true
		}
	})
}

func TestClientKeyHasPermission(t *testing.T) {
	tests := []struct {
		name        string
		permissions []Permission
		check       Permission
		want        bool
	}{
		{
			name:        "has exact permission",
			permissions: []Permission{PermissionChat, PermissionFiles},
			check:       PermissionChat,
			want:        true,
		},
		{
			name:        "does not have permission",
			permissions: []Permission{PermissionChat},
			check:       PermissionFiles,
			want:        false,
		},
		{
			name:        "admin grants all",
			permissions: []Permission{PermissionAdmin},
			check:       PermissionChat,
			want:        true,
		},
		{
			name:        "admin grants files",
			permissions: []Permission{PermissionAdmin},
			check:       PermissionFiles,
			want:        true,
		},
		{
			name:        "empty permissions",
			permissions: []Permission{},
			check:       PermissionChat,
			want:        false,
		},
		{
			name:        "check for admin with admin",
			permissions: []Permission{PermissionAdmin},
			check:       PermissionAdmin,
			want:        true,
		},
		{
			name:        "chat does not grant admin",
			permissions: []Permission{PermissionChat, PermissionFiles},
			check:       PermissionAdmin,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := &ClientKey{
				Permissions: tt.permissions,
			}
			if got := key.HasPermission(tt.check); got != tt.want {
				t.Errorf("HasPermission() = %v, want %v", got, tt.want)
			}
		})
	}
}
