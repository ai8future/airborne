// Package db provides PostgreSQL database connectivity for message persistence.
package db

import (
	"context"
	"fmt"
	"log/slog"
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
}

// NewClient creates a new PostgreSQL client with connection pool.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
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
