# Rcodegen Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix resource leaks, improve logging, and address non-deterministic behavior identified in rcodegen audit reports.

**Architecture:** Targeted fixes to individual files - no architectural changes. Each fix is isolated and independently verifiable.

**Tech Stack:** Go, gRPC, slog

---

## Task 1: Add stream.Close() to OpenAI-compat Streaming

**Files:**
- Modify: `internal/provider/compat/openai_compat.go:300`

**Step 1: Read current code and verify issue**
Verify line 300 has `stream := client.Chat.Completions.NewStreaming(ctx, reqParams)` without a corresponding `defer stream.Close()`.

**Step 2: Add defer stream.Close()**
```go
// Change from:
stream := client.Chat.Completions.NewStreaming(ctx, reqParams)
var fullText strings.Builder

// To:
stream := client.Chat.Completions.NewStreaming(ctx, reqParams)
defer stream.Close()
var fullText strings.Builder
```

**Step 3: Verify compilation**
Run: `go build ./internal/provider/compat/...`

**Step 4: Run tests**
Run: `go test ./internal/provider/compat/...`

---

## Task 2: Add stream.Close() to Anthropic Thinking Mode

**Files:**
- Modify: `internal/provider/anthropic/client.go:189`

**Step 1: Read current code and verify issue**
Verify the thinking-enabled block creates a stream without closing it:
```go
if thinkingEnabled {
    stream := client.Messages.NewStreaming(reqCtx, reqParams)
    // ... loop ...
    // No stream.Close()
}
```

**Step 2: Add stream.Close() after the loop**
```go
// Change from:
if stream.Err() != nil {
    err = stream.Err()
} else if err == nil {
    resp = &accumulated
}

// To:
stream.Close()
if stream.Err() != nil {
    err = stream.Err()
} else if err == nil {
    resp = &accumulated
}
```

**Step 3: Verify compilation**
Run: `go build ./internal/provider/anthropic/...`

**Step 4: Run tests**
Run: `go test ./internal/provider/anthropic/...`

---

## Task 3: Add Context Cancellation Check in Gemini Streaming

**Files:**
- Modify: `internal/provider/gemini/client.go` (streaming loop)

**Step 1: Find the streaming loop**
Look for `for resp, err := range client.Models.GenerateContentStream(ctx, model, contents, generateConfig)`

**Step 2: Add context check at start of loop**
```go
// Add at the start of the streaming loop:
for resp, err := range client.Models.GenerateContentStream(ctx, model, contents, generateConfig) {
    // Check for context cancellation before processing each response
    select {
    case <-ctx.Done():
        return
    default:
    }

    // ... existing code ...
}
```

**Step 3: Verify compilation**
Run: `go build ./internal/provider/gemini/...`

**Step 4: Run tests**
Run: `go test ./internal/provider/gemini/...`

---

## Task 4: Change httpcapture Logging from Info to Debug

**Files:**
- Modify: `internal/httpcapture/transport.go`

**Step 1: Find all slog.Info calls**
Search for `slog.Info("httpcapture:` in the file.

**Step 2: Replace all with slog.Debug**
```go
// Change all occurrences of:
slog.Info("httpcapture:

// To:
slog.Debug("httpcapture:
```

There should be 4 occurrences:
- "httpcapture: RoundTrip called"
- "httpcapture: captured request body"
- "httpcapture: response received"
- "httpcapture: captured response body"

**Step 3: Verify compilation**
Run: `go build ./internal/httpcapture/...`

**Step 4: Run tests**
Run: `go test ./internal/httpcapture/...`

---

## Task 5: Make DefaultProvider Deterministic

**Files:**
- Modify: `internal/tenant/config.go`

**Step 1: Read the DefaultProvider function**
Find the fallback loop that iterates over `tc.Providers` map.

**Step 2: Add sort import if not present**
```go
import (
    // ... existing imports ...
    "sort"
)
```

**Step 3: Replace non-deterministic map iteration**
```go
// Change from:
// Fall back to first enabled provider
for name, cfg := range tc.Providers {
    if cfg.Enabled {
        return name, cfg, true
    }
}

// To:
// Fall back to first enabled provider (deterministic order)
names := make([]string, 0, len(tc.Providers))
for name := range tc.Providers {
    names = append(names, name)
}
sort.Strings(names)

for _, name := range names {
    cfg := tc.Providers[name]
    if cfg.Enabled {
        return name, cfg, true
    }
}
```

**Step 4: Verify compilation**
Run: `go build ./internal/tenant/...`

**Step 5: Run tests**
Run: `go test ./internal/tenant/...`

---

## Task 6: Add Context Check Before Persistence Goroutine

**Files:**
- Modify: `internal/service/chat.go` (persistConversation function)

**Step 1: Find the goroutine in persistConversation**
Look for `go func()` that creates `persistCtx`.

**Step 2: Add context check before goroutine**
```go
// Add before the goroutine:
// Check if context is already cancelled to avoid unnecessary work
if ctx.Err() != nil {
    slog.Debug("skipping persistence, context cancelled")
    return
}

go func() {
    // ... existing goroutine code ...
}()
```

**Step 3: Verify compilation**
Run: `go build ./internal/service/...`

**Step 4: Run tests**
Run: `go test ./internal/service/...`

---

## Task 7: Add Error Check to fmt.Sscanf in Gemini

**Files:**
- Modify: `internal/provider/gemini/client.go`

**Step 1: Find the Sscanf call**
Search for `fmt.Sscanf(thinkingBudgetStr, "%d", &budget)`.

**Step 2: Add error handling**
```go
// Change from:
if thinkingBudgetStr != "" {
    var budget int
    fmt.Sscanf(thinkingBudgetStr, "%d", &budget)
    if budget > 0 {

// To:
if thinkingBudgetStr != "" {
    var budget int
    if _, err := fmt.Sscanf(thinkingBudgetStr, "%d", &budget); err != nil {
        slog.Warn("invalid thinking_budget value", "value", thinkingBudgetStr, "error", err)
        budget = 0
    }
    if budget > 0 {
```

**Step 3: Check if this pattern exists in streaming method too**
If yes, apply the same fix.

**Step 4: Verify compilation**
Run: `go build ./internal/provider/gemini/...`

**Step 5: Run tests**
Run: `go test ./internal/provider/gemini/...`

---

## Verification

After all tasks:

1. **Full build check**: `go build ./...`
2. **Full test suite**: `go test ./...`
3. **Update VERSION and CHANGELOG.md**
4. **Commit and push**

---

## Report Updates

After implementing fixes, update each source report:
- Add `Date Updated: 2026-01-28` below Date Created
- Remove fixed items from the report
- Keep unfixed items for future reference
