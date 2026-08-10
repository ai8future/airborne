# Live dashboard chat failed on a real local origin

- **Symptom:** The production dashboard could fail before sending chat on an HTTP OrbStack origin, or route to an unintended provider.
- **Root cause:** The browser path assumed `crypto.randomUUID` was available on every origin and provider selection lacked an explicit runtime default.
- **Fix:** Added a UUID fallback, explicit runtime provider routing, and production-stack browser diagnostics.
- **Verification:** The live Chromium journey and direct dashboard proxy journey passed against the production Compose stack.
