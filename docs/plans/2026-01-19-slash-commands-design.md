# Slash Commands and Image Trigger Design

**Date**: 2026-01-19
**Status**: Approved

## Overview

Implement slash command processing for user input, including fixing image generation triggers to work on user input (not AI responses).

## Commands

### `/image <prompt>`
- **Priority**: Highest (checked first)
- **Behavior**: Generate image directly, skip AI call entirely
- **Response**: Image only, no text
- **Example**: `/image a sunset over mountains` → generates and returns image

### `/ignore <text>`
- **Priority**: Normal (processed after /image check)
- **Behavior**: Strip `/ignore` and all text after it to end-of-line
- **Scope**: Line-level only; text on other lines still sent to AI
- **Example**:
  ```
  Hello world
  /ignore this is stripped
  Goodbye
  ```
  → Sends `"Hello world\nGoodbye"` to AI

### `@image <prompt>`
- **Behavior**: Alias for `/image`
- **Maintained for**: Backward compatibility with existing trigger phrase config

## Processing Order

1. Check for `/image` or configured trigger phrases (e.g., `@image`)
   - If found → extract prompt, generate image, return immediately
2. Process all `/ignore` occurrences
   - Strip from `/ignore` to end-of-line on each line
3. Check if any text remains
   - If empty → return minimal response, no AI call
   - If text remains → send to AI provider

## Implementation

### New Package: `internal/commands/`

**parser.go**
```go
package commands

// Result represents the outcome of parsing user input for commands.
type Result struct {
    // ProcessedText is the input after command processing (for AI)
    ProcessedText string

    // ImagePrompt is set if /image or trigger phrase was detected
    ImagePrompt string

    // SkipAI is true if no text should be sent to AI
    SkipAI bool
}

// Parser handles slash command detection and processing.
type Parser struct {
    imageTriggers []string // e.g., ["@image", "/image"]
}

// NewParser creates a parser with the given image trigger phrases.
func NewParser(imageTriggers []string) *Parser

// Parse processes user input for commands.
// Returns the result indicating what action to take.
func (p *Parser) Parse(input string) Result
```

### Changes to `internal/service/chat.go`

1. **In `prepareRequest()`**: Add command parsing before building provider params
2. **Remove**: `processImageGeneration()` calls from response handling (lines 276, 412)
3. **Add**: Early return path for image-only responses
4. **Add**: Early return path for empty input after `/ignore` processing

### Response Flow

```
User Input
    │
    ▼
┌─────────────────┐
│ Parse Commands  │
└────────┬────────┘
         │
    ┌────┴────┐
    │         │
    ▼         ▼
/image?    /ignore
    │         │
    ▼         ▼
Generate   Strip lines
 Image     with /ignore
    │         │
    ▼         │
 Return       ▼
 Image    Text empty?
 Only         │
         ┌────┴────┐
         │         │
         ▼         ▼
       Yes        No
         │         │
         ▼         ▼
      Return    Send to
      Empty      AI
```

## Files to Create/Modify

| File | Action |
|------|--------|
| `internal/commands/parser.go` | Create |
| `internal/commands/parser_test.go` | Create |
| `internal/service/chat.go` | Modify |

## Testing

- `/image sunset` → generates image, no AI call
- `@image sunset` → same as above (trigger phrase)
- `/ignore secret` → empty response, no AI call
- `Hello\n/ignore secret\nWorld` → AI receives "Hello\nWorld"
- `Question\n/image atom` → generates image only, ignores "Question"
- Mixed: `/ignore foo\n/image bar` → generates image of "bar"
