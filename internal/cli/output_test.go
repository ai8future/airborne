package cli

import (
	"testing"
)

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		name   string
		tokens int
		want   string
	}{
		{"zero", 0, "0"},
		{"small", 42, "42"},
		{"just under 1K", 999, "999"},
		{"exactly 1K", 1000, "1.0K"},
		{"1.5K", 1500, "1.5K"},
		{"large", 12345, "12.3K"},
		{"100K", 100000, "100.0K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatTokens(tt.tokens); got != tt.want {
				t.Errorf("FormatTokens(%d) = %q, want %q", tt.tokens, got, tt.want)
			}
		})
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		name string
		cost float64
		want string
	}{
		{"zero", 0.0, "$0.000"},
		{"small", 0.001, "$0.001"},
		{"typical", 0.025, "$0.025"},
		{"one dollar", 1.0, "$1.000"},
		{"precise", 0.12345, "$0.123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatCost(tt.cost); got != tt.want {
				t.Errorf("FormatCost(%f) = %q, want %q", tt.cost, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		ms   int
		want string
	}{
		{"zero", 0, "0ms"},
		{"sub-second", 500, "500ms"},
		{"exactly 1s", 1000, "1.0s"},
		{"1.5s", 1500, "1.5s"},
		{"long", 12345, "12.3s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatDuration(tt.ms); got != tt.want {
				t.Errorf("FormatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"RFC3339", "2026-03-21T12:30:45Z"},
		{"RFC3339 with offset", "2026-03-21T12:30:45+05:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTimestamp(tt.input)
			if got == "" {
				t.Error("FormatTimestamp returned empty string")
			}
			// Valid timestamps should be reformatted (not returned as-is)
			if got == tt.input {
				t.Errorf("FormatTimestamp(%q) was not reformatted", tt.input)
			}
		})
	}

	// Unparseable input should return as-is
	t.Run("unparseable", func(t *testing.T) {
		input := "not-a-timestamp"
		if got := FormatTimestamp(input); got != input {
			t.Errorf("FormatTimestamp(%q) = %q, want original string", input, got)
		}
	})
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello12345", 10, "hello12345"},
		{"truncated", "hello world foo bar", 10, "hello w..."},
		{"with newlines", "hello\nworld", 20, "hello world"},
		{"empty", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateString(tt.s, tt.maxLen); got != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestFormatStatus(t *testing.T) {
	successOut := FormatStatus("success")
	if successOut == "" {
		t.Error("FormatStatus('success') returned empty")
	}

	failedOut := FormatStatus("failed")
	if failedOut == "" {
		t.Error("FormatStatus('failed') returned empty")
	}

	if successOut == failedOut {
		t.Error("FormatStatus should produce different output for success vs failure")
	}
}
