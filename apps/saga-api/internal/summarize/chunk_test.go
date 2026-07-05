package summarize

import (
	"fmt"
	"strings"
	"testing"
)

func words(n int) string {
	w := make([]string, n)
	for i := range w {
		w[i] = fmt.Sprintf("w%d", i)
	}
	return strings.Join(w, " ")
}

func TestSplitShortTextIsSingleChunk(t *testing.T) {
	chunks := Split(words(100), 2000, 200)
	if len(chunks) != 1 {
		t.Fatalf("len = %d", len(chunks))
	}
}

func TestSplitOverlaps(t *testing.T) {
	chunks := Split(words(4500), 2000, 200)
	if len(chunks) != 3 {
		t.Fatalf("len = %d, want 3", len(chunks))
	}
	// chunk 2 must start 200 words before chunk 1 ended (overlap)
	c1 := strings.Fields(chunks[0])
	c2 := strings.Fields(chunks[1])
	if c2[0] != c1[len(c1)-200] {
		t.Errorf("chunk 2 starts at %s, want %s", c2[0], c1[len(c1)-200])
	}
	// all words covered: last chunk ends with the last word
	last := strings.Fields(chunks[len(chunks)-1])
	if last[len(last)-1] != "w4499" {
		t.Errorf("last word = %s", last[len(last)-1])
	}
}
