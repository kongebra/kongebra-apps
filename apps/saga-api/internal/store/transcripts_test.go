package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/db"
	"saga-api/internal/dbtest"
	"saga-api/internal/store"
)

// testPool gives this package its own database (see internal/dbtest) so
// go test ./...'s default parallel-package execution never races another
// package's TRUNCATE against this package's assertions. Shared by every
// _test.go file in package store_test.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.Pool(t, "store")
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestSaveAndGetTranscript(t *testing.T) {
	pool := testPool(t)
	tr := store.Transcript{Text: "hello world", Lang: "en", Source: "manual", Tokens: 2, Chars: 11}
	tr.Sha256 = store.Sha256(tr.Text)
	if err := store.SaveTranscript(context.Background(), pool, tr); err != nil {
		t.Fatal(err)
	}
	// idempotent second save must not error
	if err := store.SaveTranscript(context.Background(), pool, tr); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetTranscript(context.Background(), pool, tr.Sha256)
	if err != nil || got == nil || got.Text != "hello world" {
		t.Fatalf("got %+v err %v", got, err)
	}
}
