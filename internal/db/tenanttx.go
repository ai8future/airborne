package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// WithTenant runs fn inside a transaction with the airborne.tenant_id GUC set
// to tenantID (transaction-local), so RLS policies scope all queries in fn to
// that tenant.
func (c *Client) WithTenant(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
	return c.withGUCs(ctx, map[string]string{"airborne.tenant_id": tenantID}, fn)
}

// WithCrossTenant runs fn inside a transaction with airborne.cross_tenant_mode
// set to "true" (transaction-local), allowing RLS-scoped SELECTs to read
// across all tenants.
func (c *Client) WithCrossTenant(ctx context.Context, fn func(pgx.Tx) error) error {
	return c.withGUCs(ctx, map[string]string{"airborne.cross_tenant_mode": "true"}, fn)
}

// WithAdmin runs fn inside a transaction with airborne.admin_mode set to
// "true" (transaction-local), allowing writes to admin-only tables such as
// airborne_tenants.
func (c *Client) WithAdmin(ctx context.Context, fn func(pgx.Tx) error) error {
	return c.withGUCs(ctx, map[string]string{"airborne.admin_mode": "true"}, fn)
}

// withGUCs begins a transaction, sets the given GUCs as transaction-local
// (set_config's third argument is true), invokes fn, and commits on success
// or rolls back on any error.
func (c *Client) withGUCs(ctx context.Context, gucs map[string]string, fn func(pgx.Tx) error) (err error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	for name, val := range gucs {
		if _, err = tx.Exec(ctx, "SELECT set_config($1,$2,true)", name, val); err != nil {
			return fmt.Errorf("set guc %s: %w", name, err)
		}
	}

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// TenantExists reports whether id is a known, active tenant in the registry.
// The RLS policy on airborne_tenants allows open SELECT (USING (true)), so
// this works against the plain pool without any tenant GUC — validation must
// be possible before a tenant GUC exists.
func (c *Client) TenantExists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := c.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM airborne_tenants WHERE id=$1 AND status='active')", id).Scan(&ok)
	return ok, err
}

// ListTenantIDs returns the IDs of all active tenants in the registry, in
// ascending order.
func (c *Client) ListTenantIDs(ctx context.Context) ([]string, error) {
	rows, err := c.pool.Query(ctx, "SELECT id FROM airborne_tenants WHERE status='active' ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
