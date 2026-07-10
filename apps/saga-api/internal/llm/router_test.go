package llm

import (
	"context"
	"strings"
	"testing"
)

// stubProvider records the last model it was asked for and returns a fixed tag,
// so a test can assert which backend the Router picked.
type stubProvider struct {
	tag       string
	lastModel string
}

func (s *stubProvider) Chat(_ context.Context, model, _ string, _ func(string)) (string, error) {
	s.lastModel = model
	return s.tag, nil
}

func TestRouterSelectsProviderByModel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string // which backend should handle it
	}{
		{"plain local model", "gemma4:e4b", "local"},
		{"local model with version", "llama3.1:8b", "local"},
		{"cloud colon tag", "qwen3-coder:480b:cloud", "cloud"},
		{"cloud dash tag", "gpt-oss:120b-cloud", "cloud"},
		{"bare cloud suffix", "somemodel:cloud", "cloud"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local := &stubProvider{tag: "local"}
			cloud := &stubProvider{tag: "cloud"}
			r := NewRouter(local, cloud)
			got, err := r.Chat(context.Background(), tt.model, "p", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("model %q routed to %q, want %q", tt.model, got, tt.want)
			}
			// The chosen backend must receive the model verbatim.
			picked := local
			if tt.want == "cloud" {
				picked = cloud
			}
			if picked.lastModel != tt.model {
				t.Errorf("backend got model %q, want %q", picked.lastModel, tt.model)
			}
		})
	}
}

func TestRouterCloudModelWithoutKeyErrors(t *testing.T) {
	r := NewRouter(&stubProvider{tag: "local"}, nil) // nil cloud = OLLAMA_API_KEY unset
	_, err := r.Chat(context.Background(), "gpt-oss:120b-cloud", "p", nil)
	if err == nil {
		t.Fatal("want error when cloud model requested with cloud disabled")
	}
	if !strings.Contains(err.Error(), "OLLAMA_API_KEY") {
		t.Errorf("error %q should mention OLLAMA_API_KEY", err)
	}
}

func TestRouterLocalStillWorksWithoutCloud(t *testing.T) {
	r := NewRouter(&stubProvider{tag: "local"}, nil)
	got, err := r.Chat(context.Background(), "gemma4:e4b", "p", nil)
	if err != nil || got != "local" {
		t.Fatalf("local routing with cloud disabled: got %q err %v", got, err)
	}
}
