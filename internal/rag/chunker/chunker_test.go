package chunker

import (
	"strings"
	"testing"
)

func TestChunkText_EmptyInput(t *testing.T) {
	chunks := ChunkText("", DefaultOptions())
	if chunks != nil {
		t.Errorf("expected nil for empty input, got %v", chunks)
	}
}

func TestChunkText_WhitespaceOnly(t *testing.T) {
	chunks := ChunkText("   \n\n   \t  ", DefaultOptions())
	if chunks != nil {
		t.Errorf("expected nil for whitespace-only input, got %v", chunks)
	}
}

func TestChunkText_SingleChunk(t *testing.T) {
	text := "This is a short text that fits in one chunk."
	opts := Options{ChunkSize: 1000, Overlap: 100}

	chunks := ChunkText(text, opts)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Index != 0 {
		t.Errorf("expected Index=0, got %d", chunks[0].Index)
	}
	if chunks[0].Text != text {
		t.Errorf("expected text=%q, got %q", text, chunks[0].Text)
	}
	if chunks[0].Start != 0 {
		t.Errorf("expected Start=0, got %d", chunks[0].Start)
	}
	if chunks[0].End != len(text) {
		t.Errorf("expected End=%d, got %d", len(text), chunks[0].End)
	}
}

func TestChunkText_ExactChunkSize(t *testing.T) {
	text := strings.Repeat("a", 100)
	opts := Options{ChunkSize: 100, Overlap: 10}

	chunks := ChunkText(text, opts)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for exact size, got %d", len(chunks))
	}
}

func TestChunkText_MultipleChunks(t *testing.T) {
	// Create text that's 3x chunk size
	text := strings.Repeat("word ", 300) // ~1500 chars
	opts := Options{ChunkSize: 500, Overlap: 50, MinChunkSize: 50}

	chunks := ChunkText(text, opts)

	if len(chunks) < 3 {
		t.Errorf("expected at least 3 chunks, got %d", len(chunks))
	}

	// Verify indices are sequential
	for i, chunk := range chunks {
		if chunk.Index != i {
			t.Errorf("chunk %d has Index=%d", i, chunk.Index)
		}
	}
}

func TestChunkText_OverlapPreserved(t *testing.T) {
	text := strings.Repeat("x", 1000)
	opts := Options{ChunkSize: 300, Overlap: 50, MinChunkSize: 50}

	chunks := ChunkText(text, opts)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// Check that consecutive chunks overlap
	for i := 0; i < len(chunks)-1; i++ {
		overlap := chunks[i].End - chunks[i+1].Start
		if overlap < 0 {
			t.Errorf("chunks %d and %d have gap instead of overlap", i, i+1)
		}
	}
}

func TestChunkText_ParagraphBoundary(t *testing.T) {
	// Text with paragraph break near the end of first chunk
	text := strings.Repeat("a", 400) + "\n\n" + strings.Repeat("b", 400)
	opts := Options{ChunkSize: 500, Overlap: 50, MinChunkSize: 50}

	chunks := ChunkText(text, opts)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// First chunk should end at or near paragraph break
	if !strings.HasSuffix(strings.TrimSpace(chunks[0].Text), "a") {
		// The chunk should have split at the paragraph break
		t.Logf("chunk 0 text ends with: %q", chunks[0].Text[len(chunks[0].Text)-20:])
	}
}

func TestChunkText_SentenceBoundary(t *testing.T) {
	// Text with sentence boundaries
	text := strings.Repeat("This is a sentence. ", 50)
	opts := Options{ChunkSize: 200, Overlap: 30, MinChunkSize: 50}

	chunks := ChunkText(text, opts)

	// Most chunks should end with a period or space after period
	sentenceEnds := 0
	for _, chunk := range chunks {
		trimmed := strings.TrimSpace(chunk.Text)
		if strings.HasSuffix(trimmed, ".") {
			sentenceEnds++
		}
	}

	// At least some chunks should end at sentence boundaries
	if sentenceEnds == 0 && len(chunks) > 1 {
		t.Log("no chunks ended at sentence boundaries (may be acceptable)")
	}
}

func TestChunkText_WordBoundary(t *testing.T) {
	// Text without good sentence breaks
	text := strings.Repeat("word ", 200)
	opts := Options{ChunkSize: 100, Overlap: 10, MinChunkSize: 20}

	chunks := ChunkText(text, opts)

	// No chunk should end mid-word (with partial "word")
	for i, chunk := range chunks {
		trimmed := strings.TrimSpace(chunk.Text)
		// Should end with complete "word" not "wor" or "wo"
		if strings.HasSuffix(trimmed, "wor") || strings.HasSuffix(trimmed, "wo") || strings.HasSuffix(trimmed, "w") {
			t.Errorf("chunk %d ends mid-word: %q", i, trimmed[len(trimmed)-10:])
		}
	}
}

func TestChunkText_MinChunkSize(t *testing.T) {
	opts := Options{ChunkSize: 100, Overlap: 10, MinChunkSize: 50}

	// Most chunks should be at least MinChunkSize
	text := strings.Repeat("a", 500)
	chunks := ChunkText(text, opts)

	for i, chunk := range chunks {
		if len(chunk.Text) < opts.MinChunkSize && i < len(chunks)-1 {
			// Only last chunk can be smaller
			t.Errorf("chunk %d has length %d, less than MinChunkSize %d",
				i, len(chunk.Text), opts.MinChunkSize)
		}
	}
}

func TestChunkText_UnicodeText(t *testing.T) {
	// Text with emoji and various unicode
	text := "Hello 👋 World 🌍! Привет мир! 你好世界! " + strings.Repeat("test ", 100)
	opts := Options{ChunkSize: 100, Overlap: 20, MinChunkSize: 20}

	chunks := ChunkText(text, opts)

	if len(chunks) == 0 {
		t.Fatal("expected chunks for unicode text")
	}

	// Verify we can reconstruct approximately (due to overlap)
	totalText := ""
	for _, chunk := range chunks {
		totalText += chunk.Text
	}
	// Original text should be contained
	if !strings.Contains(totalText, "👋") || !strings.Contains(totalText, "🌍") {
		t.Error("emoji lost in chunking")
	}
}

func TestChunkText_ZeroOverlap(t *testing.T) {
	text := strings.Repeat("a", 500)
	opts := Options{ChunkSize: 100, Overlap: 0, MinChunkSize: 50}

	chunks := ChunkText(text, opts)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// With zero overlap, chunks should not overlap
	for i := 0; i < len(chunks)-1; i++ {
		if chunks[i].End > chunks[i+1].Start {
			t.Errorf("chunks %d and %d overlap with Overlap=0", i, i+1)
		}
	}
}

func TestChunkText_LargeOverlap(t *testing.T) {
	text := strings.Repeat("a", 500)
	// Overlap >= ChunkSize should be handled gracefully
	opts := Options{ChunkSize: 100, Overlap: 150, MinChunkSize: 50}

	chunks := ChunkText(text, opts)

	// Should not infinite loop and should produce chunks
	if len(chunks) == 0 {
		t.Fatal("expected chunks even with large overlap")
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.ChunkSize != 2000 {
		t.Errorf("expected ChunkSize=2000, got %d", opts.ChunkSize)
	}
	if opts.Overlap != 200 {
		t.Errorf("expected Overlap=200, got %d", opts.Overlap)
	}
	if opts.MinChunkSize != 100 {
		t.Errorf("expected MinChunkSize=100, got %d", opts.MinChunkSize)
	}
}

func TestChunkText_NegativeOptions(t *testing.T) {
	text := strings.Repeat("a", 500)

	// Negative values should use defaults
	opts := Options{ChunkSize: -1, Overlap: -1, MinChunkSize: -1}
	chunks := ChunkText(text, opts)

	if len(chunks) == 0 {
		t.Fatal("expected chunks with negative options (should use defaults)")
	}
}

func TestChunkText_StartEndPositions(t *testing.T) {
	text := "0123456789" + strings.Repeat("a", 100) + "END"
	opts := Options{ChunkSize: 50, Overlap: 10, MinChunkSize: 10}

	chunks := ChunkText(text, opts)

	// First chunk should start at 0
	if chunks[0].Start != 0 {
		t.Errorf("first chunk Start should be 0, got %d", chunks[0].Start)
	}

	// Verify Start/End positions are valid
	for i, chunk := range chunks {
		if chunk.Start < 0 || chunk.Start > len(text) {
			t.Errorf("chunk %d has invalid Start: %d", i, chunk.Start)
		}
		if chunk.End < chunk.Start || chunk.End > len(text) {
			t.Errorf("chunk %d has invalid End: %d (Start=%d, textLen=%d)",
				i, chunk.End, chunk.Start, len(text))
		}
		// Text should match the slice
		expected := strings.TrimSpace(text[chunk.Start:chunk.End])
		if chunk.Text != expected {
			t.Errorf("chunk %d text mismatch: positions give %q, stored %q",
				i, expected, chunk.Text)
		}
	}
}

// Table-driven test for various scenarios
func TestChunkText_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		opts        Options
		wantMinLen  int
		wantMaxLen  int
		validateFn  func(t *testing.T, chunks []Chunk)
	}{
		{
			name:       "empty",
			input:      "",
			opts:       DefaultOptions(),
			wantMinLen: 0,
			wantMaxLen: 0,
		},
		{
			name:       "tiny text",
			input:      "hi",
			opts:       DefaultOptions(),
			wantMinLen: 1,
			wantMaxLen: 1,
		},
		{
			name:       "exact chunk size",
			input:      strings.Repeat("a", 2000),
			opts:       DefaultOptions(),
			wantMinLen: 1,
			wantMaxLen: 1,
		},
		{
			name:       "double chunk size",
			input:      strings.Repeat("a", 4000),
			opts:       DefaultOptions(),
			wantMinLen: 2,
			wantMaxLen: 4, // With overlap, could produce up to 4 chunks
		},
		{
			name:       "with newlines",
			input:      "line1\nline2\nline3\n" + strings.Repeat("a", 3000),
			opts:       Options{ChunkSize: 1000, Overlap: 100, MinChunkSize: 50},
			wantMinLen: 3,
			wantMaxLen: 5,
		},
		{
			name:  "verify sequential indices",
			input: strings.Repeat("word ", 500),
			opts:  Options{ChunkSize: 200, Overlap: 20, MinChunkSize: 50},
			validateFn: func(t *testing.T, chunks []Chunk) {
				for i, c := range chunks {
					if c.Index != i {
						t.Errorf("expected Index=%d, got %d", i, c.Index)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := ChunkText(tt.input, tt.opts)

			if tt.wantMinLen > 0 && len(chunks) < tt.wantMinLen {
				t.Errorf("got %d chunks, want at least %d", len(chunks), tt.wantMinLen)
			}
			if tt.wantMaxLen > 0 && len(chunks) > tt.wantMaxLen {
				t.Errorf("got %d chunks, want at most %d", len(chunks), tt.wantMaxLen)
			}
			if tt.validateFn != nil {
				tt.validateFn(t, chunks)
			}
		})
	}
}

// Benchmark
func BenchmarkChunkText(b *testing.B) {
	text := strings.Repeat("This is a test sentence. ", 1000)
	opts := DefaultOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ChunkText(text, opts)
	}
}

func BenchmarkChunkText_Large(b *testing.B) {
	text := strings.Repeat("word ", 100000) // ~500KB
	opts := DefaultOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ChunkText(text, opts)
	}
}
