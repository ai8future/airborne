# Admin shutdown could skip HTTP cleanup

- **Symptom:** A gRPC close failure could return early and leave the admin HTTP server running.
- **Root cause:** Shutdown executed components sequentially with an early return on the first error.
- **Fix:** Always attempt both gRPC close and HTTP shutdown, then join any resulting errors.
- **Verification:** Lifecycle regression tests assert both cleanup functions run and both errors remain discoverable.
