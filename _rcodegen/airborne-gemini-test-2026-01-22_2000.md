Date Created: Thursday, January 22, 2026 20:00:00
TOTAL_SCORE: 85/100

# Test Coverage Report

## Overview
The codebase generally has good test coverage in critical modules such as `internal/validation`, `internal/tenant`, and `internal/httpcapture`. However, a significant gap was identified in `internal/db`, which contains core logic for data persistence and tenant isolation but completely lacks unit tests.

## Findings

### `internal/db` (Score: 0/10)
- **Critical Issues**: 
    - No tests exist for `repository.go`, `postgres.go`, or `models.go`.
    - `repository.go` contains complex logic for tenant isolation (table prefixes) and tenant ID validation that is untested.
    - Database interactions are not tested (understandable given the complexity of integration tests, but logic should be).

### `internal/pricing` (Score: 5/10)
- **Issues**:
    - `pricing.go` is a wrapper around an external library but lacks tests for its own logic (e.g., `Cost.Format()`).
    - Low priority compared to `internal/db`.

### `internal/httpcapture` (Score: 10/10)
- Well tested with `transport_test.go`.

### `internal/validation` (Score: 10/10)
- Excellent coverage for validation logic.

## Proposed Improvements

The most critical improvement is adding unit tests for `internal/db/repository.go`. While testing actual database queries requires an integration test setup (likely with Docker), we can and should unit test the repository configuration logic, tenant ID validation, and table name generation to ensure data isolation is correctly enforced at the configuration level.

## Proposed Tests

### `internal/db/repository_test.go`

This new test file covers:
1.  **Tenant Validation**: Ensures `NewTenantRepository` correctly validates tenant IDs against the allowed list.
2.  **Legacy Support**: Verifies `NewRepository` creates a repository with legacy table names (no prefix).
3.  **Table Name Generation**: Verifies that for a valid tenant, all table accessor methods (`threadsTable`, `messagesTable`, etc.) return the correctly prefixed table names. This is crucial for preventing data leaks between tenants.

```go
package db

import (
	"errors"
	"testing"
)

func TestNewTenantRepository(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		wantErr  bool
	}{
		{
			name:     "valid tenant ai8",
			tenantID: "ai8",
			wantErr:  false,
		},
		{
			name:     "valid tenant email4ai",
			tenantID: "email4ai",
			wantErr:  false,
		},
		{
			name:     "valid tenant zztest",
			tenantID: "zztest",
			wantErr:  false,
		},
		{
			name:     "invalid tenant",
			tenantID: "invalid",
			wantErr:  true,
		},
		{
			name:     "empty tenant",
			tenantID: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, err := NewTenantRepository(nil, tt.tenantID)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewTenantRepository() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if repo.TenantID() != tt.tenantID {
					t.Errorf("NewTenantRepository() TenantID = %v, want %v", repo.TenantID(), tt.tenantID)
				}
				// Verify table prefix
				expectedPrefix := tt.tenantID + "_airborne"
				// Since tablePrefix is unexported, we can verify it indirectly via table name getters
				if repo.threadsTable() != expectedPrefix+"_threads" {
					t.Errorf("threadsTable() = %v, want %v", repo.threadsTable(), expectedPrefix+"_threads")
				}
			} else {
				if err != nil && !errors.Is(err, ErrInvalidTenant) {
					t.Errorf("expected error %v, got %v", ErrInvalidTenant, err)
				}
			}
		})
	}
}

func TestNewRepository_Legacy(t *testing.T) {
	repo := NewRepository(nil)
	if repo.TenantID() != "" {
		t.Errorf("NewRepository() TenantID = %v, want empty string", repo.TenantID())
	}
	
	// Legacy table names
	if repo.threadsTable() != "airborne_threads" {
		t.Errorf("threadsTable() = %v, want airborne_threads", repo.threadsTable())
	}
	if repo.messagesTable() != "airborne_messages" {
		t.Errorf("messagesTable() = %v, want airborne_messages", repo.messagesTable())
	}
}

func TestRepository_TableNames(t *testing.T) {
	// We've already tested NewTenantRepository which sets the prefix.
	// This test ensures all table getters use the prefix correctly.
	repo, err := NewTenantRepository(nil, "ai8")
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	tests := []struct {
		name     string
		got      string
		want     string
	}{
		{"threadsTable", repo.threadsTable(), "ai8_airborne_threads"},
		{"messagesTable", repo.messagesTable(), "ai8_airborne_messages"},
		{"filesTable", repo.filesTable(), "ai8_airborne_files"},
		{"fileUploadsTable", repo.fileUploadsTable(), "ai8_airborne_file_provider_uploads"},
		{"vectorStoresTable", repo.vectorStoresTable(), "ai8_airborne_thread_vector_stores"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s() = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
```
