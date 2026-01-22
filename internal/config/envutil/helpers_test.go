package envutil

import (
	"os"
	"testing"
)

func TestGetStringEnv(t *testing.T) {
	os.Setenv("TEST_STRING", "value")
	defer os.Unsetenv("TEST_STRING")

	if got := GetStringEnv("TEST_STRING", "default"); got != "value" {
		t.Errorf("GetStringEnv() = %q, want %q", got, "value")
	}

	if got := GetStringEnv("NONEXISTENT", "default"); got != "default" {
		t.Errorf("GetStringEnv() = %q, want %q", got, "default")
	}
}

func TestGetIntEnv(t *testing.T) {
	os.Setenv("TEST_INT", "42")
	os.Setenv("TEST_INVALID", "not-a-number")
	defer os.Unsetenv("TEST_INT")
	defer os.Unsetenv("TEST_INVALID")

	if got := GetIntEnv("TEST_INT", 0); got != 42 {
		t.Errorf("GetIntEnv() = %d, want %d", got, 42)
	}

	if got := GetIntEnv("NONEXISTENT", 99); got != 99 {
		t.Errorf("GetIntEnv() = %d, want %d", got, 99)
	}

	if got := GetIntEnv("TEST_INVALID", 99); got != 99 {
		t.Errorf("GetIntEnv() with invalid value = %d, want default %d", got, 99)
	}
}

func TestGetBoolEnv(t *testing.T) {
	tests := []struct {
		envValue string
		want     bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"false", false},
		{"FALSE", false},
		{"0", false},
		{"invalid", false}, // Invalid values default to false
	}

	for _, tt := range tests {
		t.Run(tt.envValue, func(t *testing.T) {
			os.Setenv("TEST_BOOL", tt.envValue)
			defer os.Unsetenv("TEST_BOOL")

			if got := GetBoolEnv("TEST_BOOL", false); got != tt.want {
				t.Errorf("GetBoolEnv(%q) = %v, want %v", tt.envValue, got, tt.want)
			}
		})
	}

	if got := GetBoolEnv("NONEXISTENT", true); got != true {
		t.Errorf("GetBoolEnv() = %v, want default %v", got, true)
	}
}
