package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/db"
	"saga-api/internal/dbtest"
	"saga-api/internal/llm"
	"saga-api/internal/module"
	"saga-api/internal/store"
)

type echoModule struct{}

func (echoModule) Name() string      { return "test-echo" }
func (echoModule) InputKind() string { return "url" }
func (echoModule) Run(ctx context.Context, in json.RawMessage, d module.Deps, emit func(module.Event)) (module.Result, error) {
	return module.Result{Markdown: "x"}, nil
}

// testServer boots a server against a real Postgres with a placeholder llm
// client that no test in this file exercises. Tests that hit the translate
// endpoint use testServerWithLLM instead, pointed at a fake.
func testServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *Bus) {
	return testServerWithLLM(t, llm.New("http://unused"))
}

func testServerWithLLM(t *testing.T, llmClient *llm.Client) (*httptest.Server, *pgxpool.Pool, *Bus) {
	t.Helper()
	ctx := context.Background()
	// dbtest gives this package its own database so go test ./...'s default
	// parallel-package execution never races another package's TRUNCATE
	// against this package's assertions.
	pool := dbtest.Pool(t, "api")
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE jobs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	module.Register(echoModule{})
	bus := NewBus()
	srv := httptest.NewServer(New(pool, bus, llmClient, "test", "gemma4:e4b"))
	t.Cleanup(srv.Close)
	return srv, pool, bus
}

// fakeLLM answers every chat with a canned string, in ollama-native NDJSON
// /api/chat format (same pattern as internal/module/ytsummary's fakeLLM).
func fakeLLM(t *testing.T, reply string) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintf(w, "{\"message\":{\"content\":%q}}\n", reply)
		fmt.Fprint(w, "{\"done\":true}\n")
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

func TestGetModels(t *testing.T) {
	srv, _, _ := testServer(t)
	resp, err := http.Get(srv.URL + "/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var out struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Models) == 0 {
		t.Fatal("models array is empty")
	}
	found := false
	for _, m := range out.Models {
		if m["id"] == "qwen3.5:2b" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("qwen3.5:2b not found in models: %+v", out.Models)
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

// TestRerunJob seeds a done job that already has a recorded run (transcript +
// job_runs row), as a real job reaches after a worker completes it, then
// replays it on a different model. The new job's input must carry the same
// transcript_sha as the original run so the module skips the yt-dlp fetch -
// that is what makes an A/B between models fair (byte-identical input).
func TestRerunJob(t *testing.T) {
	srv, pool, _ := testServer(t)
	ctx := context.Background()

	transcriptText := "hello world, this is the original fetched transcript"
	sha := store.Sha256(transcriptText)
	if err := store.SaveTranscript(ctx, pool, store.Transcript{Sha256: sha, Text: transcriptText}); err != nil {
		t.Fatal(err)
	}

	var jobID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO jobs (module, input, status, result_markdown) VALUES ($1, $2, 'done', $3) RETURNING id`,
		"test-echo", []byte(`{"url":"https://youtu.be/abc123","lang":"en","model":"qwen3.5:2b"}`), "# Title\n\n- point",
	).Scan(&jobID); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRun(ctx, tx, store.Run{
		JobID:            jobID,
		TranscriptSha256: sha,
		Model:            "qwen3.5:2b",
		Tier:             "local",
		PromptVersion:    "v1",
		TargetLang:       "en",
		SummarizeLang:    "en",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(fmt.Sprintf("%s/api/jobs/%d/rerun", srv.URL, jobID),
		"application/json", strings.NewReader(`{"model":"kimi-k2.6:cloud"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201", resp.StatusCode)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	if created.ID == 0 || created.ID == jobID {
		t.Fatalf("expected a new job id, got %d (original %d)", created.ID, jobID)
	}

	var newInput []byte
	if err := pool.QueryRow(ctx, `SELECT input FROM jobs WHERE id = $1`, created.ID).Scan(&newInput); err != nil {
		t.Fatal(err)
	}
	var in struct {
		URL           string `json:"url"`
		Lang          string `json:"lang"`
		Model         string `json:"model"`
		TranscriptSha string `json:"transcript_sha"`
		RunGroup      string `json:"run_group"`
	}
	if err := json.Unmarshal(newInput, &in); err != nil {
		t.Fatal(err)
	}
	if in.URL != "https://youtu.be/abc123" {
		t.Errorf("url = %q, want original url carried over", in.URL)
	}
	if in.Model != "kimi-k2.6:cloud" {
		t.Errorf("model = %q, want the new model", in.Model)
	}
	if in.TranscriptSha != sha {
		t.Errorf("transcript_sha = %q, want %q (fetch would not be skipped)", in.TranscriptSha, sha)
	}
	if in.RunGroup != strconv.FormatInt(jobID, 10) {
		t.Errorf("run_group = %q, want %d", in.RunGroup, jobID)
	}

	// 404 on an unknown job.
	r404, err := http.Post(srv.URL+"/api/jobs/999999/rerun", "application/json", strings.NewReader(`{"model":"kimi-k2.6:cloud"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r404.Body.Close()
	if r404.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", r404.StatusCode)
	}
}
