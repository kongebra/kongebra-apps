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

func TestProcessOneCompletesJob(t *testing.T) {
	module.Register(okModule{})
	ctx := context.Background()
	pool := testPool(t)
	id, _ := queue.Enqueue(ctx, pool, "test-ok", []byte(`{}`))
	bus := api.NewBus()
	ch, cancel := bus.Subscribe(id)
	defer cancel()

	worked, err := ProcessOne(ctx, pool, module.Deps{ChunkTimeout: time.Minute}, bus)
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	job, _ := queue.Get(ctx, pool, id)
	if job.Status != "done" || *job.ResultMarkdown != "# result" {
		t.Fatalf("%+v", job)
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

func TestProcessOneFailsJob(t *testing.T) {
	module.Register(failModule{})
	ctx := context.Background()
	pool := testPool(t)
	id, _ := queue.Enqueue(ctx, pool, "test-fail", []byte(`{}`))
	bus := api.NewBus()
	worked, err := ProcessOne(ctx, pool, module.Deps{}, bus)
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	job, _ := queue.Get(ctx, pool, id)
	// first failure requeues (MaxAttempts=3)
	if job.Status != "queued" || *job.Error != "kaboom" {
		t.Fatalf("%+v", job)
	}
}

// TestProcessOneEmitsRetryingThenFailed asserts the published event stage
// tracks whether the job actually parked as terminal, not merely whether
// mod.Run errored. Requeued attempts (attempts < MaxAttempts) must publish
// "retrying" so the web UI's SSE stream stays open; only the attempt that
// parks the job as "failed" in the DB may publish a terminal "failed".
func TestProcessOneEmitsRetryingThenFailed(t *testing.T) {
	module.Register(failModule{})
	ctx := context.Background()
	pool := testPool(t)
	id, _ := queue.Enqueue(ctx, pool, "test-fail", []byte(`{}`))
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
		worked, err := ProcessOne(ctx, pool, module.Deps{}, bus)
		if err != nil || !worked {
			t.Fatalf("attempt %d: worked=%v err=%v", attempt, worked, err)
		}

		ev := drainLast()
		job, _ := queue.Get(ctx, pool, id)

		if attempt < queue.MaxAttempts {
			if ev.Stage != "retrying" {
				t.Fatalf("attempt %d: got stage %q, want %q (job: %+v)", attempt, ev.Stage, "retrying", job)
			}
			if job.Status != "queued" {
				t.Fatalf("attempt %d: got status %q, want %q", attempt, job.Status, "queued")
			}
		} else {
			if ev.Stage != "failed" {
				t.Fatalf("attempt %d: got stage %q, want %q (job: %+v)", attempt, ev.Stage, "failed", job)
			}
			if job.Status != "failed" {
				t.Fatalf("attempt %d: got status %q, want %q", attempt, job.Status, "failed")
			}
		}
		if ev.Detail != "kaboom" {
			t.Fatalf("attempt %d: got detail %q, want %q", attempt, ev.Detail, "kaboom")
		}
	}
}

func TestProcessOneEmptyQueue(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	worked, err := ProcessOne(ctx, pool, module.Deps{}, api.NewBus())
	if err != nil || worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
}
