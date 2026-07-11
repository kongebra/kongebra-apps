package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOllama streams two tokens in ollama-native NDJSON /api/chat format.
func fakeOllama(inflight *atomic.Int32, delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if n := inflight.Add(1); n > 1 {
			http.Error(w, fmt.Sprintf("overlap: %d in flight", n), 500)
			inflight.Add(-1)
			return
		}
		defer inflight.Add(-1)
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, `{"message":{"content":"hel"}}`+"\n")
		fmt.Fprint(w, `{"message":{"content":"lo"}}`+"\n")
		fmt.Fprint(w, `{"done":true}`+"\n")
	}
}

func TestChatStreamsAndAssembles(t *testing.T) {
	var inflight atomic.Int32
	srv := httptest.NewServer(fakeOllama(&inflight, 0))
	defer srv.Close()
	c := New(srv.URL)
	var tokens []string
	got, err := c.Chat(context.Background(), "m", "p", ChatOptions{}, func(tok string) { tokens = append(tokens, tok) })
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "hello" {
		t.Errorf("got %q", got.Text)
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
			_, err := c.Chat(context.Background(), "m", "p", ChatOptions{}, nil)
			errs <- err
		}()
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("call %d: %v (semaphore let calls overlap)", i, err)
		}
	}
}

func TestCloudSendsBearerAndDoesNotSerialize(t *testing.T) {
	var gotAuth string
	var inflight atomic.Int32
	var maxInflight atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		n := inflight.Add(1)
		for {
			m := maxInflight.Load()
			if n <= m || maxInflight.CompareAndSwap(m, n) {
				break
			}
		}
		defer inflight.Add(-1)
		time.Sleep(30 * time.Millisecond) // hold the connection so calls overlap
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, `{"message":{"content":"hi"}}`+"\n")
		fmt.Fprint(w, `{"done":true}`+"\n")
	}))
	defer srv.Close()

	c := NewCloud(srv.URL, "secret-key")
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			_, err := c.Chat(context.Background(), "gpt-oss:120b-cloud", "p", ChatOptions{}, nil)
			errs <- err
		}()
	}
	for i := 0; i < 3; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("cloud call %d: %v", i, err)
		}
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret-key")
	}
	if maxInflight.Load() < 2 {
		t.Errorf("max concurrent cloud calls = %d, want >= 2 (cloud must not serialize)", maxInflight.Load())
	}
}

func TestLocalSendsNoAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, `{"done":true}`+"\n")
	}))
	defer srv.Close()
	if _, err := New(srv.URL).Chat(context.Background(), "m", "p", ChatOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Errorf("local set Authorization = %q, want none", gotAuth)
	}
}

func TestChatErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", 404)
	}))
	defer srv.Close()
	c := New(srv.URL)
	_, err := c.Chat(context.Background(), "m", "p", ChatOptions{}, nil)
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestChatParsesApiChatMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("local must not send auth, got %q", got)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, `{"message":{"content":"hel"}}`+"\n")
		io.WriteString(w, `{"message":{"content":"lo"}}`+"\n")
		io.WriteString(w, `{"done":true,"eval_count":2,"eval_duration":1000000000,"prompt_eval_count":5,"load_duration":500000000}`+"\n")
	}))
	defer srv.Close()

	var toks []string
	res, err := New(srv.URL).Chat(context.Background(), "m", "hi",
		ChatOptions{Temperature: 0.2, Seed: 7}, func(s string) { toks = append(toks, s) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.OutputTokens != 2 || res.InputTokens != 5 {
		t.Fatalf("tokens = %d/%d", res.OutputTokens, res.InputTokens)
	}
	if res.EvalDuration != time.Second {
		t.Fatalf("eval_duration = %v", res.EvalDuration)
	}
	if len(toks) != 2 {
		t.Fatalf("streamed %d tokens", len(toks))
	}
}

func TestChatErrorsWithoutDoneFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"message":{"content":"partial"}}`+"\n") // stream ends, no done
	}))
	defer srv.Close()
	_, err := New(srv.URL).Chat(context.Background(), "m", "hi", ChatOptions{}, nil)
	if err == nil {
		t.Fatal("expected error when stream ends without done frame")
	}
}
