package commands

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		imageTriggers []string
		wantText      string
		wantImage     string
		wantSkipAI    bool
	}{
		{
			name:          "plain text no commands",
			input:         "Hello world",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "Hello world",
			wantImage:     "",
			wantSkipAI:    false,
		},
		{
			name:          "/image extracts prompt",
			input:         "/image a sunset",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "",
			wantImage:     "a sunset",
			wantSkipAI:    true,
		},
		{
			name:          "@image trigger works",
			input:         "@image a mountain",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "",
			wantImage:     "a mountain",
			wantSkipAI:    true,
		},
		{
			name:          "/ignore strips to end of line",
			input:         "Hello\n/ignore secret stuff\nWorld",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "Hello\nWorld",
			wantImage:     "",
			wantSkipAI:    false,
		},
		{
			name:          "/ignore at start of input",
			input:         "/ignore hidden\nVisible text",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "Visible text",
			wantImage:     "",
			wantSkipAI:    false,
		},
		{
			name:          "/ignore at end strips entire line",
			input:         "Hello\n/ignore everything",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "Hello",
			wantImage:     "",
			wantSkipAI:    false,
		},
		{
			name:          "multiple /ignore lines",
			input:         "Line1\n/ignore a\nLine2\n/ignore b\nLine3",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "Line1\nLine2\nLine3",
			wantImage:     "",
			wantSkipAI:    false,
		},
		{
			name:          "/ignore only leaves empty - skip AI",
			input:         "/ignore everything here",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "",
			wantImage:     "",
			wantSkipAI:    true,
		},
		{
			name:          "/image takes priority over other text",
			input:         "Explain physics\n/image an atom",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "",
			wantImage:     "an atom",
			wantSkipAI:    true,
		},
		{
			name:          "/image takes priority over /ignore",
			input:         "/ignore foo\n/image a cat",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "",
			wantImage:     "a cat",
			wantSkipAI:    true,
		},
		{
			name:          "case insensitive /IMAGE",
			input:         "/IMAGE a dog",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "",
			wantImage:     "a dog",
			wantSkipAI:    true,
		},
		{
			name:          "case insensitive /IGNORE",
			input:         "Hello\n/IGNORE secret\nWorld",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "Hello\nWorld",
			wantImage:     "",
			wantSkipAI:    false,
		},
		{
			name:          "/ignore mid-line strips rest",
			input:         "Keep this /ignore but not this",
			imageTriggers: []string{"@image", "/image"},
			wantText:      "Keep this",
			wantImage:     "",
			wantSkipAI:    false,
		},
		{
			name:          "empty image triggers disables image detection",
			input:         "/image a sunset",
			imageTriggers: []string{},
			wantText:      "/image a sunset",
			wantImage:     "",
			wantSkipAI:    false,
		},
		{
			name:          "nil image triggers disables image detection",
			input:         "@image a sunset",
			imageTriggers: nil,
			wantText:      "@image a sunset",
			wantImage:     "",
			wantSkipAI:    false,
		},
		{
			name:          "whitespace-only after /ignore processing",
			input:         "   \n/ignore stuff\n   ",
			imageTriggers: []string{"@image"},
			wantText:      "",
			wantImage:     "",
			wantSkipAI:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.imageTriggers)
			result := p.Parse(tt.input)

			if result.ProcessedText != tt.wantText {
				t.Errorf("ProcessedText = %q, want %q", result.ProcessedText, tt.wantText)
			}
			if result.ImagePrompt != tt.wantImage {
				t.Errorf("ImagePrompt = %q, want %q", result.ImagePrompt, tt.wantImage)
			}
			if result.SkipAI != tt.wantSkipAI {
				t.Errorf("SkipAI = %v, want %v", result.SkipAI, tt.wantSkipAI)
			}
		})
	}
}

func TestNewParser(t *testing.T) {
	p := NewParser([]string{"@image"})
	if p == nil {
		t.Fatal("NewParser returned nil")
	}
}
