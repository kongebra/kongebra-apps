package ytsummary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"saga-api/internal/llm"
	"saga-api/internal/module"
	"saga-api/internal/ytdlp"
)

type fakeFetcher struct {
	video ytdlp.Video
	err   error
}

func (f fakeFetcher) Fetch(ctx context.Context, url, lang string) (ytdlp.Video, error) {
	return f.video, f.err
}

// hangingFetcher simulates a stuck yt-dlp process: it blocks until the
// context passed to Fetch is cancelled, then returns the context's error.
type hangingFetcher struct{}

func (hangingFetcher) Fetch(ctx context.Context, url, lang string) (ytdlp.Video, error) {
	<-ctx.Done()
	return ytdlp.Video{}, ctx.Err()
}

// fakeLLM answers every chat with a canned string, in ollama-native NDJSON
// /api/chat format.
func fakeLLM(t *testing.T, reply string) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintf(w, "{\"message\":{\"content\":%q}}\n", reply)
		fmt.Fprint(w, "{\"done\":true}\n")
	}))
	t.Cleanup(srv.Close)
	return llm.New(srv.URL)
}

func deps(t *testing.T, f ytdlp.Fetcher, reply string) module.Deps {
	return module.Deps{LLM: fakeLLM(t, reply), Fetcher: f, ChunkTimeout: time.Minute}
}

func TestRunSinglePass(t *testing.T) {
	f := fakeFetcher{video: ytdlp.Video{ID: "x", Title: "Short Video", Transcript: "short transcript"}}
	var events []module.Event
	res, err := Module{}.Run(context.Background(),
		json.RawMessage(`{"url":"https://youtube.com/watch?v=x"}`),
		deps(t, f, "the summary"),
		func(e module.Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Markdown, "# Short Video") || !strings.Contains(res.Markdown, "the summary") {
		t.Errorf("markdown: %s", res.Markdown)
	}
	if res.VideoTitle != "Short Video" {
		t.Errorf("video title: %q", res.VideoTitle)
	}
	if events[0].Stage != "fetching" {
		t.Errorf("first event: %+v", events[0])
	}
}

func TestRunMapReduceEmitsChunkProgress(t *testing.T) {
	long := strings.Repeat("word ", 6000) // > singlePassWords -> map-reduce
	f := fakeFetcher{video: ytdlp.Video{ID: "x", Title: "Long Video", Transcript: long}}
	var details []string
	_, err := Module{}.Run(context.Background(),
		json.RawMessage(`{"url":"u"}`),
		deps(t, f, "part"),
		func(e module.Event) {
			if e.Detail != "" {
				details = append(details, e.Detail)
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(details, "|")
	if !strings.Contains(joined, "chunk 1/") || !strings.Contains(joined, "synthesis") {
		t.Errorf("details: %v", details)
	}
}

func TestRunFetchErrorPropagates(t *testing.T) {
	f := fakeFetcher{err: errors.New("no captions available for this video")}
	_, err := Module{}.Run(context.Background(),
		json.RawMessage(`{"url":"u"}`), deps(t, f, "x"), func(module.Event) {})
	if err == nil || !strings.Contains(err.Error(), "no captions") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunFetchTimeout(t *testing.T) {
	d := deps(t, hangingFetcher{}, "x")
	d.ChunkTimeout = 50 * time.Millisecond
	_, err := Module{}.Run(context.Background(),
		json.RawMessage(`{"url":"u"}`), d, func(module.Event) {})
	if err == nil {
		t.Fatal("want error when fetch exceeds ChunkTimeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestRunRejectsEmptyURL(t *testing.T) {
	f := fakeFetcher{}
	_, err := Module{}.Run(context.Background(),
		json.RawMessage(`{}`), deps(t, f, "x"), func(module.Event) {})
	if err == nil {
		t.Fatal("want error for empty url")
	}
}
