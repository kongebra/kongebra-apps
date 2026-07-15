package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer streams OpenAI-style chat.completion.chunk frames, then a usage
// frame (choices empty), then [DONE].
func sseServer(t *testing.T, tokens []string, promptTok, completionTok int) *httptest.Server {
	t.Helper()
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		// no [DONE]
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1", "")
	if _, err := c.Chat(context.Background(), "m", "hi", ChatOptions{}, nil); err == nil {
		t.Fatal("expected error when stream ends before [DONE]")
	}
}
