// Package ytsummary is Saga module #1: YouTube URL in, Markdown summary out.
// Short transcripts get one LLM pass; long ones get map-reduce, and the
// llm.Client semaphore is released between every call, which is the
// preemption point the platform's concurrency design relies on.
package ytsummary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"saga-api/internal/catalog"
	"saga-api/internal/llm"
	"saga-api/internal/module"
	"saga-api/internal/store"
	"saga-api/internal/summarize"
	"saga-api/internal/ytdlp"
)

const (
	chunkWords      = 2000
	overlapWords    = 200
	singlePassWords = 2500
	defaultModel    = "gemma4:e4b"
	// fixedSeed pins Ollama's sampling seed for every call this module makes.
	// A fixed (not random-per-job) seed is what makes a "local" tier run
	// reproducible (Run.Reproducible), and recording the same value we pass
	// keeps job_runs.seed truthful.
	fixedSeed = 42
)

type input struct {
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Model string `json:"model"`
	// TranscriptSha, when set, is a replay: skip Fetcher entirely and load
	// this exact stored transcript, so the two runs being compared see
	// byte-identical input instead of a fresh (possibly-drifted) fetch.
	TranscriptSha string `json:"transcript_sha,omitempty"`
	// RunGroup ties a replay's job_runs row back to the original job it
	// replays, so the eval store can group A/B runs together.
	RunGroup string `json:"run_group,omitempty"`
}

type Module struct{}

func (Module) Name() string      { return "yt-summary" }
func (Module) InputKind() string { return "url" }

func (Module) Run(ctx context.Context, raw json.RawMessage, deps module.Deps, emit func(module.Event)) (module.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return module.Result{}, fmt.Errorf("parse input: %w", err)
	}
	if in.URL == "" {
		return module.Result{}, errors.New("input.url is required")
	}
	if in.Lang == "" {
		in.Lang = "en"
	}
	if in.Model == "" {
		in.Model = defaultModel
	}

	// Norwegian-capable models summarize in Norwegian directly. English-only
	// models summarize in English, then a pinned cloud model translates -
	// English target never translates.
	targetLang := in.Lang
	summarizeLang := targetLang
	needTranslate := targetLang == "no" && !catalog.IsNorwegian(in.Model)
	if needTranslate {
		summarizeLang = "en"
	}

	emit(module.Event{Stage: "fetching"})
	var v ytdlp.Video
	var err error
	if in.TranscriptSha != "" {
		// Replay: reuse the exact transcript the original run recorded
		// instead of re-fetching. Title is left blank - refetching metadata
		// just to get it back would defeat the point of skipping the fetch.
		tr, terr := deps.Transcripts(ctx, in.TranscriptSha)
		if terr != nil {
			return module.Result{}, fmt.Errorf("load replay transcript %s: %w", in.TranscriptSha, terr)
		}
		if tr == nil {
			return module.Result{}, fmt.Errorf("replay transcript %s not found", in.TranscriptSha)
		}
		v = ytdlp.Video{Transcript: tr.Text}
	} else {
		fetchCtx, cancel := context.WithTimeout(ctx, deps.ChunkTimeout)
		v, err = deps.Fetcher.Fetch(fetchCtx, in.URL, in.Lang)
		cancel()
		if err != nil {
			return module.Result{}, err
		}
	}

	// Accumulated across every summarize-stage LLM call (single-pass = 1 call;
	// map-reduce = N map calls + 1 reduce call), so job_runs records the true
	// cost/throughput of producing the summary, not just its last call.
	var inTok, outTok int
	var evalDur, wallDur time.Duration
	chat := func(prompt string, onToken func(string)) (string, error) {
		callCtx, cancel := context.WithTimeout(ctx, deps.ChunkTimeout)
		defer cancel()
		res, err := deps.LLM.Chat(callCtx, in.Model, prompt, llm.ChatOptions{Temperature: 0.2, Seed: fixedSeed}, onToken)
		inTok += res.InputTokens
		outTok += res.OutputTokens
		evalDur += res.EvalDuration
		wallDur += res.WallClock
		return res.Text, err
	}
	streamToken := func(tok string) {
		emit(module.Event{Stage: "summarizing", Token: tok})
	}

	summarizeStart := time.Now()
	var summary string
	var chunkCount int
	if len(strings.Fields(v.Transcript)) <= singlePassWords {
		chunkCount = 1
		emit(module.Event{Stage: "summarizing", Detail: "single pass"})
		summary, err = chat(summarize.SinglePrompt(summarizeLang, v.Title, v.Transcript), streamToken)
	} else {
		chunks := summarize.Split(v.Transcript, chunkWords, overlapWords)
		chunkCount = len(chunks)
		parts := make([]string, 0, len(chunks))
		for i, c := range chunks {
			emit(module.Event{Stage: "summarizing", Detail: fmt.Sprintf("chunk %d/%d", i+1, len(chunks))})
			part, cerr := chat(summarize.MapPrompt(summarizeLang, v.Title, c), nil)
			if cerr != nil {
				return module.Result{}, fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), cerr)
			}
			parts = append(parts, part)
		}
		emit(module.Event{Stage: "summarizing", Detail: "synthesis"})
		summary, err = chat(summarize.ReducePrompt(summarizeLang, v.Title, parts), streamToken)
	}
	if err != nil {
		return module.Result{}, err
	}
	summarizeMs := time.Since(summarizeStart)
	summary = summarize.CleanMath(summary)

	run := store.Run{
		RunGroupID:     in.RunGroup,
		Model:          in.Model,
		SummarizeLang:  summarizeLang,
		TargetLang:     targetLang,
		PromptVersion:  summarize.PromptVersion,
		ChunkCount:     chunkCount,
		ResultMarkdown: summary,
		Temperature:    0.2,
		Seed:           fixedSeed,
		InputTokens:    inTok,
		OutputTokens:   outTok,
		SummarizeMs:    int(summarizeMs.Milliseconds()),
	}
	// tok/s prefers Ollama's own eval_duration (excludes network/queueing
	// overhead); falls back to measured wall time when the backend omits it
	// (Ollama Cloud never reports eval_duration - see llm.ChatResult).
	if genSecs := evalDur.Seconds(); genSecs > 0 {
		run.GenTokS = float64(outTok) / genSecs
	} else if genSecs = wallDur.Seconds(); genSecs > 0 {
		run.GenTokS = float64(outTok) / genSecs
	}

	if needTranslate {
		emit(module.Event{Stage: "translating"})
		translateStart := time.Now()
		translateCtx, cancel := context.WithTimeout(ctx, deps.ChunkTimeout)
		tr, terr := deps.LLM.Chat(translateCtx, deps.TranslateModel, summarize.TranslatePrompt("no", summary), llm.ChatOptions{Temperature: 0.2, Seed: fixedSeed}, nil)
		cancel()
		if terr != nil {
			return module.Result{}, fmt.Errorf("translate: %w", terr)
		}
		translated := summarize.CleanMath(tr.Text)
		run.TranslateModel = deps.TranslateModel
		run.TranslateMs = int(time.Since(translateStart).Milliseconds())
		run.TranslatedMarkdown = translated
		summary = translated
	}

	transcript := store.Transcript{
		Text:   v.Transcript,
		Sha256: store.Sha256(v.Transcript),
		Chars:  len(v.Transcript),
		// Tokens is left 0 - no tokenizer is wired up here. ytdlp.Video does
		// not currently expose caption language/source, so Lang/Source are
		// left empty rather than invented.
	}
	run.TranscriptSha256 = transcript.Sha256

	return module.Result{
		Markdown:         fmt.Sprintf("# %s\n\n<%s>\n\n%s\n", v.Title, in.URL, summary),
		VideoTitle:       v.Title,
		VideoDescription: v.Description,
		Run:              run,
		Transcript:       transcript,
	}, nil
}
