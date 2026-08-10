# CI E2E dependency staging path collision

- **Symptom:** The verify-E2E job could fail source-cleanliness or image-context validation after checking out required local-replacement repositories inside the Airborne worktree.
- **Root cause:** CI dependency destinations and Make overrides occupied repository paths that collided with fail-closed verification and the canonical external replacement layout.
- **Fix:** Stage dependencies under ignored, Docker-excluded `node_modules/airborne-ci-deps`, repoint external symlinks there, and run `make e2e` without colliding directory overrides.
- **Verification:** Workflow YAML, focused semantic assertions, and diff checks passed; an architect approved the change, an independent reviewer approved it, and GitHub Actions validates the pushed SHA.
