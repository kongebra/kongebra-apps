package llm_test

import (
	"context"
	"strings"
	"testing"

	"saga-api/internal/llm"
)

type fakeProvider struct {
	gotModel string
	gotOpts  llm.ChatOptions
}

func (f *fakeProvider) Chat(_ context.Context, model, _ string, opts llm.ChatOptions, _ func(string)) (llm.ChatResult, error) {
	f.gotModel = model
	f.gotOpts = opts
	return llm.ChatResult{Text: "ok", OutputTokens: 3}, nil
}

func TestRouterSelectsProviderByModel(t *testing.T) {
	local, cloud := &fakeProvider{}, &fakeProvider{}
	r := llm.NewRouter(local, cloud)
	got, err := r.Chat(context.Background(), "deepseek-v4-flash:cloud", "hi", llm.ChatOptions{Temperature: 0.2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "ok" || cloud.gotModel != "deepseek-v4-flash:cloud" {
		t.Fatalf("cloud not selected: %+v", got)
	}
	if local.gotModel != "" {
		t.Fatal("local should not have been called")
	}
	if cloud.gotOpts.Temperature != 0.2 {
		t.Fatal("options not threaded")
	}
}

func TestRouterCloudModelWithoutKeyErrors(t *testing.T) {
	r := llm.NewRouter(&fakeProvider{}, nil) // nil cloud = OLLAMA_API_KEY unset
	_, err := r.Chat(context.Background(), "gpt-oss:120b-cloud", "p", llm.ChatOptions{}, nil)
	if err == nil {
		t.Fatal("want error when cloud model requested with cloud disabled")
	}
	if !strings.Contains(err.Error(), "OLLAMA_API_KEY") {
		t.Errorf("error %q should mention OLLAMA_API_KEY", err)
	}
}

func TestRouterLocalStillWorksWithoutCloud(t *testing.T) {
	local := &fakeProvider{}
	r := llm.NewRouter(local, nil)
	got, err := r.Chat(context.Background(), "gemma4:e4b", "p", llm.ChatOptions{}, nil)
	if err != nil || got.Text != "ok" {
		t.Fatalf("local routing with cloud disabled: got %q err %v", got.Text, err)
	}
	if local.gotModel != "gemma4:e4b" {
		t.Errorf("backend got model %q, want gemma4:e4b", local.gotModel)
	}
}
