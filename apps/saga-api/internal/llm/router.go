package llm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultTemperature is the temperature every summarize/translate call sends,
// so the value recorded in job_runs.temperature can never drift from what
// was actually sent to the model.
const DefaultTemperature = 0.2

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

// Provider is the seam every LLM backend implements. It is deliberately the
// same one method the modules already call, so a third backend (OpenAI,
// Anthropic, ...) is just a new type with this method plus a line in the
// Router's selection - no changes to callers.
type Provider interface {
	Chat(ctx context.Context, model, prompt string, opts ChatOptions, onToken func(string)) (ChatResult, error)
}

// Router is a Provider that dispatches by model name: a cloud tag routes to
// the cloud backend, everything else to the local GPU.
// ponytail: selection is a suffix check, not a model registry. When a third
// provider lands, turn selectFor into a small ordered list of (match, provider)
// rules; the interface and callers stay put.
type Router struct {
	local Provider
	cloud Provider // nil when OLLAMA_API_KEY is unset (cloud disabled)
}

// NewRouter wires the local provider (required) and cloud provider (may be nil
// when no API key is configured).
func NewRouter(local, cloud Provider) *Router {
	return &Router{local: local, cloud: cloud}
}

func (r *Router) Chat(ctx context.Context, model, prompt string, opts ChatOptions, onToken func(string)) (ChatResult, error) {
	if isCloudModel(model) {
		if r.cloud == nil {
			return ChatResult{}, fmt.Errorf("llm: model %q needs Ollama Cloud but OLLAMA_API_KEY is not set", model)
		}
		return r.cloud.Chat(ctx, model, prompt, opts, onToken)
	}
	return r.local.Chat(ctx, model, prompt, opts, onToken)
}

// isCloudModel reports whether a model tag targets Ollama Cloud. Cloud tags
// look like "<model>:cloud" or "gpt-oss:120b-cloud".
func isCloudModel(model string) bool {
	return strings.HasSuffix(model, ":cloud") || strings.HasSuffix(model, "-cloud")
}
