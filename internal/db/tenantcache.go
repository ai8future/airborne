package db

import (
	"context"
	"sync"
	"time"
)

// tenantCacheTTL controls how long a cached tenant-registry snapshot is
// considered fresh before IsValidTenant re-queries the database.
const tenantCacheTTL = 30 * time.Second

// tenantCache holds an in-memory snapshot of the active tenant IDs from the
// registry (airborne_tenants), so IsValidTenant doesn't need a database
// round trip on every call. The zero value is ready to use: a nil ids map
// and zero fetchedAt mean the first call always misses and refreshes.
//
// Once populated, the ids map is never mutated in place — a refresh builds a
// brand-new map and swaps the reference under the write lock. That lets
// snapshot readers use the map after releasing the read lock without racing
// a concurrent refresh.
type tenantCache struct {
	mu        sync.RWMutex
	ids       map[string]struct{}
	fetchedAt time.Time
}

// IsValidTenant reports whether id is currently a known, active tenant in
// the registry. Results are served from a cache that is refreshed at most
// once every 30s (tenantCacheTTL), avoiding a database hit on every request
// that needs tenant-ID validation.
func (c *Client) IsValidTenant(ctx context.Context, id string) (bool, error) {
	if ids, ok := c.tenants.snapshot(); ok {
		_, valid := ids[id]
		return valid, nil
	}

	ids, err := c.tenants.refresh(ctx, c.ListTenantIDs)
	if err != nil {
		return false, err
	}
	_, valid := ids[id]
	return valid, nil
}

// snapshot returns the cached ID set if it is still within tenantCacheTTL.
func (tc *tenantCache) snapshot() (map[string]struct{}, bool) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	if tc.ids != nil && time.Since(tc.fetchedAt) < tenantCacheTTL {
		return tc.ids, true
	}
	return nil, false
}

// refresh re-populates the cache by calling list. If another goroutine
// already refreshed the cache while this call was waiting for the write
// lock, the redundant query is skipped and the freshly cached set is
// returned instead.
func (tc *tenantCache) refresh(ctx context.Context, list func(context.Context) ([]string, error)) (map[string]struct{}, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.ids != nil && time.Since(tc.fetchedAt) < tenantCacheTTL {
		return tc.ids, nil
	}

	ids, err := list(ctx)
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	tc.ids = set
	tc.fetchedAt = time.Now()

	return tc.ids, nil
}
