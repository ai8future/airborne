// Package commands handles slash command parsing for user input.
package commands

import (
	"strings"
)

// Result represents the outcome of parsing user input for commands.
type Result struct {
	// ProcessedText is the input after command processing (to send to AI)
	ProcessedText string

	// ImagePrompt is set if /image or trigger phrase was detected
	ImagePrompt string

	// SkipAI is true if no text should be sent to AI
	SkipAI bool
}

// Parser handles slash command detection and processing.
type Parser struct {
	imageTriggers []string
}

// NewParser creates a parser with the given image trigger phrases.
// Triggers should include the prefix (e.g., "@image", "/image").
func NewParser(imageTriggers []string) *Parser {
	return &Parser{
		imageTriggers: imageTriggers,
	}
}

// Parse processes user input for slash commands.
// Processing order:
// 1. Check for image triggers (/image, @image, etc.) - if found, return immediately
// 2. Process /ignore commands - strip from /ignore to end-of-line
// 3. If remaining text is empty/whitespace, set SkipAI
func (p *Parser) Parse(input string) Result {
	// Step 1: Check for image triggers (highest priority)
	if imagePrompt := p.detectImageTrigger(input); imagePrompt != "" {
		return Result{
			ProcessedText: "",
			ImagePrompt:   imagePrompt,
			SkipAI:        true,
		}
	}

	// Step 2: Process /ignore commands
	processed := p.processIgnore(input)

	// Step 3: Check if anything remains
	trimmed := strings.TrimSpace(processed)
	skipAI := trimmed == ""

	// If only whitespace remains, return empty ProcessedText
	if skipAI {
		processed = ""
	}

	return Result{
		ProcessedText: processed,
		ImagePrompt:   "",
		SkipAI:        skipAI,
	}
}

// detectImageTrigger checks for image triggers and extracts the prompt.
// Returns empty string if no trigger found.
func (p *Parser) detectImageTrigger(input string) string {
	if len(p.imageTriggers) == 0 {
		return ""
	}

	lowerInput := strings.ToLower(input)

	for _, trigger := range p.imageTriggers {
		lowerTrigger := strings.ToLower(strings.TrimSpace(trigger))
		if lowerTrigger == "" {
			continue
		}

		idx := strings.Index(lowerInput, lowerTrigger)
		if idx != -1 {
			// Extract prompt: everything after the trigger phrase
			promptStart := idx + len(lowerTrigger)
			prompt := strings.TrimSpace(input[promptStart:])
			if prompt != "" {
				return prompt
			}
		}
	}

	return ""
}

// processIgnore removes /ignore and everything after it to end-of-line.
func (p *Parser) processIgnore(input string) string {
	lines := strings.Split(input, "\n")
	var result []string

	for _, line := range lines {
		processed := p.processIgnoreLine(line)
		// Only include non-empty lines (after trimming the ignored part)
		// But preserve lines that were originally just whitespace
		if processed != "" || !strings.Contains(strings.ToLower(line), "/ignore") {
			if processed != "" {
				result = append(result, processed)
			}
		}
	}

	return strings.Join(result, "\n")
}

// processIgnoreLine handles /ignore within a single line.
func (p *Parser) processIgnoreLine(line string) string {
	lowerLine := strings.ToLower(line)
	idx := strings.Index(lowerLine, "/ignore")
	if idx == -1 {
		return line
	}

	// Keep everything before /ignore, trim trailing whitespace
	return strings.TrimRight(line[:idx], " \t")
}
