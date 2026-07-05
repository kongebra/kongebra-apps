package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/db"
	"saga-api/internal/module"
)

type echoModule struct{}

func (echoModule) Name() string      { return "test-echo" }
func (echoModule) InputKind() string { return "url" }
func (echoModule) Run(ctx context.Context, in json.RawMessage, d module.Deps, emit func(module.Event)) (string, error) {
	return "x", nil
}

func testServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *Bus) {
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
	module.Register(echoModule{})
	bus := NewBus()
	srv := httptest.NewServer(New(pool, bus, "test"))
	t.Cleanup(srv.Close)
	return srv, pool, bus
}

func postJob(t *testing.T, srv *httptest.Server, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/jobs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestCreateAndGetJob(t *testing.T) {
	srv, _, _ := testServer(t)
	resp := postJob(t, srv, `{"module":"test-echo","input":{"url":"u"}}`)
	if resp.StatusCode != 201 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	r2, _ := http.Get(srv.URL + "/api/jobs")
	var list struct {
		Jobs []map[string]any `json:"jobs"`
	}
	json.NewDecoder(r2.Body).Decode(&list)
	r2.Body.Close()
	if len(list.Jobs) != 1 {
		t.Fatalf("jobs: %+v", list.Jobs)
	}
}

func TestCreateJobUnknownModule(t *testing.T) {
	srv, _, _ := testServer(t)
	resp := postJob(t, srv, `{"module":"nope","input":{}}`)
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestGetJobNotFound(t *testing.T) {
	srv, _, _ := testServer(t)
	resp, err := http.Get(srv.URL + "/api/jobs/999999")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestEventsStreamSnapshotThenLive(t *testing.T) {
	srv, _, bus := testServer(t)
	resp := postJob(t, srv, `{"module":"test-echo","input":{"url":"u"}}`)
	var created struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/events?job=%d", srv.URL, created.ID), nil)
	es, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer es.Body.Close()
	if ct := es.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		bus.Publish(created.ID, module.Event{Stage: "done"})
	}()
	sc := bufio.NewScanner(es.Body)
	var data []string
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			data = append(data, strings.TrimPrefix(line, "data: "))
		}
		if len(data) == 2 {
			break
		}
	}
	if !strings.Contains(data[0], `"status"`) {
		t.Errorf("first event not a snapshot: %s", data[0])
	}
	if !strings.Contains(data[1], `"done"`) {
		t.Errorf("second event: %s", data[1])
	}
}
