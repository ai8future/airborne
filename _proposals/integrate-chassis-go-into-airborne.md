# Integrate chassis-go Into Airborne

**Date:** February 3, 2026

## Summary

This proposal covers the full adoption of the `chassis-go` toolkit into the Airborne codebase. Airborne already uses `log/slog`, gRPC interceptors, graceful shutdown, and retry logic — all of which have direct chassis-go equivalents. The migration replaces hand-rolled infrastructure code with chassis's standardized building blocks, reducing maintenance surface and gaining trace ID propagation, circuit breakers, and structured health checks for free.

The migration follows the incremental order recommended in INTEGRATING.md: config + logz first, then httpkit + health, then lifecycle, then call, then grpckit.

---

## Current State vs. chassis-go Mapping

| Concern | Airborne Today | chassis-go Replacement |
|---|---|---|
| **Config** | YAML file + env overrides + Doppler (`internal/config/config.go`) | `config.MustLoad[T]()` for env-based fields; keep YAML for complex nested config |
| **Logging** | Hand-wired `slog.JSONHandler` in `main.go:configureLogger()` | `logz.New(level)` + trace ID propagation via `logz.WithTraceID` |
| **gRPC interceptors** | Hand-rolled recovery, logging in `internal/server/grpc.go` (~100 lines) | `grpckit.UnaryRecovery`, `UnaryLogging`, `StreamRecovery`, `StreamLogging` |
| **HTTP middleware** | Manual CORS in `internal/admin/server.go`, no request ID / panic recovery | `httpkit.Recovery`, `httpkit.RequestID`, `httpkit.Logging` |
| **Health checks** | Custom gRPC `AdminService/Health` + HTTP `/admin/health` | `health.Handler(checks)` for HTTP, `grpckit.RegisterHealth` for gRPC |
| **Graceful shutdown** | Manual signal handling + goroutine coordination in `main.go` | `lifecycle.Run(ctx, components...)` |
| **Retry / resilience** | Custom `internal/retry` package (backoff, retryable classification) | `call.New(WithRetry, WithCircuitBreaker)` for outbound HTTP; keep `internal/retry` for provider-specific gRPC/SDK retry logic |
| **Test helpers** | No shared test infrastructure | `testkit.NewLogger`, `testkit.SetEnv`, `testkit.GetFreePort` |

---

## Implementation Plan

### Phase 1: Add Dependency and Wire logz

**Files to modify:**
- `go.mod` — add `github.com/ai8future/chassis-go`
- `cmd/airborne/main.go` — replace `configureLogger()` with `logz.New()`

**Changes:**

1. Add chassis-go to go.mod:
   ```
   go get github.com/ai8future/chassis-go
   ```

2. Replace `configureLogger()` in `cmd/airborne/main.go`:
   ```go
   // BEFORE (lines 150-171):
   // 25 lines of manual slog setup

   // AFTER:
   import (
       chassis "github.com/ai8future/chassis-go"
       "github.com/ai8future/chassis-go/logz"
   )

   logger := logz.New(cfg.Logging.Level)
   slog.SetDefault(logger)
   slog.Info("starting Airborne",
       "version", Version,
       "chassis_version", chassis.Version,
   )
   ```

3. Add trace ID propagation at the gRPC ingress point. In the logging interceptor (or a new dedicated interceptor), extract or generate a trace ID and store it in context:
   ```go
   import "github.com/ai8future/chassis-go/logz"

   // In unary interceptor:
   traceID := extractOrGenerateTraceID(ctx)
   ctx = logz.WithTraceID(ctx, traceID)
   ```

   This gives every downstream `logger.InfoContext(ctx, ...)` call automatic trace ID inclusion — a capability Airborne doesn't have today.

**What gets deleted:** The entire `configureLogger()` function (21 lines).

**Risk:** Low. `logz.New` returns `*slog.Logger`, which is exactly what the codebase already uses everywhere.

---

### Phase 2: Replace gRPC Interceptors with grpckit

**Files to modify:**
- `internal/server/grpc.go`

**Changes:**

1. Replace the four hand-rolled interceptors with chassis equivalents:
   ```go
   import "github.com/ai8future/chassis-go/grpckit"

   logger := slog.Default()

   unaryInterceptors := []grpc.UnaryServerInterceptor{
       grpckit.UnaryRecovery(logger),
       grpckit.UnaryLogging(logger),
   }
   streamInterceptors := []grpc.StreamServerInterceptor{
       grpckit.StreamRecovery(logger),
       grpckit.StreamLogging(logger),
   }
   ```

2. Wire gRPC health check via `grpckit.RegisterHealth`:
   ```go
   grpckit.RegisterHealth(server, func(ctx context.Context) error {
       if dbClient != nil {
           return dbClient.Ping(ctx)
       }
       return nil
   })
   ```

**What gets deleted:** `recoveryInterceptor()`, `streamRecoveryInterceptor()`, `loggingInterceptor()`, `streamLoggingInterceptor()` — approximately 95 lines of interceptor code from `internal/server/grpc.go`.

**Consideration:** The current `loggingInterceptor` skips health check logging (`/airborne.v1.AdminService/Health`). Verify that `grpckit.UnaryLogging` either supports similar filtering or that the extra log lines are acceptable. If not, we can wrap chassis's interceptor with a skip-filter — a 5-line wrapper is still better than maintaining the full interceptor.

---

### Phase 3: Add httpkit Middleware to Admin Server

**Files to modify:**
- `internal/admin/server.go`

**Changes:**

1. Wrap the admin HTTP mux with chassis middleware:
   ```go
   import "github.com/ai8future/chassis-go/httpkit"

   handler := httpkit.Recovery(logger)(
       httpkit.RequestID(
           httpkit.Logging(logger)(mux),
       ),
   )

   s.server = &http.Server{
       Addr:    fmt.Sprintf(":%d", s.port),
       Handler: handler,  // was: mux directly
   }
   ```

2. Replace inline JSON error responses with `httpkit.JSONError()` calls where applicable in admin handlers.

**What this adds:** Request IDs on every admin HTTP request (currently absent), structured request logging, and panic recovery for the HTTP layer. The admin server currently has no panic recovery — a panic in any handler kills the process.

**What gets deleted:** The manual CORS middleware can stay (chassis doesn't provide CORS). The inline error formatting in handlers can be replaced with `httpkit.JSONError`.

---

### Phase 4: Adopt health.Handler for HTTP Health Checks

**Files to modify:**
- `internal/admin/server.go` — replace hand-rolled `/admin/health` handler

**Changes:**

1. Define health checks as a `map[string]health.Check`:
   ```go
   import "github.com/ai8future/chassis-go/health"

   checks := map[string]health.Check{
       "self": func(_ context.Context) error { return nil },
   }
   if s.dbClient != nil {
       checks["postgres"] = func(ctx context.Context) error {
           return s.dbClient.Ping(ctx)
       }
   }
   if s.redisClient != nil {
       checks["redis"] = func(ctx context.Context) error {
           return s.redisClient.Ping(ctx)
       }
   }

   mux.Handle("GET /admin/healthz", health.Handler(checks))
   ```

2. Keep the existing `/admin/health` endpoint temporarily for backward compatibility, but mark it deprecated. The new `/admin/healthz` follows the standard convention and runs checks in parallel (the current implementation runs them sequentially).

**What this adds:** Parallel health checks, per-dependency status in the response, standard 200/503 status codes, and a consistent JSON format.

---

### Phase 5: Replace Shutdown Logic with lifecycle.Run

**Files to modify:**
- `cmd/airborne/main.go`

**Changes:**

Refactor `main()` to use `lifecycle.Run` for orchestrating the gRPC server, admin HTTP server, and cleanup:

```go
import "github.com/ai8future/chassis-go/lifecycle"

err := lifecycle.Run(context.Background(),
    // gRPC server component
    func(ctx context.Context) error {
        errCh := make(chan error, 1)
        go func() { errCh <- grpcServer.Serve(listener) }()
        select {
        case <-ctx.Done():
            grpcServer.GracefulStop()
            return nil
        case err := <-errCh:
            if err == grpc.ErrServerStopped {
                return nil
            }
            return err
        }
    },
    // Admin HTTP server component (if enabled)
    func(ctx context.Context) error {
        if adminServer == nil {
            <-ctx.Done()
            return nil
        }
        errCh := make(chan error, 1)
        go func() { errCh <- adminServer.Start() }()
        select {
        case <-ctx.Done():
            shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            return adminServer.Shutdown(shutdownCtx)
        case err := <-errCh:
            if err == http.ErrServerClosed {
                return nil
            }
            return err
        }
    },
)
if err != nil {
    slog.Error("server exited with error", "error", err)
    os.Exit(1)
}
```

**What gets deleted:** The manual `signal.NotifyContext` setup, the `<-ctx.Done()` wait, and the sequential shutdown code (~25 lines).

**What this adds:** Coordinated shutdown — if the gRPC server crashes, the admin server also shuts down cleanly (currently it would continue running orphaned). Any future background workers (job processing, etc.) can be added as additional components.

---

### Phase 6: Adopt call.Client for Outbound HTTP Calls

**Files to modify:**
- `internal/rag/embedder/ollama.go` — Ollama HTTP client
- `internal/rag/extractor/docbox.go` — Docbox HTTP client
- `internal/rag/vectorstore/qdrant.go` — Qdrant HTTP client
- `internal/imagegen/client.go` — Image generation HTTP client
- `internal/tenant/doppler.go` — Doppler API HTTP client

**Changes:**

Replace raw `http.Client` usage with `call.Client` instances, one per downstream service:

```go
import "github.com/ai8future/chassis-go/call"

var ollamaClient = call.New(
    call.WithTimeout(30 * time.Second),
    call.WithRetry(2, 500 * time.Millisecond),
    call.WithCircuitBreaker("ollama", 5, 30 * time.Second),
)
```

**What this adds:**
- **Circuit breakers** — If Qdrant or Ollama goes down, requests fail fast instead of timing out repeatedly. This is a real operational improvement.
- **Standardized retry** — Exponential backoff with jitter (the current retry package lacks jitter).
- **Timeout enforcement** — Consistent deadline propagation.

**What stays:** The `internal/retry` package stays for provider-specific retry logic (Gemini, OpenAI, Anthropic SDK calls). These use provider SDKs, not raw HTTP, so `call.Client` doesn't apply. The retry package's `IsRetryable()` classification logic is provider-specific and has no chassis equivalent.

---

### Phase 7: Adopt config.MustLoad for Environment Variables

**Files to modify:**
- `internal/config/config.go`

**Changes:**

This is the most nuanced change. Airborne's config system is significantly more complex than chassis's — it loads from YAML, overlays env vars, fetches from Doppler, and supports frozen configs. Chassis's `config.MustLoad` only handles `env` tag-based struct population.

**Recommended approach:** Use `config.MustLoad` for a new `EnvConfig` struct that captures the environment-variable-only settings, then merge into the existing config:

```go
import "github.com/ai8future/chassis-go/config"

type EnvConfig struct {
    DatabaseURL    string `env:"DATABASE_URL" required:"false"`
    AdminToken     string `env:"AIRBORNE_ADMIN_TOKEN" required:"false"`
    GRPCPort       int    `env:"AIRBORNE_GRPC_PORT" required:"false"`
    LogLevel       string `env:"AIRBORNE_LOG_LEVEL" default:"info"`
    UsesFrozen     bool   `env:"AIRBORNE_USE_FROZEN" required:"false"`
    ConfigPath     string `env:"AIRBORNE_CONFIG" default:"configs/airborne.yaml"`
}

envCfg := config.MustLoad[EnvConfig]()
```

Then the existing YAML-loading code merges `envCfg` values as overrides. This replaces the scattered `os.Getenv` calls in the config loader with a single typed struct.

**What stays:** The YAML loading, Doppler fetching, and frozen config logic. These are application-specific and outside chassis's scope.

---

### Phase 8: Adopt testkit Across Test Files

**Files to modify:**
- All `*_test.go` files that create loggers or set env vars

**Changes:**

1. Replace test logger creation:
   ```go
   // BEFORE:
   logger := slog.New(slog.NewTextHandler(io.Discard, nil))

   // AFTER:
   import "github.com/ai8future/chassis-go/testkit"
   logger := testkit.NewLogger(t)
   ```

2. Replace env var setup in tests:
   ```go
   // BEFORE:
   os.Setenv("DATABASE_URL", "postgres://...")
   defer os.Unsetenv("DATABASE_URL")

   // AFTER:
   testkit.SetEnv(t, map[string]string{
       "DATABASE_URL": "postgres://...",
   })
   ```

3. Use `testkit.GetFreePort()` for test servers instead of hardcoded ports.

**Risk:** Low. These are pure test-time changes with no production impact.

---

## What NOT to Change

1. **Provider retry logic** (`internal/retry/`) — The `IsRetryable()` function classifies errors from AI provider SDKs using string matching on error messages. This is domain-specific and has no chassis equivalent. The `call.Client` retrier only handles HTTP status codes. Keep `internal/retry` for provider SDK calls.

2. **YAML config loading** — Chassis's config package is env-only. Airborne's YAML + Doppler + frozen config system is more capable. Use chassis config as a clean env-var layer, not a replacement.

3. **Custom auth interceptors** — Airborne's tenant interceptor, static auth, and Redis-based auth are application-specific. Chassis doesn't provide auth.

4. **Custom CORS middleware** — Chassis httpkit doesn't include CORS. Keep the existing implementation.

5. **Database and Redis clients** — These are application-specific wrappers. Chassis doesn't cover data stores.

---

## Migration Order and Dependencies

```
Phase 1 (logz)           ← No dependencies, can start immediately
Phase 2 (grpckit)        ← Depends on Phase 1 (needs logger)
Phase 3 (httpkit)        ← Depends on Phase 1 (needs logger)
Phase 4 (health)         ← Depends on Phase 3 (wired into HTTP mux)
Phase 5 (lifecycle)      ← Independent, but best after Phases 2-3
Phase 6 (call)           ← Independent, can run in parallel with 2-5
Phase 7 (config)         ← Independent
Phase 8 (testkit)        ← Independent, can run at any time
```

Phases 1-5 are the core migration. Phase 6 adds the most new capability (circuit breakers). Phases 7-8 are polish.

---

## Lines of Code Impact

| Category | Deleted | Added | Net |
|---|---|---|---|
| `configureLogger()` | ~21 | ~3 | -18 |
| gRPC interceptors | ~95 | ~10 | -85 |
| HTTP middleware | ~0 | ~8 | +8 |
| Health checks | ~40 | ~15 | -25 |
| Shutdown orchestration | ~25 | ~30 | +5 |
| Outbound HTTP clients | ~0 | ~25 | +25 |
| **Total** | **~181** | **~91** | **~-90** |

Net reduction of ~90 lines of infrastructure code, with the added benefit of trace ID propagation, circuit breakers, request IDs on HTTP, and panic recovery on the admin server — none of which exist today.

---

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| chassis-go is new and may have bugs | Both codebases are internal; bugs can be fixed at the source |
| `grpckit.UnaryLogging` may not skip health checks | Wrap with a 5-line filter if needed |
| `config.MustLoad` panics could crash in CI | Only use for truly required env vars; keep `required:"false"` for optional ones |
| Circuit breaker state is global per process | Use clear, distinct names per downstream service |
| Go version requirement (1.25.5) | Airborne already uses Go 1.25.5 |
