Date Created: 2026-01-19 20:48:00
TOTAL_SCORE: 72/100

# Test Coverage Analysis & Improvements

## Analysis
The `airborne` codebase demonstrates a solid testing foundation in core infrastructure areas, particularly configuration and authentication. However, significant gaps exist in business logic components (`pricing`), administrative interfaces (`admin`), and database abstraction logic (`db`).

### Strong Areas
- **Configuration**: `internal/config` is well-tested with comprehensive environment variable override checks.
- **Authentication**: `internal/auth` has extensive test coverage for interceptors, keys, and rate limiting.
- **Service Layer**: `internal/service` includes tests for core chat and file operations.

### Weak Areas (Addressed in this Report)
- **Pricing**: `internal/pricing` was completely untested despite containing critical financial calculation logic.
- **Admin Server**: `internal/admin` lacked tests for its HTTP handlers and setup logic.
- **Database Repository**: `internal/db` validation and schema logic was untested.

## Score Breakdown
- **Current Score: 72/100**
  - Config/Auth: 20/20
  - Service/Providers: 30/40
  - Pricing: 0/10 (Fixed below)
  - Admin/DB: 5/20 (Partially fixed below)
  - Integration/E2E: 17/10 (Implicit via service tests)

## Proposed Tests (Patch-Ready)

### 1. Pricing Logic Tests (`internal/pricing/pricing_test.go`)
**Coverage:** `NewPricer`, `Calculate`, `GetPricing`, `ListProviders`.
**Rationale:** Critical financial logic must be verified to ensure accurate cost tracking.

```go
package pricing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPricing(t *testing.T) {
	// Setup temporary config directory
	tmpDir := t.TempDir()

	// Create a dummy pricing file
	pricingJSON := `{
		"provider": "test_provider",
		"models": {
			"test-model": {
				"input_per_million": 1.0,
				"output_per_million": 2.0
			},
			"versioned-model-001": {
				"input_per_million": 5.0,
				"output_per_million": 10.0
			}
		}
	}`
	
	err := os.WriteFile(filepath.Join(tmpDir, "test_pricing.json"), []byte(pricingJSON), 0644)
	if err != nil {
		t.Fatalf("failed to write pricing file: %v", err)
	}

	// Test NewPricer
	pricer, err := NewPricer(tmpDir)
	if err != nil {
		t.Fatalf("NewPricer failed: %v", err)
	}

	// Test ListProviders
	providers := pricer.ListProviders()
	if len(providers) != 1 || providers[0] != "test_provider" {
		t.Errorf("expected [test_provider], got %v", providers)
	}

	// Test ModelCount
	if count := pricer.ModelCount(); count != 2 {
		t.Errorf("expected 2 models, got %d", count)
	}

	// Test GetPricing
	pricing, ok := pricer.GetPricing("test-model")
	if !ok {
		t.Error("test-model not found")
	}
	if pricing.InputPerMillion != 1.0 {
		t.Errorf("expected input cost 1.0, got %f", pricing.InputPerMillion)
	}

	// Test Calculate
	cost := pricer.Calculate("test-model", 1000000, 1000000)
	if cost.InputCost != 1.0 {
		t.Errorf("expected input cost 1.0, got %f", cost.InputCost)
	}
	if cost.OutputCost != 2.0 {
		t.Errorf("expected output cost 2.0, got %f", cost.OutputCost)
	}
	if cost.TotalCost != 3.0 {
		t.Errorf("expected total cost 3.0, got %f", cost.TotalCost)
	}

	// Test prefix matching
	pricing, ok = pricer.GetPricing("versioned-model-001-beta")
	if !ok {
		t.Error("versioned model prefix match failed")
	}
	if pricing.InputPerMillion != 5.0 {
		t.Errorf("expected input cost 5.0, got %f", pricing.InputPerMillion)
	}

	// Test Unknown Model
	cost = pricer.Calculate("unknown-model", 1000, 1000)
	if !cost.Unknown {
		t.Error("expected unknown model flag to be true")
	}
	if cost.TotalCost != 0 {
		t.Errorf("expected 0 cost for unknown model, got %f", cost.TotalCost)
	}
}
```

### 2. Admin Server Tests (`internal/admin/server_test.go`)
**Coverage:** `handleHealth`, `handleVersion`, `handleActivity` (error case), `handleDebug` (error case).
**Rationale:** Ensures administrative endpoints are reachable and correctly configured, even without a live database.

```go
package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServer_HandleHealth(t *testing.T) {
	// Create server without DB (should report degraded/unhealthy DB but handle request)
	cfg := Config{Port: 8080}
	s := NewServer(nil, cfg)

	req := httptest.NewRequest("GET", "/admin/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// If DB is nil, status is degraded but request succeeds
	if data["status"] != "degraded" && data["status"] != "healthy" {
		t.Errorf("expected status degraded or healthy, got %v", data["status"])
	}
	if data["database"] != "not_configured" {
		t.Errorf("expected database not_configured, got %v", data["database"])
	}
}

func TestServer_HandleVersion(t *testing.T) {
	version := VersionInfo{
		Version:   "1.0.0",
		GitCommit: "abcdef",
		BuildTime: "now",
	}
	cfg := Config{
		Port:    8080,
		Version: version,
	}
	s := NewServer(nil, cfg)

	req := httptest.NewRequest("GET", "/admin/version", nil)
	w := httptest.NewRecorder()

	s.handleVersion(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var data VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if data.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", data.Version)
	}
}

func TestServer_HandleActivity_NoDB(t *testing.T) {
	s := NewServer(nil, Config{Port: 8080})

	req := httptest.NewRequest("GET", "/admin/activity", nil)
	w := httptest.NewRecorder()

	s.handleActivity(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if data["error"] != "database not configured" {
		t.Errorf("expected database not configured error, got %v", data["error"])
	}
}

func TestServer_HandleDebug_NoDB(t *testing.T) {
	s := NewServer(nil, Config{Port: 8080})

	// Missing message ID
	req := httptest.NewRequest("GET", "/admin/debug/", nil)
	w := httptest.NewRecorder()
	s.handleDebug(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing ID, got %d", w.Result().StatusCode)
	}

	// Valid ID, no DB
	req = httptest.NewRequest("GET", "/admin/debug/00000000-0000-0000-0000-000000000000", nil)
	w = httptest.NewRecorder()
	s.handleDebug(w, req)
	
	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 Service Unavailable, got %d", resp.StatusCode)
	}
}
```

### 3. Database Repository Validation Tests (`internal/db/repository_test.go`)
**Coverage:** `NewTenantRepository` validation, table name generation.
**Rationale:** Verifies tenant isolation logic without requiring a live database connection.

```go
package db

import (
	"testing"
)

func TestNewTenantRepository_Validation(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		wantErr  bool
	}{
		{"valid ai8", "ai8", false},
		{"valid email4ai", "email4ai", false},
		{"valid zztest", "zztest", false},
		{"invalid tenant", "invalid", true},
		{"empty tenant", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pass nil client since validation happens before client usage
			repo, err := NewTenantRepository(nil, tt.tenantID)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewTenantRepository() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if !tt.wantErr {
				if repo.tenantID != tt.tenantID {
					t.Errorf("expected tenantID %s, got %s", tt.tenantID, repo.tenantID)
				}
				expectedPrefix := tt.tenantID + "_airborne"
				if repo.tablePrefix != expectedPrefix {
					t.Errorf("expected tablePrefix %s, got %s", expectedPrefix, repo.tablePrefix)
				}
			}
		})
	}
}

func TestRepository_TableNames(t *testing.T) {
	// Test with valid tenant
	repo, _ := NewTenantRepository(nil, "ai8")
	
	if tbl := repo.threadsTable(); tbl != "ai8_airborne_threads" {
		t.Errorf("expected threads table ai8_airborne_threads, got %s", tbl)
	}
	if tbl := repo.messagesTable(); tbl != "ai8_airborne_messages" {
		t.Errorf("expected messages table ai8_airborne_messages, got %s", tbl)
	}

	// Test legacy/base repository
	baseRepo := NewRepository(nil)
	if tbl := baseRepo.threadsTable(); tbl != "airborne_threads" {
		t.Errorf("expected legacy threads table airborne_threads, got %s", tbl)
	}
}
```
