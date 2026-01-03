# Changelog

All notable changes to this project will be documented in this file.

## [0.3.0] - 2026-01-03

### Added
- **Multitenancy support**: AIBox can now serve multiple tenants with isolated configurations
- New `internal/tenant` package with TenantManager for per-tenant configuration
- Per-tenant provider API keys and settings via `configs/{tenant_id}.json`
- `tenant_id` field in `GenerateReplyRequest` and `SelectProviderRequest` proto messages
- TenantInterceptor for validating and injecting tenant config into gRPC context
- Secret resolution with `ENV=` and `FILE=` prefixes for API keys
- Hot-reload support for tenant configurations (SIGHUP)
- Tenant-scoped Redis key prefixes for data isolation
- Backwards-compatible single-tenant mode when only one tenant is configured

### Changed
- ChatService now uses tenant config for provider selection and credentials
- KeyStore supports tenant-scoped key prefixes via `NewTenantKeyStore()`
- gRPC server logs tenant count on startup

## [0.2.0] - 2026-01-02

### Security
- **BREAKING**: Server now requires Redis in production mode to prevent authentication bypass
- Add input size validation to prevent DoS attacks (100KB user input, 50KB instructions, 100 history messages)
- Fix rate limiting race condition with atomic Lua script
- Sanitize error messages to prevent information leakage
- Validate request IDs to prevent log injection attacks

### Added
- `startup_mode` configuration option (`production`/`development`)
- `AIBOX_STARTUP_MODE` environment variable override
- Input validation package with size limits
- Error sanitization package
- Request ID validation and generation

### Changed
- Rate limiter uses atomic Redis Lua scripts instead of separate commands

### Removed
- Unused `extractTextFromValue` function from Anthropic provider

## [0.1.0] - 2026-01-02

### Added
- Initial AIBox gRPC service definitions
- Core infrastructure with proto files for aibox, admin, files, and common services
- Go implementation with provider system (OpenAI, Anthropic, Gemini)
- Authentication system with API keys and rate limiting
- Redis client integration
- Configuration management
- gRPC server implementation
