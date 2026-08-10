# Release verification could pass with incomplete evidence

- **Symptom:** Aggregate coverage or stale generated reports could make a release appear verified while packages or required integration paths remained unproven.
- **Root cause:** Verification lacked per-package floors, clean-evidence checks, and fail-closed publication coupling.
- **Fix:** Added root and nested-module package floors, dashboard gates, required DB/E2E checks, clean-artifact assertions, and exact-tested-image publication.
- **Verification:** The audited candidate passed coverage, integration, production E2E, and `make verify` with reproducible evidence.
