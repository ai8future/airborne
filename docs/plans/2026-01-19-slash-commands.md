# Slash Commands Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `/image` and `/ignore` slash commands to user input preprocessing, fix image triggers to work on user input instead of AI responses.

**Architecture:** New `internal/commands` package parses user input before AI call. `/image` triggers image generation and skips AI. `/ignore` strips text to end-of-line. Existing `imagegen.DetectImageRequest` is reused for trigger detection.

**Tech Stack:** Go, table-driven tests

---

### Task 1: Create Command Parser with Tests

**Files:**
- Create: `internal/commands/parser.go`
- Create: `internal/commands/parser_test.go`

**Step 1: Write the failing tests**

Create `internal/commands/parser_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/commands/... -v`
Expected: FAIL - package does not exist

**Step 3: Write the implementation**

Create `internal/commands/parser.go`:

```go
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
	skipAI := strings.TrimSpace(processed) == ""

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
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/commands/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/commands/
git commit -m "feat(commands): add slash command parser for /image and /ignore"
```

---

### Task 2: Integrate Parser into Chat Service

**Files:**
- Modify: `internal/service/chat.go`

**Step 1: Add import and modify prepareRequest**

In `internal/service/chat.go`, add import:

```go
"github.com/ai8future/airborne/internal/commands"
```

**Step 2: Add command parsing to preparedRequest struct**

After line 70 (`providerCfg provider.ProviderConfig`), add:

```go
	commandResult *commands.Result // Result of slash command parsing
```

**Step 3: Add command parsing in prepareRequest**

After the validation block (after line 109 `return nil, status.Error(codes.InvalidArgument, "user_input is required")`), add command parsing:

```go
	// Parse slash commands from user input
	var commandResult *commands.Result
	tenantCfg := auth.TenantFromContext(ctx)
	if tenantCfg != nil {
		// Build image triggers list: configured triggers + /image
		imageTriggers := append([]string{"/image"}, tenantCfg.ImageGeneration.TriggerPhrases...)
		parser := commands.NewParser(imageTriggers)
		parsed := parser.Parse(req.UserInput)
		commandResult = &parsed

		// If /image detected, we'll handle it after prepareRequest returns
		// If text is empty after /ignore processing, we'll skip AI
		if !parsed.SkipAI && parsed.ImagePrompt == "" {
			// Update UserInput with processed text (after /ignore removal)
			req.UserInput = parsed.ProcessedText
		}
	}
```

**Step 4: Store commandResult in preparedRequest**

In the return statement (around line 168), add `commandResult`:

```go
	return &preparedRequest{
		provider:      selectedProvider,
		params:        params,
		ragChunks:     ragChunks,
		requestID:     requestID,
		providerCfg:   providerCfg,
		commandResult: commandResult,
	}, nil
```

**Step 5: Commit**

```bash
git add internal/service/chat.go
git commit -m "feat(chat): integrate command parser into prepareRequest"
```

---

### Task 3: Handle Image Commands in GenerateReply

**Files:**
- Modify: `internal/service/chat.go`

**Step 1: Add early return for image command in GenerateReply**

In `GenerateReply` function, after `prepared, err := s.prepareRequest(ctx, req)` error check (around line 212), add:

```go
	// Handle slash commands
	if prepared.commandResult != nil {
		// Handle /image command - generate image and return immediately
		if prepared.commandResult.ImagePrompt != "" {
			images := s.generateImageFromCommand(ctx, prepared.commandResult.ImagePrompt)
			return &pb.GenerateReplyResponse{
				Text:     "",
				Provider: pb.Provider_PROVIDER_UNSPECIFIED,
				Images:   convertGeneratedImages(images),
			}, nil
		}

		// Handle empty input after /ignore processing
		if prepared.commandResult.SkipAI {
			return &pb.GenerateReplyResponse{
				Text:     "",
				Provider: pb.Provider_PROVIDER_UNSPECIFIED,
			}, nil
		}
	}
```

**Step 2: Add helper function for image generation from command**

Add this function (after `processImageGeneration`):

```go
// generateImageFromCommand generates an image from a slash command prompt.
func (s *ChatService) generateImageFromCommand(ctx context.Context, prompt string) []provider.GeneratedImage {
	if s.imageGen == nil {
		slog.Warn("image generation requested but imageGen client is nil")
		return nil
	}

	tenantCfg := auth.TenantFromContext(ctx)
	if tenantCfg == nil {
		slog.Warn("image generation requested but no tenant config")
		return nil
	}

	imgCfg := &imagegen.Config{
		Enabled:         tenantCfg.ImageGeneration.Enabled,
		Provider:        tenantCfg.ImageGeneration.Provider,
		Model:           tenantCfg.ImageGeneration.Model,
		TriggerPhrases:  tenantCfg.ImageGeneration.TriggerPhrases,
		FallbackOnError: tenantCfg.ImageGeneration.FallbackOnError,
		MaxImages:       tenantCfg.ImageGeneration.MaxImages,
	}

	if !imgCfg.IsEnabled() {
		slog.Warn("image generation requested but not enabled for tenant")
		return nil
	}

	imgReq := &imagegen.ImageRequest{
		Prompt: prompt,
		Config: imgCfg,
	}

	// Get API keys from tenant provider config
	if geminiCfg, ok := tenantCfg.GetProvider("gemini"); ok {
		imgReq.GeminiAPIKey = geminiCfg.APIKey
	}
	if openaiCfg, ok := tenantCfg.GetProvider("openai"); ok {
		imgReq.OpenAIAPIKey = openaiCfg.APIKey
	}

	slog.Info("slash command image generation",
		"provider", imgCfg.Provider,
		"prompt_preview", truncateString(prompt, 100),
	)

	img, err := s.imageGen.Generate(ctx, imgReq)
	if err != nil {
		slog.Error("slash command image generation failed", "error", err)
		return nil
	}

	return []provider.GeneratedImage{img}
}
```

**Step 3: Add helper to convert images**

Add this function if not already present:

```go
// convertGeneratedImages converts provider images to proto images.
func convertGeneratedImages(images []provider.GeneratedImage) []*pb.GeneratedImage {
	if len(images) == 0 {
		return nil
	}
	result := make([]*pb.GeneratedImage, len(images))
	for i, img := range images {
		result[i] = convertGeneratedImage(img)
	}
	return result
}
```

**Step 4: Remove old processImageGeneration call from GenerateReply**

Find and remove line 276:
```go
generatedImages := s.processImageGeneration(ctx, result.Text)
```

And update line 278-279 to remove image handling from regular response:
```go
// Remove these lines:
if len(generatedImages) > 0 {
    result.Images = generatedImages
}
```

**Step 5: Run tests**

Run: `go test ./internal/service/... -v`
Expected: PASS (or identify any failures to fix)

**Step 6: Commit**

```bash
git add internal/service/chat.go
git commit -m "feat(chat): handle /image and /ignore commands in GenerateReply"
```

---

### Task 4: Handle Commands in GenerateReplyStream

**Files:**
- Modify: `internal/service/chat.go`

**Step 1: Add early return for commands in GenerateReplyStream**

In `GenerateReplyStream` function, after `prepared, err := s.prepareRequest(ctx, req)` error check (around line 312), add:

```go
	// Handle slash commands
	if prepared.commandResult != nil {
		// Handle /image command - generate image and return immediately
		if prepared.commandResult.ImagePrompt != "" {
			images := s.generateImageFromCommand(ctx, prepared.commandResult.ImagePrompt)
			complete := &pb.StreamComplete{
				Provider: pb.Provider_PROVIDER_UNSPECIFIED,
			}
			for _, img := range images {
				complete.Images = append(complete.Images, convertGeneratedImage(img))
			}
			return stream.Send(&pb.GenerateReplyChunk{
				Chunk: &pb.GenerateReplyChunk_Complete{
					Complete: complete,
				},
			})
		}

		// Handle empty input after /ignore processing
		if prepared.commandResult.SkipAI {
			return stream.Send(&pb.GenerateReplyChunk{
				Chunk: &pb.GenerateReplyChunk_Complete{
					Complete: &pb.StreamComplete{
						Provider: pb.Provider_PROVIDER_UNSPECIFIED,
					},
				},
			})
		}
	}
```

**Step 2: Remove old processImageGeneration call from stream completion**

Find and remove line 412:
```go
generatedImages := s.processImageGeneration(ctx, accumulatedText.String())
```

And remove the loop that adds images to complete (around line 452-454):
```go
// Remove these lines:
for _, img := range generatedImages {
    complete.Images = append(complete.Images, convertGeneratedImage(img))
}
```

**Step 3: Run tests**

Run: `go test ./internal/service/... -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/service/chat.go
git commit -m "feat(chat): handle /image and /ignore commands in GenerateReplyStream"
```

---

### Task 5: Update processImageGeneration or Remove

**Files:**
- Modify: `internal/service/chat.go`

**Step 1: Decide on processImageGeneration**

Since image triggers now run on user input (via command parser), `processImageGeneration` on AI responses is no longer needed. However, keeping it allows AI responses to trigger images if that's ever desired.

**Option A (Remove):** Delete `processImageGeneration` function entirely.
**Option B (Keep but unused):** Leave it for potential future use.

Recommended: **Option A** - Remove to avoid confusion.

**Step 2: Remove processImageGeneration function**

Delete the entire function (lines ~746-810):
```go
// processImageGeneration checks for image generation triggers and generates images.
func (s *ChatService) processImageGeneration(ctx context.Context, responseText string) []provider.GeneratedImage {
    // ... entire function body
}
```

**Step 3: Run full test suite**

Run: `go test ./... -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/service/chat.go
git commit -m "refactor(chat): remove processImageGeneration (replaced by command parser)"
```

---

### Task 6: Final Testing and Version Bump

**Files:**
- Modify: `VERSION`
- Modify: `CHANGELOG.md`

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests pass

**Step 2: Build and verify**

Run: `go build ./cmd/airborne`
Expected: Successful build

**Step 3: Read VERSION and CHANGELOG**

Run: `cat VERSION && head -50 CHANGELOG.md`

**Step 4: Update VERSION**

Increment the version appropriately.

**Step 5: Update CHANGELOG.md**

Add entry at top:

```markdown
## [X.Y.Z] - 2026-01-19

### Added
- Slash command support: `/image <prompt>` generates image directly without AI call
- Slash command support: `/ignore <text>` strips text to end-of-line before sending to AI
- New `internal/commands` package for user input preprocessing

### Changed
- Image generation triggers (`@image`, etc.) now work on user input instead of AI responses
- Empty input after `/ignore` processing returns minimal response without AI call

### Removed
- `processImageGeneration` function (replaced by command parser on user input)
```

**Step 6: Commit and push**

```bash
git add VERSION CHANGELOG.md
git commit -m "chore: bump version for slash commands feature

Added /image and /ignore slash commands.
Fixed image triggers to work on user input.

Claude:Opus 4.5

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
git push
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Create command parser with tests | `internal/commands/parser.go`, `internal/commands/parser_test.go` |
| 2 | Integrate parser into prepareRequest | `internal/service/chat.go` |
| 3 | Handle commands in GenerateReply | `internal/service/chat.go` |
| 4 | Handle commands in GenerateReplyStream | `internal/service/chat.go` |
| 5 | Remove old processImageGeneration | `internal/service/chat.go` |
| 6 | Final testing and version bump | `VERSION`, `CHANGELOG.md` |
