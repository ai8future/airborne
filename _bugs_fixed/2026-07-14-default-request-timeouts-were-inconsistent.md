# Default request timeouts were inconsistent

- **Symptom:** Some request paths could terminate at legacy or uneven deadlines instead of allowing the safer two-minute window.
- **Root cause:** Timeout defaults were duplicated across clients and transports rather than following one bounded policy.
- **Fix:** Standardized default request deadlines at 120 seconds while retaining explicit route-specific longer bounds where required.
- **Verification:** Regression tests cover configured defaults and the complete release passed `make verify` at audited candidate `cdded891`.
