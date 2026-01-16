# Codebase Analysis & Fix Report

**Date Created:** 2026-01-16 14:10:00
**Date Updated:** 2026-01-16 (Claude:Opus 4.5)

## Summary
This report analyzes the `airborne` codebase for bugs, performance issues, and code smells.

## Findings & Fixes

### ~~1. Logic Error: Anthropic History Truncation (Critical)~~ **FIXED**
**Status:** Fixed in v1.0.3
**File:** `internal/provider/anthropic/client.go`
- Issue: `buildMessages` function was discarding newest messages instead of oldest when truncating
- Fix: Reversed truncation logic to prioritize most recent messages

### ~~2. Resource Leak: Unclosed Streams (Major)~~ **FIXED**
**Status:** Fixed in v1.0.4
**Files:** `internal/provider/openai/client.go`, `internal/provider/anthropic/client.go`
- Issue: Streaming goroutines didn't call `Close()` on stream objects
- Fix: Added `defer stream.Close()` to both streaming implementations

### 3. Performance Bottleneck: Bcrypt on Every Request (Major) **DEFERRED**
**File:** `internal/auth/keys.go`
- Issue: `ValidateKey` uses `bcrypt.CompareHashAndPassword` on every request (~100ms+)
- Status: **DEFERRED** - Requires architectural decision (cache vs hash algorithm change)

### ~~4. Observability Issue: Missing Error Details in Logs~~ **FIXED**
**Status:** Fixed in v1.0.5
**File:** `internal/errors/sanitize.go`
- Issue: Error details were not logged when sanitizing for client
- Fix: Added error to log arguments: `slog.Error("provider error occurred", "error", err)`

### 5. Memory Risk: Redis Scan (Low) **DEFERRED**
**File:** `internal/redis/client.go`
- Issue: `Scan` accumulates all keys in memory
- Status: **DEFERRED** - Low priority, only affects very large datasets

### 6. Stability Risk: RAG Ingestion Batching **DEFERRED**
**File:** `internal/rag/service.go`
- Issue: Large files processed in single batch
- Status: **DEFERRED** - Enhancement for large file handling

### 7. Security Risk: File Upload Validation **DEFERRED**
**File:** `internal/service/files.go`
- Issue: No magic byte validation for uploaded files
- Status: **DEFERRED** - Enhancement

### 8. Configuration Limitation: Secret Paths **DEFERRED**
**File:** `internal/tenant/secrets.go`
- Issue: Hardcoded secret directories
- Status: **DEFERRED** - Enhancement

### 9. Observability Limitation: Unreachable Debug Logging **DEFERRED**
**File:** `internal/service/chat.go`
- Issue: No config flag to enable provider debug logging
- Status: **DEFERRED** - Enhancement

## Summary
- Fixed: #1, #2, #4 (Critical, Major, Medium)
- Deferred: #3, #5, #6, #7, #8, #9 (various priorities)
