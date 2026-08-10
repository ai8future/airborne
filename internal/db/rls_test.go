package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// TestSuspendedTenant_InvalidatesValidation proves a tenant flipped to
// status='suspended' stops validating: TenantExists (direct, active-only query)
// reports false immediately, and IsValidTenant on a fresh client (empty cache,
// so it reads ListTenantIDs which is active-only) also reports false. The
// tenants table is NOT truncated between tests, so zztest is restored to active
// via t.Cleanup or later tests that assume it is active would break.
func TestSuspendedTenant_InvalidatesValidation(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	setStatus := func(status string) {
		if err := c.WithAdmin(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE airborne_tenants SET status = $1 WHERE id = 'zztest'", status)
			return err
		}); err != nil {
			t.Fatalf("set zztest status=%s: %v", status, err)
		}
	}
	setStatus("suspended")
	t.Cleanup(func() { setStatus("active") })

	exists, err := c.TenantExists(ctx, "zztest")
	if err != nil {
		t.Fatalf("TenantExists(zztest): %v", err)
	}
	if exists {
		t.Error("TenantExists(zztest) = true after suspension, want false")
	}

	// A fresh client starts with an empty cache, so IsValidTenant reads the
	// registry (active-only) rather than a stale pre-suspension snapshot.
	fresh := newTestClient(t)
	valid, err := fresh.IsValidTenant(ctx, "zztest")
	if err != nil {
		t.Fatalf("IsValidTenant(zztest) on fresh client: %v", err)
	}
	if valid {
		t.Error("IsValidTenant(zztest) = true after suspension, want false (active-only registry)")
	}
}

// TestWithGUCs_PanicReleasesConn proves withGUCs's unconditional deferred
// rollback releases the pooled connection even when fn panics. The pool is
// capped small (pgkit floors MinConns at 2, so 1 is rejected) and MORE panics
// than the pool holds are run: had any panicking transaction leaked its conn,
// the pool would be exhausted and the follow-up transaction below would block
// until its context deadline instead of succeeding.
func TestWithGUCs_PanicReleasesConn(t *testing.T) {
	if appDSN == "" {
		t.Skip("no Postgres test container (Docker unavailable)")
	}
	ctx := context.Background()

	const poolMax = 2
	c, err := NewClient(ctx, Config{URL: appDSN, MaxConnections: poolMax})
	if err != nil {
		t.Fatalf("connect capped pool: %v", err)
	}
	t.Cleanup(func() {
		truncateAll(t)
		c.Close()
	})

	// Run more panicking transactions than the pool can hold. Each fn panics
	// inside the tx; recover so the test process survives. The panic must
	// propagate out of WithTenant (withGUCs does not swallow it), and the
	// deferred rollback must return the conn — otherwise iteration poolMax+1
	// finds an exhausted pool.
	for i := 0; i < poolMax+2; i++ {
		func() {
			callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("iteration %d: expected WithTenant to propagate the panic from fn", i)
				}
			}()
			_ = c.WithTenant(callCtx, "ai8", func(tx pgx.Tx) error {
				panic("boom in tx")
			})
		}()
	}

	// The pool must still be usable. A short deadline turns a leaked connection
	// into a fast, deterministic failure rather than a hang.
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var got string
	if err := c.WithTenant(waitCtx, "ai8", func(tx pgx.Tx) error {
		return tx.QueryRow(waitCtx,
			"SELECT current_setting('airborne.tenant_id', true)").Scan(&got)
	}); err != nil {
		t.Fatalf("WithTenant after panics failed (connection likely leaked): %v", err)
	}
	if got != "ai8" {
		t.Fatalf("post-panic tenant GUC = %q, want ai8", got)
	}
}

// TestRLS_TenantIsolation proves the per-tenant SELECT policy on
// airborne_chats: a row inserted under the ai8 GUC is invisible to a
// different tenant's ordinary (non-cross-tenant) SELECT.
func TestRLS_TenantIsolation(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	chatID := uuid.New()
	if err := c.WithTenant(ctx, "ai8", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO airborne_chats (id, tenant_id, user_id, title) VALUES ($1, $2, $3, $4)",
			chatID, "ai8", "user-1", "ai8 chat")
		return err
	}); err != nil {
		t.Fatalf("insert as ai8: %v", err)
	}

	var count int
	if err := c.WithTenant(ctx, "email4ai", func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT count(*) FROM airborne_chats WHERE id = $1", chatID).Scan(&count)
	}); err != nil {
		t.Fatalf("select as email4ai: %v", err)
	}
	if count != 0 {
		t.Fatalf("email4ai SELECT saw %d row(s) for ai8's chat %s, want 0 (RLS isolation broken)", count, chatID)
	}
}

// TestRLS_CrossTenantRead proves the cross_tenant_mode escape hatch: a row
// inserted under the ai8 GUC IS visible (with its true tenant_id intact) to
// a WithCrossTenant transaction, regardless of tenant.
func TestRLS_CrossTenantRead(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	chatID := uuid.New()
	if err := c.WithTenant(ctx, "ai8", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO airborne_chats (id, tenant_id, user_id, title) VALUES ($1, $2, $3, $4)",
			chatID, "ai8", "user-1", "ai8 chat")
		return err
	}); err != nil {
		t.Fatalf("insert as ai8: %v", err)
	}

	var gotTenant string
	if err := c.WithCrossTenant(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT tenant_id FROM airborne_chats WHERE id = $1", chatID).Scan(&gotTenant)
	}); err != nil {
		t.Fatalf("select under WithCrossTenant: %v", err)
	}
	if gotTenant != "ai8" {
		t.Fatalf("cross-tenant read got tenant_id %q, want %q", gotTenant, "ai8")
	}
}

// TestRLS_InsertWrongTenantRejected proves the INSERT policy's WITH CHECK:
// under the ai8 GUC, an INSERT that stamps tenant_id='email4ai' must be
// rejected by RLS (SQLSTATE 42501, "row-level security"), not silently
// accepted or rejected for some unrelated reason (e.g. a FK/type error).
func TestRLS_InsertWrongTenantRejected(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	err := c.WithTenant(ctx, "ai8", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO airborne_chats (id, tenant_id, user_id, title) VALUES ($1, $2, $3, $4)",
			uuid.New(), "email4ai", "user-1", "wrong tenant chat")
		return err
	})
	if err == nil {
		t.Fatal("insert with tenant_id='email4ai' under ai8 GUC succeeded, want RLS WITH CHECK rejection")
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected a *pgconn.PgError (RLS violation), got %T: %v", err, err)
	}
	if pgErr.Code != "42501" {
		t.Fatalf("expected SQLSTATE 42501 (insufficient_privilege / RLS violation), got %q (message: %q)", pgErr.Code, pgErr.Message)
	}
	if !strings.Contains(pgErr.Message, "row-level security") {
		t.Fatalf("expected error message to mention row-level security, got: %q", pgErr.Message)
	}
}

// TestRLS_TenantIDImmutable proves the airborne_forbid_tenant_move trigger:
// an UPDATE that changes tenant_id on an existing row is rejected with the
// trigger's specific exception message, even though the actor is the row's
// own tenant (i.e. this isn't just the RLS UPDATE policy blocking access).
func TestRLS_TenantIDImmutable(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	chatID := uuid.New()
	if err := c.WithTenant(ctx, "ai8", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO airborne_chats (id, tenant_id, user_id, title) VALUES ($1, $2, $3, $4)",
			chatID, "ai8", "user-1", "immutable test chat")
		return err
	}); err != nil {
		t.Fatalf("insert as ai8: %v", err)
	}

	err := c.WithTenant(ctx, "ai8", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"UPDATE airborne_chats SET tenant_id = $1 WHERE id = $2", "email4ai", chatID)
		return err
	})
	if err == nil {
		t.Fatal("UPDATE changing tenant_id from ai8 to email4ai succeeded, want immutability trigger error")
	}
	if !strings.Contains(err.Error(), "tenant_id is immutable") {
		t.Fatalf("expected error containing %q, got: %v", "tenant_id is immutable", err)
	}
}
