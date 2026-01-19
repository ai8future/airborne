# Provider Refactoring Plan
Date: 2026-01-19
Status: Proposed

## Objective
Refactor the `internal/provider` package to reduce code duplication between OpenAI and Anthropic clients (and future providers). Currently, both clients implement their own HTTP client setup, retry logic, configuration parsing, and streaming management, resulting in significant boilerplate.

## Proposed Changes

### 1. Create `internal/provider/base` Package
We will create a new package (or a shared struct in `internal/provider`) to encapsulate common functionality.

#### `BaseClient` Struct
This struct will hold shared dependencies and configuration:
```go
type BaseClient struct {
    APIKey          string
    BaseURL         string
    HTTPClient      *http.Client // With capture and timeout
    RetryMax        int
    Logger          *slog.Logger
}
```

#### Shared Functionality
-   **Client Initialization**: Standardize `httpcapture` integration and validation of `BaseURL`.
-   **Configuration Parsing**: Helper methods to safely extract string/int/bool values from `map[string]string` (ExtraOptions).
-   **Retry Logic**: A shared `ExecuteWithRetry` method that handles the backoff loop and error classification (using a callback for the actual API call).
-   **Stream Management**: A generic stream handler or channel management utility if the SDKs allow sufficient abstraction.

### 2. Refactor `internal/provider/openai`
-   Update `NewClient` to initialize `BaseClient`.
-   Replace manual retry loops in `GenerateReply` with `BaseClient.ExecuteWithRetry`.
-   Use shared configuration helpers.

### 3. Refactor `internal/provider/anthropic`
-   Update `NewClient` to initialize `BaseClient`.
-   Replace manual retry loops and HTTP setup with `BaseClient` methods.

## Benefits
-   **Maintainability**: Fix bugs (like retry logic or logging) in one place.
-   **Consistency**: Ensure all providers behave similarly regarding timeouts, logging, and error reporting.
-   **Extensibility**: Adding a new provider (e.g., DeepSeek, Mistral) becomes much faster.

## Risks
-   **SDK Differences**: OpenAI and Anthropic Go SDKs might have subtle differences in how they handle `context` or `http.Client` injection that need careful handling.
-   **Regression**: Existing features like "Thinking" (Anthropic) or "Structured Output" (OpenAI) must be preserved.

## Implementation Steps
1.  Create `internal/provider/base/client.go`.
2.  Implement `ExecuteWithRetry` and config helpers.
3.  Refactor `openai` provider.
4.  Run `go test ./internal/provider/openai/...`.
5.  Refactor `anthropic` provider.
6.  Run `go test ./internal/provider/anthropic/...`.
7.  Verify `internal/service` tests pass.
