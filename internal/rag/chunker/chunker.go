// Package chunker provides text chunking for RAG pipelines.
package chunker

import (
	"strings"
	"unicode"
)

// Chunk represents a segment of text with position information.
type Chunk struct {
	// Index is the chunk's position in the sequence (0-based).
	Index int

	// Text is the chunk content.
	Text string

	// Start is the character offset in the original text.
	Start int

	// End is the ending character offset in the original text.
	End int
}

// Options configures the chunking behavior.
type Options struct {
	// ChunkSize is the target size in characters (default: 2000).
	ChunkSize int

	// Overlap is the number of overlapping characters between chunks (default: 200).
	Overlap int

	// MinChunkSize is the minimum chunk size; smaller chunks are merged (default: 100).
	MinChunkSize int
}

// DefaultOptions returns the default chunking options.
func DefaultOptions() Options {
	return Options{
		ChunkSize:    2000,
		Overlap:      200,
		MinChunkSize: 100,
	}
}

// ChunkText splits text into overlapping chunks.
// It attempts to split on paragraph and sentence boundaries when possible.
func ChunkText(text string, opts Options) []Chunk {
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 2000
	}
	if opts.Overlap < 0 {
		opts.Overlap = 0
	}
	if opts.Overlap >= opts.ChunkSize {
		opts.Overlap = opts.ChunkSize / 4
	}
	if opts.MinChunkSize <= 0 {
		opts.MinChunkSize = 100
	}

	text = strings.TrimSpace(text)
	if len(text) == 0 {
		return nil
	}

	// If text is smaller than chunk size, return as single chunk
	if len(text) <= opts.ChunkSize {
		return []Chunk{{
			Index: 0,
			Text:  text,
			Start: 0,
			End:   len(text),
		}}
	}

	var chunks []Chunk
	start := 0

	for start < len(text) {
		end := start + opts.ChunkSize
		if end > len(text) {
			end = len(text)
		}

		// Try to find a good break point (paragraph, sentence, or word boundary)
		if end < len(text) {
			breakPoint := findBreakPoint(text, start, end)
			if breakPoint > start+opts.MinChunkSize {
				end = breakPoint
			}
		}

		chunkText := strings.TrimSpace(text[start:end])
		if len(chunkText) >= opts.MinChunkSize || start+opts.ChunkSize >= len(text) {
			chunks = append(chunks, Chunk{
				Index: len(chunks),
				Text:  chunkText,
				Start: start,
				End:   end,
			})
		}

		// Move start forward, accounting for overlap
		start = end - opts.Overlap
		if len(chunks) > 0 && start <= chunks[len(chunks)-1].Start {
			// Prevent infinite loop if overlap is too large
			start = end
		}
	}

	return chunks
}

// findBreakPoint looks for a good place to break the text.
// It prefers paragraph breaks, then sentence breaks, then word breaks.
func findBreakPoint(text string, start, end int) int {
	segment := text[start:end]

	// Look for paragraph break (double newline) in the last 20% of the segment
	searchStart := len(segment) * 80 / 100
	if idx := strings.LastIndex(segment[searchStart:], "\n\n"); idx != -1 {
		return start + searchStart + idx + 2
	}

	// Look for single newline in the last 30% of the segment
	searchStart = len(segment) * 70 / 100
	if idx := strings.LastIndex(segment[searchStart:], "\n"); idx != -1 {
		return start + searchStart + idx + 1
	}

	// Look for sentence boundary (. ! ?) followed by space in the last 30%
	searchStart = len(segment) * 70 / 100
	for i := len(segment) - 1; i >= searchStart; i-- {
		if i+1 < len(segment) && isSentenceEnd(rune(segment[i])) && unicode.IsSpace(rune(segment[i+1])) {
			return start + i + 1
		}
	}

	// Look for word boundary (space) in the last 20%
	searchStart = len(segment) * 80 / 100
	if idx := strings.LastIndex(segment[searchStart:], " "); idx != -1 {
		return start + searchStart + idx + 1
	}

	// No good break point found, use the end
	return end
}

func isSentenceEnd(r rune) bool {
	return r == '.' || r == '!' || r == '?'
}
