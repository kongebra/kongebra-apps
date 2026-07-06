package summarize

import (
	"strings"
	"testing"
)

func TestPromptsCarryLanguageAndTitle(t *testing.T) {
	p := SinglePrompt("no", "My Video", "transcript here")
	if !strings.Contains(p, "Norwegian") || !strings.Contains(p, "My Video") {
		t.Errorf("single prompt missing lang/title: %s", p)
	}
	m := MapPrompt("en", "My Video", "chunk text")
	if !strings.Contains(m, "English") || !strings.Contains(m, "chunk text") {
		t.Errorf("map prompt: %s", m)
	}
	r := ReducePrompt("no", "My Video", []string{"part a", "part b"})
	if !strings.Contains(r, "part a") || !strings.Contains(r, "part b") || !strings.Contains(r, "Norwegian") {
		t.Errorf("reduce prompt: %s", r)
	}
}

func TestTranslatePrompt(t *testing.T) {
	p := TranslatePrompt("no", "# Title\n\n- point")
	if !strings.Contains(p, "Norwegian") || !strings.Contains(p, "# Title") {
		t.Errorf("translate prompt: %s", p)
	}
}
