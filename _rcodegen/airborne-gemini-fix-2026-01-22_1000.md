Date Created: 2026-01-22_1000

# Gemini Code Audit & Fix Report

## Executive Summary

This report identifies critical and major issues in the `airborne` codebase, specifically focusing on system reliability and security. The primary findings relate to database connection error handling, connection string parsing, and temporary file management.

Three specific fixes are proposed:
1.  **Enforce Fail-Fast for Database Connections**: Prevent the server from starting in a degraded state when database persistence is explicitly enabled but fails to connect.
2.  **Robust Database URL Parsing**: Replace fragile string manipulation with `net/url` parsing to safely handle PostgreSQL connection strings.
3.  **Secure Temporary File Creation**: Improve the security and concurrency safety of writing CA certificates to disk.

## Issues & Fixes

### 1. Silent Failure on Database Connection Error

**Severity:** Critical
**Location:** `internal/server/grpc.go`

**Description:**
When `cfg.Database.Enabled` is set to `true`, the application attempts to connect to the database. However, if the connection fails (e.g., wrong credentials, database down), the error is logged, and the server continues to start *without* a database connection. This leads to a running application that appears healthy but fails to persist data as expected by the configuration.

**Fix:**
Modify `NewGRPCServer` to return the error if database connection fails when `Database.Enabled` is true. This ensures the application fails fast, alerting the operator to the configuration or infrastructure issue immediately.

### 2. Fragile Database URL Construction

**Severity:** Major
**Location:** `internal/db/postgres.go`

**Description:**
The code currently uses string manipulation (`strings.Contains`, concatenation) to append SSL parameters to the database URL.
```go
if !strings.Contains(dbURL, "sslmode=") {
    if strings.Contains(dbURL, "?") {
        dbURL += "&sslmode=verify-full"
    } else {
        dbURL += "?sslmode=verify-full"
    }
}
```
This approach is brittle and can fail with complex connection strings (e.g., those already containing encoded characters or multiple parameters).

**Fix:**
Use the standard `net/url` package to parse the connection string, manipulate the query parameters safely, and reconstruct the URL.

### 3. Insecure/Conflicting Temporary File Path

**Severity:** Medium
**Location:** `internal/db/postgres.go`

**Description:**
The `writeCACertToFile` function writes the CA certificate to a hardcoded path `/tmp/airborne-certs/supabase-ca.crt`.
1.  **Race Condition:** If multiple instances of the application (e.g., during parallel testing or on a shared host) run simultaneously with different certificates, they will overwrite each other's file.
2.  **Security:** Using a fixed path in `/tmp` is generally discouraged due to potential pre-creation attacks (though `0700` permissions on the directory help).

**Fix:**
Use `os.CreateTemp` (or `os.MkdirTemp`) to create a unique file for each process instance. This avoids collisions and allows the OS to handle unique naming.

## Patch-Ready Diffs

### Fix 1: Enforce Fail-Fast for Database Connections

```go
<<<<
	// Initialize database if enabled
	var dbClient *db.Client
	if cfg.Database.Enabled {
		var dbErr error
		dbClient, dbErr = db.NewClient(context.Background(), db.Config{
			URL:            cfg.Database.URL,
			MaxConnections: cfg.Database.MaxConnections,
			LogQueries:     cfg.Database.LogQueries,
			CACert:         cfg.Database.CACert,
		})
		if dbErr != nil {
			slog.Error("failed to connect to database", "error", dbErr)
			// Continue without database - it's optional
		} else {
			slog.Info("database connection established for message persistence")
		}
	}
====
	// Initialize database if enabled
	var dbClient *db.Client
	if cfg.Database.Enabled {
		var dbErr error
		dbClient, dbErr = db.NewClient(context.Background(), db.Config{
			URL:            cfg.Database.URL,
			MaxConnections: cfg.Database.MaxConnections,
			LogQueries:     cfg.Database.LogQueries,
			CACert:         cfg.Database.CACert,
		})
		if dbErr != nil {
			return nil, nil, fmt.Errorf("failed to connect to database: %w", dbErr)
		}
		slog.Info("database connection established for message persistence")
	}
>>>>
```

### Fix 2 & 3: Robust URL Parsing and Secure Temp Files

```go
<<<<
package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Client wraps a PostgreSQL connection pool.
====
package db

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Client wraps a PostgreSQL connection pool.
>>>>
```

```go
<<<<
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
====
	// If CA certificate provided, write to temp file and add to connection string
	if cfg.CACert != "" {
		certPath, err := writeCACertToFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to write CA certificate: %w", err)
		}

		// Parse URL to safely append parameters
		u, err := url.Parse(dbURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse database URL: %w", err)
		}

		q := u.Query()
		if !q.Has("sslmode") {
			q.Set("sslmode", "verify-full")
		}
		if !q.Has("sslrootcert") {
			q.Set("sslrootcert", certPath)
		}
		u.RawQuery = q.Encode()
		dbURL = u.String()

		slog.Info("database SSL configured with custom CA certificate", "cert_path", certPath)
	}

	poolConfig, err := pgxpool.ParseConfig(dbURL)
>>>>
```

```go
<<<<
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
====
// writeCACertToFile writes a PEM-encoded CA certificate to a temporary file.
// Returns the path to the certificate file.
func writeCACertToFile(certPEM string) (string, error) {
	// Create a temp directory for the cert
	// We use a temp dir per process to avoid collisions
	certDir := os.TempDir()
	
	// Create a unique file
	f, err := os.CreateTemp(certDir, "airborne-supabase-ca-*.crt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for certificate: %w", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte(certPEM)); err != nil {
		return "", fmt.Errorf("failed to write certificate content: %w", err)
	}

	return f.Name(), nil
}
>>>>
```
