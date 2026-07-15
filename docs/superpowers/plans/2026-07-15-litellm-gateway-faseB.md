# LiteLLM Gateway - Fase B (saga-api refactor) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Refactor `saga-api` to consume the live in-cluster LiteLLM gateway (`http://litellm.litellm.svc:4000/v1`, OpenAI-compatible) instead of talking to Ollama directly, keeping the model catalog/tiers unchanged. One streaming OpenAI client replaces the native Ollama client + cloud client + suffix-Router.

**Architecture:** The `llm.Provider` seam (`Chat(ctx, model, prompt, opts, onToken) -> ChatResult`) stays; callers (`ytsummary.go`, `server.go`) are untouched. A new OpenAI-compatible SSE streaming client is the sole implementation. `EvalDuration` stays 0 (no native timing over OpenAI) so tok/s uses the existing wall-clock fallback. Token counts come from the `stream_options.include_usage` trailing frame (PoC-verified reliable in Fase A). Local capacity becomes 2 (both boxes, LiteLLM owns per-box serialization).

**Tech Stack:** Go 1.26, saga-api module (`apps/saga-api`). Live dependency: the Fase A LiteLLM gateway (already deployed + verified) and the `saga-litellm` k8s secret in `saga-api-prod` (already created).

**Spec:** `docs/superpowers/specs/2026-07-13-litellm-gateway-design.md`.

## Global Constraints

- **Model IDs are the catalog IDs verbatim** (`qwen3.5:2b`, `deepseek-v4-flash:cloud`) - passed through to LiteLLM unchanged. No `:cloud`-suffix logic in saga-api anymore; LiteLLM's config maps names to backends.
- **`think:false` for local models is mandatory** (Fase A PoC: qwen3.5:2b burns the whole token budget on `reasoning_tokens` and returns EMPTY content otherwise). Set it in the LiteLLM config's local `litellm_params` (Task 1), not per-request - most robust and benefits every consumer.
- **Token counts** from the `include_usage` trailing frame; `completion_tokens` includes reasoning tokens (acceptable for tok/s; it is real generated work).
- **tok/s = wall-clock** (EvalDuration stays 0). The existing fallback in `ytsummary.go` already handles this - do not fake an EvalDuration.
- **No new Go dependencies** - hand-rolled SSE parse, same spirit as the current hand-rolled NDJSON client.
- **Reversibility:** the deploy cutover (Task 10) keeps a rollback path (revert saga-api env to direct Ollama) until verified.
- **No em-dash**; plain hyphen. Comments may be Norwegian. Mark simplifications `// ponytail:`.
- **Do NOT touch `platform/litellm/` ingress** or anything the `llm.klyngo.no` work owns; Task 1's config edit touches only `config.yaml`'s local `litellm_params`.

---

## File Structure

**kongebra-apps (`apps/saga-api`):**
- Rewrite: `internal/llm/llm.go` -> OpenAI-compatible SSE streaming client (`New(baseURL, apiKey)`).
- Delete: `internal/llm/router.go`, `internal/llm/router_test.go`, `internal/llm/tier_routing_test.go`.
- Modify: `internal/llm/llm_test.go` -> OpenAI SSE wire shape.
- Modify: `internal/config/config.go` -> `LiteLLMURL`, `LiteLLMAPIKey`; remove `OllamaURL`/`OllamaCloudURL`/`OllamaAPIKey`.
- Modify: `main.go` -> single client, boot `cloud_enabled` health check, drop Router wiring.
- Modify: `internal/api/server.go` -> `cloudEnabled` now set from the health check (constructor arg stays a bool).
- Modify: `internal/api/server_test.go`, `internal/module/ytsummary/ytsummary_test.go` -> Provider fake instead of native client.
- Modify: `internal/summarize/clean.go` + `clean_test.go` -> strip `<think>...</think>`.
- Modify: `internal/worker/worker.go` -> local slot cap 1 -> 2.

**kongebra-gitops:**
- Modify: `platform/litellm/config.yaml` -> add `think: false` (or the ollama passthrough) to each local `litellm_params` (Task 1).
- Modify: `apps/saga-api/base/deployment.yaml` -> env `LITELLM_URL`, `LITELLM_API_KEY` (from `saga-litellm` secret); remove `OLLAMA_URL`/`OLLAMA_API_KEY`.
- Modify: saga-api egress NetworkPolicy -> allow `litellm.litellm.svc` ClusterIP :4000; remove the direct box + `0.0.0.0/0:443` allows once verified.

---

## Task 1: LiteLLM config - `think: false` for local models (gitops, prerequisite)

**Files:**
- Modify: `kongebra-gitops/platform/litellm/config.yaml`

**Interfaces:**
- Produces: local Ollama deployments stop emitting reasoning tokens, so a bounded completion returns real content. Verify empirically (Fase A PoC showed empty content without it).

- [ ] **Step 1: Confirm no in-flight collision** - `git -C kongebra-gitops log --oneline -5 platform/litellm/config.yaml` and check the `llm.klyngo.no` work is not mid-edit on this file. If it is, coordinate before editing.

- [ ] **Step 2: Add `think: false` to each LOCAL `litellm_params`** (the 10 local entries, both boxes). LiteLLM forwards non-OpenAI `litellm_params` keys to the Ollama provider; `think:false` there is not subject to `drop_params` (which only strips REQUEST params). Example for one entry:

```yaml
      - model_name: qwen3.5:2b
        litellm_params: { model: ollama_chat/qwen3.5:2b, api_base: http://100.125.242.93:11434, max_parallel_requests: 1, think: false }
```

Apply to all 10 local entries. Leave cloud entries unchanged.

- [ ] **Step 3: Verify + commit + deploy**

```bash
kubectl kustomize platform/litellm >/dev/null && echo OK
git add platform/litellm/config.yaml
git commit -m "fix(litellm): think:false for local Ollama models (reasoning burned the budget)"
git push  # ArgoCD selfHeal syncs; litellm reads config on restart
```

- [ ] **Step 4: Verify content is non-empty** (repeat the Fase A PoC for `qwen3.5:2b` with `max_tokens:150`): content is now a real sentence, `reasoning_tokens` ~0.

---

## Task 2: OpenAI-compatible streaming client

**Files:**
- Rewrite: `apps/saga-api/internal/llm/llm.go`
- Modify: `apps/saga-api/internal/llm/llm_test.go`

**Interfaces:**
- Consumes: `ChatOptions`, `ChatResult` (unchanged, in router.go today -> move the type block into llm.go when router.go is deleted in Task 3; for this task keep them where they are and just rewrite the client).
- Produces: `func New(baseURL, apiKey string) *Client` and `(*Client) Chat(...) (ChatResult, error)` implementing `Provider`. Speaks `POST {baseURL}/chat/completions` with SSE. `baseURL` includes the `/v1` suffix.

- [ ] **Step 1: Write the failing test** - replace `llm_test.go`'s native-NDJSON fake server with an OpenAI SSE one.

```go
package llm

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer streams OpenAI-style chat.completion.chunk frames then a usage
// frame then [DONE].
func sseServer(t *testing.T, tokens []string, promptTok, completionTok int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testkey" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, tok := range tokens {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", tok)
			if fl != nil {
				fl.Flush()
			}
		}
		fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d}}\n\n", promptTok, completionTok)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func TestChatStreamsAndCountsTokens(t *testing.T) {
	srv := sseServer(t, []string{"Hello", ", ", "world"}, 11, 3)
	defer srv.Close()
	c := New(srv.URL+"/v1", "testkey")
	var streamed strings.Builder
	res, err := c.Chat(context.Background(), "qwen3.5:2b", "hi",
		ChatOptions{Temperature: 0.2, Seed: 42}, func(tok string) { streamed.WriteString(tok) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Hello, world" {
		t.Errorf("text = %q", res.Text)
	}
	if streamed.String() != "Hello, world" {
		t.Errorf("streamed = %q", streamed.String())
	}
	if res.InputTokens != 11 || res.OutputTokens != 3 {
		t.Errorf("tokens in=%d out=%d", res.InputTokens, res.OutputTokens)
	}
	if res.EvalDuration != 0 {
		t.Errorf("EvalDuration must stay 0 (no native timing); got %v", res.EvalDuration)
	}
	if res.WallClock <= 0 {
		t.Error("WallClock should be measured")
	}
}

func TestChatAuthErrorSurfaces(t *testing.T) {
	srv := sseServer(t, []string{"x"}, 1, 1)
	defer srv.Close()
	c := New(srv.URL+"/v1", "wrongkey")
	if _, err := c.Chat(context.Background(), "m", "hi", ChatOptions{}, nil); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestChatErrorsIfStreamEndsWithoutDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		// no [DONE]
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1", "")
	if _, err := c.Chat(context.Background(), "m", "hi", ChatOptions{}, nil); err == nil {
		t.Fatal("expected error when stream ends before [DONE]")
	}
	_ = bufio.NewReader
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd apps/saga-api && go test ./internal/llm/ -run TestChatStreams -count=1`
Expected: FAIL (New signature mismatch / native client still present).

- [ ] **Step 3: Rewrite `llm.go`** as the OpenAI SSE client. Request: `stream:true`, `stream_options.include_usage:true`, `temperature`, `seed`, `messages`. No `think`/`num_ctx` (dropped - unused; think is handled in the LiteLLM config, Task 1). No app-side semaphore (LiteLLM owns per-box concurrency now).

```go
// Package llm speaks the OpenAI-compatible chat-completions streaming API to
// the in-cluster LiteLLM gateway, which fans out to the two Ollama boxes
// (round-robin) and Ollama Cloud. One Client serves every model - the gateway
// routes by model name. tok/s is wall-clock: the OpenAI wire carries token
// counts (via stream_options.include_usage) but not Ollama's native
// eval_duration, so ChatResult.EvalDuration stays 0 and callers fall back to
// WallClock. Local-GPU serialization moved into LiteLLM (max_parallel_requests
// per deployment), so this Client carries no app-wide semaphore.
// ponytail: hand-rolled SSE parse (~90 lines) instead of an SDK; swap for the
// openai-go SDK if we ever need tools/images/logprobs.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string // includes /v1
	apiKey  string
	httpc   *http.Client
}

// New builds a client for the LiteLLM gateway. baseURL includes the /v1 suffix;
// apiKey is the virtual key (empty allowed for an unauthenticated local test server).
func New(baseURL, apiKey string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, httpc: &http.Client{}}
}

func (c *Client) Chat(ctx context.Context, model, prompt string, opts ChatOptions, onToken func(string)) (ChatResult, error) {
	body, err := json.Marshal(map[string]any{
		"model":          model,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
		"temperature":    opts.Temperature,
		"seed":           opts.Seed,
		"messages":       []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return ChatResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ChatResult{}, fmt.Errorf("litellm: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	start := time.Now()
	var sb strings.Builder
	var res ChatResult
	sawDone := false
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "[DONE]" {
			sawDone = true
			break
		}
		var frame struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			continue
		}
		if len(frame.Choices) > 0 && frame.Choices[0].Delta.Content != "" {
			tok := frame.Choices[0].Delta.Content
			sb.WriteString(tok)
			if onToken != nil {
				onToken(tok)
			}
		}
		// Usage rides a trailing choices-empty frame (stream_options.include_usage).
		if frame.Usage != nil {
			res.InputTokens = frame.Usage.PromptTokens
			res.OutputTokens = frame.Usage.CompletionTokens
		}
	}
	if err := sc.Err(); err != nil {
		return ChatResult{}, err
	}
	if !sawDone {
		return ChatResult{}, fmt.Errorf("litellm: stream ended before [DONE]")
	}
	res.Text = sb.String()
	res.WallClock = time.Since(start)
	// EvalDuration/LoadDuration intentionally left 0: not carried by the OpenAI
	// wire. tok/s falls back to WallClock (see ytsummary.go).
	return res, nil
}
```

- [ ] **Step 4: Run to verify pass** (after Task 3 deletes router.go, since ChatOptions/ChatResult move). If running before Task 3, temporarily keep the type block; the sequence below deletes router.go in Task 3 and moves the types. Simplest: do Task 2 + Task 3 together, then run.

Run: `cd apps/saga-api && go test ./internal/llm/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit** (combined with Task 3).

---

## Task 3: Delete the Router + move the shared types

**Files:**
- Delete: `internal/llm/router.go`, `internal/llm/router_test.go`, `internal/llm/tier_routing_test.go`
- Modify: `internal/llm/llm.go` (add the `ChatOptions`/`ChatResult`/`Provider`/`DefaultTemperature` block moved from router.go)

**Interfaces:**
- Produces: `llm` package with one client + the `Provider` interface + `ChatOptions`/`ChatResult`/`DefaultTemperature`. No `Router`, `NewRouter`, `NewCloud`, `isCloudModel`.

- [ ] **Step 1: Move the type block** from `router.go` into `llm.go` (top of file): `DefaultTemperature`, `ChatOptions`, `ChatResult`, `Provider`. Keep them verbatim.

- [ ] **Step 2: Delete the files**

```bash
git rm internal/llm/router.go internal/llm/router_test.go internal/llm/tier_routing_test.go
```

(`tier_routing_test.go` asserted Router-suffix-vs-catalog-tier agreement - a premise that no longer exists. Its concern is now the LiteLLM config's job, verified in Fase A. Deleting, not porting.)

- [ ] **Step 3: Build + test the package**

Run: `cd apps/saga-api && go build ./internal/llm/ && go test ./internal/llm/ -count=1`
Expected: builds, tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/llm/
git commit -m "refactor(saga-api): OpenAI streaming client via LiteLLM; drop native/cloud Router"
```

---

## Task 4: Config - LiteLLM env, drop Ollama env

**Files:**
- Modify: `internal/config/config.go`

**Interfaces:**
- Produces: `Config.LiteLLMURL` (default `http://litellm.litellm.svc:4000/v1`), `Config.LiteLLMAPIKey` (`os.Getenv`, no default). Remove `OllamaURL`, `OllamaCloudURL`, `OllamaAPIKey`. Keep `TranslateModel`, `SAGACloudConcurrency`, etc.

- [ ] **Step 1: Edit `config.go`** - replace the three Ollama fields + their `Load()` lines:

```go
	LiteLLMURL    string
	LiteLLMAPIKey string // virtual key; no default
```
```go
		LiteLLMURL:    getenv("LITELLM_URL", "http://litellm.litellm.svc:4000/v1"),
		LiteLLMAPIKey: os.Getenv("LITELLM_API_KEY"),
```

Keep `TranslateModel` default `deepseek-v4-flash:cloud` (still a valid LiteLLM model name).

- [ ] **Step 2: Build** (will fail at main.go until Task 5) - `go build ./internal/config/` alone passes.

Run: `cd apps/saga-api && go build ./internal/config/`
Expected: PASS.

- [ ] **Step 3: Commit** (combined with Task 5).

---

## Task 5: main.go - single client + `cloud_enabled` health check

**Files:**
- Modify: `main.go`
- Modify: `internal/api/server.go` (only if the constructor needs it - `cloudEnabled` stays a bool arg)

**Interfaces:**
- Consumes: `llm.New`, `cfg.LiteLLMURL`, `cfg.LiteLLMAPIKey`.
- Produces: `deps.LLM = llm.New(cfg.LiteLLMURL, cfg.LiteLLMAPIKey)`. `cloudEnabled` computed at boot by querying `{LiteLLMURL}/models` and checking at least one catalog cloud-tier model is present.

- [ ] **Step 1: Replace the Router wiring** in `main.go`:

```go
	llmClient := llm.New(cfg.LiteLLMURL, cfg.LiteLLMAPIKey)
	cloudEnabled := probeCloud(ctx, cfg.LiteLLMURL, cfg.LiteLLMAPIKey)
	if !cloudEnabled {
		// No cloud model reachable -> the pinned cloud translate model is unusable;
		// fall back to a local Norwegian-capable model (same intent as before).
		cfg.TranslateModel = "gemma4:e4b"
	}
```

Replace `deps.LLM: router` with `deps.LLM: llmClient`, and pass `cloudEnabled` into `api.New(...)`.

- [ ] **Step 2: Add `probeCloud`** - a boot health check (best-effort; a failure means "cloud off", never a boot failure):

```go
// probeCloud reports whether the LiteLLM gateway currently serves any cloud-tier
// catalog model. Drives the frontend Turbo gate honestly (vs a static env that
// drifts from reachability). Best-effort: any error -> false.
func probeCloud(ctx context.Context, baseURL, apiKey string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return false
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	for _, m := range body.Data {
		if cm, ok := catalog.Get(m.ID); ok && cm.Tier == "cloud" {
			return true
		}
	}
	return false
}
```

Add imports: `net/http`, `strings`, `encoding/json`, `time`, `saga-api/internal/catalog`.

- [ ] **Step 3: Build**

Run: `cd apps/saga-api && go build ./...`
Expected: PASS (config + main + api all compile).

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go main.go internal/api/server.go
git commit -m "feat(saga-api): point at LiteLLM; boot-probe cloud_enabled from /v1/models"
```

---

## Task 6: Strip `<think>` blocks in CleanMath

**Files:**
- Modify: `internal/summarize/clean.go`
- Modify: `internal/summarize/clean_test.go`

**Interfaces:**
- Produces: `CleanMath` also removes `<think>...</think>` (and a dangling unterminated `<think>` to end-of-text). Belt-and-suspenders: Task 1 disables thinking at the gateway, but a cloud reasoning model could still inline a stray tag.

- [ ] **Step 1: Write the failing test** (add to `clean_test.go`):

```go
func TestCleanMathStripsThink(t *testing.T) {
	in := "<think>let me reason\nstep by step</think>\n# Summary\n\n- point"
	got := CleanMath(in)
	if strings.Contains(got, "reason") || strings.Contains(got, "<think>") {
		t.Errorf("think block not stripped: %q", got)
	}
	if !strings.Contains(got, "# Summary") {
		t.Errorf("summary content lost: %q", got)
	}
}
```

(Add `"strings"` import to the test if missing.)

- [ ] **Step 2: Run to verify fail**

Run: `cd apps/saga-api && go test ./internal/summarize/ -run TestCleanMathStripsThink -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement** - add a think regex + apply it first in `CleanMath`:

```go
// Reasoning models may inline <think>...</think> before the answer. Strip it
// (including an unterminated trailing one) before math normalization.
var thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)
var thinkOpenRe = regexp.MustCompile(`(?s)<think>.*$`)
```

In `CleanMath`, at the top:

```go
func CleanMath(s string) string {
	s = thinkRe.ReplaceAllString(s, "")
	s = thinkOpenRe.ReplaceAllString(s, "")
	s = cmdRe.ReplaceAllStringFunc(s, func(m string) string {
		return mathCmd[m[1:]]
	})
	return wrapRe.ReplaceAllString(s, "$1")
}
```

- [ ] **Step 4: Run to verify pass** (+ the existing clean tests)

Run: `cd apps/saga-api && go test ./internal/summarize/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/summarize/clean.go internal/summarize/clean_test.go
git commit -m "feat(saga-api): strip <think> blocks from model output"
```

---

## Task 7: Worker local capacity 1 -> 2

**Files:**
- Modify: `internal/worker/worker.go`

**Interfaces:**
- Produces: local dispatch runs up to 2 concurrent jobs (two boxes). LiteLLM's per-deployment `max_parallel_requests:1` prevents overloading a single box; the second local job routes to the other box.

- [ ] **Step 1: Edit `worker.go`** - the local slot channel:

```go
	// Two local GPU boxes behind LiteLLM (round-robin); LiteLLM caps each box at
	// 1 in-flight, so 2 concurrent local jobs land one per box. (Was 1 when saga
	// spoke to a single box directly.)
	local := make(chan struct{}, 2)
```

- [ ] **Step 2: Build + run worker tests**

Run: `cd apps/saga-api && go build ./... && TEST_DATABASE_URL=... go test ./internal/worker/ -count=1`
Expected: PASS (worker tests are tier/lease/double-run focused; the cap change does not break them - verify).

- [ ] **Step 3: Commit**

```bash
git add internal/worker/worker.go
git commit -m "feat(saga-api): local worker capacity 2 (both GPU boxes via LiteLLM)"
```

---

## Task 8: Fix the remaining test doubles

**Files:**
- Modify: `internal/api/server_test.go`, `internal/module/ytsummary/ytsummary_test.go`

**Interfaces:**
- Produces: tests build a small `Provider` fake instead of the deleted native `llm.New(url)` httptest server.

- [ ] **Step 1: Add a `Provider` fake** these tests can share (e.g. in each test file or a tiny `internal/llm/llmtest` helper). Minimal shape:

```go
type fakeLLM struct{ text string }

func (f fakeLLM) Chat(_ context.Context, _ , _ string, _ llm.ChatOptions, onToken func(string)) (llm.ChatResult, error) {
	if onToken != nil {
		onToken(f.text)
	}
	return llm.ChatResult{Text: f.text, InputTokens: 5, OutputTokens: 7, WallClock: time.Millisecond}, nil
}
```

Replace the previous `fakeLLM(t, "...")` / `llm.New("http://unused")` constructions in `testServer`/`testServerWithLLM` and the ytsummary test with this fake (it satisfies `llm.Provider`).

- [ ] **Step 2: Build + run the full suite**

Run: `cd apps/saga-api && TEST_DATABASE_URL=... go test ./... -count=1`
Expected: all packages PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/api/server_test.go internal/module/ytsummary/ytsummary_test.go
git commit -m "test(saga-api): Provider fakes replace the native-client test servers"
```

---

## Task 9: E2E against the live gateway (controller-run)

**No committed files** - verification. Run a local saga-api against the LIVE LiteLLM (via `kubectl port-forward svc/litellm 4000` + `LITELLM_URL=http://localhost:4000/v1` + the virtual key) and a throwaway Postgres.

- [ ] **Step 1:** Submit a local-tier job (`qwen3.5:2b`, lang `no`): verify streamed tokens reach the SSE UI, the summary is NON-EMPTY Norwegian (Task 1 think:false working), and `job_runs` records real InputTokens/OutputTokens + wall-clock tok/s.
- [ ] **Step 2:** Submit a cloud-tier job (`deepseek-v4-flash:cloud`): verify content + counts.
- [ ] **Step 3:** Verify `/api/models` `cloud_enabled:true` (probe found cloud models).
- [ ] **Step 4:** Confirm two local jobs run concurrently (both boxes) without error.

---

## Task 10: Deploy cutover (gitops, reversible)

**Files (kongebra-gitops):**
- Modify: `apps/saga-api/base/deployment.yaml` - env
- Modify: saga-api egress NetworkPolicy

**Interfaces:**
- Produces: prod saga-api talks to LiteLLM; egress allows the LiteLLM ClusterIP.

- [ ] **Step 1: Env** - add to the saga-api container:

```yaml
            - name: LITELLM_URL
              value: "http://litellm.litellm.svc:4000/v1"
            - name: LITELLM_API_KEY
              valueFrom: { secretKeyRef: { name: saga-litellm, key: key } }
```

Remove `OLLAMA_URL` and the `OLLAMA_API_KEY` secretKeyRef.

- [ ] **Step 2: Egress netpol** - add an allow to the LiteLLM service (namespaceSelector `litellm` + port 4000). Keep the direct-Ollama + `0.0.0.0/0:443` allows for one deploy as a rollback cushion, then remove in a follow-up once verified.

- [ ] **Step 3: kustomize build + commit + merge + sync + verify**

```bash
kubectl kustomize apps/saga-api/overlays/prod >/dev/null && echo OK
# commit, push, merge to main, prod-gate approve, ArgoCD sync
kubectl -n saga-api-prod rollout status deploy/saga-api
```

- [ ] **Step 4: Live verify on prod** - a real summarize (local + Turbo) via saga.kongebra.no; then remove the now-unused direct-Ollama egress rules in a follow-up commit.

- [ ] **Step 5: Update OPINIONS.md** - the LLM-chokepoint principle: local concurrency is now 2 (both boxes), LiteLLM owns per-box serialization; saga-api no longer holds a global n=1 semaphore.

---

## Self-Review Notes (controller)

- **Task 1 (think:false) is a hard prerequisite** - without it local summaries come back EMPTY (Fase A PoC). Do it + verify content before the saga-api E2E.
- **Task 2+3 run together** (types move from router.go to llm.go on delete) - build only after both.
- **`drop_params:true`** in the LiteLLM config drops `seed` for cloud vendors that reject it (fine - reproducibility is only trusted for local per `worker.go`). It does NOT drop `litellm_params.think` (that is provider config, not a request param).
- **tok/s wall-clock** is intended - do not synthesize EvalDuration.
- **Cutover reversibility** - keep direct-Ollama egress one extra deploy; rollback = revert env.
- **No `platform/litellm/` ingress edits** - that is the `llm.klyngo.no` work's territory; Task 1 touches only `config.yaml`.
