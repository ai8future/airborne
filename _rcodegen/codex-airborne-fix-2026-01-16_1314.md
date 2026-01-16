# Airborne Bug and Code Smell Analysis Report
Date Created: 2026-01-16 13:14:38 +0100
Date Updated: 2026-01-16 (Claude:Opus 4.5)

## Scope and Validation
- Scanned core auth, service, provider, RAG, and validation code paths for correctness and safety issues.
- Tests executed: `go test ./...`, `go vet ./...`.

## Findings and Fixes

### ~~1) High: Tenant provider config mutation leaks across requests~~ **FIXED**
- **Status:** Fixed in v1.0.2
- Location: `internal/service/chat.go`
- Issue: `buildProviderConfig` assigns `tenant.ProviderConfig.ExtraOptions` directly to the request config and then merges request overrides into it. Because maps are reference types, this mutates the shared tenant config map.
- Fix: Deep-copy the tenant `ExtraOptions` map before applying request overrides.

### 2) Medium: Gemini file upload fallback discards file data and reads entire file into memory **DEFERRED**
- Location: `internal/provider/gemini/filestore.go`
- Issue: `UploadFileToFileSearchStore` reads the entire file into memory, then attempts a "metadata fallback" that posts JSON metadata without the file body.
- Status: **DEFERRED** - Requires significant refactor for streaming upload support

## Notes
- Finding #1 was fixed in v1.0.2
