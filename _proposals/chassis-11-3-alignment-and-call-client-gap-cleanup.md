# Chassis 11.3.0 Alignment and Call-Client Gap Cleanup

**Date:** July 4, 2026

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

## Summary

A completeness audit (July 4, 2026) found airborne is a near-complete chassis-go install: every integration point INTEGRATING.md treats as core for a server+CLI is adopted. This plan closes the gaps the audit found: stale declared pins (chassis-go 11.1.8 vs 11.3.0, addons 1.2.3 vs 1.2.10), one junk file, one missing `RequireMajor` test gate, one middleware-ordering deviation, and the single substantive gap — 11 raw `http.DefaultClient.Do` calls in the Gemini filestore that bypass `call.Client`'s retry/circuit-breaking while every other outbound client has it.

**Goal:** Bring airborne to a fully aligned, fully resilient chassis-go 11.3.0 install.

**Architecture:** Four small corrective tasks plus one finalizer. The "upgrade" is an *alignment*: airborne builds via local `replace` directives pointing at `../../chassis_suite/`, so it **already compiles against chassis 11.3.0 / addons 1.2.10 source** — the go.mod `require` lines and `VERSION.chassis` are stale metadata that must be brought to match reality (this matters for the `repos_chassis*` ecosystem scanners and any future non-replace build). The filestore work is a mechanical swap: `call.Client.Do(req)` is a drop-in for `http.DefaultClient.Do(req)`, and every filestore request body is `bytes.NewReader(...)` or nil, which `http.NewRequestWithContext` makes rewindable (`GetBody` auto-set) — safe under `call`'s retry (chassis 11.1.15 added consumed-body rewind protection).

**Tech Stack:** Go 1.26, chassis-go/v11 (11.3.0), chassis-go-addons pgkit/rediskit/ssrfcheck (1.2.10), httptest for the new retry test.

## Design Rationale & Explicit Non-Goals

- **Why alignment, not migration:** same major (v11) — no import-path or `RequireMajor` change; the audit verified none of the 11.2.0 breaking changes (webhook, seal, meilikit, `kafkakit.PublishBatch`) intersect airborne's usage (airborne uses `kafkakit.Publish`). Payoffs riding the alignment: `call` consumed-body-rewind fix (11.1.15), ssrfcheck NAT64 SSRF-bypass block (addons 1.2.9), registry path hardening, heartbeatkit per-publish timeout, kafkakit bounded producer retries.
- **NOT adopting** (audit verdict — no real use case; do not add): `seal`, `tick`, `cache`, `flagz`, `webhook`, `tracekit`, `schemakit`, `phasekit`, `registrykit`, `lakekit`, `inngestkit`, addons `blob`/`dlkit`/`feedkit`/`mailkit`. Chassis `errors` and custom `metrics` are optional consistency items, deferred.
- **NOT adopting pgkit's `BeginTenantTx`/`SetTenant` RLS helpers** (new in addons 1.2.7): airborne's hand-rolled helpers (`internal/db/tenanttx.go`) drive three GUCs (`airborne.tenant_id`, `airborne.cross_tenant_mode`, `airborne.admin_mode`) that the baseline migration's RLS policies key on; pgkit's helper standardizes a different GUC, so adopting it would force a migration/policy rework for zero behavior gain. Explicitly out of scope.
- **CLI raw `http.Client`** (`internal/cli/client.go:20`): acceptable for a CLI per the audit; out of scope.

## Global Constraints

- **Local replaces are the build reality.** `go.mod` replaces chassis-go and all three addons to `../../chassis_suite/...`. Changing the `require` versions does NOT change compiled code; it aligns metadata. All verification still runs the full suite.
- **`VERSION.chassis` format:** bare version, no `v` prefix (e.g. `11.3.0`).
- **Docker required for `internal/db` tests** (testcontainers). If Docker is down, start OrbStack first — do not skip the db suite; it is the tenant-isolation verification.
- **Repo policy:** VERSION is read at the LAST moment (never pre-read); revisions cap at 15 then bump minor. CHANGELOG entry names the coding agent + model. Commit+push only after VERSION+CHANGELOG (Task 5). Per-task commits are local only until Task 5 pushes.
- **Do not touch** the RLS GUC helpers, migration policies, or admin JSON wire contract.
- All commits end with trailer: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

## File Structure

- Delete: `VERSION.chassis.go.txt` (junk — contains `1`; the real marker is `VERSION.chassis`)
- Modify: `internal/db/testmain_test.go` (add `RequireMajor` gate)
- Modify: `go.mod` (4 require lines), `VERSION.chassis` (11.3.0)
- Modify: `internal/admin/server.go:120-135` (middleware order)
- Modify: `internal/provider/gemini/filestore.go` (package-level `call.Client`, 11 call sites)
- Create: `internal/provider/gemini/filestore_test.go` (retry behavior test)
- Modify (Task 5): `VERSION`, `CHANGELOG.md`

---

### Task 1: Hygiene — delete junk file, add missing RequireMajor gate

**Files:**
- Delete: `VERSION.chassis.go.txt`
- Modify: `internal/db/testmain_test.go`

**Interfaces:** none produced/consumed.

- [ ] **Step 1: Delete the junk file**

```bash
git rm VERSION.chassis.go.txt
```

(Contains just `1`; not a chassis convention — zero references in chassis-go docs. The real ecosystem marker is `VERSION.chassis`.)

- [ ] **Step 2: Add the RequireMajor gate to the db TestMain**

`internal/db/testmain_test.go` is the only TestMain in the repo without the chassis version gate (the other five have it, e.g. `internal/server/testmain_test.go:11`). Add the import and make `chassis.RequireMajor(11)` the FIRST line of `TestMain`:

```go
import (
	// ... existing imports ...
	chassis "github.com/ai8future/chassis-go/v11"
)

func TestMain(m *testing.M) {
	chassis.RequireMajor(11)
	ctx := context.Background()
	// ... existing body unchanged ...
}
```

- [ ] **Step 3: Run the db suite to verify the gate doesn't break it**

Run: `go test -mod=mod -count=1 ./internal/db/ 2>&1 | tail -3`
Expected: `ok github.com/ai8future/airborne/internal/db` (container run, ~7s)

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: remove junk VERSION.chassis.go.txt; add RequireMajor gate to db TestMain"
```

---

### Task 2: Align chassis pins with the local-replace reality (11.3.0 / 1.2.10)

**Files:**
- Modify: `go.mod` (require lines 6-9), `go.sum` (via tidy), `VERSION.chassis`

**Interfaces:** none — metadata alignment; compiled code is unchanged (replaces already point at 11.3.0 / 1.2.10 source).

- [ ] **Step 1: Bump the four require lines**

In `go.mod`, change:

```
github.com/ai8future/chassis-go-addons/pgkit v1.2.3      → v1.2.10
github.com/ai8future/chassis-go-addons/rediskit v1.2.3   → v1.2.10
github.com/ai8future/chassis-go-addons/ssrfcheck v1.2.3  → v1.2.10
github.com/ai8future/chassis-go/v11 v11.1.8              → v11.3.0
```

Leave all `replace` directives untouched.

- [ ] **Step 2: Tidy**

Run: `go mod tidy`
Expected: exits clean. `git diff go.mod` shows exactly the four version strings (plus any go.sum churn). Because of the local replaces there are no chassis checksums in go.sum — that is expected and correct.

- [ ] **Step 3: Update the ecosystem marker**

```bash
echo "11.3.0" > VERSION.chassis
```

- [ ] **Step 4: Full verification sweep**

Run: `go build -mod=mod ./... && go vet -mod=mod ./... && go test -mod=mod -count=1 ./... 2>&1 | grep -cE "^ok" && go test -mod=mod -count=1 ./... 2>&1 | grep -E "FAIL" ; true`
Expected: build+vet clean; all packages `ok`; zero FAIL lines.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum VERSION.chassis
git commit -m "chore: align chassis pins with chassis-go 11.3.0 and addons 1.2.10"
```

---

### Task 3: Admin middleware order — Timeout outside Logging

**Files:**
- Modify: `internal/admin/server.go` (~lines 120-135)

**Interfaces:** none — the handler chain shape changes, endpoints and wire contract do not.

**Why:** chassis guard docs prescribe "Timeout inside Recovery but outside Logging" so that timeout responses are themselves logged as completed requests. Current stack has Timeout innermost (inside Logging).

- [ ] **Step 1: Reorder the stack**

In `internal/admin/server.go`, the current stack is:

```go
	// Stack chassis-go httpkit middleware: Recovery → CORS → Tracing → RequestID → Logging → routes
	logger := slog.Default()
	handler := httpkit.Recovery(logger)(
		guard.CORS(guard.CORSConfig{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "POST", "OPTIONS"},
			AllowHeaders: []string{"Content-Type", "Authorization"},
		})(
			httpkit.Tracing()(
				httpkit.RequestID(
					httpkit.Logging(logger)(
						guard.Timeout(30 * time.Second)(mux),
					),
				),
			),
		),
	)
```

Replace with (Timeout now wraps Logging):

```go
	// Stack chassis-go httpkit middleware: Recovery → CORS → Tracing → RequestID → Timeout → Logging → routes
	// (guard docs: Timeout inside Recovery but OUTSIDE Logging, so timed-out
	// requests are still logged as completed 503s.)
	logger := slog.Default()
	handler := httpkit.Recovery(logger)(
		guard.CORS(guard.CORSConfig{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "POST", "OPTIONS"},
			AllowHeaders: []string{"Content-Type", "Authorization"},
		})(
			httpkit.Tracing()(
				httpkit.RequestID(
					guard.Timeout(30*time.Second)(
						httpkit.Logging(logger)(mux),
					),
				),
			),
		),
	)
```

- [ ] **Step 2: Run admin tests**

Run: `go test -mod=mod -count=1 ./internal/admin/ -v 2>&1 | tail -5`
Expected: PASS (handler tests exercise the endpoints, not the stack order; they must stay green).

- [ ] **Step 3: Commit**

```bash
git add internal/admin/server.go
git commit -m "fix(admin): move guard.Timeout outside Logging per chassis guard ordering"
```

---

### Task 4: Route Gemini filestore through call.Client (TDD)

**Files:**
- Modify: `internal/provider/gemini/filestore.go` (11 sites: lines ~147, 173, 226, 289, 337, 488, 503, 582, 631, 668, 726)
- Create: `internal/provider/gemini/filestore_test.go`

**Interfaces:**
- Produces: package-level `fileStoreClient *call.Client` used by all filestore HTTP calls. No exported signature changes.

**Why:** these 11 Gemini file-API calls are the only outbound HTTP in the codebase without retry/circuit-breaking (every other client — imagegen, ollama, docbox, qdrant, doppler — uses `call.Client`). All request bodies are `bytes.NewReader(...)` or nil, so retries are body-safe (`GetBody` is auto-set by `http.NewRequestWithContext` for `*bytes.Reader`; chassis ≥11.1.15 refuses to resend consumed non-rewindable bodies rather than corrupting them).

- [ ] **Step 1: Write the failing retry test**

Create `internal/provider/gemini/filestore_test.go`. `FileStoreConfig.BaseURL` is honored by `getBaseURL()` (filestore.go:98), so the test can point the real code at an httptest server that fails once with 503 then succeeds — proving retries are active (with `http.DefaultClient` the first 503 would surface as an error/unexpected status):

```go
package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestFileStore_RetriesTransientFailure proves filestore calls go through
// call.Client's retry (an http.DefaultClient path would fail on the first 503).
func TestFileStore_RetriesTransientFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "transient", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(fileSearchStoreResponse{
			Name:        "fileSearchStores/test-store",
			DisplayName: "test",
		})
	}))
	defer srv.Close()

	cfg := FileStoreConfig{APIKey: "test-key", BaseURL: srv.URL}
	result, err := GetFileSearchStore(context.Background(), cfg, "test-store")
	if err != nil {
		t.Fatalf("expected retry to recover from one 503, got error: %v", err)
	}
	if result == nil || calls.Load() != 2 {
		t.Fatalf("result=%v calls=%d; want non-nil result after exactly 2 calls", result, calls.Load())
	}
}
```

(If `FileStoreConfig`'s API-key field is named differently, mirror the struct's actual field names — check the struct definition at the top of filestore.go; do not change the struct.)

- [ ] **Step 2: Run — expect FAIL**

Run: `go test -mod=mod ./internal/provider/gemini/ -run TestFileStore_RetriesTransientFailure -v`
Expected: FAIL — with `http.DefaultClient` the 503 is returned/errored on the first call (calls=1). If it unexpectedly PASSES, stop: the call path may already retry — re-read filestore.go before proceeding.

- [ ] **Step 3: Add the package client and swap the 11 sites**

At the top of `filestore.go` (after imports; add `"time"` and the call import):

```go
import (
	// ... existing imports ...
	"time"

	"github.com/ai8future/chassis-go/v11/call"
)

// fileStoreClient carries retry + circuit-breaking for all Gemini file-API
// calls, mirroring the imagegen/docbox/qdrant outbound clients. Bodies are
// bytes.NewReader/nil (rewindable), so retries are safe.
var fileStoreClient = call.New(
	call.WithTimeout(90*time.Second),
	call.WithRetry(2, 1*time.Second),
	call.WithCircuitBreaker("gemini-filestore", 3, 60*time.Second),
)
```

Then replace ALL 11 occurrences of `http.DefaultClient.Do(` with `fileStoreClient.Do(` (lines ~147, 173, 226, 289, 337, 488, 503, 582, 631, 668, 726). Verify none remain:

```bash
grep -n "http.DefaultClient" internal/provider/gemini/filestore.go
```

Expected: no output.

- [ ] **Step 4: Run — expect PASS**

Run: `go test -mod=mod ./internal/provider/gemini/ -run TestFileStore_RetriesTransientFailure -v`
Expected: PASS (2 calls: one 503, one 200).

- [ ] **Step 5: Full package + build**

Run: `go test -mod=mod -count=1 ./internal/provider/... 2>&1 | tail -8 && go build -mod=mod ./... && go vet -mod=mod ./internal/provider/gemini/`
Expected: all `ok`, build+vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/gemini/filestore.go internal/provider/gemini/filestore_test.go
git commit -m "feat(gemini): route filestore HTTP through call.Client (retry + circuit breaker)"
```

---

### Task 5: VERSION, CHANGELOG, commit, push (finalizer)

**Files:**
- Modify: `VERSION` (read at the LAST moment), `CHANGELOG.md` (top entry)

- [ ] **Step 1:** Read `VERSION` now (not before). Bump per repo policy (revision +1; if revision would exceed 15, bump minor instead).
- [ ] **Step 2:** Add a CHANGELOG.md entry at the top: chassis alignment to 11.3.0 / addons 1.2.10 (note: local replaces meant builds already used this source; pins+marker now match), junk-file removal, db TestMain RequireMajor gate, admin Timeout/Logging ordering fix, Gemini filestore now behind call.Client with retry test. End with the agent line naming the coding agent + model.
- [ ] **Step 3:** Final sweep: `go build -mod=mod ./... && go test -mod=mod -count=1 ./... 2>&1 | grep -E "FAIL|ok" | tail -25` — zero FAIL.
- [ ] **Step 4:**

```bash
git add -A
git commit -m "Save v<VERSION>: chassis 11.3.0 alignment, filestore call.Client resilience"
git push
```

If push is rejected (remote ahead): `git pull --rebase` and push again; escalate rather than force on non-trivial conflicts.

---

## Deferred (explicitly out of scope)

- **pgkit `BeginTenantTx`/`SetTenant` adoption** — would force RLS GUC/policy rework for zero behavior gain (see Design Rationale).
- **chassis `errors` package for gRPC handlers; custom `metrics` recorder** — optional consistency improvements, not completeness requirements.
- **Unused addons** (`blob`, `dlkit`, `feedkit`, `mailkit`) and unused kits (`seal`, `tick`, `cache`, `flagz`, `webhook`, `tracekit`, `schemakit`, `phasekit`, `registrykit`, `lakekit`, `inngestkit`) — no airborne use case.
- **CLI raw http.Client** — acceptable for a CLI.
- **CI repair** (GitHub Actions cannot resolve the local chassis replaces — pre-existing, repo-wide; paths: vendoring or multi-repo checkout) — separate infra effort.

## Self-Review Notes

- **Spec coverage:** all five audit plan items are tasks (junk file → T1; RequireMajor → T1; core+addons pins + VERSION.chassis → T2; middleware nit → T3; filestore → T4); audit's optional item 6 (pgkit helpers) explicitly deferred with rationale. ✓
- **Placeholder scan:** every code step shows the actual code; the one conditional (FileStoreConfig field names) is bounded with an exact instruction. ✓
- **Type consistency:** `fileStoreClient` name used consistently in T4 steps 1/3; `chassis` import alias matches the repo's existing pattern (`internal/server/testmain_test.go:7`). ✓
- **Key honesty point encoded:** the pins are metadata under local replaces — the plan claims alignment, not a behavior-changing upgrade; the one behavior-changing task (T4) carries its own behavioral test. ✓
