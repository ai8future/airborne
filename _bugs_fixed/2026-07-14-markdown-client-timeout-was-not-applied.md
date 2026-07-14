# Markdown client timeout was not applied

- **Symptom:** Markdown parse, render, and chunk operations could run without the configured default deadline.
- **Root cause:** The client retained timeout configuration but did not wrap individual operation contexts with it.
- **Fix:** Applied the 120-second operation deadline unless the caller already supplied an earlier deadline.
- **Verification:** Protocol tests exercise every operation, timeout behavior, and preservation of earlier caller deadlines.
