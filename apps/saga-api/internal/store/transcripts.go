// Package store owns the durable eval tables: content-addressed transcripts
// and per-run records (job_runs). Kept separate from queue (which owns the
// live job lifecycle) so the eval store can evolve on its own.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Transcript struct {
	Sha256 string
	Text   string
	Tokens int
	Chars  int
	Lang   string
	Source string
}

func Sha256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func SaveTranscript(ctx context.Context, pool *pgxpool.Pool, t Transcript) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO transcripts (sha256, text, tokens, chars, lang, source)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (sha256) DO NOTHING`,
		t.Sha256, t.Text, t.Tokens, t.Chars, t.Lang, t.Source)
	return err
}

func GetTranscript(ctx context.Context, pool *pgxpool.Pool, sha string) (*Transcript, error) {
	var t Transcript
	err := pool.QueryRow(ctx,
		`SELECT sha256, text, tokens, chars, lang, source FROM transcripts WHERE sha256 = $1`, sha).
		Scan(&t.Sha256, &t.Text, &t.Tokens, &t.Chars, &t.Lang, &t.Source)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &t, err
}
