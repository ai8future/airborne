# Admin shutdown nil server

`internal/admin.Server.Shutdown` dereferenced `s.server` after a lazy gRPC client
had been created on a partially initialized server. It now closes the gRPC client
and returns successfully when no HTTP server was installed. Coverage includes the
lazy-client lifecycle path.
