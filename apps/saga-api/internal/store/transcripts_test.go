package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/db"
	"saga-api/internal/store"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
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
