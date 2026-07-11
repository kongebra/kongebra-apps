// Package module defines the Saga module contract. A module is a specialized
// assistant: it takes a JSON input, does its pipeline, and returns Markdown.
// The registry is a plain map filled at startup; a new module is a new Go
// file plus one Register call. ponytail: no plugin system, no dynamic config;
// revisit only if modules ever ship outside this binary.
package module

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"saga-api/internal/llm"
	"saga-api/internal/store"
	"saga-api/internal/ytdlp"
)

// Event is a progress signal streamed to the UI over SSE.
// Token carries streamed LLM output; Stage/Detail carry pipeline state.
type Event struct {
	Stage  string `json:"stage"`
	Detail string `json:"detail,omitempty"`
	Token  string `json:"token,omitempty"`
}

type Deps struct {
	LLM            llm.Provider
	Fetcher        ytdlp.Fetcher
	ChunkTimeout   time.Duration // per-LLM-call budget, not per-job
	TranslateModel string        // pinned cloud (or local fallback) model for "no" translate pass
}

// Result is what a module produces on success: the rendered Markdown plus
// any video metadata worth surfacing on the job (front-page list title, etc).
// Run and Transcript are the eval-store records: the module fills in the
// metrics/timings/langs/tokens it alone has visibility into (everything but
// the job-level fields the worker adds - JobID, Tier, cost); the worker
// writes both to Postgres in the same transaction as job completion.
type Result struct {
	Markdown         string
	VideoTitle       string
	VideoDescription string
	Run              store.Run
	Transcript       store.Transcript
}

type Module interface {
	Name() string
	InputKind() string // "url" now; "text", "file" later
	Run(ctx context.Context, input json.RawMessage, deps Deps, emit func(Event)) (Result, error)
}

var registry = map[string]Module{}

func Register(m Module)              { registry[m.Name()] = m }
func Get(name string) (Module, bool) { m, ok := registry[name]; return m, ok }

func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
