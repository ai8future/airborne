# rcodegen Bug Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix actionable bugs identified in rcodegen fix reports (Gemini, Claude)

**Architecture:** Direct fixes to streaming paths and test synchronization

**Tech Stack:** Go, gRPC, Anthropic SDK

---

## Summary of Fixes

| Priority | Issue | Source | Description |
|----------|-------|--------|-------------|
| HIGH | Missing Image Generation in Streaming | Gemini | `GenerateReplyStream` doesn't call `processImageGeneration` |
| HIGH | Failing Test - tenant_not_found | Claude | Test expects error but interceptor now falls back gracefully |
| MEDIUM | Anthropic Streaming Missing Thinking Blocks | Gemini | Only `TextDelta` handled, `ThinkingDelta` ignored |
| LOW | Data Race in tenantStream | Claude | No mutex protection on `tenantSet`/`tenantCfg` |

---

## Task 1: Add Image Generation to Streaming

**Files:**
- Modify: `internal/service/chat.go` (GenerateReplyStream method)

**Step 1: Add text accumulator variable**

After the `streamChunks` creation (around line 282), add:

```go
var accumulatedText strings.Builder
```

**Step 2: Accumulate text in ChunkTypeText case**

In the switch statement, after sending the text chunk, add:

```go
accumulatedText.WriteString(chunk.Text)
```

**Step 3: Call processImageGeneration in ChunkTypeComplete case**

Before building the final pbChunk for ChunkTypeComplete, add:

```go
// Check for image generation trigger in accumulated response
generatedImages := s.processImageGeneration(ctx, accumulatedText.String())
for _, img := range generatedImages {
    complete.Images = append(complete.Images, convertGeneratedImage(img))
}
```

**Step 4: Run tests**

```bash
go build ./... && go test ./internal/service/... -v
```

**Step 5: Commit**

```bash
git add internal/service/chat.go
git commit -m "feat(streaming): add image generation support to streaming path"
```

---

## Task 2: Fix Failing tenant_not_found Test

**Files:**
- Modify: `internal/auth/tenant_interceptor_test.go`

**Step 1: Understand the issue**

The test expects an error when tenant_not_found is sent, but the interceptor now extracts tenant_id from metadata headers and falls back to single-tenant mode. The mock stream doesn't set metadata, so the interceptor falls back instead of erroring.

**Step 2: Update test to provide metadata with the tenant ID**

Find the "tenant_not_found" test case and update the mockServerStream creation to include metadata context:

```go
// Add metadata with the tenant ID that should trigger the not-found error
md := metadata.Pairs("x-tenant-id", tt.recvMsg.TenantId)
ctx := metadata.NewIncomingContext(context.Background(), md)
ss := &mockServerStream{
    ctx:     ctx,  // Use context with metadata instead of context.Background()
    recvMsg: tt.recvMsg,
}
```

**Step 3: Run the specific test**

```bash
go test ./internal/auth/... -run TestStreamInterceptor_TenantExtraction/tenant_not_found -v
```

Expected: PASS

**Step 4: Run all auth tests**

```bash
go test ./internal/auth/... -v
```

**Step 5: Commit**

```bash
git add internal/auth/tenant_interceptor_test.go
git commit -m "fix(test): update tenant_not_found test to use metadata context"
```

---

## Task 3: Add Anthropic ThinkingDelta Support in Streaming

**Files:**
- Modify: `internal/provider/anthropic/client.go` (GenerateReplyStream method)

**Step 1: Verify ThinkingDelta exists in SDK**

Check the Anthropic SDK for the ThinkingDelta type:

```bash
grep -r "ThinkingDelta" $(go env GOPATH)/pkg/mod/github.com/anthropics/anthropic-sdk-go* 2>/dev/null | head -5
```

**Step 2: Add ThinkingDelta case handler**

In the streaming switch statement (around line 398), add handling for `ThinkingDelta` after the `TextDelta` case:

```go
case anthropic.ThinkingDelta:
    // Stream thinking content as text so users see model reasoning
    ch <- provider.StreamChunk{
        Type: provider.ChunkTypeText,
        Text: deltaVariant.Thinking,
    }
```

**Step 3: Run tests**

```bash
go build ./... && go test ./internal/provider/anthropic/... -v
```

**Step 4: Commit**

```bash
git add internal/provider/anthropic/client.go
git commit -m "feat(anthropic): add ThinkingDelta support in streaming"
```

---

## Task 4: Add Mutex to tenantStream (LOW PRIORITY)

**Files:**
- Modify: `internal/auth/tenant_interceptor.go`

**Step 1: Add sync import if not present**

Ensure `sync` is imported.

**Step 2: Add sync.Mutex to tenantStream struct**

```go
type tenantStream struct {
    grpc.ServerStream
    interceptor *TenantInterceptor
    mu          sync.Mutex  // Add this
    tenantSet   bool
    tenantCfg   *tenant.TenantConfig
}
```

**Step 3: Add mutex protection to Context() method**

```go
func (s *tenantStream) Context() context.Context {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.tenantCfg != nil {
        return context.WithValue(s.ServerStream.Context(), TenantContextKey, s.tenantCfg)
    }
    return s.ServerStream.Context()
}
```

**Step 4: Add mutex protection to RecvMsg() method**

```go
func (s *tenantStream) RecvMsg(m interface{}) error {
    s.mu.Lock()
    alreadySet := s.tenantSet
    s.mu.Unlock()

    if err := s.ServerStream.RecvMsg(m); err != nil {
        return err
    }

    if !alreadySet {
        tenantID := extractTenantID(m)
        cfg, err := s.interceptor.resolveTenant(tenantID)
        if err != nil {
            return err
        }
        s.mu.Lock()
        s.tenantCfg = cfg
        s.tenantSet = true
        s.mu.Unlock()
    }

    return nil
}
```

**Step 5: Run tests**

```bash
go test ./internal/auth/... -v
```

**Step 6: Commit**

```bash
git add internal/auth/tenant_interceptor.go
git commit -m "fix(auth): add mutex protection to tenantStream"
```

---

## Verification

After all tasks:

```bash
go test ./... 2>&1 | grep -E "(PASS|FAIL|ok)"
go vet ./...
```

---

## Report Updates

After each fix, update the corresponding report file:
1. Add `Date Updated: 2026-01-17` below `Date Created`
2. Remove the fixed items from the report
3. Leave any unfixed items in place
