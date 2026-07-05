package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOllama streams two tokens in OpenAI SSE format.
func fakeOllama(inflight *atomic.Int32, delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if n := inflight.Add(1); n > 1 {
			http.Error(w, fmt.Sprintf("overlap: %d in flight", n), 500)
			inflight.Add(-1)
			return
		}
		defer inflight.Add(-1)
		time.Sleep(delay)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

func TestChatStreamsAndAssembles(t *testing.T) {
	var inflight atomic.Int32
	srv := httptest.NewServer(fakeOllama(&inflight, 0))
	defer srv.Close()
	c := New(srv.URL)
	var tokens []string
	got, err := c.Chat(context.Background(), "m", "p", func(tok string) { tokens = append(tokens, tok) })
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("got %q", got)
	}
	if strings.Join(tokens, "") != "hello" {
		t.Errorf("tokens %v", tokens)
	}
}

func TestChatSerializesConcurrentCalls(t *testing.T) {
	var inflight atomic.Int32
	srv := httptest.NewServer(fakeOllama(&inflight, 50*time.Millisecond))
	defer srv.Close()
	c := New(srv.URL)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := c.Chat(context.Background(), "m", "p", nil)
			errs <- err
		}()
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("call %d: %v (semaphore let calls overlap)", i, err)
		}
	}
}

func TestChatErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", 404)
	}))
	defer srv.Close()
	c := New(srv.URL)
	_, err := c.Chat(context.Background(), "m", "p", nil)
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v", err)
	}
}
