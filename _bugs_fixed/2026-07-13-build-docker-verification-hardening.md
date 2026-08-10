# Build and Docker verification hardening

Ralph build verification exposed several release-blocking issues:
- Go 1.26.4/1.26.2 metadata left Airborne exposed to a reachable standard-library TLS vulnerability reported by `govulncheck`.
- Server Docker/CI builds did not provide every local `go.mod` replace target and could drift against dirty sibling checkouts.
- Docker build contexts could include local secret files, and Compose could rebuild from an unstaged context instead of the verified image.
- The server image copied 0600 config templates for a non-root runtime user, causing startup failures.
- Dashboard Docker assumed a committed `public/` directory and used `next lint`, which is no longer a valid Next 16 verification path.
- Buf lint config used a deprecated category without documenting the intentional shared request/stream naming exceptions.

Fixed by pinning/staging local replace snapshots, upgrading Go to 1.26.5, hardening Docker ignores and image permissions, aligning Compose with the Makefile-built image, replacing dashboard lint with TypeScript checking, and refreshing Buf lint configuration.

Verified with Go tests/race/vet/build/coverage, dashboard build/audit/typecheck, govulncheck, focused gosec, Buf lint, Docker image builds, Compose startup, Docker secret-exclusion proofs, and a clean pinned-dependency workspace smoke.
