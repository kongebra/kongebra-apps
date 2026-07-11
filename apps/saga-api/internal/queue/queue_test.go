package queue

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/db"
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
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE jobs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestEnqueueClaim(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	id, err := Enqueue(ctx, pool, "yt-summary", []byte(`{"url":"u"}`))
	if err != nil {
		t.Fatal(err)
	}
	job, err := Claim(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.ID != id || job.Status != "running" || job.Attempts != 1 {
		t.Fatalf("claim: %+v", job)
	}
	// queue now empty
	if j2, _ := Claim(ctx, pool); j2 != nil {
		t.Fatalf("expected empty queue, got %+v", j2)
	}
}

func TestFailRequeuesUntilMaxAttempts(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	id, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`))
	for i := 1; i <= MaxAttempts; i++ {
		job, _ := Claim(ctx, pool)
		if job == nil {
			t.Fatalf("attempt %d: nothing to claim", i)
		}
		if err := Fail(ctx, pool, job.ID, "boom"); err != nil {
			t.Fatal(err)
		}
	}
	job, _ := Get(ctx, pool, id)
	if job.Status != "failed" || *job.Error != "boom" {
		t.Fatalf("after %d attempts: %+v", MaxAttempts, job)
	}
	if j, _ := Claim(ctx, pool); j != nil {
		t.Fatal("failed job must not be claimable")
	}
}

func TestRequeueStale(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	_, _ = Enqueue(ctx, pool, "yt-summary", []byte(`{}`))
	job, _ := Claim(ctx, pool)
	// simulate a dead worker: backdate the lease
	if _, err := pool.Exec(ctx,
		`UPDATE jobs SET lease_at = now() - interval '1 hour' WHERE id = $1`, job.ID); err != nil {
		t.Fatal(err)
	}
	n, err := RequeueStale(ctx, pool, 10*time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := Get(ctx, pool, job.ID)
	if got.Status != "queued" {
		t.Fatalf("status = %s, want queued", got.Status)
	}
}

func TestRequeueStaleTerminalSetsFinishedAt(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	_, _ = Enqueue(ctx, pool, "yt-summary", []byte(`{}`))
	job, _ := Claim(ctx, pool)
	// simulate a dead worker that already exhausted attempts, with a stale lease
	if _, err := pool.Exec(ctx,
		`UPDATE jobs SET attempts = $2, lease_at = now() - interval '1 hour' WHERE id = $1`,
		job.ID, MaxAttempts); err != nil {
		t.Fatal(err)
	}
	n, err := RequeueStale(ctx, pool, 10*time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := Get(ctx, pool, job.ID)
	if got.Status != "failed" {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	if got.FinishedAt == nil {
		t.Fatal("finished_at must be set on terminal stale-lease failure")
	}
}

func TestCompleteAndFailOnlyAffectRunning(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	id, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`))
	job, err := Claim(ctx, pool)
	if err != nil || job == nil {
		t.Fatalf("claim: job=%+v err=%v", job, err)
	}
	if err := Complete(ctx, pool, job.ID, "the result"); err != nil {
		t.Fatal(err)
	}
	got, _ := Get(ctx, pool, id)
	if got.Status != "done" || got.ResultMarkdown == nil || *got.ResultMarkdown != "the result" {
		t.Fatalf("after Complete on running job: %+v", got)
	}

	id2, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`))
	job2, err := Claim(ctx, pool)
	if err != nil || job2 == nil {
		t.Fatalf("claim: job=%+v err=%v", job2, err)
	}
	// Simulate the job having been rescued and reclaimed by another worker
	// (e.g. RequeueStale ran, then a different worker claimed it) while the
	// original slow worker is still holding a reference to it.
	if _, err := pool.Exec(ctx, `UPDATE jobs SET status = 'queued' WHERE id = $1`, job2.ID); err != nil {
		t.Fatal(err)
	}

	if err := Complete(ctx, pool, job2.ID, "should not land"); err != nil {
		t.Fatal(err)
	}
	got2, _ := Get(ctx, pool, id2)
	if got2.Status != "queued" || got2.ResultMarkdown != nil {
		t.Fatalf("Complete must be a no-op on a non-running job: %+v", got2)
	}

	if err := Fail(ctx, pool, job2.ID, "should not land either"); err != nil {
		t.Fatal(err)
	}
	got3, _ := Get(ctx, pool, id2)
	if got3.Status != "queued" || got3.Error != nil {
		t.Fatalf("Fail must be a no-op on a non-running job: %+v", got3)
	}
}

func TestRetryResetsFailedJob(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	id, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`))
	for i := 0; i < MaxAttempts; i++ {
		j, _ := Claim(ctx, pool)
		_ = Fail(ctx, pool, j.ID, "x")
	}
	ok, err := Retry(ctx, pool, id)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	job, _ := Get(ctx, pool, id)
	if job.Status != "queued" || job.Attempts != 0 {
		t.Fatalf("%+v", job)
	}
	// retry on a non-failed job reports false
	if ok, _ := Retry(ctx, pool, id); ok {
		t.Fatal("retry on queued job must be a no-op")
	}
}
