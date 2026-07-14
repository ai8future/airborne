# Frozen config unreadable in non-root CI container

- **Symptom:** Production E2E could fail on Linux hosted CI when the non-root Airborne container attempted to read the bind-mounted frozen configuration.
- **Root cause:** The host-side snapshot inherited restrictive temporary-directory permissions and was mounted without an explicit readable mode.
- **Fix:** After confirming the snapshot contains no plaintext test secrets, set it to mode `0644` before Compose startup.
- **Test:** The fast suite now verifies the exact permission command, its ordering, and the `0600` to `0644` mode transition; `make test-fast` passes.
