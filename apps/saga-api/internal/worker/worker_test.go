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
func (okModule) Run(ctx context.Context, in json.RawMessage, d module.Deps, emit func(module.Event)) (string, error) {
	emit(module.Event{Stage: "working"})
	return "# result", nil
}

type failModule struct{}

func (failModule) Name() string      { return "test-fail" }
func (failModule) InputKind() string { return "url" }
func (failModule) Run(ctx context.Context, in json.RawMessage, d module.Deps, emit func(module.Event)) (string, error) {
	return "", errors.New("kaboom")
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
	if _, err := pool.Exec(ctx, `TRUNCATE jobs RESTART IDENTITY`); err != nil {
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

func TestProcessOneEmptyQueue(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	worked, err := ProcessOne(ctx, pool, module.Deps{}, api.NewBus())
	if err != nil || worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
}
