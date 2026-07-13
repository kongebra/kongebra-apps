package queue

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/db"
	"saga-api/internal/dbtest"
)

// testPool gives this package its own database (see internal/dbtest) so
// go test ./...'s default parallel-package execution never races another
// package's TRUNCATE against this package's assertions, then truncates
// jobs so each test in this package still starts from a clean slate.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.Pool(t, "queue")
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
	id, err := Enqueue(ctx, pool, "yt-summary", []byte(`{"url":"u"}`), "local")
	if err != nil {
		t.Fatal(err)
	}
	job, err := Claim(ctx, pool, "w1", []string{"local"})
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.ID != id || job.Status != "running" || job.Attempts != 1 {
		t.Fatalf("claim: %+v", job)
	}
	if job.Tier != "local" || job.LeaseOwner == nil || *job.LeaseOwner != "w1" {
		t.Fatalf("claim tier/owner: %+v", job)
	}
	// queue now empty
	if j2, _ := Claim(ctx, pool, "w1", []string{"local"}); j2 != nil {
		t.Fatalf("expected empty queue, got %+v", j2)
	}
}

func TestClaimIgnoresOtherTiers(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	id, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`), "cloud")
	// a local claimer must not pick up a cloud job
	if j, _ := Claim(ctx, pool, "w1", []string{"local"}); j != nil {
		t.Fatalf("local claimer took a cloud job: %+v", j)
	}
	// a cloud claimer picks it up
	j, _ := Claim(ctx, pool, "w1", []string{"cloud"})
	if j == nil || j.ID != id || j.Tier != "cloud" {
		t.Fatalf("cloud claim: %+v", j)
	}
}

// TestCompleteIsFencedByOwner is the load-bearing double-run guard: after a
// stale rescue re-claims a job under a new owner, the original owner's write
// must be fenced out (0 rows), while the new owner completes normally.
func TestCompleteIsFencedByOwner(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	id, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`), "local")
	a, _ := Claim(ctx, pool, "owner-A", []string{"local"})
	if a == nil || a.ID != id {
		t.Fatalf("claim A: %+v", a)
	}
	// Make the lease look stale, then rescue via the real production path
	// (RequeueStale), not a hand-rolled approximation of it.
	if _, err := pool.Exec(ctx,
		`UPDATE jobs SET lease_at = now() - interval '1 hour' WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	n, err := RequeueStale(ctx, pool, 10*time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("RequeueStale: n=%d err=%v", n, err)
	}
	b, _ := Claim(ctx, pool, "owner-B", []string{"local"})
	if b == nil || b.ID != a.ID {
		t.Fatal("expected re-claim of same job")
	}
	okA := completeOwnedTx(t, ctx, pool, a.ID, "A-md", "owner-A")
	if okA {
		t.Fatal("owner-A must be fenced out after rescue")
	}
	okB := completeOwnedTx(t, ctx, pool, b.ID, "B-md", "owner-B")
	if !okB {
		t.Fatal("owner-B should complete")
	}
}

// completeOwnedTx is the test-only wrapper that runs CompleteOwnedTx inside
// its own transaction (as the worker does in completeWithRun), since
// production code never calls it outside a caller-managed transaction.
func completeOwnedTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64, markdown, owner string) bool {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	ok, err := CompleteOwnedTx(ctx, tx, id, markdown, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestFailRequeuesUntilMaxAttempts(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	id, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`), "local")
	for i := 1; i <= MaxAttempts; i++ {
		job, _ := Claim(ctx, pool, "w1", []string{"local"})
		if job == nil {
			t.Fatalf("attempt %d: nothing to claim", i)
		}
		if err := Fail(ctx, pool, job.ID, "boom", "w1"); err != nil {
			t.Fatal(err)
		}
	}
	job, _ := Get(ctx, pool, id)
	if job.Status != "failed" || *job.Error != "boom" {
		t.Fatalf("after %d attempts: %+v", MaxAttempts, job)
	}
	if j, _ := Claim(ctx, pool, "w1", []string{"local"}); j != nil {
		t.Fatal("failed job must not be claimable")
	}
}

func TestRequeueStale(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	_, _ = Enqueue(ctx, pool, "yt-summary", []byte(`{}`), "local")
	job, _ := Claim(ctx, pool, "w1", []string{"local"})
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
	_, _ = Enqueue(ctx, pool, "yt-summary", []byte(`{}`), "local")
	job, _ := Claim(ctx, pool, "w1", []string{"local"})
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

	id, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`), "local")
	job, err := Claim(ctx, pool, "w1", []string{"local"})
	if err != nil || job == nil {
		t.Fatalf("claim: job=%+v err=%v", job, err)
	}
	ok := completeOwnedTx(t, ctx, pool, job.ID, "the result", "w1")
	if !ok {
		t.Fatal("CompleteOwnedTx must affect the running job")
	}
	got, _ := Get(ctx, pool, id)
	if got.Status != "done" || got.ResultMarkdown == nil || *got.ResultMarkdown != "the result" {
		t.Fatalf("after Complete on running job: %+v", got)
	}

	id2, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`), "local")
	job2, err := Claim(ctx, pool, "w1", []string{"local"})
	if err != nil || job2 == nil {
		t.Fatalf("claim: job=%+v err=%v", job2, err)
	}
	// Simulate the job having been rescued and reclaimed by another worker
	// (e.g. RequeueStale ran, then a different worker claimed it) while the
	// original slow worker is still holding a reference to it.
	if _, err := pool.Exec(ctx, `UPDATE jobs SET status = 'queued' WHERE id = $1`, job2.ID); err != nil {
		t.Fatal(err)
	}

	okC := completeOwnedTx(t, ctx, pool, job2.ID, "should not land", "w1")
	if okC {
		t.Fatal("CompleteOwnedTx must be a no-op on a non-running job")
	}
	got2, _ := Get(ctx, pool, id2)
	if got2.Status != "queued" || got2.ResultMarkdown != nil {
		t.Fatalf("Complete must be a no-op on a non-running job: %+v", got2)
	}

	if err := Fail(ctx, pool, job2.ID, "should not land either", "w1"); err != nil {
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
	id, _ := Enqueue(ctx, pool, "yt-summary", []byte(`{}`), "local")
	for i := 0; i < MaxAttempts; i++ {
		j, _ := Claim(ctx, pool, "w1", []string{"local"})
		_ = Fail(ctx, pool, j.ID, "x", "w1")
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
