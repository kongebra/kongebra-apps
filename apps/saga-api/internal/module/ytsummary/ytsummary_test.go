package ytsummary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"saga-api/internal/llm"
	"saga-api/internal/module"
	"saga-api/internal/store"
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

// cannedLLM is a Provider fake: every Chat streams then returns a canned reply.
type cannedLLM struct{ reply string }

func (c cannedLLM) Chat(_ context.Context, _, _ string, _ llm.ChatOptions, onToken func(string)) (llm.ChatResult, error) {
	if onToken != nil && c.reply != "" {
		onToken(c.reply)
	}
	return llm.ChatResult{Text: c.reply, InputTokens: 5, OutputTokens: 7, WallClock: time.Millisecond}, nil
}

// fakeLLM returns a Provider that answers every chat with reply.
func fakeLLM(t *testing.T, reply string) llm.Provider {
	t.Helper()
	return cannedLLM{reply: reply}
}

func deps(t *testing.T, f ytdlp.Fetcher, reply string) module.Deps {
	return module.Deps{LLM: fakeLLM(t, reply), Fetcher: f, ChunkTimeout: time.Minute}
}

// llmCall records one Chat invocation's model and prompt, so a test can
// assert which model saw which prompt without decoding an HTTP wire format.
type llmCall struct {
	model  string
	prompt string
}

// recordingLLM is a fake llm.Provider that answers every Chat call with a
// canned reply while recording the (model, prompt) pair.
type recordingLLM struct {
	reply string
	calls []llmCall
}

func (r *recordingLLM) Chat(ctx context.Context, model, prompt string, opts llm.ChatOptions, onToken func(string)) (llm.ChatResult, error) {
	r.calls = append(r.calls, llmCall{model: model, prompt: prompt})
	if onToken != nil {
		onToken(r.reply)
	}
	return llm.ChatResult{
		Text:         r.reply,
		InputTokens:  10,
		OutputTokens: 20,
		EvalDuration: 50 * time.Millisecond,
		WallClock:    60 * time.Millisecond,
	}, nil
}

func TestRunConditionalTranslate(t *testing.T) {
	const translateModel = "deepseek-v4-flash:cloud"

	cases := []struct {
		name              string
		lang              string
		model             string
		wantSummarizeWord string // word from langName() expected in the summarize prompt
		wantTranslate     bool
	}{
		{name: "english target never translates", lang: "en", model: "qwen3.5:2b", wantSummarizeWord: "English", wantTranslate: false},
		{name: "norwegian-capable model summarizes directly", lang: "no", model: "gemma4:e4b", wantSummarizeWord: "Norwegian", wantTranslate: false},
		{name: "english-only model summarizes in english then translates", lang: "no", model: "qwen3.5:2b", wantSummarizeWord: "English", wantTranslate: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fakeFetcher{video: ytdlp.Video{ID: "x", Title: "Video", Transcript: "short transcript"}}
			rec := &recordingLLM{reply: "the summary"}
			d := module.Deps{LLM: rec, Fetcher: f, ChunkTimeout: time.Minute, TranslateModel: translateModel}
			raw := json.RawMessage(fmt.Sprintf(`{"url":"u","lang":%q,"model":%q}`, tc.lang, tc.model))

			_, err := Module{}.Run(context.Background(), raw, d, func(module.Event) {})
			if err != nil {
				t.Fatal(err)
			}

			if len(rec.calls) == 0 {
				t.Fatal("expected at least one LLM call")
			}
			summarizeCall := rec.calls[0]
			if summarizeCall.model != tc.model {
				t.Errorf("summarize call model = %q, want %q", summarizeCall.model, tc.model)
			}
			if !strings.Contains(summarizeCall.prompt, tc.wantSummarizeWord) {
				t.Errorf("summarize prompt = %q, want it to contain %q", summarizeCall.prompt, tc.wantSummarizeWord)
			}

			if tc.wantTranslate {
				if len(rec.calls) != 2 {
					t.Fatalf("want 2 LLM calls (summarize + translate), got %d", len(rec.calls))
				}
				translateCall := rec.calls[1]
				if translateCall.model != translateModel {
					t.Errorf("translate call model = %q, want %q", translateCall.model, translateModel)
				}
				if !strings.Contains(translateCall.prompt, "Translate") {
					t.Errorf("translate prompt = %q, want it to look like a translate prompt", translateCall.prompt)
				}
			} else if len(rec.calls) != 1 {
				t.Errorf("want no translate call, got %d total calls: %+v", len(rec.calls), rec.calls)
			}
		})
	}
}

// TestRunMetrics proves the module accumulates per-run eval-store metrics
// (job_runs, written by the worker in a later step) instead of discarding
// llm.ChatResult token/timing data after building the markdown.
func TestRunMetrics(t *testing.T) {
	f := fakeFetcher{video: ytdlp.Video{ID: "x", Title: "Video", Transcript: "short transcript"}}
	rec := &recordingLLM{reply: "the summary"}
	d := module.Deps{LLM: rec, Fetcher: f, ChunkTimeout: time.Minute}

	res, err := Module{}.Run(context.Background(),
		json.RawMessage(`{"url":"u"}`), d, func(module.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Run.OutputTokens <= 0 {
		t.Errorf("Run.OutputTokens = %d, want > 0", res.Run.OutputTokens)
	}
	if res.Run.ChunkCount < 1 {
		t.Errorf("Run.ChunkCount = %d, want >= 1", res.Run.ChunkCount)
	}
	if res.Transcript.Sha256 == "" {
		t.Error("Transcript.Sha256 is empty, want a content hash")
	}
	if want := len("short transcript"); res.Transcript.Chars != want {
		t.Errorf("Transcript.Chars = %d, want %d", res.Transcript.Chars, want)
	}
}

// poisonFetcher fails the test if Fetch is ever called - used to prove a
// replay (transcript_sha set) never re-fetches.
type poisonFetcher struct{ t *testing.T }

func (p poisonFetcher) Fetch(ctx context.Context, url, lang string) (ytdlp.Video, error) {
	p.t.Fatal("Fetch called on a replay - transcript_sha should have skipped it")
	return ytdlp.Video{}, nil
}

func TestRunReplayReusesStoredTranscriptSkipsFetch(t *testing.T) {
	const text = "the original transcript text"
	sha := store.Sha256(text)
	rec := &recordingLLM{reply: "the summary"}
	d := module.Deps{
		LLM:     rec,
		Fetcher: poisonFetcher{t: t},
		Transcripts: func(ctx context.Context, s string) (*store.Transcript, error) {
			if s != sha {
				t.Fatalf("Transcripts called with %q, want %q", s, sha)
			}
			return &store.Transcript{Sha256: sha, Text: text}, nil
		},
		ChunkTimeout: time.Minute,
	}

	res, err := Module{}.Run(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"url":"u","model":"m","transcript_sha":%q,"run_group":"42"}`, sha)),
		d, func(module.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transcript.Sha256 != sha {
		t.Errorf("Transcript.Sha256 = %q, want %q (same text -> same hash)", res.Transcript.Sha256, sha)
	}
	if res.Run.RunGroupID != "42" {
		t.Errorf("Run.RunGroupID = %q, want 42", res.Run.RunGroupID)
	}
	if len(rec.calls) == 0 || !strings.Contains(rec.calls[0].prompt, text) {
		t.Errorf("summarize prompt did not carry the reused transcript text: %+v", rec.calls)
	}
}

func TestRunReplayMissingTranscriptErrors(t *testing.T) {
	d := module.Deps{
		LLM:     &recordingLLM{reply: "x"},
		Fetcher: poisonFetcher{t: t},
		Transcripts: func(ctx context.Context, s string) (*store.Transcript, error) {
			return nil, nil
		},
		ChunkTimeout: time.Minute,
	}
	_, err := Module{}.Run(context.Background(),
		json.RawMessage(`{"url":"u","transcript_sha":"deadbeef"}`), d, func(module.Event) {})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want a not-found error", err)
	}
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
