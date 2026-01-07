# Code Fix Report (2026-01-07-16) - Remaining Issues

## Overview
- Scope: manual review of core runtime, tenant, RAG, and file service code paths.
- Tests: not run (changes are provided as patch-ready diffs only).
- Note: No code was modified in the workspace per instruction; patches are provided for application.

## Last Updated
- 2026-01-07: Removed issues fixed in versions 0.4.5 through 0.5.3

---

## Remaining Issue

### Tenant ID normalization mismatch breaks lookups
**Impact**: `TenantInterceptor.resolveTenant` lowercases incoming `tenant_id`, but `loadTenants` stores tenant IDs as-is. Mixed-case IDs in config will never match, leading to `tenant not found` even when configured.

**Fix**: Normalize tenant IDs to lowercase and trimmed form during load so lookups and duplicate detection are consistent.

**Patch**:
```diff
diff --git a/internal/tenant/loader.go b/internal/tenant/loader.go
--- a/internal/tenant/loader.go
+++ b/internal/tenant/loader.go
@@
-		// Skip files without tenant_id (e.g., shared config files)
-		if cfg.TenantID == "" {
+		cfg.TenantID = strings.TrimSpace(cfg.TenantID)
+		// Skip files without tenant_id (e.g., shared config files)
+		if cfg.TenantID == "" {
 			continue
 		}
+		cfg.TenantID = strings.ToLower(cfg.TenantID)
```

---

## Notes / Follow-ups
- FileService still does not support multi-tenant selection because the proto lacks `tenant_id`; the fixes above use authenticated client ID as a namespace to reduce cross-client leakage.
- If you want `client_id` to remain required for CreateFileStore, consider adding `client_id` to Upload/Delete/Get requests (proto change) so the namespace stays consistent.

---

## Fixed Issues (removed from this report)
The following issues have been fixed and removed from this report:
- Issue 1: FileService namespaces and permissions - Fixed in v0.4.5
- Issue 3: Chunker panic edge case - Fixed in v0.5.1
- Issue 4: RAG_ENABLED env var override - Fixed in v0.5.3
