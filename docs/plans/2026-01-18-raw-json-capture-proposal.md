# Proposal: Raw JSON Capture for Debug Inspector

## Overview

To fully populate the debug inspector with raw HTTP request/response bodies, we need to enable the existing capture infrastructure and pass the data through to persistence.

## Current State (Already Implemented!)

The infrastructure is already in place:

1. **`internal/httpcapture/transport.go`** - HTTP transport wrapper that captures request/response bodies
2. **All providers already use it** when `debug` mode is enabled
3. **`GenerateResult`** already has `RequestJSON` and `ResponseJSON` fields (lines 276-280 in provider.go)
4. **Providers return captured JSON** in those fields when debug is enabled

## What's Missing

Only two small changes needed:

### 1. Enable debug capture (choose one approach)

**Option A: Always capture (simplest)**
```go
// In internal/provider/openai/client.go (and anthropic, gemini, compat)
// Change from:
if c.debug {
    capture = httpcapture.New()
    ...
}

// To:
capture = httpcapture.New()  // Always capture
clientOpts = append(clientOpts, option.WithHTTPClient(capture.Client()))
```

**Option B: Config-driven capture (recommended for production)**
```go
// Add to GenerateParams
type GenerateParams struct {
    // ... existing fields ...
    EnableDebugCapture bool  // Enable raw JSON capture for debugging
}

// In providers:
if params.EnableDebugCapture {
    capture = httpcapture.New()
    ...
}
```

### 2. Pass captured JSON to persistence

```go
// In internal/service/chat.go, update persistConversation:
func (s *ChatService) persistConversation(ctx context.Context, req *pb.GenerateReplyRequest, result provider.GenerateResult, providerName, model string) {
    // ... existing tenant/user extraction ...

    // Build debug info from captured data
    var debugInfo *db.DebugInfo
    if len(result.RequestJSON) > 0 || len(result.ResponseJSON) > 0 {
        debugInfo = &db.DebugInfo{
            SystemPrompt:    req.Instructions,
            RawRequestJSON:  string(result.RequestJSON),
            RawResponseJSON: string(result.ResponseJSON),
        }
    }

    go func() {
        persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        err := s.repo.PersistConversationTurnWithDebug(
            persistCtx,
            threadID, tenantID, userID,
            req.UserInput, result.Text,
            providerName, model, result.ResponseID,
            inputTokens, outputTokens, processingTimeMs, costUSD,
            debugInfo,  // Pass debug info
        )
        if err != nil {
            slog.Error("failed to persist conversation", "error", err, ...)
        }
    }()
}
```

## Files to Modify

| File | Change |
|------|--------|
| `internal/provider/openai/client.go` | Enable capture (remove `if c.debug` check or add config flag) |
| `internal/provider/anthropic/client.go` | Enable capture |
| `internal/provider/gemini/client.go` | Enable capture |
| `internal/provider/compat/openai_compat.go` | Enable capture |
| `internal/service/chat.go` | Pass `result.RequestJSON`/`ResponseJSON` to persistence |

## Considerations

1. **Performance**: JSON capture adds ~10-50KB memory per request. For high-volume production, consider:
   - Config flag to enable/disable
   - Sampling (capture every Nth request)
   - Size limits (truncate large payloads)

2. **Streaming**: Current capture doesn't work with streaming responses. The SSE chunks aren't captured.
   Consider: Skip JSON capture for streaming, or accumulate chunks in stream handler.

3. **Privacy**: Raw JSON contains full prompts and responses. Mitigations:
   - Admin-only endpoint access
   - Optional: Redact sensitive fields before storage

4. **Storage**: Large payloads could bloat database. Consider:
   - JSONB compression (PostgreSQL does this automatically)
   - Max size limits (e.g., 100KB per field)
   - TTL-based cleanup for old debug data

## Implementation Effort

**Estimated: 30 minutes**

The infrastructure is already built - we just need to:
1. Remove the `if c.debug` guards in 4 provider files
2. Update `persistConversation()` to pass the captured JSON

Alternatively, add a config flag for more control over when capture is enabled.
