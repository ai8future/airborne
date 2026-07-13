package cli

import (
	"io"
	"os"
	"strings"
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

func TestPrintOutputContracts(t *testing.T) {
	activity := Activity{ID: "a1", ThreadID: "thread-1", Tenant: "ai8", Model: "model", Provider: "provider", InputTokens: 10, OutputTokens: 20, CostUSD: .1, GroundingCostUSD: .02, ProcessingTimeMs: 50, Status: "success", Timestamp: "2026-01-01T00:00:00Z", FullContent: "full content"}
	debug := &DebugResponse{MessageID: "m1", ThreadID: "thread-1", TenantID: "ai8", Timestamp: activity.Timestamp, RequestProvider: "provider", ResponseModel: "model", TokensIn: 10, TokensOut: 20, CostUSD: .1, GroundingQueries: 1, GroundingCostUSD: .02, DurationMs: 50, SystemPrompt: "system", UserInput: "input", ResponseText: "reply", Status: "success"}
	testResult := &TestResponse{Model: "model", Provider: "provider", InputTokens: 10, OutputTokens: 20, ProcessingMs: 50, Reply: "reply"}

	output := captureStdout(t, func() {
		PrintActivityTable([]Activity{activity})
		PrintActivityDetail(activity)
		PrintDebugInfo(debug)
		PrintThreadMessages([]ThreadMessage{{Role: "user", Timestamp: activity.Timestamp, Model: "model", Content: "message"}, {Role: "assistant", Timestamp: activity.Timestamp, Content: "answer"}})
		PrintTestResult(testResult)
	})
	for _, want := range []string{"TIME", "thread-1", "Grounding:", "system", "message", "answer", "reply"} {
		if !strings.Contains(output, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestPrintDebugInfoOmitsGroundingWhenUnused(t *testing.T) {
	output := captureStdout(t, func() {
		PrintDebugInfo(&DebugResponse{Timestamp: "invalid", Status: "failed"})
	})
	if strings.Contains(output, "Grounding:") {
		t.Fatalf("unexpected grounding section:\n%s", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	previous := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	defer func() { os.Stdout = previous }()

	fn()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}
