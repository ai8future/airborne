# Codebase Concerns

**Analysis Date:** 2026-01-14

## Tech Debt

**Lingering AIBOX_* Environment Variables:**
- Issue: Project renamed from "aibox" to "airborne" but old env var names remain in tenant package
- Files: `internal/tenant/env.go` (lines 51-129), `internal/server/grpc.go` (line 84)
- Why: Incomplete migration during rename
- Impact: Deployments using AIRBORNE_* vars won't work with tenant configuration; only legacy single-tenant mode uses new names
- Fix approach: Update `internal/tenant/env.go` to read AIRBORNE_* variables, keeping AIBOX_* as deprecated fallback

**Duplicate Configuration Logic:**
- Issue: Nearly identical environment variable loading in two places
- Files: `internal/config/config.go` (lines 185-276), `internal/tenant/env.go` (lines 37-129)
- Why: Multi-tenant support added separately from main config
- Impact: Maintenance burden, inconsistent behavior possible
- Fix approach: Extract shared parsing logic to common utility

## Known Bugs

**None detected** - codebase appears stable

## Security Considerations

**Environment Variable Expansion in Config:**
- Risk: Config values containing `$` could be unexpectedly expanded
- File: `internal/config/config.go` (lines 287-292)
- Current mitigation: Only `${VAR}` pattern explicitly handled, others passed to `os.ExpandEnv()`
- Recommendations: Document behavior, consider escaping mechanism for literal `$`

**SSRF Prevention:**
- Risk: Custom base URLs for LLM providers could target internal services
- Files: `internal/validation/url.go`, all provider clients
- Current mitigation: URL validation blocks private IPs, dangerous protocols
- Recommendations: Current implementation is solid

## Performance Bottlenecks

**None detected** - service is primarily I/O bound to external LLM APIs

## Fragile Areas

**Interceptor Chain Order:**
- File: `internal/server/grpc.go`
- Why fragile: Four interceptors must run in specific order (recovery → logging → tenant → auth)
- Common failures: Changing order breaks authentication flow
- Safe modification: Add tests before changing, document dependencies
- Test coverage: Limited integration tests for full chain

## Scaling Limits

**Rate Limiting State:**
- Current capacity: Depends on Redis capacity
- Limit: Redis memory for rate limit counters
- Symptoms at limit: Rate limit checks fail
- Scaling path: Redis cluster or disable rate limiting

## Dependencies at Risk

**None detected** - all dependencies are actively maintained:
- OpenAI SDK: v1.12.0 (active)
- Anthropic SDK: v1.19.0 (active)
- Google GenAI: v1.40.0 (active)
- go-redis: v9.17.2 (active)

## Missing Critical Features

**ListFileStores Not Implemented:**
- Problem: Returns `codes.Unimplemented` error
- File: `internal/service/files.go` (line 286)
- Current workaround: None, feature unavailable
- Blocks: Client applications cannot list file stores for RAG
- Implementation complexity: Low (iterate collections in Qdrant)

## Test Coverage Gaps

**Ignored Rate Limit Errors:**
- What's not tested: Error handling when Redis Expire fails
- Files: `internal/auth/ratelimit.go` (line 109), `internal/service/chat.go` (lines 227, 321)
- Risk: Rate limit counters could persist indefinitely
- Priority: Low (Redis rarely fails for expire operations)
- Difficulty to test: Need Redis failure injection

**Unused Development Auth Interceptors:**
- What: Dead code - development auth interceptors defined but never used
- Files: `internal/server/grpc.go` (lines 325-371)
- Risk: None (harmless)
- Priority: Low
- Action: Remove or document purpose

## Documentation Gaps

**Missing .env.example:**
- Problem: No template for required/optional environment variables
- Current workaround: Read source code or CHANGELOG
- Recommendation: Create `.env.example` in repository root

**RAG Configuration:**
- Problem: ChunkSize/ChunkOverlap valid ranges not documented
- File: `internal/rag/service.go`
- Recommendation: Add GoDoc comments with valid ranges

---

*Concerns audit: 2026-01-14*
*Update as issues are fixed or new ones discovered*
