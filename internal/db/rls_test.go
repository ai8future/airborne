package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestWithTenant_SetsGUC(t *testing.T) {
	c := newTestClient(t)
	var got string
	if err := c.WithTenant(context.Background(), "ai8", func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			"SELECT current_setting('airborne.tenant_id', true)").Scan(&got)
	}); err != nil {
		t.Fatal(err)
	}
	if got != "ai8" {
		t.Fatalf("got %q", got)
	}
}

// TestIsValidTenant_CachesAndRefreshes exercises the real registry-backed
// cache: seeded tenants validate, an unknown ID doesn't, a newly admin-inserted
// tenant is invisible while the cache is fresh, and becomes visible once the
// cache is forced stale and IsValidTenant refreshes from the database.
func TestIsValidTenant_CachesAndRefreshes(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	for _, tt := range []struct {
		id    string
		valid bool
	}{
		{"ai8", true},
		{"email4ai", true},
		{"zztest", true},
		{"nope", false},
	} {
		ok, err := c.IsValidTenant(ctx, tt.id)
		if err != nil {
			t.Fatalf("IsValidTenant(%q): %v", tt.id, err)
		}
		if ok != tt.valid {
			t.Errorf("IsValidTenant(%q) = %v, want %v", tt.id, ok, tt.valid)
		}
	}

	// Insert a new tenant via an admin-mode tx (RLS requires admin_mode for
	// writes to airborne_tenants).
	if err := c.WithAdmin(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO airborne_tenants (id, name) VALUES ('newco', 'New Co')")
		return err
	}); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	t.Cleanup(func() {
		_ = c.WithAdmin(context.Background(), func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(),
				"DELETE FROM airborne_tenants WHERE id = 'newco'")
			return err
		})
	})

	// Cache is still fresh (populated moments ago by the calls above), so the
	// new tenant should not be visible yet.
	ok, err := c.IsValidTenant(ctx, "newco")
	if err != nil {
		t.Fatalf("IsValidTenant(newco) pre-expiry: %v", err)
	}
	if ok {
		t.Fatalf("IsValidTenant(newco) = true before cache expiry, want false (stale cache expected)")
	}

	// White-box: force the cached snapshot to be considered stale, the same
	// way the passage of tenantCacheTTL would, and confirm a refresh occurs.
	c.tenants.mu.Lock()
	c.tenants.fetchedAt = time.Now().Add(-tenantCacheTTL - time.Second)
	c.tenants.mu.Unlock()

	ok, err = c.IsValidTenant(ctx, "newco")
	if err != nil {
		t.Fatalf("IsValidTenant(newco) post-expiry: %v", err)
	}
	if !ok {
		t.Fatalf("IsValidTenant(newco) = false after cache refresh, want true")
	}
}
