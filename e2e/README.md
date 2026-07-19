# Deterministic production-image E2E

`e2e/run.sh` creates a unique Compose project, resolves the active Docker endpoint
(including OrbStack contexts), and never treats missing Docker as a skipped test.
It writes logs and response evidence to `e2e/artifacts/` and tears down containers,
networks, volumes, and orphan services on every exit.

The caller must provide digest-pinned PostgreSQL and Redis images plus the exact
Airborne production image under test:

```bash
POSTGRES_E2E_IMAGE='postgres@sha256:<digest>' \
REDIS_E2E_IMAGE='redis@sha256:<digest>' \
AIRBORNE_E2E_IMAGE='airborne:e2e-tested' \
e2e/run.sh
```

The shared Makefile should build `airborne:e2e-tested` from the current SHA before
calling this script, invoke the resolver test as part of fast verification, and
pass the image reference to CI. The production-image harness verifies:

- the public health endpoint plus missing, invalid, and valid admin auth;
- direct gRPC chat and CLI chat against the deterministic provider stub;
- byte-identical keyed gRPC replay without a second provider dispatch;
- durable activity/message persistence through the non-owner application role;
- PostgreSQL row-level-security denial across tenants;
- the strict disabled-upload response;
- secret-free frozen configuration and restart readiness;
- provider 429, 500, malformed JSON, and a real delayed response that exceeds a bounded test-client timeout;
- the live dashboard proxy and a Chromium browser journey; and
- removal of every Compose container, network, and volume on exit.

The E2E Redis instance is intentionally unauthenticated, non-persistent, and
tmpfs-backed. It exists only to exercise keyed idempotency in this isolated
test topology; it does not satisfy or weaken the authenticated,
non-evicting, persistent/replicated Redis readiness contract for production.

The Compose runtime network is internal. Containers can reach only the local
PostgreSQL, ephemeral Redis, and provider-stub services, so the suite cannot
accidentally call a live provider. `make e2e` builds the probe, CLI, freezer,
and exact production image automatically. Dashboard dependencies and Chromium
must already be installed (`cd dashboard && npm ci && npx playwright install chromium`).
