package ytdlp

import (
	"context"
	"strings"
	"testing"
)

func TestExecFetch(t *testing.T) {
	f := Exec{Bin: "testdata/fake-yt-dlp.sh", WorkDir: t.TempDir()}
	v, err := f.Fetch(context.Background(), "https://youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != "abc123" || v.Title != "Test Video" || v.DurationSec != 63 {
		t.Errorf("meta: %+v", v)
	}
	if !strings.Contains(v.Transcript, "hello world") {
		t.Errorf("transcript: %q", v.Transcript)
	}
}

func TestPickSubtitleNoCaptions(t *testing.T) {
	// empty dir = yt-dlp wrote no subtitle files
	_, err := pickSubtitle(t.TempDir(), "abc123")
	if err == nil || !strings.Contains(err.Error(), "no captions") {
		t.Fatalf("err = %v", err)
	}
}
