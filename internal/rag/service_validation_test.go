// Package rag contains tests for tenant/store ID validation.
package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/cliffpyles/aibox/internal/rag/testutil"
)

func TestValidateCollectionParts_ValidInputs(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		storeID  string
	}{
		{"simple", "tenant1", "store1"},
		{"with_underscores", "tenant_1", "store_1"},
		{"with_hyphens", "tenant-1", "store-1"},
		{"mixed", "tenant-1_test", "store_2-prod"},
		{"uppercase", "TENANT", "STORE"},
		{"mixedcase", "TenantId", "StoreId"},
		{"numbers_only_after_first", "t123", "s456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCollectionParts(tt.tenantID, tt.storeID)
			if err != nil {
				t.Errorf("validateCollectionParts(%q, %q) returned error: %v", tt.tenantID, tt.storeID, err)
			}
		})
	}
}

func TestValidateCollectionParts_InvalidInputs(t *testing.T) {
	tests := []struct {
		name        string
		tenantID    string
		storeID     string
		errContains string
	}{
		{"empty_tenant", "", "store1", "tenant_id is required"},
		{"empty_store", "tenant1", "", "store_id is required"},
		{"whitespace_tenant", "   ", "store1", "tenant_id is required"},
		{"whitespace_store", "tenant1", "   ", "store_id is required"},
		{"path_traversal_tenant", "../admin", "store1", "tenant_id contains invalid characters"},
		{"path_traversal_store", "tenant1", "../admin", "store_id contains invalid characters"},
		{"slash_tenant", "tenant/evil", "store1", "tenant_id contains invalid characters"},
		{"slash_store", "tenant1", "store/evil", "store_id contains invalid characters"},
		{"dot_tenant", ".hidden", "store1", "tenant_id contains invalid characters"},
		{"dot_store", "tenant1", ".hidden", "store_id contains invalid characters"},
		{"starts_with_hyphen", "-tenant", "store1", "tenant_id contains invalid characters"},
		{"starts_with_underscore", "_tenant", "store1", "tenant_id contains invalid characters"},
		{"special_chars", "tenant@1", "store1", "tenant_id contains invalid characters"},
		{"space_in_middle", "ten ant", "store1", "tenant_id contains invalid characters"},
		{"backslash", "tenant\\1", "store1", "tenant_id contains invalid characters"},
		{"unicode", "tenant\u00e9", "store1", "tenant_id contains invalid characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCollectionParts(tt.tenantID, tt.storeID)
			if err == nil {
				t.Errorf("validateCollectionParts(%q, %q) should have returned error", tt.tenantID, tt.storeID)
				return
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestValidateCollectionParts_MaxLength(t *testing.T) {
	// Valid: exactly at max length
	validTenant := strings.Repeat("a", maxCollectionPartLen)
	validStore := strings.Repeat("b", maxCollectionPartLen)
	if err := validateCollectionParts(validTenant, validStore); err != nil {
		t.Errorf("should accept IDs at max length: %v", err)
	}

	// Invalid: exceeds max length
	tooLongTenant := strings.Repeat("a", maxCollectionPartLen+1)
	err := validateCollectionParts(tooLongTenant, "store1")
	if err == nil {
		t.Error("should reject tenant_id exceeding max length")
	} else if !strings.Contains(err.Error(), "tenant_id exceeds") {
		t.Errorf("unexpected error message: %v", err)
	}

	tooLongStore := strings.Repeat("b", maxCollectionPartLen+1)
	err = validateCollectionParts("tenant1", tooLongStore)
	if err == nil {
		t.Error("should reject store_id exceeding max length")
	} else if !strings.Contains(err.Error(), "store_id exceeds") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateCollectionParts_TrimSpace(t *testing.T) {
	// Leading/trailing whitespace should be trimmed before validation
	err := validateCollectionParts("  tenant1  ", "  store1  ")
	if err != nil {
		t.Errorf("should trim whitespace and accept: %v", err)
	}
}

func TestIngest_ValidationError(t *testing.T) {
	svc := NewService(
		testutil.NewMockEmbedder(4),
		testutil.NewMockStore(),
		testutil.NewMockExtractor(),
		DefaultServiceOptions(),
	)

	ctx := context.Background()
	_, err := svc.Ingest(ctx, IngestParams{
		TenantID: "../evil",
		StoreID:  "store1",
		Filename: "test.txt",
	})

	if err == nil {
		t.Error("Ingest should fail with invalid tenant_id")
	}
	if !strings.Contains(err.Error(), "tenant_id contains invalid characters") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRetrieve_ValidationError(t *testing.T) {
	svc := NewService(
		testutil.NewMockEmbedder(4),
		testutil.NewMockStore(),
		testutil.NewMockExtractor(),
		DefaultServiceOptions(),
	)

	ctx := context.Background()
	_, err := svc.Retrieve(ctx, RetrieveParams{
		TenantID: "tenant1",
		StoreID:  "../evil",
		Query:    "test",
	})

	if err == nil {
		t.Error("Retrieve should fail with invalid store_id")
	}
	if !strings.Contains(err.Error(), "store_id contains invalid characters") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateStore_ValidationError(t *testing.T) {
	svc := NewService(
		testutil.NewMockEmbedder(4),
		testutil.NewMockStore(),
		testutil.NewMockExtractor(),
		DefaultServiceOptions(),
	)

	ctx := context.Background()
	err := svc.CreateStore(ctx, "", "store1")

	if err == nil {
		t.Error("CreateStore should fail with empty tenant_id")
	}
	if !strings.Contains(err.Error(), "tenant_id is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeleteStore_ValidationError(t *testing.T) {
	svc := NewService(
		testutil.NewMockEmbedder(4),
		testutil.NewMockStore(),
		testutil.NewMockExtractor(),
		DefaultServiceOptions(),
	)

	ctx := context.Background()
	err := svc.DeleteStore(ctx, "tenant1", "")

	if err == nil {
		t.Error("DeleteStore should fail with empty store_id")
	}
	if !strings.Contains(err.Error(), "store_id is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStoreInfo_ValidationError(t *testing.T) {
	svc := NewService(
		testutil.NewMockEmbedder(4),
		testutil.NewMockStore(),
		testutil.NewMockExtractor(),
		DefaultServiceOptions(),
	)

	ctx := context.Background()
	_, err := svc.StoreInfo(ctx, "tenant/evil", "store1")

	if err == nil {
		t.Error("StoreInfo should fail with path traversal in tenant_id")
	}
	if !strings.Contains(err.Error(), "tenant_id contains invalid characters") {
		t.Errorf("unexpected error: %v", err)
	}
}
