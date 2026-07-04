# Failover-Success Turns Were Never Persisted

**Date:** 2026-07-04
**Severity:** High (silent data loss on a supported, intentional code path)
**Status:** Fixed in commit `7514abe`

## Problem

`GenerateReply` (`internal/service/chat.go`) supports automatic failover: if the primary provider
errors and `EnableFailover` is set, a fallback provider is tried. When the **fallback succeeded**,
the handler returned the response directly (`s.buildResponse(fallbackResult, ...)`) without ever
calling `s.persistConversation`. The primary-success path always persisted; the failover-success
path silently did not.

## Impact

- No `chat_message` row was written for the assistant's fallback reply — the turn vanished from
  chat history on next load.
- No activity/debug row, so the request was invisible in the admin dashboard and cost tracking.
- No cost/usage accounting for the fallback call.
- After A10 (idempotent `GenerateReply` with `external_ref` correlation), a caller replaying by
  `external_ref` would find nothing to correlate to, since no row was ever written.

This was a real-traffic-path bug, not a corner case: any tenant with failover enabled and a
flaky/rate-limited primary provider would lose data exactly when failover was doing its job.

## Fix

`internal/service/chat.go` (~L369–401): the fallback-success branch now routes through the same
`s.persistConversation(...)` call as the primary path, guarded by `s.dbClient != nil &&
fallbackResult.Usage != nil` — identical to the primary path's guard. Same async-goroutine
semantics, same `external_ref` correlation via `externalRefFromRequest(req)`, tokens/cost/debug
taken from the fallback result instead of the primary one.

As part of the same fix, the fallback config now resolves model aliases through
`s.applyModelRegistry` (previously only `buildProviderConfig`), matching the primary path — so a
registered alias resolves correctly on the failover path too, not just the primary one.

## Verification

`go test -mod=mod -count=1 ./internal/service/...` passing; idempotency/failover tests in
`internal/service/idempotency_test.go` and `internal/service/chat_test.go` cover the persisted
path. See `.superpowers/sdd/task-8-report.md` ("Fix 1") for the full review trail.
