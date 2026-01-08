# Small Code Test Report
Date Created: 2026-01-07 23:03:36 +0100
Date Updated: 2026-01-08

## Fixed in v0.5.14-0.6.1

### 1) Chat service helper logic - FIXED (v0.6.1)
Added tests for tenant-aware provider selection, config merging, and RAG prompt formatting in `internal/service/chat_test.go`.

### 2) Admin service authorization/health responses - FIXED (v0.6.1)
Added tests for Health/Ready/Version endpoints with permission checks in `internal/service/admin_test.go`.

### 3) Tenant interceptor behavior - FIXED (v0.6.0)
Added tests for tenant resolution and FileService skip behavior in `internal/auth/tenant_interceptor_test.go`.

### 4) Config loader env overrides - FIXED (v0.5.14)
Added tests for environment variable overrides and ${VAR} expansion in `internal/config/config_test.go`.

## All issues from this report have been fixed.
