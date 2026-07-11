# SAGA: model tiers, cloud provider, conditional translation, eval harness, and frontend redesign

Date: 2026-07-10
Status: draft (pending Svein final spec review; two expert fan-out rounds folded in)
Apps touched: `apps/saga-api` (Go), `apps/saga-web` (TanStack Start)

## 1. Context and goals

SAGA summarizes YouTube transcripts into Markdown.
Today it talks only to a self-hosted Ollama over the tailnet, exposes a raw model dropdown, forces navigation to the job page on submit, has an all-grayscale theme, and persists only the final Markdown.

This change delivers, in one specced sprint (built in the phases of section 12):

1. A two-tier model concept (Lokal / Turbo) plus an Advanced picker with per-model Speed/Precision meters, persisted in `localStorage`.
2. A full frontend redesign: hero, dashboard card grid, detail drawer, no forced navigation, a real accent palette.
3. Ollama Cloud as a second backend, selected per-model, cap sized at boot.
4. Conditional translation: Norwegian-capable models summarize Norwegian directly; English-only local models summarize in English then translate.
5. A durable Postgres eval store plus telemetry, so models can be compared later on byte-identical inputs (replay) with a blind, sound quality signal.

North star: compare models fairly on real jobs.
Every backend and eval decision below is checked against that.

Non-goals (section 11): batch/multi-URL submit, non-Ollama providers, Whisper ASR fallback, chapter-aware chunking, clickable timestamps, auth, multi-rater eval, Elo/significance testing.

## 2. Model catalog

Numbers from the 2026-07-10 benchmark: SAGA's real summary prompt, ~1000-token transcript, Q4_K_M locally, `think:false` for thinking models.
Local tok/s is server-reported generation rate; cloud tok/s is wall-clock (cloud omits `eval_duration`).

Caveat: ratings were measured on a single-pass ~1000-token input.
Real videos over ~2500 words hit map-reduce, where speed and quality differ.
The meter ratings are labeled "short-input" in the UI and in the catalog.

The catalog is owned by the backend (a Go package), exposed via `GET /models`, and consumed by the web app, so the backend default and the web list never drift.
Each entry: `{ id, label, tier: "local"|"cloud", norwegian: bool, speed: 1-4, precision: 1-4, priceInPerMtok, priceOutPerMtok, note }`.
`norwegian` drives conditional translate (section 3); price is null for local.

### Cloud tier (`:cloud` suffix routes here; all Norwegian-capable)

| id | Speed | Precision | tok/s | note |
|---|---|---|---|---|
| deepseek-v4-flash:cloud | 4 | 4 | 184 | Turbo default. Best translator, large context. |
| gemini-3-flash-preview:cloud | 4 | 4 | 176 | Richest structure. Preview (build drifts, see 6.6). |
| kimi-k2.6:cloud | 4 | 4 | 125 | Flawless Norwegian; already on Hermes. |
| kimi-k2.7-code:cloud | 4 | 4 | 116 | Flawless Norwegian despite code tuning. |
| glm-5.2:cloud | 3 | 4 | 87 | Newest, clean. |
| minimax-m3:cloud | 3 | 4 | 69 | Flawless, rich structure. |

### Local tier (free, self-hosted)

| id | norwegian | Speed | Precision | tok/s | note |
|---|---|---|---|---|---|
| qwen3.5:2b | no | 2 | 3 | 76 | Local default. Excellent structured English. |
| qwen3.5:0.8b | no | 3 | 2 | 113 | Fastest local; drops structure, hallucinates. |
| qwen3.5:4b | no | 2 | 3 | 57 | Best local quality. |
| gemma4:e4b | yes | 2 | 3 | 50 | Old default; can do Norwegian directly (translate fallback). |
| minicpm5:fp16 | no | 3 | 2 | 92 | English-only speed option. |

Dropped: `qwen3.5:9b`, `gemma4:31b` cloud, `minicpm5` q8/latest as Norwegian producers (gibberish).

## 3. Pipeline: summarize + conditional translate

Two output languages chosen at submit: English or Norwegian.
`summarizeLang` is decoupled from `targetLang` (today `ytsummary` passes `in.Lang` straight into the prompts; that must change).

Decision per job:

- Target English: summarize in English. No translate.
- Target Norwegian AND model `norwegian: true` (all cloud + `gemma4:e4b`): summarize directly in Norwegian. No translate.
- Target Norwegian AND model `norwegian: false`: summarize in English (`summarizeLang = "en"`), then translate to Norwegian with `TRANSLATE_MODEL`.

Rationale: forcing Norwegian-capable models through English+translate is two lossy hops, double cost, and turns a Norwegian-source video into a no->en->no round-trip.
Translate is only the crutch for English-only local models.

`TRANSLATE_MODEL` default `deepseek-v4-flash:cloud`; the cloud->local (`gemma4:e4b`) fallback when `OLLAMA_API_KEY` is unset is resolved at boot in config, not by catching Router errors mid-pipeline.

The conditional decision lives in the `ytsummary` module (which has the catalog via `Deps`), never in the worker.

Persistence: the English summary and the Norwegian translation are stored per run (6.1), not clobbered.
UI: when translate runs, the drawer shows an "Oversetter ..." inline state after the summary streams, then swaps in the Norwegian result.

## 4. Frontend redesign (`apps/saga-web`)

Stack stays: TanStack Start, React 19, Tailwind v4, shadcn/radix, lucide. Light/dark stays.

### 4.1 Palette (identity)

Theme is currently chroma-zero grayscale. Introduce two reserved accents:
- Brand (Nordic teal) `--brand` ~ `oklch(0.72 0.12 175)` / `oklch(0.78 0.13 175)` dark: wordmark mark, primary button, focus ring, active affordances.
- Turbo (amber) reserved exclusively for the Turbo state ~ `oklch(0.72 0.15 70)` / `oklch(0.82 0.16 75)` dark. Nowhere else.
- Meters/body stay neutral. Status colors become light/dark CSS-var pairs (`--status-*`), not raw single tailwind shades.

### 4.2 Layout

- **Hero:** centered vertical. Large URL input (`max-w-2xl h-14 text-lg rounded-xl`) with submit as an icon button docked inside the field's right end. Tier toggle directly under. Language + `Avansert ▾` as quiet tertiary controls below. Generous breathing room on first visit, collapsing when jobs exist.
- **Dashboard:** job card grid (`sm:grid-cols-2 lg:grid-cols-3`): 16:9 thumbnail top, title (`line-clamp-2`), author, footer status pill + progress/ETA (`tabular-nums`). Hover by elevation, not fill. 5s poll.
- **Empty:** onboarding copy + mark. **Loading:** skeleton cards. **Error:** dedicated state + "Prøv igjen".

### 4.3 Tier toggle + Advanced picker + meters

- Segmented control, sliding raised thumb. `Lokal` = neutral; `Turbo` = amber text + lucide `Zap` + faint amber tint. One-line subtitle per chip (`Lokal - gratis, egen maskin` / `⚡ Turbo - raskere og skarpere, via sky`). Tier defaults carry an "Anbefalt" badge.
- Cloud unavailable (`OLLAMA_API_KEY` unset): Turbo segment disabled + tooltip ("Sky ikke konfigurert"). Enforced server-side too (a cloud model with no key errors clearly), not UI-only.
- `Avansert ▾`: catalog grouped `Lokal` / `Sky`, 3-zone rows: name · one-line `note` (truncated, full in `title`) · two right-aligned meters (fixed width, column-aligned). Selected row: `bg-accent` + brand left border.
- **Meter mark spec:** bars not dots. 4 segments `w-1.5 h-3.5 rounded-[2px]` `gap-[3px]`; filled `bg-foreground/85`, empty `bg-foreground/12` (theme-safe). 12px lucide icon prefix (`Gauge` Speed, `Target` Presisjon). Wrapper `role="img" aria-label="Speed 3 of 4"`; numeric `3/4` shown. One-time legend + tooltip atop the list. `note` is primary; meters are tiebreaker.
- Cloud models get a 12px cloud glyph.

### 4.4 Submit behavior + drawer

- **Optimistic insert mandatory.** On POST success: prepend a `queued` card (returned `id` + pasted URL placeholder), highlight it, clear+refocus the input, reconcile on next poll. No redirect.
- **Drawer, one owner.** The dashboard route owns a Radix `Sheet`. `/jobs/$id` renders the dashboard route with the Sheet open for that id (one component owns drawer + SSE lifecycle); the dashboard renders behind it even on cold deep-load.
  - Opening pushes history (`/jobs/$id`); Back closes and lands on `/`; Escape closes; close returns focus to the originating card. `aria-modal`/focus-trap/scroll-lock from Radix.
  - Nonexistent id on cold load: drawer shows "job not found" + close to `/`; dashboard renders behind.
  - Streaming region: no aggressive `aria-live`.
- **Drawer visual:** right Sheet `w-full sm:max-w-xl`, slide `duration-200`, backdrop `bg-black/40 backdrop-blur-sm`. Sticky header: status pill + title + close. Actions: English/Norsk view toggle, "Åpne på YouTube", copy-Markdown, download `.md`. Body scrolls; Markdown drops `max-w-none` (prose caps ~65ch).

### 4.5 Per-state cards + list

- `queued`: "I kø" + spinner. `running`: progress + ETA + pulsing dot + thin progress bar under thumbnail. `failed`: error snippet + Retry on the card. `done`: duration + model used ("laget med qwen3.5:2b - Lokal").
- List filter chips (Alle / Kjører / Ferdig / Feilet) + title text filter.
- `localStorage` selection validated against the catalog on load; fall back to tier default if the stored model was dropped.

### 4.6 Compare view (eval payoff) - BLIND

- Opens two runs of the **same transcript** side by side.
- **Blind by default:** model identity, cost, and tok/s are HIDDEN; left/right order randomized. The rater picks better / tie; the choice is stored as a pairwise preference (6.3). A reveal toggle shows identities + the run records (model, tokens, tok/s, duration, cost) after rating.
- Reachable from a job's "Re-run with another model" action (6.2) and from history.

## 5. Backend: providers and concurrency (`apps/saga-api`)

Builds on `feat/saga-cloud-provider` (Provider interface, Router by suffix, `NewCloud` bearer, `OLLAMA_CLOUD_URL`/`OLLAMA_API_KEY`), with these changes.

### 5.1 Provider interface carries options and returns metrics

Today `Provider.Chat(ctx, model, prompt, onToken) (string, error)` has no path for temperature/seed in or metrics out.
Change to:
- `ChatOptions{ Temperature, Seed, Think, NumCtx }`
- `ChatResult{ Text, InputTokens, OutputTokens, EvalDuration, LoadDuration, WallClock }`
- `Chat(ctx, model, prompt, opts, onToken) (ChatResult, error)`

This ripples through `Provider`, `Router`, `module.Deps.LLM`, and callers - decide it before the `/api/chat` switch.

### 5.2 Ollama-native `/api/chat`

Switch local and cloud off `/v1/chat/completions` to `/api/chat` (NDJSON; the `done:true` frame carries `eval_count`, `eval_duration`, `prompt_eval_count`, `load_duration`).
Plumb `temperature=0.2`, fixed `seed`, and local `num_ctx` into `options`.
**`sawDone` guard:** track that the `done:true` frame arrived; if the stream ends without it (timeout/cancel/early EOF), return an error and write no run row - otherwise partial/zero metrics get recorded as success.
tok/s rule: `eval_count / (eval_duration>0 ? eval_duration : wallClock)`, guard divide-by-zero. Cloud tok/s is wall-clock (includes prompt-eval + network); noted as such.

### 5.3 Concurrency: acquire-before-claim + fence token (fixes double-run)

The current lease is safe only because one worker never waits on a semaphore while holding a job.
A pool + two semaphores breaks that: a worker that `Claim`s then blocks acquiring a semaphore stops heart-beating, its `lease_at` freezes, `RequeueStale` re-queues it, another worker claims it, and the job double-runs - writing a duplicate `job_runs` row that poisons the eval store.

Fixes:
- **Acquire the tier capacity slot BEFORE `Claim`, not inside `Chat`.** A claimed job is then always actively progressing, so the lease invariant holds and head-of-line starvation disappears.
- **Fence token:** `lease_owner uuid` set at `Claim`; `SetProgress`/`Complete`/`Fail` and the `job_runs` INSERT all carry `AND lease_owner = $owner`, so a rescued-and-reclaimed job's original worker no-ops.
- **Tier-aware claim:** add a `tier` column to `jobs` set at enqueue (from the model's catalog tier, not brittle SQL on the JSON). Claim "oldest queued in a tier with a free slot."
- **Pool shape:** one dispatcher owns `RequeueStale` + claim and feeds N workers over a channel (not N copies of `Run`, which would run `RequeueStale` N times per tick). Start N=4.
- Local semaphore `n=1` (single GPU). Cloud: buffered-channel semaphore sized at boot from `SAGA_CLOUD_CONCURRENCY` (default 3). `// ponytail:` runtime-resizable limiter cut as over-engineering for single-user; revisit if multi-user.
- **`leaseTimeout` derived from `CHUNK_TIMEOUT`** (e.g. `CHUNK_TIMEOUT + margin`), not a hard-coded 20m const that silently breaks when `CHUNK_TIMEOUT` is raised.

### 5.4 Determinism

Local runs pin `temperature=0.2`, fixed `seed`, fixed `num_ctx` - reproducible.
Cloud does not honor `seed` on shared infra - cloud runs are labeled non-reproducible in the run record. Do not claim cloud A/B is seed-stable.

## 6. Durable eval store, replay, quality, cost

Tempo retention is ~7 days; the comparison horizon is longer.
Postgres is the durable source of truth for comparison; OTel (section 7) is live ops. `job_runs.trace_id` bridges them.
Migrations use the existing `db.Migrate` runner (add `0004_transcripts.sql`, `0005_job_runs.sql`).

### 6.1 Schema

- **`transcripts`**: `sha256 PK, text, tokens, chars, lang, source (manual|auto), created_at`. Written at fetch time. (Enables replay - a sha cannot be un-hashed.)
- **`job_runs`** (one row per successful model execution; a job has many via replay): `id, job_id FK, run_group_id, transcript_sha256 FK, model, model_build (nullable, for drifting :preview), tier, prompt_version, target_lang, summarize_lang, translate_model (nullable), reproducible bool, temperature, seed, num_ctx, input_tokens, output_tokens, gen_tok_s, summarize_ms, translate_ms, total_ms, summarize_cost_usd, translate_cost_usd, chunk_count, result_markdown, translated_markdown (nullable), eval_set_tag (nullable), created_at`.
  Index + FK on `job_id`. `jobs` keeps a pointer to the latest/default run for the drawer.
- Aggregation rules for multi-chunk (map-reduce) runs: `output_tokens` = sum over calls; `input_tokens` = sum of `prompt_eval_count`; `summarize_ms` = wall clock of the whole summarize phase; `gen_tok_s` = total output tokens / total generation time.
- Cost: `summarize_cost_usd = priceIn*input_tokens + priceOut*output_tokens` (summary model); `translate_cost_usd` computed separately with the translate model's price. Comparison on the pre-translate English artifact uses `summarize_cost_usd` only.

### 6.2 Replay (key capability)

- Transcript persisted + fingerprinted at fetch.
- "Re-run with another model" loads the stored transcript by sha and skips `ytdlp`, so both models see byte-identical input.
- **N=3 runs** per (model, transcript) by default (nondeterminism, esp. cloud); tagged with one `run_group_id`; compare on median tok/s / cost, quality per run. Configurable.

### 6.3 Quality capture (blind pairwise)

- The Compare view (4.6) is the rating surface: blind, order-randomized, pick better/tie.
- Stored as a pairwise row: `pair_id, left_run_id, right_run_id, preference (left|right|tie), order_shown, rated_artifact (english|norwegian), blind=true, note (nullable), created_at`.
- Win-rate over accumulated pairs is the model signal. A coarse absolute thumbs on a single run stays as an optional secondary.
- `rated_artifact` separates the two eval questions: "best summarizer" rates the English artifact (translate out of the loop; run a dedicated English-target track so every model produces English); "best delivered product" rates the Norwegian.

### 6.4 Comparison hygiene

- Compare enforces same `prompt_version` and same `target_lang`; warns on mismatch. `prompt_version` is a constant in the `summarize` package, bumped when prompts change.
- A fixed **benchmark playlist** (~12-20 videos, stratified by transcript length bucket single-pass/map-reduce and by source language nb/en) tagged via `eval_set_tag` separates curated eval runs from ad-hoc, and turns transcript length from noise into a stratum.

## 7. Telemetry (OpenTelemetry)

OTel SDK, OTLP HTTP to `otel-lgtm.observability:4318` (`OTEL_EXPORTER_OTLP_ENDPOINT`); local dev to local otel-lgtm. This is a deliberate ops-learning goal alongside the durable store, not the comparison substrate.

- **Job span is a new root span started in the worker at claim time** (the POST request's trace context does not survive the async DB hop). Children: `fetch_transcript` -> `summarize` (per-chunk spans, `reduce`) -> `translate` (only when it runs). `job_runs.trace_id` records the trace.
- **Attributes:** `job.id`, `model`, `model_build`, `tier`, `target_lang`, `summarize_lang`, `transcript.chars`, `transcript.tokens`, `transcript.source`, `transcript.lang`, `chunk.count`, `output.tokens`, `gen.tok_per_s`, `prompt.tok_per_s`, `load.ms`, `translate.model`, `translate.ms`, `summarize_cost_usd`, `translate_cost_usd`, `video.id`.
- **Metrics** (instruments created once at boot): histograms `saga.summary.duration{model,tier}`, `saga.summary.tokens_per_second{model}`, `saga.translate.duration{model}`, `saga.transcript.chars`, `saga.chunks`, `saga.cost_usd{model}`; counter `saga.jobs_total{model,tier,status}`.
- **Shutdown:** `main.go` fire-and-forgets the worker today. Add a `WaitGroup`; on SIGTERM order shutdown: cancel ctx -> wait workers -> `tp.Shutdown`/`mp.Shutdown` (flush) -> `pool.Close`, so in-flight spans/metrics flush.

## 8. Configuration

Env: `OLLAMA_URL` (existing), `OLLAMA_CLOUD_URL` (default `https://ollama.com`), `OLLAMA_API_KEY` (empty disables cloud), `SAGA_CLOUD_CONCURRENCY` (default 3), `TRANSLATE_MODEL` (default `deepseek-v4-flash:cloud`), `OTEL_EXPORTER_OTLP_ENDPOINT`.
`OLLAMA_API_KEY` cluster secret added in kongebra-gitops (sealed/external secret) as follow-up.

## 9. Testing

- Go: table tests for the Router (suffix routing, no-key error, local no-auth), the conditional-translate decision (English target; Norwegian+capable; Norwegian+English-only), the boot-sized cloud semaphore, config loading, cost computation (summary vs translate split), and the tok/s / `sawDone` logic.
  **Concurrency test proving the fix:** a slow holder + a blocked claimer must NOT let the job be rescued and double-run (assert a single `job_runs` row via the fence token). `httptest.Server` fakes both `/api/chat` endpoints; assert bearer on cloud, absent on local.
  `go build/test/vet ./...` and `gofmt -l .` clean.
- Web: `tsc --noEmit` clean; tests for the `GET /models` catalog shape, tier->model mapping, `localStorage` round-trip + stale-model fallback, the drawer routing contract (open/close/back/deep-load), and the blind Compare (identities hidden pre-rating, order randomized).
- End-to-end: run a job on each tier locally; confirm summary renders, translate animation shows only when expected, one `job_runs` row lands with real numbers, replay reuses the transcript (same `transcript_sha256`), a blind pairwise rating persists, and telemetry spans reach the local otel-lgtm.

## 10. (reserved)

## 11. Out of scope (YAGNI)

Batch/multi-URL/playlist submit; non-Ollama providers (seam supports one, none built); Whisper ASR for missing captions; chapter-aware or multi-level-reduce chunking; clickable video timestamps (but per-segment timestamps are retained at parse so it is not foreclosed); auth (tailnet-only); multi-rater agreement, Elo/Bradley-Terry, significance testing, seed sweeps; runtime-resizable cloud limiter; cost dashboard beyond a simple cumulative figure; optional LLM-judge pre-ranking (P3, later).

## 12. Build sequencing (de-risk; specced together, built in order)

The risk is a large multi-surface change where the open-ended redesign drags the well-defined capability.

1. **Backend, headless.** Provider interface change -> `/api/chat` + metric capture -> `transcripts` + `job_runs` + fence token + acquire-before-claim + dispatcher/pool -> cloud provider finalize -> conditional translate -> replay. Verify by curl/E2E: a run row lands with real numbers per tier; replay reuses the transcript (same sha); no double-run under the concurrency test. Value is provable with zero UI.
2. **Minimal frontend for the capability.** Tier toggle + restyled existing picker + a bare blind Compare view over run records. The north star lands here.
3. **Redesign polish (timeboxed).** Palette, hero, card grid, drawer, meters, optimistic insert, per-state cards, filters, states. Purely visual; cannot break the capability; iterate to taste.
4. Telemetry (OTel) folded in during phase 1 backend where cheap, its dashboards after.

## 13. Open decisions

- Worker pool size: start 4, tune from telemetry.
- `minicpm5:fp16` shown in Advanced at launch or hidden behind an "English-only" note.
- Meter thresholds (tok/s -> 1-4) live in the catalog, tune freely.
- N (repeat runs) default 3, configurable.
