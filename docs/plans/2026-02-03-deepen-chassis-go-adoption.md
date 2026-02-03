# Deepen Chassis-Go Adoption Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace custom HTTP clients and retry logic with chassis-go's `call.Client` for resilient outbound calls, adopt `health.Handler` more broadly, and consolidate scattered `os.Getenv` calls where practical.

**Architecture:** The airborne codebase already integrates chassis-go for logging (`logz`), lifecycle management (`lifecycle`), HTTP/gRPC middleware (`httpkit`/`grpckit`), health checks (`health`), and test helpers (`testkit`). This plan deepens adoption by replacing 6 raw `http.Client{}` instances with `call.Client` (retries, circuit breaking, timeouts), eliminating the custom `internal/retry` package in favor of chassis-go's built-in retry, and adding health checks for external dependencies (Qdrant, Ollama, Docbox).

**Tech Stack:** Go 1.25.5, chassis-go (call, health, config, testkit)

**Note:** The CHANGELOG for v1.8.0 mentioned `call.Client` was skipped due to a "context cancellation issue with response body reading." Before executing this plan, verify that issue has been resolved in the current chassis-go version. If not, Tasks 1-4 should be deferred.

---

### Task 1: Replace RAG Qdrant HTTP Client with call.Client

**Files:**
- Modify: `internal/rag/vectorstore/qdrant.go:39-41`
- Modify: `internal/rag/vectorstore/qdrant.go` (imports)
- Test: `internal/rag/vectorstore/qdrant_test.go`

**Context:** Qdrant is the vector database used for RAG. It currently uses a bare `&http.Client{Timeout: cfg.Timeout}`. Qdrant calls are critical-path for RAG queries — retries and circuit breaking add resilience.

**Step 1: Write a failing test**

Create or update `internal/rag/vectorstore/qdrant_test.go` with a test that verifies retry behavior on transient Qdrant errors:

```go
func TestQdrantRetriesOn503(t *testing.T) {
    attempts := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        attempts++
        if attempts < 3 {
            w.WriteHeader(http.StatusServiceUnavailable)
            return
        }
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"result":[]}`))
    }))
    defer srv.Close()

    store := NewStore(Config{URL: srv.URL, Timeout: 5 * time.Second})
    // Call a method that hits Qdrant
    // Assert it succeeds after retries
    if attempts < 3 {
        t.Errorf("expected at least 3 attempts, got %d", attempts)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/rag/vectorstore/ -run TestQdrantRetriesOn503 -v`
Expected: FAIL — current bare http.Client does not retry.

**Step 3: Replace http.Client with call.Client**

In `internal/rag/vectorstore/qdrant.go`, change the struct field and constructor:

```go
import "github.com/ai8future/chassis-go/call"

type Store struct {
    url    string
    client *call.Client
}

func NewStore(cfg Config) *Store {
    return &Store{
        url: cfg.URL,
        client: call.New(
            call.WithTimeout(cfg.Timeout),
            call.WithRetry(3, 500*time.Millisecond),
            call.WithCircuitBreaker("qdrant", 5, 30*time.Second),
        ),
    }
}
```

Update all `s.client.Do(req)` call sites — the signature is the same (`Do(*http.Request) (*http.Response, error)`), so no downstream changes needed.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/rag/vectorstore/ -run TestQdrantRetriesOn503 -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./internal/rag/... -v`
Expected: All pass.

**Step 6: Commit**

```bash
git add internal/rag/vectorstore/qdrant.go internal/rag/vectorstore/qdrant_test.go
git commit -m "feat: replace Qdrant http.Client with chassis-go call.Client for retries and circuit breaking"
```

---

### Task 2: Replace RAG Ollama HTTP Client with call.Client

**Files:**
- Modify: `internal/rag/embedder/ollama.go:64-67`
- Test: `internal/rag/embedder/ollama_test.go`

**Context:** Ollama provides embeddings for RAG. Same pattern as Task 1 — bare http.Client replaced with call.Client.

**Step 1: Write a failing test for retry on Ollama 503**

```go
func TestOllamaRetriesOnTransientError(t *testing.T) {
    attempts := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        attempts++
        if attempts < 2 {
            w.WriteHeader(http.StatusServiceUnavailable)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]any{
            "embedding": []float64{0.1, 0.2, 0.3},
        })
    }))
    defer srv.Close()

    embedder := New(Config{URL: srv.URL, Model: "test", Timeout: 5 * time.Second})
    _, err := embedder.Embed(context.Background(), "test text")
    if err != nil {
        t.Fatalf("expected success after retry, got: %v", err)
    }
    if attempts < 2 {
        t.Errorf("expected at least 2 attempts, got %d", attempts)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/rag/embedder/ -run TestOllamaRetriesOnTransientError -v`

**Step 3: Replace http.Client with call.Client**

In `internal/rag/embedder/ollama.go`:

```go
import "github.com/ai8future/chassis-go/call"

type Embedder struct {
    url    string
    model  string
    client *call.Client
}

func New(cfg Config) *Embedder {
    return &Embedder{
        url:   cfg.URL,
        model: cfg.Model,
        client: call.New(
            call.WithTimeout(cfg.Timeout),
            call.WithRetry(3, 500*time.Millisecond),
            call.WithCircuitBreaker("ollama", 5, 30*time.Second),
        ),
    }
}
```

**Step 4: Run tests**

Run: `go test ./internal/rag/embedder/ -v`

**Step 5: Commit**

```bash
git add internal/rag/embedder/ollama.go internal/rag/embedder/ollama_test.go
git commit -m "feat: replace Ollama http.Client with chassis-go call.Client"
```

---

### Task 3: Replace RAG Docbox HTTP Client with call.Client

**Files:**
- Modify: `internal/rag/extractor/docbox.go:50-52`
- Test: `internal/rag/extractor/docbox_test.go`

**Context:** Docbox extracts text from uploaded documents. Same pattern.

**Step 1: Write failing test**

Same pattern as Tasks 1-2: httptest server returning 503 on first attempt, success on second.

**Step 2: Run test — expect FAIL**

**Step 3: Replace http.Client with call.Client**

```go
client: call.New(
    call.WithTimeout(cfg.Timeout),
    call.WithRetry(3, 500*time.Millisecond),
    call.WithCircuitBreaker("docbox", 5, 30*time.Second),
),
```

**Step 4: Run tests — expect PASS**

Run: `go test ./internal/rag/extractor/ -v`

**Step 5: Commit**

```bash
git add internal/rag/extractor/docbox.go internal/rag/extractor/docbox_test.go
git commit -m "feat: replace Docbox http.Client with chassis-go call.Client"
```

---

### Task 4: Replace Imagegen Gemini HTTP Client with call.Client

**Files:**
- Modify: `internal/imagegen/gemini.go:106`
- Test: `internal/imagegen/client_test.go`

**Context:** The Gemini image generation client creates a one-shot `http.Client` per request at line 106. This should be a shared `call.Client` on the struct with retry for transient failures.

**Step 1: Write failing test for retry**

Add test to `internal/imagegen/client_test.go`:

```go
func TestGeminiImageRetryOnTransient(t *testing.T) {
    attempts := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        attempts++
        if attempts < 2 {
            w.WriteHeader(http.StatusBadGateway)
            return
        }
        // Return valid Gemini image response
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]any{
            "candidates": []map[string]any{
                {"content": map[string]any{
                    "parts": []map[string]any{
                        {"inlineData": map[string]any{
                            "mimeType": "image/png",
                            "data":     "base64data",
                        }},
                    },
                }},
            },
        })
    }))
    defer srv.Close()
    // Configure client to use test server URL
    // Assert retry happened
}
```

**Step 2: Run test — expect FAIL**

**Step 3: Refactor to use call.Client as struct field**

In `internal/imagegen/gemini.go`, add a `call.Client` field to the Gemini client struct and initialize it in the constructor:

```go
import "github.com/ai8future/chassis-go/call"

// In the struct:
httpClient *call.Client

// In constructor:
httpClient: call.New(
    call.WithTimeout(90 * time.Second),
    call.WithRetry(2, 1*time.Second),
    call.WithCircuitBreaker("gemini-imagegen", 3, 60*time.Second),
),
```

Replace the per-request `&http.Client{Timeout: geminiTimeout}` at line 106 with `g.httpClient`.

**Step 4: Run tests — expect PASS**

Run: `go test ./internal/imagegen/ -v`

**Step 5: Commit**

```bash
git add internal/imagegen/gemini.go internal/imagegen/client_test.go
git commit -m "feat: replace Gemini imagegen http.Client with chassis-go call.Client"
```

---

### Task 5: Replace Doppler HTTP Client with call.Client

**Files:**
- Modify: `internal/tenant/doppler.go:45`
- Test: `internal/tenant/doppler_test.go`

**Context:** The Doppler client fetches secrets at startup. A transient failure here would prevent the service from starting. Adding retries makes startup more resilient.

**Step 1: Write failing test**

```go
func TestDopplerRetriesOnTransient(t *testing.T) {
    attempts := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        attempts++
        if attempts < 2 {
            w.WriteHeader(http.StatusBadGateway)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]string{"KEY": "VALUE"})
    }))
    defer srv.Close()
    // Test Doppler fetch with test server
}
```

**Step 2: Run test — expect FAIL**

**Step 3: Replace with call.Client**

```go
import "github.com/ai8future/chassis-go/call"

httpClient: call.New(
    call.WithTimeout(10 * time.Second),
    call.WithRetry(3, 1*time.Second),
),
```

No circuit breaker needed — Doppler is only called at startup/reload, not on the hot path.

**Step 4: Run tests — expect PASS**

Run: `go test ./internal/tenant/ -v`

**Step 5: Commit**

```bash
git add internal/tenant/doppler.go internal/tenant/doppler_test.go
git commit -m "feat: replace Doppler http.Client with chassis-go call.Client"
```

---

### Task 6: Add Health Checks for RAG Dependencies

**Files:**
- Modify: `internal/admin/server.go` (the `/admin/healthz` handler setup)
- Modify: `internal/server/grpc.go` (gRPC health check wiring)
- Modify: `internal/rag/embedder/ollama.go` (add Ping method)
- Modify: `internal/rag/vectorstore/qdrant.go` (add Ping method)
- Test: Add health check tests

**Context:** The `/admin/healthz` endpoint currently uses `health.Handler` but may only have basic checks. Adding Qdrant, Ollama, and database ping checks gives operators real visibility into dependency health.

**Step 1: Add Ping() method to Qdrant store**

In `internal/rag/vectorstore/qdrant.go`:

```go
func (s *Store) Ping(ctx context.Context) error {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url+"/healthz", nil)
    if err != nil {
        return err
    }
    resp, err := s.client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("qdrant health check returned %d", resp.StatusCode)
    }
    return nil
}
```

**Step 2: Add Ping() method to Ollama embedder**

In `internal/rag/embedder/ollama.go`:

```go
func (e *Embedder) Ping(ctx context.Context) error {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.url+"/api/tags", nil)
    if err != nil {
        return err
    }
    resp, err := e.client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("ollama health check returned %d", resp.StatusCode)
    }
    return nil
}
```

**Step 3: Wire checks into health.Handler**

Where the admin server sets up `/admin/healthz`, add the RAG dependency checks to the checks map. Only add them if the RAG components are initialized (they're optional):

```go
checks := map[string]health.Check{
    "self": func(_ context.Context) error { return nil },
}
if dbClient != nil {
    checks["postgres"] = func(ctx context.Context) error {
        return dbClient.Ping(ctx)
    }
}
if qdrantStore != nil {
    checks["qdrant"] = qdrantStore.Ping
}
if ollamaEmbedder != nil {
    checks["ollama"] = ollamaEmbedder.Ping
}
mux.Handle("GET /admin/healthz", health.Handler(checks))
```

**Step 4: Test health endpoint**

Run: `go test ./internal/admin/ -v`

**Step 5: Commit**

```bash
git add internal/rag/vectorstore/qdrant.go internal/rag/embedder/ollama.go internal/admin/server.go internal/server/grpc.go
git commit -m "feat: add RAG dependency health checks via chassis-go health.Handler"
```

---

### Task 7: Evaluate Removing internal/retry in Favor of call.Client

**Files:**
- Review: `internal/retry/backoff.go`, `internal/retry/retryable.go`, `internal/retry/defaults.go`, `internal/retry/context.go`
- Search: All callers of `retry.IsRetryable`, `retry.SleepWithBackoff`, `retry.EnsureTimeout`

**Context:** The `internal/retry` package provides `IsRetryable()`, `SleepWithBackoff()`, `EnsureTimeout()`, and constants (`MaxRetries=3`, `RequestTimeout=3min`, `BackoffBase=500ms`). These are used by AI provider clients (OpenAI, Anthropic, etc.) which use SDK clients, not raw `http.Client`. Since `call.Client` wraps `http.Client.Do()`, it can't directly replace retry logic inside SDK calls.

**Decision:** Keep `internal/retry` for now. It serves a different layer — it retries at the SDK/provider level, not the HTTP transport level. `call.Client` handles transport-level retries (raw HTTP calls to Qdrant, Ollama, Docbox, Doppler). Document this distinction.

**Step 1: Add a doc comment to internal/retry/defaults.go**

```go
// Package retry provides application-level retry utilities for AI provider SDK calls.
// For HTTP transport-level retries (raw outbound HTTP calls), use chassis-go/call.Client.
// This package is for retrying higher-level operations where the SDK manages its own HTTP client.
```

**Step 2: Commit**

```bash
git add internal/retry/defaults.go
git commit -m "docs: clarify retry package scope vs chassis-go call.Client"
```

---

### Task 8: Log chassis.Version at Startup

**Files:**
- Modify: `cmd/airborne/main.go`

**Context:** The INTEGRATING.md recommends logging `chassis.Version` at startup for production diagnostics.

**Step 1: Add import and log line**

In `cmd/airborne/main.go`, add to the startup log block:

```go
import chassis "github.com/ai8future/chassis-go"

// In the startup section, after logger creation:
logger.Info("starting airborne",
    "version", version,
    "chassis_version", chassis.Version,
    "commit", gitCommit,
)
```

**Step 2: Run application and verify log output**

Run: `go build ./cmd/airborne/ && ./bin/airborne --help`
Expected: No build errors.

**Step 3: Commit**

```bash
git add cmd/airborne/main.go
git commit -m "feat: log chassis-go version at startup per toolkit recommendation"
```

---

### Task 9: Consolidate Config Doppler HTTP Client (config.go:416)

**Files:**
- Modify: `internal/config/config.go:416`

**Context:** There's an `&http.Client{Timeout: 10 * time.Second}` in `config.go` at line 416 for Doppler API calls (same as `internal/tenant/doppler.go`). Replace with `call.Client` for consistency.

**Step 1: Replace with call.Client**

```go
import "github.com/ai8future/chassis-go/call"

client := call.New(
    call.WithTimeout(10 * time.Second),
    call.WithRetry(3, 1*time.Second),
)
```

**Step 2: Run tests**

Run: `go test ./internal/config/ -v`

**Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: replace config Doppler http.Client with chassis-go call.Client"
```

---

## What This Plan Does NOT Change (and Why)

| Component | Reason to keep as-is |
|---|---|
| `internal/config/` (YAML+env+Doppler loader) | Far more sophisticated than `config.MustLoad` — supports YAML files, Doppler secrets, ENV/FILE prefix resolution, frozen configs. Not a candidate for replacement. |
| `internal/retry/` package | Operates at SDK/provider level, not HTTP transport level. `call.Client` handles transport retries; this package handles business-logic retries around SDK calls. Different layers. |
| `internal/httpcapture/` | Creates `http.Client` with custom transport for request/response body capture. This is a debugging tool, not an outbound call — `call.Client` can't replace it. |
| `internal/cli/client.go` | CLI tool's HTTP client for admin API. Low-criticality, no retry needed. |
| `os.Getenv` calls in `internal/tenant/` | These are part of the tenant configuration system which has its own well-designed loading logic (frozen configs, Doppler, file-based). Replacing with `config.MustLoad` would require a redesign of the tenant system for no benefit. |
| `os.Getenv` calls in `cmd/` tools | CLI tools (freeze, cli) read 1-2 env vars. `config.MustLoad` is overkill. |

---

## Summary of Changes

| Task | Component | Change |
|---|---|---|
| 1 | Qdrant vectorstore | bare http.Client → call.Client with retry + circuit breaker |
| 2 | Ollama embedder | bare http.Client → call.Client with retry + circuit breaker |
| 3 | Docbox extractor | bare http.Client → call.Client with retry + circuit breaker |
| 4 | Gemini imagegen | per-request http.Client → shared call.Client with retry + circuit breaker |
| 5 | Doppler tenant client | bare http.Client → call.Client with retry |
| 6 | Health checks | Add Qdrant + Ollama ping to /admin/healthz |
| 7 | Retry package | Document scope distinction vs call.Client |
| 8 | Startup logging | Log chassis.Version for diagnostics |
| 9 | Config Doppler client | bare http.Client → call.Client with retry |

**Net effect:** 6 raw `http.Client` instances replaced with resilient `call.Client` (retries + circuit breaking), richer health checks, better startup diagnostics. No unnecessary changes to systems that work well as-is.
