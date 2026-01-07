# Small Code Refactor Report
Date Created: 2026-01-07 23:03:36 +0100
Date Updated: 2026-01-08

## Opportunities (no diffs)
- Consolidate config sources (`internal/config` + `internal/tenant`) into a single loader to avoid divergent env parsing and duplicated defaults.
- Introduce small interfaces for Redis and provider clients to enable unit tests without external services and simplify dependency injection.
- Stream file ingestion (chunked read + incremental embed) instead of buffering full uploads in memory to reduce peak RAM usage.
- Centralize logging fields (tenant ID, client ID, request ID) in interceptors for consistent observability and easier audit trails.

## Fixed in v0.5.6
- Extract provider selection + request-building into shared helpers - FIXED: Added `prepareRequest()` helper to chat service
