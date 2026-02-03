# chassis-go call.Client Context Cancellation Bug

**Date:** 2026-02-03
**Severity:** Medium (design issue, not a runtime crash)
**Status:** Upstream issue identified, not yet fixed

## Problem

`call.Client.Do()` wraps the request context with `context.WithTimeout()` and uses `defer cancel()`. When `Do()` returns, the deferred cancel fires immediately, canceling the context that the response body is being read through.

This means any caller that reads `resp.Body` **after** `Do()` returns will get `context canceled` errors, because the context tied to the HTTP response has already been canceled.

## Code Path

In `chassis-go/call/call.go`:
```go
func (c *Client) Do(req *http.Request) (*http.Response, error) {
    ctx := req.Context()
    if _, ok := ctx.Deadline(); !ok {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, c.timeout)
        defer cancel()  // <-- This cancels the response body's context
        req = req.WithContext(ctx)
    }
    // ... execute request ...
    return resp, err  // caller reads resp.Body AFTER cancel() fires
}
```

## Impact

Cannot use `call.Client` as a drop-in replacement for `*http.Client` in code that follows the standard Go pattern:
```go
resp, err := client.Do(req)
defer resp.Body.Close()
json.NewDecoder(resp.Body).Decode(&result)  // FAILS: context canceled
```

This affects all three RAG clients (ollama, qdrant, docbox) in Airborne.

## Suggested Fix (upstream)

The `http.Client.Timeout` field (which `call.New` already sets) handles request timeouts without canceling the response body context. Remove the redundant `context.WithTimeout` wrapper, or only cancel on error paths.
