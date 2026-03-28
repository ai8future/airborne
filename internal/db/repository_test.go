package db

import (
	"errors"
	"strings"
	"testing"
)

func TestNewTenantRepository_ValidTenants(t *testing.T) {
	for _, tenantID := range []string{"ai8", "email4ai", "zztest"} {
		t.Run(tenantID, func(t *testing.T) {
			repo, err := NewTenantRepository(nil, tenantID)
			if err != nil {
				t.Fatalf("NewTenantRepository(%q) error = %v", tenantID, err)
			}
			if repo.TenantID() != tenantID {
				t.Errorf("TenantID() = %q, want %q", repo.TenantID(), tenantID)
			}
		})
	}
}

func TestNewTenantRepository_InvalidTenants(t *testing.T) {
	invalidTenants := []string{"", "unknown", "admin", "AI8", "root"}
	for _, tenantID := range invalidTenants {
		t.Run("invalid_"+tenantID, func(t *testing.T) {
			_, err := NewTenantRepository(nil, tenantID)
			if err == nil {
				t.Fatalf("expected error for invalid tenant %q", tenantID)
			}
			if !errors.Is(err, ErrInvalidTenant) {
				t.Errorf("expected ErrInvalidTenant, got: %v", err)
			}
		})
	}
}

func TestRepository_TableNames_Legacy(t *testing.T) {
	repo := NewRepository(nil)

	tests := []struct {
		method string
		got    string
		want   string
	}{
		{"threadsTable", repo.threadsTable(), "airborne_threads"},
		{"messagesTable", repo.messagesTable(), "airborne_messages"},
		{"filesTable", repo.filesTable(), "airborne_files"},
		{"fileUploadsTable", repo.fileUploadsTable(), "airborne_file_provider_uploads"},
		{"vectorStoresTable", repo.vectorStoresTable(), "airborne_thread_vector_stores"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s() = %q, want %q", tt.method, tt.got, tt.want)
			}
		})
	}
}

func TestRepository_TableNames_Tenant(t *testing.T) {
	repo, err := NewTenantRepository(nil, "ai8")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		method string
		got    string
		want   string
	}{
		{"threadsTable", repo.threadsTable(), "ai8_airborne_threads"},
		{"messagesTable", repo.messagesTable(), "ai8_airborne_messages"},
		{"filesTable", repo.filesTable(), "ai8_airborne_files"},
		{"fileUploadsTable", repo.fileUploadsTable(), "ai8_airborne_file_provider_uploads"},
		{"vectorStoresTable", repo.vectorStoresTable(), "ai8_airborne_thread_vector_stores"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s() = %q, want %q", tt.method, tt.got, tt.want)
			}
		})
	}
}

func TestRepository_TableNames_AllTenants(t *testing.T) {
	for tenantID := range ValidTenantIDs {
		t.Run(tenantID, func(t *testing.T) {
			repo, err := NewTenantRepository(nil, tenantID)
			if err != nil {
				t.Fatal(err)
			}

			prefix := tenantID + "_airborne_"

			if !strings.HasPrefix(repo.threadsTable(), prefix) {
				t.Errorf("threadsTable() = %q, expected prefix %q", repo.threadsTable(), prefix)
			}
			if !strings.HasPrefix(repo.messagesTable(), prefix) {
				t.Errorf("messagesTable() = %q, expected prefix %q", repo.messagesTable(), prefix)
			}
			if !strings.HasPrefix(repo.filesTable(), prefix) {
				t.Errorf("filesTable() = %q, expected prefix %q", repo.filesTable(), prefix)
			}
		})
	}
}

func TestValidTenantIDs(t *testing.T) {
	expected := []string{"ai8", "email4ai", "zztest"}
	for _, id := range expected {
		if !ValidTenantIDs[id] {
			t.Errorf("expected %q in ValidTenantIDs", id)
		}
	}

	if len(ValidTenantIDs) != len(expected) {
		t.Errorf("ValidTenantIDs has %d entries, want %d", len(ValidTenantIDs), len(expected))
	}
}
