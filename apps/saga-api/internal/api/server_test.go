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
	"saga-api/internal/llm"
	"saga-api/internal/module"
)

type echoModule struct{}

func (echoModule) Name() string      { return "test-echo" }
func (echoModule) InputKind() string { return "url" }
func (echoModule) Run(ctx context.Context, in json.RawMessage, d module.Deps, emit func(module.Event)) (string, error) {
	return "x", nil
}

// testServer boots a server against a real Postgres with a placeholder llm
// client that no test in this file exercises. Tests that hit the translate
// endpoint use testServerWithLLM instead, pointed at a fake.
func testServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *Bus) {
	return testServerWithLLM(t, llm.New("http://unused"))
}

func testServerWithLLM(t *testing.T, llmClient *llm.Client) (*httptest.Server, *pgxpool.Pool, *Bus) {
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
	srv := httptest.NewServer(New(pool, bus, llmClient, "test"))
	t.Cleanup(srv.Close)
	return srv, pool, bus
}

// fakeLLM answers every chat with a canned string (same pattern as
// internal/module/ytsummary's fakeLLM).
func fakeLLM(t *testing.T, reply string) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", reply)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return llm.New(srv.URL)
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

func TestTranslateJob(t *testing.T) {
	srv, pool, _ := testServerWithLLM(t, fakeLLM(t, "# Tittel\n\n- punkt"))
	ctx := context.Background()

	var doneID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO jobs (module, input, status, result_markdown) VALUES ($1, $2, 'done', $3) RETURNING id`,
		"test-echo", []byte(`{}`), "# Title\n\n- point").Scan(&doneID); err != nil {
		t.Fatal(err)
	}

	resp := postJob(t, srv, `{"module":"test-echo","input":{"url":"u"}}`)
	var created struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	// 409: job not done.
	r409, err := http.Post(fmt.Sprintf("%s/api/jobs/%d/translate", srv.URL, created.ID),
		"application/json", strings.NewReader(`{"lang":"no"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r409.Body.Close()
	if r409.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409", r409.StatusCode)
	}

	// 200: job done, translation returned and cached.
	r200, err := http.Post(fmt.Sprintf("%s/api/jobs/%d/translate", srv.URL, doneID),
		"application/json", strings.NewReader(`{"lang":"no"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r200.Body.Close()
	if r200.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", r200.StatusCode)
	}
	var out struct {
		TranslatedMarkdown string `json:"translated_markdown"`
	}
	json.NewDecoder(r200.Body).Decode(&out)
	if !strings.Contains(out.TranslatedMarkdown, "Tittel") {
		t.Errorf("translated_markdown = %q", out.TranslatedMarkdown)
	}

	g, err := http.Get(fmt.Sprintf("%s/api/jobs/%d", srv.URL, doneID))
	if err != nil {
		t.Fatal(err)
	}
	defer g.Body.Close()
	var job map[string]any
	json.NewDecoder(g.Body).Decode(&job)
	if job["translated_lang"] != "no" {
		t.Errorf("translated_lang = %v", job["translated_lang"])
	}
	md, _ := job["translated_markdown"].(string)
	if !strings.Contains(md, "Tittel") {
		t.Errorf("translated_markdown not persisted: %v", job["translated_markdown"])
	}
}
