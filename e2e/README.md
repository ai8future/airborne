# Deterministic production-image E2E

`e2e/run.sh` creates a unique Compose project, resolves the active Docker endpoint
(including OrbStack contexts), and never treats missing Docker as a skipped test.
It writes logs and response evidence to `e2e/artifacts/` and tears down containers,
networks, volumes, and orphan services on every exit.

The caller must provide a digest-pinned PostgreSQL image and the exact Airborne
production image under test:

```bash
POSTGRES_E2E_IMAGE='postgres@sha256:<digest>' \
AIRBORNE_E2E_IMAGE='airborne:e2e-tested' \
e2e/run.sh
```

The shared Makefile should build `airborne:e2e-tested` from the current SHA before
calling this script, invoke the resolver test as part of fast verification, and
pass the image reference to CI. Dashboard/browser and frozen-config scenarios are
intentionally reserved for their dedicated dashboard/config suites; this harness
provides the isolated stack and black-box boundary checks they consume.
