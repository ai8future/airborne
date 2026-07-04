package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// appDSN connects as the non-privileged airborne_app role (RLS ENFORCED).
// ownerDSN connects as the container owner/superuser (RLS BYPASSED) — setup/cleanup only.
var appDSN, ownerDSN string

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Disambiguate "Docker unavailable" (skip) from "our own setup is broken"
	// (fail loud). Without this check, a missing/renamed migration file would
	// make tcpostgres.Run fail the same way a missing Docker daemon would,
	// silently masking a broken migration behind an exit-0 skip.
	if _, err := os.Stat("../../migrations/001_baseline.sql"); err != nil {
		fmt.Fprintf(os.Stderr, "db test setup failed (migration file): %v\n", err)
		os.Exit(1)
	}

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("airborne"),
		tcpostgres.WithUsername("owner"), tcpostgres.WithPassword("owner"),
		tcpostgres.WithInitScripts("../../migrations/001_baseline.sql"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		// Docker unavailable: skip the package's DB tests (do not fail).
		fmt.Fprintf(os.Stderr, "skipping db tests, docker unavailable: %v\n", err)
		os.Exit(0)
	}
	ownerDSN, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		// Docker present but setup broke: FAIL loudly, never mask with exit 0.
		fmt.Fprintf(os.Stderr, "db test setup failed (dsn): %v\n", err)
		os.Exit(1)
	}
	// RLS only enforces against a non-superuser, non-owner role. Create it and
	// grant DML; tests connect as airborne_app so FORCE RLS actually applies.
	if err := createAppRole(ctx, ownerDSN); err != nil {
		fmt.Fprintf(os.Stderr, "db test setup failed (role): %v\n", err)
		os.Exit(1)
	}
	appDSN = strings.Replace(ownerDSN, "owner:owner@", "airborne_app:app@", 1)

	code := m.Run()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

func createAppRole(ctx context.Context, ownerDSN string) error {
	pool, err := pgxpool.New(ctx, ownerDSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE ROLE airborne_app LOGIN PASSWORD 'app' NOSUPERUSER NOBYPASSRLS;
		GRANT USAGE ON SCHEMA public TO airborne_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO airborne_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO airborne_app;`)
	return err
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	if appDSN == "" {
		t.Skip("no Postgres test container (Docker unavailable)")
	}
	// Connect as airborne_app (NOT the owner) so RLS is enforced.
	c, err := NewClient(context.Background(), Config{URL: appDSN})
	if err != nil {
		t.Fatalf("connect test db as airborne_app: %v", err)
	}
	t.Cleanup(func() { truncateAll(t) })
	return c
}

// truncateAll cleans up as the owner (airborne_app lacks TRUNCATE, and RLS would
// otherwise scope a DELETE to a single tenant).
func truncateAll(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), ownerDSN)
	if err != nil {
		t.Logf("cleanup connect: %v", err)
		return
	}
	defer pool.Close()
	_, err = pool.Exec(context.Background(),
		`TRUNCATE airborne_chat_files, airborne_chat_message_debug, airborne_chat_messages,
		          airborne_chat_vector_stores, airborne_files, airborne_chats RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Logf("truncate: %v", err)
	}
}
