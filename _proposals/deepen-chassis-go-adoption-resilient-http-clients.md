# Deepen Chassis-Go Adoption: Resilient HTTP Clients

**Date:** February 3, 2026

## Summary

Airborne already integrates chassis-go for logging, lifecycle, middleware, and health checks. This proposal deepens adoption by replacing 6 raw `http.Client` instances with chassis-go's `call.Client` (adding retries, circuit breaking, and timeout enforcement), enriching health checks with RAG dependency monitoring, and logging the chassis version at startup. The plan is surgical — it changes what benefits from the toolkit and explicitly documents what should stay as-is and why.

---

## Current State

**Already using chassis-go (since v1.8.0):**
- `logz` — structured JSON logging with trace IDs
- `lifecycle` — coordinated graceful shutdown (SIGTERM/SIGINT)
- `grpckit` — gRPC interceptors (recovery, logging) + health registration
- `httpkit` — HTTP middleware (recovery, request ID, logging)
- `health` — parallel health check handler at `/admin/healthz`
- `testkit` — test environment helpers

**Not yet adopted:**
- `call.Client` — resilient HTTP client (was skipped in v1.8.0 due to a context cancellation bug)
- `config.MustLoad` — env-based config (airborne has a more sophisticated YAML+Doppler system)

## Proposed Changes

### Replace Raw HTTP Clients with call.Client (Tasks 1-5, 9)

Six `&http.Client{}` instances across the codebase make outbound HTTP calls without retries or circuit breaking:

| Location | Service | Current | Proposed |
|---|---|---|---|
| `internal/rag/vectorstore/qdrant.go:39` | Qdrant vector DB | bare client | call.Client + retry(3) + breaker("qdrant") |
| `internal/rag/embedder/ollama.go:64` | Ollama embeddings | bare client | call.Client + retry(3) + breaker("ollama") |
| `internal/rag/extractor/docbox.go:50` | Docbox text extraction | bare client | call.Client + retry(3) + breaker("docbox") |
| `internal/imagegen/gemini.go:106` | Gemini image gen | per-request client | shared call.Client + retry(2) + breaker("gemini-imagegen") |
| `internal/tenant/doppler.go:45` | Doppler secrets | bare client | call.Client + retry(3), no breaker |
| `internal/config/config.go:416` | Doppler (config) | bare client | call.Client + retry(3), no breaker |

### Enrich Health Checks (Task 6)

Add Qdrant and Ollama ping checks to the existing `health.Handler` at `/admin/healthz`. Currently the endpoint only checks basic self-health. With RAG dependencies monitored, operators get real visibility into whether the full system is functional.

### Log chassis.Version at Startup (Task 8)

Per INTEGRATING.md recommendation, log the chassis-go library version during startup for production diagnostics and upgrade tracking.

### Document Retry Package Scope (Task 7)

`internal/retry` and `call.Client` operate at different layers. Document the distinction: `internal/retry` handles SDK/provider-level retries (around OpenAI, Anthropic SDKs), while `call.Client` handles HTTP transport-level retries (raw HTTP calls to infrastructure services).

## What This Does NOT Change

| Component | Reason |
|---|---|
| `internal/config/` (YAML+Doppler loader) | More sophisticated than `config.MustLoad` — supports YAML, Doppler, ENV/FILE prefixes, frozen configs |
| `internal/retry/` package | Different layer — retries SDK operations, not HTTP transport |
| `internal/httpcapture/` | Debugging transport wrapper, not an outbound call pattern |
| `os.Getenv` in `internal/tenant/` | Part of well-designed tenant config system |
| CLI tool HTTP clients | Low-criticality, no retry needed |

## Risk Assessment

- **Pre-requisite:** Verify the `call.Client` context cancellation bug from v1.8.0 is resolved before executing Tasks 1-5 and 9
- **Circuit breaker names** are global singletons — names chosen are service-specific to avoid collisions
- **Retry budgets** are conservative (2-3 attempts) to avoid cascading latency

## Implementation Plan

Full step-by-step plan with TDD approach: `docs/plans/2026-02-03-deepen-chassis-go-adoption.md`

Use `superpowers:executing-plans` to implement.
