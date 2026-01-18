Date Created: 2026-01-18 21:23:00
TOTAL_SCORE: 78/100

# AUDIT

### CRITICAL: PII Leakage via Raw JSON Persistence
The `ChatService` captures and persists the full raw HTTP request and response bodies (including PII in prompts and responses) to the database. This occurs in `internal/service/chat.go` using the `httpcapture` package. This violates privacy principles as sensitive user data is stored in the `raw_request_json` and `raw_response_json` columns.

### HIGH: Tests Failing Due to Strict SSRF Validation
The security hardening (SSRF protection) correctly rejects non-existent domains like `custom.example.com`, but this breaks existing unit tests that rely on these fake domains. The tests need to be updated to use resolvable loopback addresses (e.g., `127.0.0.1`).

### MEDIUM: Unused Development Auth Interceptor
The `developmentAuthInterceptor` in `internal/server/grpc.go` is defined but unused. While not currently active, it represents dead code that bypasses authentication and could be accidentally enabled.

# TESTS

### Fix `internal/rag/extractor/docbox_test.go`
The test fails because the strict validator rejects `custom.example.com`.

```go
<<<<
	tests := []struct {
		name    string
		config  DocboxConfig
		wantURL string
	}{
		{
			name: "Custom URL",
			config: DocboxConfig{
				BaseURL: "https://custom.example.com:8080",
			},
			wantURL: "https://custom.example.com:8080",
		},
====
	tests := []struct {
		name    string
		config  DocboxConfig
		wantURL string
	}{
		{
			name: "Custom URL",
			config: DocboxConfig{
				BaseURL: "http://127.0.0.1:8080",
			},
			wantURL: "http://127.0.0.1:8080",
		},
>>>>
```

### Fix `internal/service/chat_test.go`
The test `TestPrepareRequest_CustomBaseURLRequiresAdmin` fails due to DNS lookup on a fake domain.

```go
<<<<
		{
			name: "Custom BaseURL Requires Admin",
			req: &pb.GenerateReplyRequest{
				UserInput: "test",
				ProviderConfigs: map[string]*pb.ProviderConfig{
					"openai": {BaseUrl: "https://custom.example.com"},
				},
			},
			userPerms: []auth.Permission{auth.PermissionChat},
			wantErr:   true,
			errCode:   codes.PermissionDenied,
		},
====
		{
			name: "Custom BaseURL Requires Admin",
			req: &pb.GenerateReplyRequest{
				UserInput: "test",
				ProviderConfigs: map[string]*pb.ProviderConfig{
					"openai": {BaseUrl: "http://127.0.0.1:8080"},
				},
			},
			userPerms: []auth.Permission{auth.PermissionChat},
			wantErr:   true,
			errCode:   codes.PermissionDenied,
		},
>>>>
```

# FIXES

### Disable Raw JSON Persistence
Prevent sensitive data from being stored in the database.

**File:** `internal/service/chat.go`

```go
<<<<
	// Build debug info from captured JSON (if available)
	var debugInfo *db.DebugInfo
	if len(result.RequestJSON) > 0 || len(result.ResponseJSON) > 0 {
		debugInfo = &db.DebugInfo{
			SystemPrompt:    req.Instructions,
			RawRequestJSON:  string(result.RequestJSON),
			RawResponseJSON: string(result.ResponseJSON),
		}
	}

	// Run persistence in background goroutine
====
	// Build debug info from captured JSON (if available)
	// SECURITY: Raw JSON persistence disabled to prevent PII leakage.
	// Re-enable only for debugging with proper PII redaction.
	var debugInfo *db.DebugInfo
	/*
	if len(result.RequestJSON) > 0 || len(result.ResponseJSON) > 0 {
		debugInfo = &db.DebugInfo{
			SystemPrompt:    req.Instructions,
			RawRequestJSON:  string(result.RequestJSON),
			RawResponseJSON: string(result.ResponseJSON),
		}
	}
	*/

	// Run persistence in background goroutine
>>>>
```

# REFACTOR

### Remove `httpcapture` Package
The `internal/httpcapture` package provides a facility that encourages capturing full request/response bodies. Unless this is strictly needed for non-production debugging, it should be removed or refactored to support automatic redaction of headers (Auth) and bodies.

### Centralize Validation Logic
The DNS/SSRF validation logic seems to be implicitly running during configuration loading or request preparation. It would be better to have a centralized `Validator` service that can be mocked in tests, allowing tests to use semantic URLs like `custom.example.com` without triggering actual network calls.
