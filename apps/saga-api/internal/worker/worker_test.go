package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/api"
	"saga-api/internal/db"
	"saga-api/internal/module"
	"saga-api/internal/queue"
)

type okModule struct{}

func (okModule) Name() string      { return "test-ok" }
func (okModule) InputKind() string { return "url" }
func (okModule) Run(ctx context.Context, in json.RawMessage, d module.Deps, emit func(module.Event)) (module.Result, error) {
	emit(module.Event{Stage: "working"})
	return module.Result{Markdown: "# result"}, nil
}

type failModule struct{}

func (failModule) Name() string      { return "test-fail" }
func (failModule) InputKind() string { return "url" }
func (failModule) Run(ctx context.Context, in json.RawMessage, d module.Deps, emit func(module.Event)) (module.Result, error) {
	return module.Result{}, errors.New("kaboom")
}

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

// claimOne claims the single queued local job for a test worker and returns it
// with its fence owner.
func claimOne(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*queue.Job, string) {
	t.Helper()
	owner := "test-worker"
	job, err := queue.Claim(ctx, pool, owner, []string{"local"})
	if err != nil || job == nil {
		t.Fatalf("claim: job=%+v err=%v", job, err)
	}
	return job, owner
}

func TestProcessCompletesJob(t *testing.T) {
	module.Register(okModule{})
	ctx := context.Background()
	pool := testPool(t)
	id, _ := queue.Enqueue(ctx, pool, "test-ok", []byte(`{}`), "local")
	bus := api.NewBus()
	ch, cancel := bus.Subscribe(id)
	defer cancel()

	job, owner := claimOne(t, ctx, pool)
	process(ctx, pool, module.Deps{ChunkTimeout: time.Minute}, bus, job, owner)
	got, _ := queue.Get(ctx, pool, id)
	if got.Status != "done" || *got.ResultMarkdown != "# result" {
		t.Fatalf("%+v", got)
	}
	// terminal event published
	var last module.Event
	for len(ch) > 0 {
		last = <-ch
	}
	if last.Stage != "done" {
		t.Errorf("last event: %+v", last)
	}
}

// TestProcessFencedOutDoesNotComplete proves the double-run guard at the worker
// level: a worker whose job was rescued and reclaimed by another owner must not
// write a done result nor publish a terminal "done".
func TestProcessFencedOutDoesNotComplete(t *testing.T) {
	module.Register(okModule{})
	ctx := context.Background()
	pool := testPool(t)
	id, _ := queue.Enqueue(ctx, pool, "test-ok", []byte(`{}`), "local")
	bus := api.NewBus()

	job, owner := claimOne(t, ctx, pool)
	// Simulate a stale rescue + reclaim by a different worker while this one runs.
	if _, err := pool.Exec(ctx,
		`UPDATE jobs SET status='running', lease_owner='other-worker' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	process(ctx, pool, module.Deps{ChunkTimeout: time.Minute}, bus, job, owner)
	got, _ := queue.Get(ctx, pool, id)
	if got.Status == "done" || got.ResultMarkdown != nil {
		t.Fatalf("fenced-out worker wrote a result: %+v", got)
	}
	if got.LeaseOwner == nil || *got.LeaseOwner != "other-worker" {
		t.Fatalf("fence owner clobbered: %+v", got)
	}
}

func TestProcessFailsJob(t *testing.T) {
	module.Register(failModule{})
	ctx := context.Background()
	pool := testPool(t)
	id, _ := queue.Enqueue(ctx, pool, "test-fail", []byte(`{}`), "local")
	bus := api.NewBus()
	job, owner := claimOne(t, ctx, pool)
	process(ctx, pool, module.Deps{}, bus, job, owner)
	got, _ := queue.Get(ctx, pool, id)
	// first failure requeues (MaxAttempts=3)
	if got.Status != "queued" || *got.Error != "kaboom" {
		t.Fatalf("%+v", got)
	}
}

// TestProcessOneEmitsRetryingThenFailed asserts the published event stage
// tracks whether the job actually parked as terminal, not merely whether
// mod.Run errored. Requeued attempts (attempts < MaxAttempts) must publish
// "retrying" so the web UI's SSE stream stays open; only the attempt that
// parks the job as "failed" in the DB may publish a terminal "failed".
func TestProcessEmitsRetryingThenFailed(t *testing.T) {
	module.Register(failModule{})
	ctx := context.Background()
	pool := testPool(t)
	id, _ := queue.Enqueue(ctx, pool, "test-fail", []byte(`{}`), "local")
	bus := api.NewBus()
	ch, cancel := bus.Subscribe(id)
	defer cancel()

	drainLast := func() module.Event {
		t.Helper()
		var last module.Event
		for len(ch) > 0 {
			last = <-ch
		}
		return last
	}

	for attempt := 1; attempt <= queue.MaxAttempts; attempt++ {
		job, owner := claimOne(t, ctx, pool)
		process(ctx, pool, module.Deps{}, bus, job, owner)

		ev := drainLast()
		after, _ := queue.Get(ctx, pool, id)

		if attempt < queue.MaxAttempts {
			if ev.Stage != "retrying" {
				t.Fatalf("attempt %d: got stage %q, want %q (job: %+v)", attempt, ev.Stage, "retrying", after)
			}
			if after.Status != "queued" {
				t.Fatalf("attempt %d: got status %q, want %q", attempt, after.Status, "queued")
			}
		} else {
			if ev.Stage != "failed" {
				t.Fatalf("attempt %d: got stage %q, want %q (job: %+v)", attempt, ev.Stage, "failed", after)
			}
			if after.Status != "failed" {
				t.Fatalf("attempt %d: got status %q, want %q", attempt, after.Status, "failed")
			}
		}
		if ev.Detail != "kaboom" {
			t.Fatalf("attempt %d: got detail %q, want %q", attempt, ev.Detail, "kaboom")
		}
	}
}

func TestClaimEmptyQueue(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	job, err := queue.Claim(ctx, pool, "test-worker", []string{"local"})
	if err != nil || job != nil {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}
