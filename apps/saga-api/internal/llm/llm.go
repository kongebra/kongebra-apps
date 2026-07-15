// Package llm speaks the OpenAI-compatible chat-completions streaming API to
// the in-cluster LiteLLM gateway, which fans out to the two Ollama boxes
// (round-robin) and Ollama Cloud. One Client serves every model - the gateway
// routes by model name. tok/s is wall-clock: the OpenAI wire carries token
// counts (via stream_options.include_usage) but not Ollama's native
// eval_duration, so ChatResult.EvalDuration stays 0 and callers fall back to
// WallClock. Local-GPU serialization moved into LiteLLM (max_parallel_requests
// per deployment), so this Client carries no app-wide semaphore.
// ponytail: hand-rolled SSE parse instead of an SDK; swap for the openai-go SDK
// if we ever need tools/images/logprobs.
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

// DefaultTemperature is the temperature every summarize/translate call sends,
// so the value recorded in job_runs.temperature can never drift from what
// was actually sent to the model.
const DefaultTemperature = 0.2

// ChatOptions are the per-call knobs. Think/NumCtx are Ollama-native and do not
// survive the OpenAI boundary; they are unused (thinking is disabled at the
// gateway via litellm_params). Kept in the struct so callers/records are stable.
type ChatOptions struct {
	Temperature float64
	Seed        int
	Think       bool
	NumCtx      int
}

// ChatResult is one completed chat: the text plus the metrics needed for the
// eval store. EvalDuration is always 0 over the OpenAI wire (no native timing);
// tok/s uses WallClock as the denominator (see ytsummary.go).
type ChatResult struct {
	Text         string
	InputTokens  int
	OutputTokens int
	EvalDuration time.Duration
	LoadDuration time.Duration
	WallClock    time.Duration
}

// Provider is the seam every LLM backend implements. Modules call this; the
// concrete backend is the LiteLLM client below.
type Provider interface {
	Chat(ctx context.Context, model, prompt string, opts ChatOptions, onToken func(string)) (ChatResult, error)
}

type Client struct {
	baseURL string // includes /v1
	apiKey  string
	httpc   *http.Client
}

// New builds a client for the LiteLLM gateway. baseURL includes the /v1 suffix;
// apiKey is the virtual key (empty allowed for an unauthenticated test server).
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
	// EvalDuration/LoadDuration intentionally 0: not carried by the OpenAI wire.
	return res, nil
}
