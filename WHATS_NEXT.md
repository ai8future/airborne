# What's Next — airborne

## Where This Stands Today

Airborne is the most operationally mature service in `ai_suite`. The core is genuinely finished, not scaffolded: a gRPC gateway (`cmd/airborne`) with a full interceptor chain, RLS-backed multi-tenancy (`migrations/001_baseline.sql` — shared tables with `tenant_id`, `FORCE ROW LEVEL SECURITY`, per-transaction `airborne.tenant_id` GUC), two auth modes with bcrypt-hashed API keys and atomic Redis Lua rate limiting, RAG across Qdrant/OpenAI/Gemini stores, image generation, cost tracking via `pricing_db`, OpenTelemetry throughout, a Next.js operator dashboard, and a hardened keyed-idempotency contract (`internal/service/idempotency.go`) built specifically so `email_ai_svc` can retry safely. The quality signals are unusually strong: **85 test files, zero `TODO`/`FIXME` markers anywhere in `internal/` or `cmd/`**, a real production-image E2E harness (`e2e/run.sh`) that refuses to treat missing Docker as a skip, and enforced coverage floors. The last ~15 releases (CHANGELOG 1.10.3 → 1.10.9) were almost entirely hardening — fail-closed idempotency, SSRF guards, admin timeout policy, non-root container readability. It has one real production consumer (`email_suite/email_ai_svc`, which pins `github.com/ai8future/airborne v1.10.12` in its `go.mod`) and a full Kubernetes deployment (`infra_suite/infra_ai8/apps/airborne/` — 3 replicas, HPA, PDB, Istio, ExternalSecrets).

The gaps are not in the engine, they are in **reach and release discipline**. First, the headline claim doesn't hold: the README and `ai_suite/README.md` advertise "20+ providers", `api/proto/airborne/v1/common.proto` declares 21 enum values, and 13 OpenAI-compatible clients exist and pass tests — but `internal/service/chat.go` routes exactly three. Everything else returns `unknown provider`. Second, this repo has a release-topology problem that is actively biting: local `main` sits at v1.9.5 while `origin/main` is 212 commits ahead at v1.10.10, the working tree carries 181 uncommitted modifications that are *themselves* behind `origin/main` by ~9,900 lines, and — most importantly — **tags `v1.10.11` and `v1.10.12` are not on `main` at all**; they exist only on `origin/release/email-ai-task0-airborne-1.10.12`. The one production consumer depends on a version that never landed on the mainline. Third, the docs have drifted from the code: `PRODUCT.md` still describes table-prefix isolation (`ai8_airborne_threads`) and migrations `007`/`008`/`009` that do not exist in this repo, and still reports "Version: 1.9.5".

## Product Direction

### 1. Ship the provider fleet — turn 3 routed providers into 16 (Effort: M)

**Why it matters:** This is the single largest gap between what Airborne promises and what it does. The value proposition is vendor independence; today the gateway is a three-vendor gateway with sixteen vendors' worth of dead code.

**What's actually blocking it** is small and mechanical, which is why this is M and not L. `PRODUCT.md` already says it: *"Adding a provider to the live path is a wiring task, not a from-scratch task."* Concretely:
- `ChatService` holds three fixed fields — `openaiProvider`, `geminiProvider`, `anthropicProvider` (`internal/service/chat.go:49-51`) — eagerly constructed in `NewChatService` (`chat.go:82-84`). This needs to become a `map[string]provider.Provider` registry.
- Two hardcoded switches must become registry lookups: the proto-enum→name switch (`chat.go:862-880`) and the name→instance switch (`chat.go:891-899`).
- `internal/provider/names.go` defines only three constants; it needs the full set.
- Each of the 13 clients (`internal/provider/{deepseek,grok,mistral,perplexity,cohere,together,fireworks,openrouter,deepinfra,hyperbolic,nebius,cerebras,upstage}`) is already a ~40-line wrapper over `internal/provider/compat/openai_compat.go`. They correctly report `SupportsNativeContinuity() == false` and `SupportsFileSearch() == false`, and the routing layer *already* handles that case via manual context injection in `retrieveRAGContext` (`chat.go:905`). The hard part is done.

**What it unlocks:** OpenRouter alone is a meta-unlock (hundreds of models behind one client). Cost arbitrage becomes real — `pricing_db` already prices 27 providers while Airborne routes 3. It also makes the `SelectProvider` RPC meaningful as a routing-policy surface rather than a three-way coin flip.

### 2. Fix release topology so the mainline is the product (Effort: S)

**Why it matters:** `email_ai_svc` depends on `airborne v1.10.12`, a tag reachable only from `origin/release/email-ai-task0-airborne-1.10.12`. Anyone who builds from `main` gets code the flagship consumer does not run. Combined with a local checkout that is 212 commits stale and 181 files dirty, "what is actually deployed" is currently unanswerable from the repo alone.

**What it unlocks:** Trustworthy `go get`, a coherent CHANGELOG, and the ability to reason about what shipped. This is the cheapest high-leverage item on the list.

### 3. Make tenant failover order actually govern failover (Effort: S)

**Why it matters:** This is a live correctness gap, not a feature request. `internal/tenant/config.go:46` defines `FailoverConfig{Enabled, Order}`, `loader.go:148-153` *validates* that every name in `failover.order` references a real provider, and `TenantConfig.DefaultProvider()` (`config.go:61-85`) honors the order when picking the **initial** provider. But when a request actually fails over, `getFallbackProvider` (`chat.go:752-776`) ignores the tenant config entirely and runs a hardcoded three-way map (openai→gemini, gemini→openai, anthropic→openai, default→gemini). Operators configure a failover chain, it validates cleanly, and then it is silently discarded at the moment it matters.

**What it unlocks:** Real per-tenant resilience policy, and it is a prerequisite for Direction 1 — a 16-provider fleet with a hardcoded 3-way fallback is worse than useless.

### 4. Promote the model registry into a product surface (Effort: M)

**Why it matters:** There is a genuinely good feature hiding here that nobody can see. `airborne_models` (`migrations/001_baseline.sql:135`) plus `applyModelRegistry` (`chat.go:790`) and `mergeRegistryParams` (`chat.go:815`) implement per-tenant model **aliasing**: a tenant asks for `"fast-summarizer"`, the registry resolves it to a `base_model_id` and layers in default `temperature`/`top_p`/`max_output_tokens`, with request and tenant values always winning over registry defaults. That is a clean, correctly-precedenced indirection layer — and it has no admin RPC, no dashboard screen, and no mention in README or PRODUCT.md.

**What it unlocks:** Consumers stop hardcoding vendor model strings. Model migrations become a database update instead of a redeploy of every caller — which matters immediately, since `configs/airborne.yaml` still ships `gpt-4o`, `gemini-3-pro-preview`, and `claude-sonnet-4-20250514` as defaults. Paired with Direction 1, an alias can be repointed across *vendors*, which is the actual realization of vendor independence.

### 5. Close the RLS verification hole in CI (Effort: S)

**Why it matters:** The README says it plainly (lines 271-284): the tenant-isolation suite in `internal/db` needs Docker/testcontainers and **silently skips rather than fails** when Docker is absent. It is described as "the **only** verification that tenant data is actually isolated by Row-Level Security." A silent skip on the one test guarding cross-tenant data leakage is the highest-severity process risk in the repo — especially given the README's own warning that connecting as a superuser or table owner silently disables RLS with no error.

**What it unlocks:** The ability to ship schema changes without a manual local ritual. Note `.github/workflows/docker-build.yml` *does* have a `test-integration` job, so the fix is likely making the skip fatal in CI rather than building new infrastructure.

### 6. Wire tool calling to `dispatch` (Effort: L)

**Why it matters:** `provider.GenerateParams` already carries `Tools []Tool` and `ToolResults []ToolResult`, and `chat.go` converts them both ways (`convertTools:1155`, `convertToolResults:1171`, `convertToolCall:1186`). But Airborne only *passes tools through* — the caller must execute them and call back. Meanwhile `ai_suite/dispatch` is a tool-orchestration runtime that wraps shell, CLI, Python, Docker, HTTP, and gRPC execution in JSON envelopes.

**What it unlocks:** Server-side tool execution loops — the difference between an inference gateway and an agent runtime. **This is deliberately last.** `PRODUCT.md` draws a hard line: *"Airborne owns inference, not application workflows."* Crossing it is a real architectural decision, not an increment, and it should not happen before Directions 1-5 land.

## Near-Term (next 1-2 releases)

1. **Reconcile the repo state.** Fast-forward local `main` to `origin/main`, resolve the 181-file dirty working tree, and decide whether `release/email-ai-task0-airborne-1.10.11` and `-1.10.12` merge to `main` or get re-cut from it. Do this before any code change — the current state makes every other change hard to review.
2. **Fix `getFallbackProvider` (`chat.go:752`)** to consult `TenantConfig.Failover.Order` instead of the hardcoded map. Small, self-contained, and already has validated config feeding it.
3. **Make the `internal/db` RLS suite fail rather than skip when Docker is unavailable in CI.**
4. **Refresh `PRODUCT.md`.** Delete the table-prefix isolation description and the migrations `007`/`008`/`009` references (only `001_baseline.sql` exists), and stop pinning "Version: 1.9.5" — that line is now three tags stale and PRODUCT.md is the file agents are told to trust.
5. **Update stale model defaults** in `configs/airborne.yaml` (`gpt-4o`, `claude-sonnet-4-20250514`).
6. **Fix the `airborne-cli` default URL.** `cmd/airborne-cli/main.go:29,41` defaults to `http://localhost:50054`, which matches neither the gRPC port (50612) nor the admin port (8473). The README currently documents passing `--url` on every invocation as the workaround; just change the default to 8473.

## Mid-Term

- **Provider registry refactor** (Direction 1): replace the three `ChatService` fields and two switches with a map-based registry, extend `internal/provider/names.go`, and route the 13 existing compat clients. Add per-provider capability reporting to `SelectProvider` so callers can discover what a provider supports.
- **Model registry admin surface** (Direction 4): `AdminService` RPCs for model alias CRUD, plus a dashboard screen alongside the existing activity/debug views in `dashboard/src/app/`.
- **Provider-aware failover with health tracking.** Once >3 providers are routed, static ordering isn't enough — circuit-breaking on repeated provider failure. `internal/retry/` exists as a starting point.
- **Publish a client library.** `email_ai_svc/internal/airborne/client.go` is a well-built, hardened gRPC client — bounded payloads, typed `ErrRetryable`/`ErrPermanent`/`ErrAmbiguous` errors, `ErrorInfo` classification. Every future consumer will rewrite it. Note PRODUCT.md flags that README once referenced a `pkg/client/` that has never existed; this would finally make that true.
- **Reconcile dev/prod port defaults.** The repo defaults to gRPC 50612 while `infra_ai8` overrides `AIRBORNE_GRPC_PORT=50051` everywhere. Consistent, but a needless divergence between local and deployed reality.

## Long-Term / Frontier

- **Semantic routing.** `SelectProvider` already accepts content triggers, continuity, and user tier. With a real fleet, route by task shape — cheap models for classification, reasoning models for analysis, long-context models for RAG-heavy turns — with `pricing_db` closing the cost/quality loop automatically.
- **Server-side tool execution via `dispatch`** (Direction 6), if and only if the inference/workflow boundary is deliberately redrawn.
- **Prompt and response caching** keyed on the existing idempotency infrastructure. The hard part (fail-closed keyed storage with owner tokens and atomic compare-and-set) is already built and battle-tested in `internal/service/idempotency.go`.
- **Multi-provider consensus** — fan a prompt across providers and reconcile, for high-stakes extraction where `EnableStructuredOutput` already produces comparable JSON.
- **Self-hosted inference as a first-class provider.** `ollamabox` is already in the suite and Ollama is already a dependency for RAG embeddings.

## Risks & Open Questions

- **Release topology is the top risk.** A production consumer pinned to a tag that isn't on `main` will eventually cause a build that silently lacks the fail-closed idempotency work. Resolve before anything else.
- **Does adding providers dilute the hardening?** The last 15 releases bought fail-closed idempotency, SSRF validation, bounded reads, and RLS. Thirteen new routes are thirteen new paths through the security invariants that `PRODUCT.md` explicitly marks non-negotiable — custom `base_url` overrides require `admin` permission and SSRF validation, and compat providers are exactly the ones most likely to want custom base URLs.
- **RLS enforcement depends on an operational precondition the code cannot check.** If `DATABASE_URL` authenticates as superuser or table owner, RLS is silently bypassed with no error. Today this is a README instruction and a post-deploy `SELECT rolsuper, rolbypassrls`. Should Airborne assert this at startup and refuse to serve? That would convert the single highest-severity deployment risk into a crash-on-boot.
- **Is one production consumer enough signal?** Nearly all recent hardening was driven by `email_ai_svc`'s specific needs (the `eai1_` key format is literally accommodated in the validation contract). Direction 1 is a bet that more consumers arrive. If they don't, Directions 2-5 still pay for themselves and Direction 1 may not.
- **Who owns the Solstice schema contract?** `PRODUCT.md` describes `*_airborne_jobs` and `*_airborne_archives` as hosted-but-not-written-by Airborne — yet those migrations aren't in this repo. Either the doc is stale or the schema lives elsewhere now. Worth resolving explicitly, since it's documented as a cross-service contract.
- **Do the 13 compat clients actually work against live APIs?** Their tests (e.g. `internal/provider/deepseek/`) assert configuration and capability predicates, not real request/response behavior. Wiring them into routing is mechanical; validating them against live vendor APIs is the real cost, and is where the M in Direction 1 is concentrated.
