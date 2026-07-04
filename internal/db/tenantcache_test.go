package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestTenantCache_StaleFallback proves availability-over-freshness: a failed
// registry refresh serves the last good snapshot instead of the error; the
// error only propagates when there has never been a successful fetch; and a
// failed refresh does not extend fetchedAt, so the next call retries the
// registry and picks up a recovered result.
func TestTenantCache_StaleFallback(t *testing.T) {
	ctx := context.Background()
	dbDown := errors.New("db down")
	failing := func(context.Context) ([]string, error) { return nil, dbDown }

	t.Run("no prior snapshot propagates the error", func(t *testing.T) {
		tc := &tenantCache{}
		if _, err := tc.refresh(ctx, failing); !errors.Is(err, dbDown) {
			t.Fatalf("refresh error = %v, want %v", err, dbDown)
		}
	})

	t.Run("stale snapshot served on refresh failure", func(t *testing.T) {
		tc := &tenantCache{}
		ids, err := tc.refresh(ctx, func(context.Context) ([]string, error) { return []string{"ai8"}, nil })
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ids["ai8"]; !ok {
			t.Fatal("primed cache missing ai8")
		}

		// Expire the snapshot, then break the registry: the stale set must be
		// served without error.
		tc.mu.Lock()
		tc.fetchedAt = time.Now().Add(-2 * tenantCacheTTL)
		tc.mu.Unlock()

		ids, err = tc.refresh(ctx, failing)
		if err != nil {
			t.Fatalf("refresh with a prior snapshot should serve stale, got error %v", err)
		}
		if _, ok := ids["ai8"]; !ok {
			t.Error("stale snapshot missing ai8")
		}

		// Recovery: the failed refresh must not have extended fetchedAt, so
		// this call re-queries and replaces the snapshot.
		ids, err = tc.refresh(ctx, func(context.Context) ([]string, error) { return []string{"acme"}, nil })
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ids["acme"]; !ok {
			t.Error("recovered refresh missing acme")
		}
		if _, ok := ids["ai8"]; ok {
			t.Error("recovered refresh still serves ai8, want replaced snapshot")
		}
	})
}
