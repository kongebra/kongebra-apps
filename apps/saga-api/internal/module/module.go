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
	LLM          *llm.Client
	Fetcher      ytdlp.Fetcher
	ChunkTimeout time.Duration // per-LLM-call budget, not per-job
}

type Module interface {
	Name() string
	InputKind() string // "url" now; "text", "file" later
	Run(ctx context.Context, input json.RawMessage, deps Deps, emit func(Event)) (string, error)
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
