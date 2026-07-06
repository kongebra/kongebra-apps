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

	"saga-api/internal/module"
	"saga-api/internal/summarize"
)

const (
	chunkWords      = 2000
	overlapWords    = 200
	singlePassWords = 2500
	defaultModel    = "gemma4:e4b"
)

type input struct {
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Model string `json:"model"`
}

type Module struct{}

func (Module) Name() string      { return "yt-summary" }
func (Module) InputKind() string { return "url" }

func (Module) Run(ctx context.Context, raw json.RawMessage, deps module.Deps, emit func(module.Event)) (string, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if in.URL == "" {
		return "", errors.New("input.url is required")
	}
	if in.Lang == "" {
		in.Lang = "en"
	}
	if in.Model == "" {
		in.Model = defaultModel
	}

	emit(module.Event{Stage: "fetching"})
	fetchCtx, cancel := context.WithTimeout(ctx, deps.ChunkTimeout)
	v, err := deps.Fetcher.Fetch(fetchCtx, in.URL)
	cancel()
	if err != nil {
		return "", err
	}

	chat := func(prompt string, onToken func(string)) (string, error) {
		callCtx, cancel := context.WithTimeout(ctx, deps.ChunkTimeout)
		defer cancel()
		return deps.LLM.Chat(callCtx, in.Model, prompt, onToken)
	}
	streamToken := func(tok string) {
		emit(module.Event{Stage: "summarizing", Token: tok})
	}

	var summary string
	if len(strings.Fields(v.Transcript)) <= singlePassWords {
		emit(module.Event{Stage: "summarizing", Detail: "single pass"})
		summary, err = chat(summarize.SinglePrompt(in.Lang, v.Title, v.Transcript), streamToken)
	} else {
		chunks := summarize.Split(v.Transcript, chunkWords, overlapWords)
		parts := make([]string, 0, len(chunks))
		for i, c := range chunks {
			emit(module.Event{Stage: "summarizing", Detail: fmt.Sprintf("chunk %d/%d", i+1, len(chunks))})
			part, cerr := chat(summarize.MapPrompt(in.Lang, v.Title, c), nil)
			if cerr != nil {
				return "", fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), cerr)
			}
			parts = append(parts, part)
		}
		emit(module.Event{Stage: "summarizing", Detail: "synthesis"})
		summary, err = chat(summarize.ReducePrompt(in.Lang, v.Title, parts), streamToken)
	}
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("# %s\n\n<%s>\n\n%s\n", v.Title, in.URL, summary), nil
}
