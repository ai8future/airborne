# Coding Conventions

**Analysis Date:** 2026-01-14

## Naming Patterns

**Files:**
- snake_case.go for all Go files (`config.go`, `rate_limit.go`)
- *_test.go alongside source files (co-located)
- client.go for provider implementations
- service.go for main orchestration logic

**Functions:**
- PascalCase for exported: `GenerateReply`, `NewClient`, `ValidateProviderURL`
- camelCase for unexported: `defaultConfig`, `parseAPIKey`, `buildMessages`
- No special prefix for async functions
- Handler pattern: `handleX` not commonly used (gRPC service methods instead)

**Variables:**
- camelCase for all variables: `ctx`, `cfg`, `err`, `requestID`
- Single-letter receivers: `c *Client`, `s *Service`, `a *Authenticator`
- Constants: UPPER_SNAKE_CASE for unexported, PascalCase for exported
- Examples: `defaultModel`, `maxAttempts`, `requestTimeout`

**Types:**
- PascalCase for all types: `Config`, `Provider`, `GenerateParams`
- No I prefix for interfaces: `Provider` not `IProvider`
- Struct embedding for gRPC: `pb.UnimplementedAIBoxServiceServer`
- Enums: PascalCase values (`ChunkTypeText`, `ChunkTypeError`)

## Code Style

**Formatting:**
- gofmt with -s flag (simplify)
- Tab indentation (Go standard)
- No line length limit enforced
- Run via: `make fmt`

**Linting:**
- golangci-lint (optional, skipped if not installed)
- Run via: `make lint`
- No .golangci.yml - uses defaults

## Import Organization

**Order:**
1. Standard library imports
2. (blank line)
3. Third-party imports (google.golang.org, github.com/*)
4. (blank line)
5. Internal imports (github.com/ai8future/airborne/...)

**Example from `cmd/airborne/main.go`:**
```go
import (
    "context"
    "flag"
    "fmt"
    "log/slog"
    "net"
    "os"
    "os/signal"
    "syscall"

    "google.golang.org/grpc"
    "google.golang.org/grpc/health"
    "google.golang.org/grpc/health/grpc_health_v1"

    airbornev1 "github.com/ai8future/airborne/gen/go/airborne/v1"
    "github.com/ai8future/airborne/internal/config"
    "github.com/ai8future/airborne/internal/server"
)
```

**Path Aliases:**
- `airbornev1` for generated protobuf code
- `pb` commonly used in service files
- `sanitize` for error handling package

## Error Handling

**Patterns:**
- Return errors up, handle at boundaries
- Wrap with context: `fmt.Errorf("failed to X: %w", err)`
- Package-level error variables: `var ErrKeyNotFound = errors.New("key not found")`

**Error Wrapping:**
- Always use `%w` verb for wrappable errors
- Include context in wrap message
- Example: `return fmt.Errorf("failed to hash secret: %w", err)`

**Error Types:**
- Validation errors returned immediately (fail fast)
- Internal errors logged, generic message to client
- gRPC status codes for structured errors

## Logging

**Framework:**
- log/slog (Go standard library)
- Structured logging with key-value pairs

**Patterns:**
- Format: `slog.Info("message", "key", value, "key2", value2)`
- Levels: Debug, Info, Warn, Error
- Context: Include requestID, clientID where available

**Examples:**
```go
slog.Info("starting gRPC server", "address", address)
slog.Error("failed to generate reply", "error", err, "provider", provider.Name())
```

**Where to log:**
- Service boundaries (incoming requests)
- External service calls
- Errors before returning
- State transitions

## Comments

**When to Comment:**
- Explain "why", not "what"
- Document public APIs (GoDoc style)
- Explain non-obvious algorithms
- Note security considerations

**GoDoc Style:**
```go
// ValidateProviderURL validates a URL intended for use as a provider base URL.
// It performs security checks to prevent SSRF attacks by:
// - Requiring HTTPS protocol for non-localhost addresses
// - Blocking dangerous protocols (file://, javascript:, etc.)
// - Blocking private IP ranges
func ValidateProviderURL(rawURL string) error {
```

**TODO Comments:**
- Format: `// TODO: description`
- No username (use git blame)
- Link to issue if exists

## Function Design

**Size:**
- Keep under ~100 lines
- Extract helpers for complex logic
- One level of abstraction per function

**Parameters:**
- Use structs for 4+ parameters (e.g., `GenerateParams`)
- Context always first parameter
- Options pattern for optional configuration

**Return Values:**
- (result, error) for fallible operations
- Return early for guard clauses
- Named returns only for defer clarity

## Module Design

**Exports:**
- Named exports only (Go doesn't have default exports)
- Capitalize for public, lowercase for private
- Interface in separate file if large

**Package Structure:**
- One package per directory
- Package name matches directory
- internal/ for private packages

**Circular Dependencies:**
- Avoided by layered architecture
- Services depend on providers, not reverse
- Common types in shared packages

---

*Convention analysis: 2026-01-14*
*Update when patterns change*
