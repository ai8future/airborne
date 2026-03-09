# chassis-go-addons integration bugs

**Date:** 2026-03-09

## Bug 1: rediskit error wrapping breaks IsNil check

**File:** `internal/redis/client.go`

When rediskit was integrated, `Get()` calls now return errors wrapped by rediskit (e.g., `rediskit: get "key": redis: nil`). The `IsNil()` function used `err == goredis.Nil` which fails on wrapped errors. Fixed by switching to `errors.Is(err, goredis.Nil)`.

## Bug 2: Removed isPrivateIP function still referenced in tests

**File:** `internal/validation/url_test.go`

The `isPrivateIP` function was removed from `url.go` when ssrfcheck was integrated, but `TestIsPrivateIP` still called it, causing a compile error. Rewrote as `TestIsBlockedIP` using `ssrfcheck.IsBlockedIP` with updated expectations (loopback is now correctly blocked). Removed orphaned `parseIPHelper`/`parseIPv4` test helpers.
