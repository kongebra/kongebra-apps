package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/dbtest"
)

// testPool gives this package its own database (see internal/dbtest) so
// go test ./...'s default parallel-package execution never races another
// package's TRUNCATE against this package's assertions.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.Pool(t, "db")
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// running twice must be a no-op, not an error
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs`).Scan(&n); err != nil {
		t.Fatalf("jobs table missing: %v", err)
	}
}
