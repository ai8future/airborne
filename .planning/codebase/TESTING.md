# Testing Patterns

**Analysis Date:** 2026-01-14

## Test Framework

**Runner:**
- Go standard `testing` package
- No external test frameworks

**Assertion Library:**
- Go standard (no testify, gomega, etc.)
- Direct comparison with `if got != want`
- `t.Errorf()` for failures

**Run Commands:**
```bash
make test                    # Run all tests with race detection
go test ./...                # Run all tests
go test -v ./internal/...    # Verbose, internal packages only
go test -run TestName ./...  # Run specific test
make test-coverage           # Generate coverage report
```

## Test File Organization

**Location:**
- Co-located with source files
- Pattern: `source.go` → `source_test.go`
- Same package (not `_test` suffix)

**Naming:**
- `*_test.go` for all test files
- No distinction between unit/integration in filename

**Structure:**
```
internal/
  config/
    config.go
    config_test.go       # 500+ lines, comprehensive
  auth/
    keys.go
    keys_test.go
    ratelimit.go
    ratelimit_test.go
  rag/
    service.go
    service_test.go
    testutil/
      mocks.go           # Shared mocks for RAG components
```

## Test Structure

**Suite Organization:**
```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name     string
        input    InputType
        want     OutputType
        wantErr  bool
    }{
        {name: "valid input", input: ..., want: ...},
        {name: "empty input", input: ..., wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := FunctionName(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

**Patterns:**
- Table-driven tests preferred
- Use `t.Run()` for subtests
- Use `t.Helper()` in helper functions
- Use `t.TempDir()` for file-based tests

## Mocking

**Framework:**
- No mocking framework (interfaces + manual mocks)
- Mock implementations in `testutil/` packages

**Patterns:**
```go
// internal/rag/testutil/mocks.go
type MockEmbedder struct {
    EmbedFunc func(ctx context.Context, text string) ([]float32, error)
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
    return m.EmbedFunc(ctx, text)
}
```

**Redis Mocking:**
- Uses `github.com/alicebob/miniredis/v2`
- In-memory Redis for auth/rate limit tests

**What to Mock:**
- External services (LLM APIs, Qdrant, Ollama)
- Redis (via miniredis)
- File system (via t.TempDir())
- Time (not commonly mocked)

**What NOT to Mock:**
- Internal pure functions
- Configuration loading (use test configs)
- Error handling logic

## Fixtures and Factories

**Test Data:**
```go
// Inline struct for simple cases
input := InputType{
    Field1: "value",
    Field2: 42,
}

// Helper function for complex cases
func newTestConfig(t *testing.T) *config.Config {
    t.Helper()
    return &config.Config{
        Server: config.ServerConfig{Port: 50051},
        // ...
    }
}
```

**Temporary Files:**
```go
func TestFileOperation(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.yaml")
    // write test file, run test
}
```

**Location:**
- Factory functions in test file
- Shared mocks in `testutil/` packages
- No separate fixtures directory

## Coverage

**Requirements:**
- No enforced coverage target
- Coverage tracked for awareness
- Focus on critical paths

**Configuration:**
- Built-in Go coverage tooling
- Output: `coverage.out`, `coverage.html`

**View Coverage:**
```bash
make test-coverage
open coverage.html
```

## Test Types

**Unit Tests:**
- Test single function/method in isolation
- Mock all external dependencies
- Fast execution (<100ms per test)
- Examples: `TestParseAPIKey`, `TestGenerateRandomString`

**Integration Tests:**
- Test multiple components together
- Use miniredis for Redis
- Examples: `TestLoad_EnvOverridesYAML`, `TestManagerReload`

**E2E Tests:**
- Not present in codebase
- Manual testing for full flows

## Common Patterns

**Async Testing:**
```go
// Context with timeout
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

result, err := asyncFunction(ctx)
```

**Error Testing:**
```go
// Check error occurrence
if (err != nil) != tt.wantErr {
    t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
}

// Check specific error
if !errors.Is(err, ErrExpected) {
    t.Errorf("got error %v, want %v", err, ErrExpected)
}
```

**Table-Driven Tests:**
```go
tests := []struct {
    name    string
    input   string
    want    Result
    wantErr bool
}{
    {"valid", "input", Result{}, false},
    {"invalid", "", Result{}, true},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test body
    })
}
```

**Snapshot Testing:**
- Not used in this codebase
- Prefer explicit assertions

## Test Statistics

- **Total Go Files:** 68
- **Test Files:** 26 (38% of source files)
- **Test Coverage:** Comprehensive for config, auth, tenant packages
- **Race Detection:** Enabled by default (`-race` flag)

---

*Testing analysis: 2026-01-14*
*Update when test patterns change*
