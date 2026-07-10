// Package llm speaks OpenAI-compatible chat streaming to an Ollama backend.
// Both the local GPU and Ollama Cloud expose the same wire shape, so one
// Client serves both: they differ only by base URL, an optional bearer key,
// and whether calls are serialized. The local box is one GPU, one model
// loaded, so its Client carries an n=1 semaphore that serializes calls
// app-wide; callers release it between map-reduce chunks, which is what lets
// a future interactive module cut in line at chunk boundaries. Cloud calls
// take no semaphore (cloud runs many at once). A Router (router.go) picks the
// Client per model.
// ponytail: hand-rolled OpenAI-compat client (~80 lines) instead of an SDK;
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

// Chat sends one user prompt, streams tokens to onToken (nil ok), returns
// the assembled response. Cancellation/timeout via ctx.
func (c *Client) Chat(ctx context.Context, model, prompt string, onToken func(string)) (string, error) {
	if c.sem != nil {
		select {
		case c.sem <- struct{}{}:
			defer func() { <-c.sem }()
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	body, err := json.Marshal(map[string]any{
		"model":  model,
		"stream": true,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ollama: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	var sb strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // tolerate keep-alive/comment lines
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			tok := chunk.Choices[0].Delta.Content
			sb.WriteString(tok)
			if onToken != nil {
				onToken(tok)
			}
		}
	}
	return sb.String(), sc.Err()
}
