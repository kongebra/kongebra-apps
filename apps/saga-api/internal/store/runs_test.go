package store_test

import (
	"context"
	"testing"

	"saga-api/internal/store"
)

func TestInsertRun(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `TRUNCATE job_runs RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}

	var jobID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO jobs (module, input) VALUES ('yt-summary', '{}') RETURNING id`,
	).Scan(&jobID)
	if err != nil {
		t.Fatal(err)
	}

	r := store.Run{
		JobID:            jobID,
		Model:            "llama3.1:8b",
		Tier:             "cloud",
		PromptVersion:    "v1",
		TargetLang:       "en",
		SummarizeLang:    "en",
		OutputTokens:     123,
		SummarizeCostUSD: 0.0042,
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRun(ctx, tx, r); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job_runs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 job_runs row, got %d", n)
	}

	var model string
	var outputTokens int
	var summarizeCostUSD float64
	err = pool.QueryRow(ctx,
		`SELECT model, output_tokens, summarize_cost_usd FROM job_runs WHERE job_id = $1`, jobID,
	).Scan(&model, &outputTokens, &summarizeCostUSD)
	if err != nil {
		t.Fatal(err)
	}
	if model != r.Model || outputTokens != r.OutputTokens || summarizeCostUSD != r.SummarizeCostUSD {
		t.Fatalf("round-trip mismatch: got model=%q output_tokens=%d summarize_cost_usd=%v",
			model, outputTokens, summarizeCostUSD)
	}
}
