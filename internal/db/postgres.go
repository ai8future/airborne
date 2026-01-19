// Package db provides PostgreSQL database connectivity for message persistence.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Client wraps a PostgreSQL connection pool.
type Client struct {
	pool       *pgxpool.Pool
	logQueries bool
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

	poolConfig, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Configure pool settings
	if cfg.MaxConnections > 0 {
		poolConfig.MaxConns = int32(cfg.MaxConnections)
	} else {
		poolConfig.MaxConns = 10 // Default
	}

	// Connection health check settings
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.HealthCheckPeriod = 1 * time.Minute

	// Create the pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connectivity
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	slog.Info("database connection established",
		"max_connections", poolConfig.MaxConns,
	)

	return &Client{
		pool:       pool,
		logQueries: cfg.LogQueries,
	}, nil
}

// Pool returns the underlying connection pool for direct access.
func (c *Client) Pool() *pgxpool.Pool {
	return c.pool
}

// Close closes the database connection pool.
func (c *Client) Close() {
	if c.pool != nil {
		c.pool.Close()
		slog.Info("database connection closed")
	}
}

// Ping verifies the database connection is alive.
func (c *Client) Ping(ctx context.Context) error {
	return c.pool.Ping(ctx)
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
