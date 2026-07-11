# SAGA Backend Phase 1 (headless) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make SAGA route jobs to a chosen local/cloud Ollama model, capture per-run metrics + cost, store a fingerprinted transcript and a durable `job_runs` record, and safely run several jobs concurrently - all verifiable by curl with zero UI.

**Architecture:** The `Provider` seam gains options-in / metrics-out. The LLM client switches to ollama-native `/api/chat` so token metrics are captured. A Postgres `transcripts` table (content-addressed by sha256) plus a `job_runs` table become the durable eval store. A single dispatcher acquires a per-tier capacity slot *before* claiming a job (fixing the double-run bug in the naive pool) and a `lease_owner` fence token guards every write. Conditional translation lives in the `ytsummary` module driven by a backend model catalog.

**Tech Stack:** Go 1.26, pgx/v5, ollama `/api/chat`, OpenTelemetry Go SDK (OTLP HTTP), `golang.org/x/sync` (already a dep).

## Global Constraints

- Go module is `saga-api`; each app is its own module (no shared root go.mod).
- Distroless image, no shell: health checks are binary flags, never curl/CMD-SHELL.
- No em-dash in code/comments/docs; plain hyphen `-`.
- Code identifiers in English; existing comments are English - stay English.
- Mark deliberate simplifications with `// ponytail:` (what + upgrade path).
- Branch first: work on `feat/saga-cloud-provider` (already exists, has the provider seam). NEVER commit to `main`.
- No `Co-Authored-By` trailer in commits.
- Migrations: ordered embedded SQL files under `internal/db/migrations/`, applied by `db.Migrate`. Files use the pgx simple protocol (no bind params in the SQL file).
- `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` must all be clean at the end of every task. Run from `apps/saga-api/`.

---

## File map (Phase 1)

- `internal/llm/llm.go` - modify: `ChatOptions`, `ChatResult`, `/api/chat` client, options + metrics.
- `internal/llm/router.go` - modify: interface signature + dispatch.
- `internal/catalog/catalog.go` - create: model catalog (single source of truth).
- `internal/module/module.go` - modify: `Deps.LLM` uses new signature; add `Deps.Catalog`.
- `internal/module/ytsummary/ytsummary.go` - modify: conditional translate, summarizeLang decouple, per-run metrics collection.
- `internal/db/migrations/0004_transcripts.sql` - create.
- `internal/db/migrations/0005_job_runs.sql` - create.
- `internal/db/migrations/0006_lease_fence.sql` - create.
- `internal/store/transcripts.go` - create: transcript persistence.
- `internal/store/runs.go` - create: `job_runs` write.
- `internal/queue/queue.go` - modify: owner fence, tier column, claim-by-tier.
- `internal/worker/worker.go` - modify: dispatcher + pool, acquire-before-claim, lease derive.
- `internal/api/server.go` - modify: `GET /models`, `POST /api/jobs/{id}/rerun`.
- `internal/obs/otel.go` - create: OTel setup + shutdown.
- `main.go` - modify: wire catalog, semaphores, OTel, shutdown order.
- `internal/config/config.go` - modify: `SAGACloudConcurrency`, `TranslateModel`, `OTELEndpoint`.

---

## Task 1: Provider interface carries options and returns metrics

**Files:**
- Modify: `internal/llm/llm.go`, `internal/llm/router.go`
- Modify: `internal/module/module.go`, `internal/module/ytsummary/ytsummary.go`, `internal/api/server.go`
- Test: `internal/llm/router_test.go`, `internal/llm/llm_test.go` (adapt existing)

**Interfaces:**
- Produces:
  - `type ChatOptions struct { Temperature float64; Seed int; Think bool; NumCtx int }`
  - `type ChatResult struct { Text string; InputTokens, OutputTokens int; EvalDuration, LoadDuration, WallClock time.Duration }`
  - `Provider.Chat(ctx context.Context, model, prompt string, opts ChatOptions, onToken func(string)) (ChatResult, error)`
- Consumes: nothing (foundation task).

This task only changes signatures and threads `ChatResult`/`ChatOptions` through; the client still speaks `/v1` and returns zero metrics. Task 2 populates metrics.

- [ ] **Step 1: Adapt the router test to the new signature (failing).**

In `internal/llm/router_test.go`, change the fake provider and assertions:

```go
type fakeProvider struct {
	gotModel string
	gotOpts  llm.ChatOptions
}

func (f *fakeProvider) Chat(_ context.Context, model, _ string, opts llm.ChatOptions, _ func(string)) (llm.ChatResult, error) {
	f.gotModel = model
	f.gotOpts = opts
	return llm.ChatResult{Text: "ok", OutputTokens: 3}, nil
}

func TestRouterSelectsProviderByModel(t *testing.T) {
	local, cloud := &fakeProvider{}, &fakeProvider{}
	r := llm.NewRouter(local, cloud)
	got, err := r.Chat(context.Background(), "deepseek-v4-flash:cloud", "hi", llm.ChatOptions{Temperature: 0.2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "ok" || cloud.gotModel != "deepseek-v4-flash:cloud" {
		t.Fatalf("cloud not selected: %+v", got)
	}
	if local.gotModel != "" {
		t.Fatal("local should not have been called")
	}
	if cloud.gotOpts.Temperature != 0.2 {
		t.Fatal("options not threaded")
	}
}
```

- [ ] **Step 2: Run to verify it fails to compile/pass.**

Run: `go test ./internal/llm/ -run TestRouterSelects -v`
Expected: FAIL (signature mismatch: `Chat` does not implement `Provider`).

- [ ] **Step 3: Define the types and change the interface in `router.go`.**

Replace the `Provider` interface in `internal/llm/router.go`:

```go
import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ChatOptions are the per-call knobs plumbed into the ollama request.
type ChatOptions struct {
	Temperature float64
	Seed        int
	Think       bool
	NumCtx      int // 0 = server default
}

// ChatResult is one completed chat: the text plus the metrics needed for the
// eval store. EvalDuration is 0 when the backend omits it (Ollama Cloud); use
// WallClock as the fallback denominator for tok/s.
type ChatResult struct {
	Text         string
	InputTokens  int
	OutputTokens int
	EvalDuration time.Duration
	LoadDuration time.Duration
	WallClock    time.Duration
}

type Provider interface {
	Chat(ctx context.Context, model, prompt string, opts ChatOptions, onToken func(string)) (ChatResult, error)
}
```

Update `Router.Chat` to the new signature:

```go
func (r *Router) Chat(ctx context.Context, model, prompt string, opts ChatOptions, onToken func(string)) (ChatResult, error) {
	if isCloudModel(model) {
		if r.cloud == nil {
			return ChatResult{}, fmt.Errorf("llm: model %q needs Ollama Cloud but OLLAMA_API_KEY is not set", model)
		}
		return r.cloud.Chat(ctx, model, prompt, opts, onToken)
	}
	return r.local.Chat(ctx, model, prompt, opts, onToken)
}
```

- [ ] **Step 4: Update the client in `llm.go` to the new signature (still `/v1`, zero metrics for now).**

Change `func (c *Client) Chat(ctx context.Context, model, prompt string, onToken func(string)) (string, error)` to return `(ChatResult, error)` and accept `opts ChatOptions`. Keep the existing `/v1/chat/completions` body but wrap the assembled string:

```go
func (c *Client) Chat(ctx context.Context, model, prompt string, _ ChatOptions, onToken func(string)) (ChatResult, error) {
	// ... existing acquire/semaphore + request/stream code, but every
	// `return "", err` becomes `return ChatResult{}, err` ...
	// at the end:
	return ChatResult{Text: sb.String()}, sc.Err()
}
```

Adapt `internal/llm/llm_test.go` similarly (assert `.Text`).

- [ ] **Step 5: Update callers.**

`internal/module/module.go`: `Deps.LLM` type is already `llm.Provider` - no change needed there. `internal/module/ytsummary/ytsummary.go`: the `chat` closure now returns `(llm.ChatResult, error)`; for this task just use `.Text`:

```go
chat := func(prompt string, onToken func(string)) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, deps.ChunkTimeout)
	defer cancel()
	res, err := deps.LLM.Chat(callCtx, in.Model, prompt, llm.ChatOptions{Temperature: 0.2}, onToken)
	return res.Text, err
}
```

(Add the `saga-api/internal/llm` import to ytsummary.) `internal/api/server.go` `translate`: `md, err := s.llm.Chat(...)` becomes `res, err := s.llm.Chat(r.Context(), defaultTranslateModel, summarize.TranslatePrompt(req.Lang, *job.ResultMarkdown), llm.ChatOptions{Temperature: 0.2}, nil)` then use `res.Text`.

- [ ] **Step 6: Build + test + vet + fmt.**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: builds, all pass, `gofmt -l .` prints nothing.

- [ ] **Step 7: Commit.**

```bash
git add -A && git commit -m "refactor(saga-api): Provider carries ChatOptions and returns ChatResult"
```

---

## Task 2: Switch the client to ollama-native /api/chat with metrics

**Files:**
- Modify: `internal/llm/llm.go`
- Test: `internal/llm/llm_test.go`

**Interfaces:**
- Consumes: `ChatOptions`, `ChatResult` (Task 1).
- Produces: a client that POSTs `/api/chat` NDJSON, honors options, populates `ChatResult` metrics, and errors when the `done` frame never arrives.

- [ ] **Step 1: Write the failing test (metrics + bearer + sawDone).**

Add to `internal/llm/llm_test.go`:

```go
func TestChatParsesApiChatMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("local must not send auth, got %q", got)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, `{"message":{"content":"hel"}}`+"\n")
		io.WriteString(w, `{"message":{"content":"lo"}}`+"\n")
		io.WriteString(w, `{"done":true,"eval_count":2,"eval_duration":1000000000,"prompt_eval_count":5,"load_duration":500000000}`+"\n")
	}))
	defer srv.Close()

	var toks []string
	res, err := llm.New(srv.URL).Chat(context.Background(), "m", "hi",
		llm.ChatOptions{Temperature: 0.2, Seed: 7}, func(s string) { toks = append(toks, s) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.OutputTokens != 2 || res.InputTokens != 5 {
		t.Fatalf("tokens = %d/%d", res.OutputTokens, res.InputTokens)
	}
	if res.EvalDuration != time.Second {
		t.Fatalf("eval_duration = %v", res.EvalDuration)
	}
	if len(toks) != 2 {
		t.Fatalf("streamed %d tokens", len(toks))
	}
}

func TestChatErrorsWithoutDoneFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"message":{"content":"partial"}}`+"\n") // stream ends, no done
	}))
	defer srv.Close()
	_, err := llm.New(srv.URL).Chat(context.Background(), "m", "hi", llm.ChatOptions{}, nil)
	if err == nil {
		t.Fatal("expected error when stream ends without done frame")
	}
}
```

(Ensure imports: `io`, `net/http`, `net/http/httptest`, `time`.)

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/llm/ -run TestChat -v`
Expected: FAIL (client still calls `/v1`, no metrics, no done-guard).

- [ ] **Step 3: Rewrite the client request + stream loop.**

In `internal/llm/llm.go`, replace the request build and stream parse. Request body:

```go
type apiOptions struct {
	Temperature float64 `json:"temperature"`
	Seed        int     `json:"seed"`
	NumCtx      int     `json:"num_ctx,omitempty"`
}
body, err := json.Marshal(map[string]any{
	"model":    model,
	"stream":   true,
	"think":    opts.Think,
	"messages": []map[string]string{{"role": "user", "content": prompt}},
	"options":  apiOptions{Temperature: opts.Temperature, Seed: opts.Seed, NumCtx: opts.NumCtx},
})
// ...
req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
```

Keep the bearer header logic (cloud client sets `c.bearer`). Stream loop (NDJSON, not SSE `data:`):

```go
start := time.Now()
var sb strings.Builder
var res ChatResult
sawDone := false
sc := bufio.NewScanner(resp.Body)
sc.Buffer(make([]byte, 1<<20), 1<<20)
for sc.Scan() {
	line := strings.TrimSpace(sc.Text())
	if line == "" {
		continue
	}
	var frame struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Done            bool  `json:"done"`
		EvalCount       int   `json:"eval_count"`
		EvalDuration    int64 `json:"eval_duration"`
		PromptEvalCount int   `json:"prompt_eval_count"`
		LoadDuration    int64 `json:"load_duration"`
	}
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		continue
	}
	if frame.Message.Content != "" {
		sb.WriteString(frame.Message.Content)
		if onToken != nil {
			onToken(frame.Message.Content)
		}
	}
	if frame.Done {
		res.OutputTokens = frame.EvalCount
		res.InputTokens = frame.PromptEvalCount
		res.EvalDuration = time.Duration(frame.EvalDuration)
		res.LoadDuration = time.Duration(frame.LoadDuration)
		sawDone = true
	}
}
if err := sc.Err(); err != nil {
	return ChatResult{}, err
}
if !sawDone {
	return ChatResult{}, fmt.Errorf("ollama: stream ended before done frame")
}
res.Text = sb.String()
res.WallClock = time.Since(start)
return res, nil
```

- [ ] **Step 4: Run tests.**

Run: `go test ./internal/llm/ -v`
Expected: PASS (including the existing cloud bearer + local no-auth tests, now on `/api/chat`).

- [ ] **Step 5: Commit.**

```bash
git add -A && git commit -m "feat(saga-api): switch llm client to native /api/chat with token metrics"
```

---

## Task 3: Model catalog package + GET /models

**Files:**
- Create: `internal/catalog/catalog.go`, `internal/catalog/catalog_test.go`
- Modify: `internal/api/server.go`, `internal/module/module.go`, `main.go`

**Interfaces:**
- Produces:
  - `type Model struct { ID, Label, Tier string; Norwegian bool; Speed, Precision int; PriceInPerMtok, PriceOutPerMtok float64; Note string }`
  - `func All() []Model`
  - `func Get(id string) (Model, bool)`
  - `func IsNorwegian(id string) bool` (false when unknown)
- Consumes: nothing.

- [ ] **Step 1: Write the failing test.**

`internal/catalog/catalog_test.go`:

```go
package catalog_test

import (
	"testing"
	"saga-api/internal/catalog"
)

func TestCatalogLookups(t *testing.T) {
	if _, ok := catalog.Get("qwen3.5:2b"); !ok {
		t.Fatal("local default missing")
	}
	if !catalog.IsNorwegian("deepseek-v4-flash:cloud") {
		t.Fatal("cloud must be norwegian-capable")
	}
	if catalog.IsNorwegian("qwen3.5:2b") {
		t.Fatal("qwen3.5:2b is english-only")
	}
	if catalog.IsNorwegian("nonexistent") {
		t.Fatal("unknown model must default to false")
	}
	if len(catalog.All()) < 10 {
		t.Fatalf("catalog too small: %d", len(catalog.All()))
	}
}
```

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/catalog/ -v`
Expected: FAIL (package does not exist).

- [ ] **Step 3: Write the catalog.**

`internal/catalog/catalog.go` - transcribe section 2 of the spec. Full content:

```go
// Package catalog is the single source of truth for the model list: tiers,
// Norwegian capability (drives conditional translate), the UI meter ratings,
// and per-model price. The API serves it (GET /models) so the web app never
// keeps its own copy. ponytail: a static slice, not a DB table; make it a
// table only if models need editing without a redeploy.
package catalog

type Model struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	Tier            string  `json:"tier"` // "local" | "cloud"
	Norwegian       bool    `json:"norwegian"`
	Speed           int     `json:"speed"`     // 1-4, short-input benchmark
	Precision       int     `json:"precision"` // 1-4, short-input benchmark
	PriceInPerMtok  float64 `json:"priceInPerMtok"`
	PriceOutPerMtok float64 `json:"priceOutPerMtok"`
	Note            string  `json:"note"`
}

var models = []Model{
	{"deepseek-v4-flash:cloud", "DeepSeek V4 Flash", "cloud", true, 4, 4, 0, 0, "Turbo default. Best translator, large context."},
	{"gemini-3-flash-preview:cloud", "Gemini 3 Flash (preview)", "cloud", true, 4, 4, 0, 0, "Richest structure."},
	{"kimi-k2.6:cloud", "Kimi K2.6", "cloud", true, 4, 4, 0, 0, "Flawless Norwegian."},
	{"kimi-k2.7-code:cloud", "Kimi K2.7 Code", "cloud", true, 4, 4, 0, 0, "Flawless Norwegian."},
	{"glm-5.2:cloud", "GLM 5.2", "cloud", true, 3, 4, 0, 0, "Newest, clean."},
	{"minimax-m3:cloud", "MiniMax M3", "cloud", true, 3, 4, 0, 0, "Rich structure."},
	{"qwen3.5:2b", "Qwen3.5 2B", "local", false, 2, 3, 0, 0, "Local default. Excellent structured English."},
	{"qwen3.5:0.8b", "Qwen3.5 0.8B", "local", false, 3, 2, 0, 0, "Fastest local; rougher."},
	{"qwen3.5:4b", "Qwen3.5 4B", "local", false, 2, 3, 0, 0, "Best local quality."},
	{"gemma4:e4b", "Gemma4 e4b", "local", true, 2, 3, 0, 0, "Can do Norwegian directly."},
	{"minicpm5:fp16", "MiniCPM5 fp16", "local", false, 3, 2, 0, 0, "English-only speed option."},
}

// ponytail: cloud prices left 0 until Ollama Cloud publishes per-model rates;
// fill priceIn/priceOutPerMtok here and cost_usd populates automatically.

func All() []Model { return append([]Model(nil), models...) }

func Get(id string) (Model, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

func IsNorwegian(id string) bool {
	m, ok := Get(id)
	return ok && m.Norwegian
}
```

- [ ] **Step 4: Run test.**

Run: `go test ./internal/catalog/ -v`
Expected: PASS.

- [ ] **Step 5: Serve GET /models + inject into Deps.**

In `internal/module/module.go`, add to `Deps`: `Catalog func(id string) (catalog.Model, bool)` - or simpler, since catalog is a package, callers use `catalog.IsNorwegian` directly; skip the Deps field. In `internal/api/server.go` register:

```go
mux.HandleFunc("GET /models", func(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"models": catalog.All()})
})
```

(import `saga-api/internal/catalog`.)

- [ ] **Step 6: Add an endpoint test.**

`internal/api/server_test.go` - add a test hitting `GET /models` expecting a non-empty `models` array with `qwen3.5:2b` present. (Follow the existing server_test pattern.)

- [ ] **Step 7: Build/test/vet/fmt + commit.**

```bash
go build ./... && go test ./... && go vet ./... && gofmt -l .
git add -A && git commit -m "feat(saga-api): model catalog package + GET /models"
```

---

## Task 4: transcripts table + persistence

**Files:**
- Create: `internal/db/migrations/0004_transcripts.sql`, `internal/store/transcripts.go`, `internal/store/transcripts_test.go`

**Interfaces:**
- Produces:
  - `func Sha256(text string) string` (hex)
  - `func SaveTranscript(ctx, pool, t Transcript) error` (idempotent upsert by sha)
  - `func GetTranscript(ctx, pool, sha string) (*Transcript, error)`
  - `type Transcript struct { Sha256, Text, Lang, Source string; Tokens, Chars int }`

- [ ] **Step 1: Write the migration.**

`internal/db/migrations/0004_transcripts.sql`:

```sql
CREATE TABLE transcripts (
    sha256     text PRIMARY KEY,
    text       text NOT NULL,
    tokens     int NOT NULL DEFAULT 0,
    chars      int NOT NULL DEFAULT 0,
    lang       text NOT NULL DEFAULT '',
    source     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
```

- [ ] **Step 2: Write the failing test.**

`internal/store/transcripts_test.go` - a DB test guarded by `TEST_DATABASE_URL` (follow the pattern in `internal/db/db_test.go`; skip when unset):

```go
func TestSaveAndGetTranscript(t *testing.T) {
	pool := testPool(t) // helper mirroring db_test.go; skips if no TEST_DATABASE_URL
	tr := store.Transcript{Text: "hello world", Lang: "en", Source: "manual", Tokens: 2, Chars: 11}
	tr.Sha256 = store.Sha256(tr.Text)
	if err := store.SaveTranscript(context.Background(), pool, tr); err != nil {
		t.Fatal(err)
	}
	// idempotent second save must not error
	if err := store.SaveTranscript(context.Background(), pool, tr); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetTranscript(context.Background(), pool, tr.Sha256)
	if err != nil || got == nil || got.Text != "hello world" {
		t.Fatalf("got %+v err %v", got, err)
	}
}
```

- [ ] **Step 3: Run to verify failure.**

Run: `go test ./internal/store/ -run TestSaveAndGet -v`
Expected: FAIL (package missing).

- [ ] **Step 4: Implement `internal/store/transcripts.go`.**

```go
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
```

- [ ] **Step 5: Run test (needs `TEST_DATABASE_URL`; else it skips).**

Run: `TEST_DATABASE_URL=$SAGA_TEST_DB go test ./internal/store/ -run TestSaveAndGet -v`
Expected: PASS (or SKIP if no DB - note it and rely on the E2E in Task 11).

- [ ] **Step 6: Commit.**

```bash
git add -A && git commit -m "feat(saga-api): transcripts table + content-addressed store"
```

---

## Task 5: job_runs table + write

**Files:**
- Create: `internal/db/migrations/0005_job_runs.sql`, `internal/store/runs.go`, `internal/store/runs_test.go`

**Interfaces:**
- Produces:
  - `type Run struct { ... }` (fields below)
  - `func InsertRun(ctx, tx pgx.Tx, r Run) error` (takes a tx - written in the Complete transaction, Task 8)

- [ ] **Step 1: Write the migration.**

`internal/db/migrations/0005_job_runs.sql`:

```sql
CREATE TABLE job_runs (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id              bigint NOT NULL REFERENCES jobs(id),
    run_group_id        text NOT NULL DEFAULT '',
    transcript_sha256   text REFERENCES transcripts(sha256),
    model               text NOT NULL,
    model_build         text,
    tier                text NOT NULL,
    prompt_version      text NOT NULL,
    target_lang         text NOT NULL,
    summarize_lang      text NOT NULL,
    translate_model     text,
    reproducible        boolean NOT NULL DEFAULT false,
    temperature         double precision NOT NULL DEFAULT 0,
    seed                int NOT NULL DEFAULT 0,
    num_ctx             int NOT NULL DEFAULT 0,
    input_tokens        int NOT NULL DEFAULT 0,
    output_tokens       int NOT NULL DEFAULT 0,
    gen_tok_s           double precision NOT NULL DEFAULT 0,
    summarize_ms        int NOT NULL DEFAULT 0,
    translate_ms        int NOT NULL DEFAULT 0,
    total_ms            int NOT NULL DEFAULT 0,
    summarize_cost_usd  double precision NOT NULL DEFAULT 0,
    translate_cost_usd  double precision NOT NULL DEFAULT 0,
    chunk_count         int NOT NULL DEFAULT 0,
    result_markdown     text,
    translated_markdown text,
    eval_set_tag        text,
    trace_id            text,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX job_runs_job_idx ON job_runs (job_id);
```

- [ ] **Step 2: Write the failing test.**

`internal/store/runs_test.go` (DB-guarded): insert a job, begin a tx, `InsertRun`, commit, then `SELECT count(*)` = 1. Assert `model`, `output_tokens`, `summarize_cost_usd` round-trip.

- [ ] **Step 3: Run to verify failure.**

Run: `go test ./internal/store/ -run TestInsertRun -v`
Expected: FAIL.

- [ ] **Step 4: Implement `internal/store/runs.go`.**

```go
import (
	"context"
	"github.com/jackc/pgx/v5"
)

type Run struct {
	JobID             int64
	RunGroupID        string
	TranscriptSha256  string
	Model             string
	ModelBuild        string
	Tier              string
	PromptVersion     string
	TargetLang        string
	SummarizeLang     string
	TranslateModel    string
	Reproducible      bool
	Temperature       float64
	Seed              int
	NumCtx            int
	InputTokens       int
	OutputTokens      int
	GenTokS           float64
	SummarizeMs       int
	TranslateMs       int
	TotalMs           int
	SummarizeCostUSD  float64
	TranslateCostUSD  float64
	ChunkCount        int
	ResultMarkdown    string
	TranslatedMarkdown string
	EvalSetTag        string
	TraceID           string
}

func InsertRun(ctx context.Context, tx pgx.Tx, r Run) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO job_runs (job_id, run_group_id, transcript_sha256, model, model_build,
			tier, prompt_version, target_lang, summarize_lang, translate_model, reproducible,
			temperature, seed, num_ctx, input_tokens, output_tokens, gen_tok_s,
			summarize_ms, translate_ms, total_ms, summarize_cost_usd, translate_cost_usd,
			chunk_count, result_markdown, translated_markdown, eval_set_tag, trace_id)
		VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),$6,$7,$8,$9,NULLIF($10,''),$11,
			$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,NULLIF($25,''),NULLIF($26,''),NULLIF($27,''))`,
		r.JobID, r.RunGroupID, r.TranscriptSha256, r.Model, r.ModelBuild,
		r.Tier, r.PromptVersion, r.TargetLang, r.SummarizeLang, r.TranslateModel, r.Reproducible,
		r.Temperature, r.Seed, r.NumCtx, r.InputTokens, r.OutputTokens, r.GenTokS,
		r.SummarizeMs, r.TranslateMs, r.TotalMs, r.SummarizeCostUSD, r.TranslateCostUSD,
		r.ChunkCount, r.ResultMarkdown, r.TranslatedMarkdown, r.EvalSetTag, r.TraceID)
	return err
}
```

- [ ] **Step 5: Run test + commit.**

```bash
TEST_DATABASE_URL=$SAGA_TEST_DB go test ./internal/store/ -run TestInsertRun -v
git add -A && git commit -m "feat(saga-api): job_runs table + InsertRun (tx)"
```

---

## Task 6: Concurrency - fence token, tier column, acquire-before-claim, dispatcher pool

**Files:**
- Create: `internal/db/migrations/0006_lease_fence.sql`
- Modify: `internal/queue/queue.go`, `internal/worker/worker.go`, `internal/config/config.go`, `main.go`
- Test: `internal/worker/worker_test.go` (double-run guard), `internal/queue/queue_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `queue.Claim(ctx, pool, owner string, tiers []string) (*Job, error)` - claims oldest queued job whose `tier` is in `tiers`, stamping `lease_owner = owner`.
  - `Job.Tier string`, `Job.LeaseOwner string`.
  - `queue.Complete/Fail/SetProgress(... , owner string)` - all fence-guarded.
  - `worker.Run(ctx, pool, deps, bus, cloudSlots int)` - dispatcher + pool.

- [ ] **Step 1: Migration.**

`internal/db/migrations/0006_lease_fence.sql`:

```sql
ALTER TABLE jobs ADD COLUMN tier text NOT NULL DEFAULT 'local';
ALTER TABLE jobs ADD COLUMN lease_owner text;
```

- [ ] **Step 2: Failing test - the double-run guard.**

In `internal/queue/queue_test.go` (DB-guarded), prove the fence: claim a job as owner A; simulate a stale rescue (manually `UPDATE jobs SET status='queued', lease_owner=NULL WHERE id=$1`); claim again as owner B; then `Complete(..., ownerA)` must affect 0 rows (fenced out) while `Complete(..., ownerB)` affects 1.

```go
func TestCompleteIsFencedByOwner(t *testing.T) {
	pool := testPool(t)
	id := enqueueTest(t, pool, "local")
	a, _ := queue.Claim(ctx, pool, "owner-A", []string{"local"})
	// rescue back to queued (as RequeueStale would)
	pool.Exec(ctx, `UPDATE jobs SET status='queued', lease_owner=NULL WHERE id=$1`, a.ID)
	b, _ := queue.Claim(ctx, pool, "owner-B", []string{"local"})
	if b == nil || b.ID != a.ID {
		t.Fatal("expected re-claim of same job")
	}
	okA, _ := queue.CompleteOwned(ctx, pool, a.ID, "A-md", "owner-A")
	if okA {
		t.Fatal("owner-A must be fenced out after rescue")
	}
	okB, _ := queue.CompleteOwned(ctx, pool, b.ID, "B-md", "owner-B")
	if !okB {
		t.Fatal("owner-B should complete")
	}
}
```

(Have `Complete` return `(bool, error)` = did it affect a row; rename to `CompleteOwned` or keep name + add owner param. Use `CompleteOwned` returning bool to make the fence observable.)

- [ ] **Step 3: Run to verify failure.**

Run: `TEST_DATABASE_URL=$SAGA_TEST_DB go test ./internal/queue/ -run TestCompleteIsFenced -v`
Expected: FAIL (Claim/Complete signatures lack owner).

- [ ] **Step 4: Update queue.go.**

Add `Tier`, `LeaseOwner *string` to `Job` and `jobCols`/`scanJob`. Rewrite `Claim`:

```go
func Claim(ctx context.Context, pool *pgxpool.Pool, owner string, tiers []string) (*Job, error) {
	j, err := scanJob(pool.QueryRow(ctx, `
		UPDATE jobs SET status='running', attempts=attempts+1, started_at=now(),
			lease_at=now(), lease_owner=$1
		WHERE id = (
			SELECT id FROM jobs WHERE status='queued' AND tier = ANY($2)
			ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		RETURNING `+jobCols))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return j, err
}
```

Add `owner` fence + bool return to the writers:

```go
func CompleteOwned(ctx context.Context, pool *pgxpool.Pool, id int64, markdown, owner string) (bool, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE jobs SET status='done', result_markdown=$2, finished_at=now(),
			error=NULL, progress='' WHERE id=$1 AND status='running' AND lease_owner=$3`,
		id, markdown, owner)
	return tag.RowsAffected() == 1, err
}
```

Do the same owner guard for `SetProgress` (add `owner` param, `AND lease_owner=$3`) and `Fail`. `Enqueue` gains a `tier` param and writes it: `INSERT INTO jobs (module, input, tier) VALUES ($1,$2,$3)`.

- [ ] **Step 5: Run test.**

Run: `TEST_DATABASE_URL=$SAGA_TEST_DB go test ./internal/queue/ -v`
Expected: PASS.

- [ ] **Step 6: Rewrite worker as dispatcher + pool (acquire-before-claim).**

Replace `worker.Run` with a dispatcher owning `RequeueStale` and per-tier capacity, claiming only after a slot is free, then handing the job to a goroutine that always makes progress:

```go
// Run starts one dispatcher + a bounded set of job goroutines. A tier slot is
// acquired BEFORE Claim, so a claimed job never blocks on a semaphore while
// holding a lease (which would freeze its heartbeat and let RequeueStale
// double-run it). cloudSlots caps concurrent cloud jobs; local is always 1.
func Run(ctx context.Context, pool *pgxpool.Pool, deps module.Deps, bus *api.Bus, cloudSlots int) {
	local := make(chan struct{}, 1)
	cloud := make(chan struct{}, cloudSlots)
	var wg sync.WaitGroup
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-t.C:
		}
		if n, err := queue.RequeueStale(ctx, pool, leaseTimeout(deps.ChunkTimeout)); err != nil {
			log.Printf("worker: requeue stale: %v", err)
		} else if n > 0 {
			log.Printf("worker: rescued %d stale job(s)", n)
		}
		dispatch(ctx, pool, deps, bus, local, "local", &wg)
		dispatch(ctx, pool, deps, bus, cloud, "cloud", &wg)
	}
}

func dispatch(ctx context.Context, pool *pgxpool.Pool, deps module.Deps, bus *api.Bus,
	slots chan struct{}, tier string, wg *sync.WaitGroup) {
	for {
		select {
		case slots <- struct{}{}: // acquire BEFORE claim
		default:
			return // tier full this tick
		}
		owner := uuid.NewString()
		job, err := queue.Claim(ctx, pool, owner, []string{tier})
		if err != nil || job == nil {
			<-slots // nothing to run, release
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			process(ctx, pool, deps, bus, job, owner)
		}()
	}
}
```

`process` is the former `ProcessOne` body, with every `queue.SetProgress/Complete/Fail` call passing `owner`. Add `leaseTimeout(chunk time.Duration) time.Duration { return chunk + 5*time.Minute }` and delete the hard-coded const. Add imports `sync`, `github.com/google/uuid` (add to go.mod: `go get github.com/google/uuid`). Local tier semaphore capacity 1 encodes the single-GPU rule; the old `llm.Client` n=1 semaphore can stay as defense-in-depth or be removed - keep it, it is harmless.

- [ ] **Step 7: The concurrency double-run test at the worker level.**

`internal/worker/worker_test.go` (DB-guarded): enqueue one local job whose fake LLM sleeps longer than a shortened `leaseTimeout`; run two dispatch ticks; assert exactly one `job_runs` row after completion (proves rescue + fence do not double-write). If wiring a full worker is heavy, the queue-level `TestCompleteIsFencedByOwner` (Step 2) is the load-bearing guarantee; mark this worker-level test as the E2E check in Task 11 if it is too heavy to unit-test.

- [ ] **Step 8: Update Enqueue callers + main.go wiring.**

`api/server.go createJob`: derive tier from the input model via `catalog.Get(model).Tier` (default "local" when absent) and pass to `queue.Enqueue`. `main.go`: `go worker.Run(ctx, pool, deps, bus, cfg.SAGACloudConcurrency)`. Add `SAGACloudConcurrency int` to config (`getint("SAGA_CLOUD_CONCURRENCY", 3)`; add a `getint` helper).

- [ ] **Step 9: Build/test/vet/fmt + commit.**

```bash
go build ./... && go test ./... && go vet ./... && gofmt -l .
git add -A && git commit -m "feat(saga-api): fenced tier-aware claim + dispatcher pool (fixes double-run)"
```

---

## Task 7: Conditional translate in ytsummary

**Files:**
- Modify: `internal/module/ytsummary/ytsummary.go`, `internal/summarize/prompt.go` (constant), `internal/config/config.go`, `main.go`
- Test: `internal/module/ytsummary/ytsummary_test.go`

**Interfaces:**
- Consumes: `catalog.IsNorwegian`, `llm.ChatResult`, `config.TranslateModel`.
- Produces: `summarize.PromptVersion` constant; ytsummary summarizes in `summarizeLang` and translates only when `targetLang == "no" && !catalog.IsNorwegian(model)`.

- [ ] **Step 1: Add the prompt-version constant.**

In `internal/summarize/prompt.go`: `const PromptVersion = "2026-07-10"` with a comment: bump when any prompt in this file changes.

- [ ] **Step 2: Write the failing decision test.**

`ytsummary_test.go` - a table test with a fake `llm.Provider` and fake `ytdlp.Fetcher` asserting: (a) `lang=en` -> summarizeLang "en", no translate call; (b) `lang=no` + model `gemma4:e4b` (norwegian) -> summarizeLang "no", no translate; (c) `lang=no` + model `qwen3.5:2b` (english-only) -> summarizeLang "en" AND a translate call to the configured translate model. The fake provider records `(model, prompt)` per call.

- [ ] **Step 3: Run to verify failure.**

Run: `go test ./internal/module/ytsummary/ -v`
Expected: FAIL.

- [ ] **Step 4: Implement the decision.**

In `Run`, after resolving `in.Model`:

```go
targetLang := in.Lang
summarizeLang := targetLang
needTranslate := targetLang == "no" && !catalog.IsNorwegian(in.Model)
if needTranslate {
	summarizeLang = "en"
}
```

Pass `summarizeLang` (not `in.Lang`) into `SinglePrompt`/`MapPrompt`/`ReducePrompt`. After producing `summary` and `CleanMath`, if `needTranslate`:

```go
if needTranslate {
	emit(module.Event{Stage: "translating"})
	tr, terr := deps.LLM.Chat(ctx, deps.TranslateModel, summarize.TranslatePrompt("no", summary), llm.ChatOptions{Temperature: 0.2}, nil)
	if terr != nil {
		return module.Result{}, fmt.Errorf("translate: %w", terr)
	}
	summary = summarize.CleanMath(tr.Text)
}
```

Add `TranslateModel string` to `module.Deps`; set it in `main.go` from `cfg.TranslateModel` resolved at boot (`TRANSLATE_MODEL` default `deepseek-v4-flash:cloud`, but fall back to `gemma4:e4b` when `cfg.OllamaAPIKey == ""`). Add `TranslateModel` to config.

- [ ] **Step 5: Run test + commit.**

```bash
go test ./internal/module/ytsummary/ -v
git add -A && git commit -m "feat(saga-api): conditional translate driven by catalog norwegian flag"
```

---

## Task 8: Collect per-run metrics + write job_runs on success

**Files:**
- Modify: `internal/module/module.go` (Result carries run metrics), `internal/module/ytsummary/ytsummary.go`, `internal/worker/worker.go`
- Test: extend `ytsummary_test.go`

**Interfaces:**
- Consumes: `store.InsertRun`, `store.SaveTranscript`, `catalog.Get`, `llm.ChatResult`.
- Produces: `module.Result.Run store.Run` (partially filled by the module: tokens, timings, model, langs, chunk_count, markdown, transcript sha); the worker fills `JobID`, `TraceID`, `Tier`, cost, and writes it in the Complete tx.

- [ ] **Step 1: Extend Result + accumulate metrics in ytsummary (failing test).**

Add `Run store.Run` and `Transcript store.Transcript` to `module.Result`. In `ytsummary.Run`, sum `ChatResult.OutputTokens`/`InputTokens` across all summarize calls, track summarize wall time, chunk count, set `Model`, `SummarizeLang`, `TargetLang`, `PromptVersion`, `TranslateModel` (when used) + `TranslateMs`, and the transcript (`store.Transcript{Text: v.Transcript, Sha256: store.Sha256(v.Transcript), Lang: v.<caption lang>, Source: ...}`). Extend the ytsummary test to assert `res.Run.OutputTokens > 0` and `res.Run.ChunkCount >= 1` and `res.Transcript.Sha256 != ""`.

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/module/ytsummary/ -run TestRunMetrics -v`
Expected: FAIL.

- [ ] **Step 3: Implement accumulation + cost in the worker.**

In `worker.process`, on success (before `Complete`): open a tx, `SaveTranscript`, compute cost from catalog price + tokens, fill `Run.JobID/Tier/TraceID/GenTokS/SummarizeCostUSD/Reproducible`, `InsertRun`, then `CompleteOwned` in the SAME tx, commit. Use `pool.Begin`; on any error roll back and fall through to `Fail`. tok/s: `genTokS := float64(out) / evalOrWallSeconds`. `Reproducible = catalog.Get(model).Tier == "local"`.

```go
// success path, replacing the bare Complete:
tx, err := pool.Begin(ctx)
if err == nil {
	run := res.Run
	run.JobID = job.ID
	run.Tier = job.Tier
	run.TraceID = traceIDFrom(ctx)
	m, _ := catalog.Get(run.Model)
	run.SummarizeCostUSD = m.PriceInPerMtok*float64(run.InputTokens)/1e6 + m.PriceOutPerMtok*float64(run.OutputTokens)/1e6
	run.Reproducible = m.Tier == "local"
	if err = store.InsertRun(ctx, tx, run); err == nil {
		if _, err = completeOwnedTx(ctx, tx, job.ID, res.Markdown, owner); err == nil {
			err = tx.Commit(ctx)
		}
	}
	if err != nil {
		tx.Rollback(ctx)
	}
}
```

(Add a `completeOwnedTx` variant of `CompleteOwned` that runs on a `pgx.Tx`; `queue` exposes both, or move the UPDATE inline.) `job_runs` is written ONLY here (success), so retries do not create noise rows.

- [ ] **Step 4: Run test + build/vet/fmt.**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: PASS/clean.

- [ ] **Step 5: Commit.**

```bash
git add -A && git commit -m "feat(saga-api): write job_runs + transcript + cost in the Complete tx"
```

---

## Task 9: Replay - re-run a job on another model reusing the stored transcript

**Files:**
- Modify: `internal/api/server.go`, `internal/module/ytsummary/ytsummary.go` (accept a pre-fetched transcript), `internal/module/module.go`
- Test: `internal/api/server_test.go`

**Interfaces:**
- Produces: `POST /api/jobs/{id}/rerun {"model": "..."}` -> enqueues a new job for the same URL + a `replay_of` sha so the module skips fetch. Response `{"id": <newJobID>}`.

- [ ] **Step 1: Failing endpoint test.**

`server_test.go`: seed a done job with a stored transcript; POST `/api/jobs/{id}/rerun` with a different model; assert 201 + a new job id, and that the new job's input carries the transcript sha (so fetch is skipped).

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/api/ -run TestRerun -v`
Expected: FAIL.

- [ ] **Step 3: Implement.**

Add the route. Handler loads the job's latest `job_runs` row to get `transcript_sha256`, builds a new `yt-summary` input `{url, lang, model, transcript_sha}` (add `TranscriptSha string json:"transcript_sha,omitempty"` to ytsummary `input`), enqueues with the new model's tier. In `ytsummary.Run`, when `in.TranscriptSha != ""`, load the transcript via a `deps` accessor (add `Transcripts func(ctx, sha) (*store.Transcript, error)` to `Deps`, wired in main from `store.GetTranscript`) instead of fetching, so replay input is byte-identical. `run_group_id`: set to the original job's id string so replays of one job group together.

- [ ] **Step 4: Run test + commit.**

```bash
go test ./... && go vet ./... && gofmt -l .
git add -A && git commit -m "feat(saga-api): replay endpoint reusing the fingerprinted transcript"
```

---

## Task 10: OpenTelemetry wiring

**Files:**
- Create: `internal/obs/otel.go`
- Modify: `main.go`, `internal/worker/worker.go`, `internal/config/config.go`
- Test: `internal/obs/otel_test.go` (setup returns a shutdown func without error when endpoint unset)

**Interfaces:**
- Produces: `obs.Setup(ctx, endpoint, serviceVersion string) (shutdown func(context.Context) error, err error)`; a root span started in the worker at claim time; metric instruments created once.

- [ ] **Step 1: Add deps + config.**

`go get go.opentelemetry.io/otel go.opentelemetry.io/otel/sdk go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp`. Add `OTELEndpoint string` to config (`OTEL_EXPORTER_OTLP_ENDPOINT`, default empty = disabled).

- [ ] **Step 2: Failing test.**

`otel_test.go`: `obs.Setup(ctx, "", "test")` returns a non-nil shutdown and nil error (disabled path is a no-op), and shutdown returns nil.

- [ ] **Step 3: Implement `obs.Setup`.**

When endpoint is empty, return a no-op shutdown. Otherwise build an OTLP HTTP trace exporter + meter provider pointed at the endpoint, register globally, create the histogram/counter instruments once (exported as package vars used by the worker), return a shutdown that flushes both providers.

- [ ] **Step 4: Root span at claim + shutdown order.**

In `worker.process`, start a new root span (`tracer.Start(context.Background(), "job", trace.WithNewRoot(), attrs...)`) - the request trace context does not survive the async DB hop. Record `job.id`, `model`, `tier`, `target_lang`, token/timing attrs, and record the metric instruments. Put `traceIDFrom(ctx)` = `span.SpanContext().TraceID().String()` into the run record (Task 8). In `main.go`: call `obs.Setup`, add a `sync.WaitGroup` around the worker, and order shutdown: `stop()` (cancel) -> wait workers -> `otelShutdown(ctx)` -> `pool.Close()`.

- [ ] **Step 5: Build/test/vet/fmt + commit.**

```bash
go build ./... && go test ./... && go vet ./... && gofmt -l .
git add -A && git commit -m "feat(saga-api): OpenTelemetry traces + metrics, flushed on shutdown"
```

---

## Task 11: End-to-end verification (curl, no UI)

**Files:** none (verification only). Requires a real Postgres and tailnet Ollama; set `DATABASE_URL`, `OLLAMA_URL`, and `OLLAMA_API_KEY` (from the scratchpad key file for the session).

- [ ] **Step 1: Run migrations + start the server.**

Run: `DATABASE_URL=... OLLAMA_API_KEY=... go run .` and confirm the log shows the new migrations applied and modules registered.

- [ ] **Step 2: Local-tier job.**

`curl -s localhost:8080/api/jobs -d '{"module":"yt-summary","input":{"url":"<short vid>","lang":"en","model":"qwen3.5:2b"}}'` -> poll `GET /api/jobs/{id}` until done. Confirm a `job_runs` row: `SELECT model, tier, output_tokens, gen_tok_s, summarize_ms FROM job_runs WHERE job_id=<id>;` has real numbers.

- [ ] **Step 3: Norwegian on an English-only model triggers translate.**

Same URL, `lang=no`, `model=qwen3.5:2b`. Confirm the SSE stream shows a `translating` stage and `translated_markdown` is Norwegian; `job_runs.summarize_lang='en'`, `target_lang='no'`, `translate_model` set.

- [ ] **Step 4: Turbo-tier job.**

`model=deepseek-v4-flash:cloud`, `lang=no`. Confirm direct-Norwegian (no `translating` stage), `job_runs.tier='cloud'`, `reproducible=false`.

- [ ] **Step 5: Replay yields byte-identical input.**

`curl -s localhost:8080/api/jobs/<id>/rerun -d '{"model":"kimi-k2.6:cloud"}'` -> new job. Confirm the new `job_runs.transcript_sha256` equals the original's (`SELECT transcript_sha256 FROM job_runs WHERE job_id IN (<orig>,<new>);`) and no `fetching` stage appeared on the replay.

- [ ] **Step 6: Concurrency + no double-run.**

Enqueue 5 cloud jobs at once; confirm at most `SAGA_CLOUD_CONCURRENCY` run concurrently (watch progress) and each job produces exactly one `job_runs` row (`SELECT job_id, count(*) FROM job_runs GROUP BY job_id HAVING count(*) > 1;` returns nothing).

- [ ] **Step 7: Telemetry.**

With `OTEL_EXPORTER_OTLP_ENDPOINT` pointed at a local otel-lgtm, confirm a `job` trace with child spans and `saga.*` metrics appear in Grafana.

- [ ] **Step 8: Commit any fixes found, then open the PR for Phase 1.**

```bash
git add -A && git commit -m "test(saga-api): phase-1 e2e verification notes"
```

---

## Self-review notes (author)

- Spec coverage: §2 catalog (T3), §3 conditional translate (T7), §5.1 interface (T1), §5.2 /api/chat (T2), §5.3 fence+pool+tier (T6), §5.4 determinism (T7/T8), §6.1 transcripts+job_runs (T4/T5/T8), §6.2 replay (T9), §7 OTel (T10). §6.3 blind pairwise quality + §6.4 benchmark set + §4 frontend are Phase 2/3 (separate plans). §6 cost split: `summarize_cost_usd`/`translate_cost_usd` columns present (T5); translate cost population is a Phase-2 follow-up (translate runs inside the module; wire its ChatResult tokens into `TranslateCostUSD` when the compare view needs it - flagged).
- DB-dependent tests skip without `TEST_DATABASE_URL`; the load-bearing fence guarantee has both a unit test (T6.2) and an E2E (T11.6).
- Deferred to Phase 2 explicitly: translate token/cost capture into the run record, N=3 repeat-run orchestration, `eval_set_tag` population.
```
