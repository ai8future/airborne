# Admin and dashboard security hardening

Fixed a security-hardening bundle from the July 6, 2026 audit:

- HTTP admin routes other than health probes were reachable without their own bearer/API-key check, including privileged proxy and inspection endpoints.
- Dashboard API routes proxied admin operations without a dashboard-side credential gate.
- Stored rendered HTML and arbitrary citation URLs could reach the dashboard UI.
- FileService custom provider `base_url` overrides were available to files-only callers instead of admin-only callers.
- Several upload/history/capture/provider-response paths lacked tight body or integer-narrowing bounds.

The fix adds fail-closed admin/dashboard auth, explicit CORS, CSRF checks for cookie auth, safe dashboard rendering/linking, admin-only custom base URLs, escaped provider/resource URL construction, bounded body reads, and regression tests.
