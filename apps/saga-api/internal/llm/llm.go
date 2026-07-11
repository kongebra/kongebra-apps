// Package llm speaks ollama-native chat streaming (POST /api/chat, NDJSON
// frames) to an Ollama backend. Both the local GPU and Ollama Cloud expose
// the same wire shape, so one Client serves both: they differ only by base
// URL, an optional bearer key, and whether calls are serialized. The local
// box is one GPU, one model loaded, so its Client carries an n=1 semaphore
// that serializes calls app-wide; callers release it between map-reduce
// chunks, which is what lets a future interactive module cut in line at
// chunk boundaries. Cloud calls take no semaphore (cloud runs many at once).
// A Router (router.go) picks the Client per model.
// ponytail: hand-rolled ollama-native client (~100 lines) instead of an SDK;
// swap for an SDK if we ever need tools/images/logprobs.
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
	baseURL string
	bearer  string // Authorization: Bearer <bearer>; empty = no auth (local)
	httpc   *http.Client
	sem     chan struct{} // nil = no app-wide serialization (cloud)
}

// New builds a client for the local single-GPU Ollama: no auth header, and an
// n=1 semaphore that serializes every call app-wide.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpc:   &http.Client{}, // per-call deadline comes from ctx
		sem:     make(chan struct{}, 1),
	}
}

// NewCloud builds a client for Ollama Cloud: bearer auth, and NO semaphore -
// cloud runs many calls concurrently, so serializing it would needlessly block
// cloud work behind a local job (the whole point of adding cloud).
func NewCloud(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		bearer:  apiKey,
		httpc:   &http.Client{},
	}
}

// apiOptions is the "options" object in the ollama /api/chat request body.
type apiOptions struct {
	Temperature float64 `json:"temperature"`
	Seed        int     `json:"seed"`
	NumCtx      int     `json:"num_ctx,omitempty"`
}

// Chat sends one user prompt, streams tokens to onToken (nil ok), returns
// the assembled response plus token/duration metrics. Cancellation/timeout
// via ctx.
func (c *Client) Chat(ctx context.Context, model, prompt string, opts ChatOptions, onToken func(string)) (ChatResult, error) {
	if c.sem != nil {
		select {
		case c.sem <- struct{}{}:
			defer func() { <-c.sem }()
		case <-ctx.Done():
			return ChatResult{}, ctx.Err()
		}
	}

	body, err := json.Marshal(map[string]any{
		"model":    model,
		"stream":   true,
		"think":    opts.Think,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
		"options":  apiOptions{Temperature: opts.Temperature, Seed: opts.Seed, NumCtx: opts.NumCtx},
	})
	if err != nil {
		return ChatResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ChatResult{}, fmt.Errorf("ollama: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

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
}
