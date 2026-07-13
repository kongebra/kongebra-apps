// Package dbtest gives each test package its own Postgres database.
//
// go test ./... builds one test binary per package and, by default, runs
// those binaries in parallel (GOMAXPROCS-many at once). If two packages'
// tests point at the same database and TRUNCATE the same tables, one
// package's TRUNCATE races another package's assertions - a real, observed
// flake (TestCompleteIsFencedByOwner failing intermittently under the
// default parallel `go test ./...`, always passing under `-p 1`). Giving
// every package its own database removes the shared state instead of trying
// to serialize around it.
package dbtest

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Note: this package deliberately does NOT import saga-api/internal/db.
// internal/db's own test file is package db (an internal test file, not
// db_test), so it can call db.Migrate directly without an import - but if
// dbtest imported db, db's test file importing dbtest would be an import
// cycle ("db" -> "dbtest" -> "db"). Every caller migrates its own pool via
// db.Migrate after calling Pool.

// duplicateDatabase is Postgres error code 42P04, raised by CREATE DATABASE
// when the database already exists.
const duplicateDatabase = "42P04"

// Pool returns a pgxpool.Pool connected to a database dedicated to the
// calling test package (created if it does not exist yet), deriving the
// database name from TEST_DATABASE_URL's own dbname plus suffix (e.g. base
// "saga_test" + suffix "queue" -> "saga_test_queue"). It skips the test if
// TEST_DATABASE_URL is not set. Callers must run their own db.Migrate on the
// returned pool.
//
// suffix is a required, explicit argument (not derived via reflection/PC
// tricks) so ownership is obvious at the call site and collisions are a
// visible code review problem, not a runtime one. Every DB-touching test
// package must pass its own unique suffix.
func Pool(t *testing.T, suffix string) *pgxpool.Pool {
	t.Helper()

	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()

	cfg, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		t.Fatalf("dbtest: parse TEST_DATABASE_URL: %v", err)
	}
	dbName := cfg.ConnConfig.Database + "_" + suffix

	ensureDatabase(ctx, t, cfg.ConnConfig, dbName)

	cfg.ConnConfig.Database = dbName
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("dbtest: connect to %s: %v", dbName, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// ensureDatabase creates dbName on the server described by connCfg if it
// does not already exist. CREATE DATABASE cannot run inside a transaction
// and has no IF NOT EXISTS clause, so this does a check-then-create and
// tolerates the duplicate_database race from another package's test binary
// creating the same database concurrently.
func ensureDatabase(ctx context.Context, t *testing.T, connCfg *pgx.ConnConfig, dbName string) {
	t.Helper()

	maintCfg := connCfg.Copy()
	maintCfg.Database = "postgres"
	conn, err := pgx.ConnectConfig(ctx, maintCfg)
	if err != nil {
		t.Fatalf("dbtest: connect to maintenance db: %v", err)
	}
	defer conn.Close(ctx)

	var exists bool
	err = conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, dbName,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("dbtest: check database %s exists: %v", dbName, err)
	}
	if exists {
		return
	}

	// dbName is the fixed TEST_DATABASE_URL dbname plus a package-supplied
	// constant suffix - never user input - so building the identifier
	// directly (quoted via pgx.Identifier) is safe.
	ident := pgx.Identifier{dbName}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == duplicateDatabase {
			return // another package's test binary created it first
		}
		t.Fatalf("dbtest: create database %s: %v", dbName, err)
	}
}
