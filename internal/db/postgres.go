// Package db provides PostgreSQL database connectivity for message persistence.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ai8future/chassis-go-addons/pgkit"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Client wraps a PostgreSQL connection pool.
type Client struct {
	pg          *pgkit.Pool
	pool        *pgxpool.Pool // cached reference for internal package access
	logQueries  bool
	tenantRepos map[string]*Repository
	mu          sync.RWMutex
	tenants     tenantCache // cached registry of active tenant IDs; see tenantcache.go
}

// Config holds database connection configuration.
type Config struct {
	URL            string
	MaxConnections int
	LogQueries     bool
	CACert         string // PEM-encoded CA certificate for SSL verification
}

// NewClient creates a new PostgreSQL client with connection pool.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	dbURL := cfg.URL

	// If CA certificate provided, write to temp file and add to connection string
	if cfg.CACert != "" {
		certPath, err := writeCACertToFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to write CA certificate: %w", err)
		}
		// Append sslmode and sslrootcert to URL if not already present
		if !strings.Contains(dbURL, "sslmode=") {
			if strings.Contains(dbURL, "?") {
				dbURL += "&sslmode=verify-full"
			} else {
				dbURL += "?sslmode=verify-full"
			}
		}
		if !strings.Contains(dbURL, "sslrootcert=") {
			dbURL += "&sslrootcert=" + certPath
		}
		slog.Info("database SSL configured with custom CA certificate", "cert_path", certPath)
	}

	opts := pgkit.Options{DSN: dbURL}
	if cfg.MaxConnections > 0 {
		if cfg.MaxConnections > math.MaxInt32 {
			return nil, fmt.Errorf("database max_connections exceeds supported maximum %d", math.MaxInt32)
		}
		opts.MaxConns = int32(cfg.MaxConnections)
	}

	pg, err := pgkit.Open(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	slog.Info("database connection established",
		"max_connections", opts.MaxConns,
	)

	return &Client{
		pg:          pg,
		pool:        pg.Pool(),
		logQueries:  cfg.LogQueries,
		tenantRepos: make(map[string]*Repository),
	}, nil
}

// Pool returns the underlying connection pool for direct access.
func (c *Client) Pool() *pgxpool.Pool {
	return c.pg.Pool()
}

// Close closes the database connection pool.
func (c *Client) Close() {
	c.pg.Close()
	slog.Info("database connection closed")
}

// Ping verifies the database connection is alive.
func (c *Client) Ping(ctx context.Context) error {
	return c.pg.Ping(ctx)
}

// TenantRepository returns a repository scoped to a specific tenant's tables.
// The repository is cached for efficiency and is thread-safe.
func (c *Client) TenantRepository(tenantID string) (*Repository, error) {
	// Fast path: check if already cached (read lock)
	c.mu.RLock()
	if repo, ok := c.tenantRepos[tenantID]; ok {
		c.mu.RUnlock()
		return repo, nil
	}
	c.mu.RUnlock()

	// Slow path: create new repository (write lock)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if repo, ok := c.tenantRepos[tenantID]; ok {
		return repo, nil
	}

	// Create new tenant repository
	repo, err := NewTenantRepository(c, tenantID)
	if err != nil {
		return nil, err
	}

	c.tenantRepos[tenantID] = repo
	slog.Debug("created tenant repository", "tenant_id", tenantID)
	return repo, nil
}

// logQuery logs a query if logging is enabled.
func (c *Client) logQuery(query string, args ...interface{}) {
	if c.logQueries {
		slog.Debug("executing query", "sql", query, "args", args)
	}
}

// writeCACertToFile writes a PEM-encoded CA certificate to a temporary file.
// Returns the path to the certificate file.
func writeCACertToFile(certPEM string) (string, error) {
	// Use a stable path so we don't create multiple files on restarts
	certDir := "/tmp/airborne-certs"
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create cert directory: %w", err)
	}

	certPath := filepath.Join(certDir, "supabase-ca.crt")

	// Write the certificate
	if err := os.WriteFile(certPath, []byte(certPEM), 0600); err != nil {
		return "", fmt.Errorf("failed to write certificate file: %w", err)
	}

	return certPath, nil
}
