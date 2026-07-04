// Package ytdlp fetches video metadata and subtitles by shelling out to the
// yt-dlp binary. Runs on the home node in production: YouTube aggressively
// rate-limits datacenter ASNs, so transcript fetching needs a residential IP.
package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"saga-api/internal/vtt"
)

type Video struct {
	ID          string
	Title       string
	DurationSec int
	Transcript  string
}

type Fetcher interface {
	Fetch(ctx context.Context, url string) (Video, error)
}

// Exec is the real yt-dlp-backed Fetcher.
type Exec struct {
	Bin     string // path to yt-dlp binary
	WorkDir string // writable dir for subtitle downloads (emptyDir in k8s)
}

// subLangPreference: Norwegian first, then English, then whatever exists.
var subLangPreference = []string{"no", "nb", "en"}

func (e Exec) Fetch(ctx context.Context, url string) (Video, error) {
	var v Video

	meta, err := e.run(ctx, "-J", "--skip-download", url)
	if err != nil {
		return v, err
	}
	var m struct {
		ID       string  `json:"id"`
		Title    string  `json:"title"`
		Duration float64 `json:"duration"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		return v, fmt.Errorf("parse yt-dlp metadata: %w", err)
	}
	v.ID, v.Title, v.DurationSec = m.ID, m.Title, int(m.Duration)

	dir, err := os.MkdirTemp(e.WorkDir, "saga-sub-*")
	if err != nil {
		return v, err
	}
	defer os.RemoveAll(dir)

	if _, err := e.run(ctx,
		"--skip-download", "--write-subs", "--write-auto-subs",
		"--sub-langs", "no,nb,en", "--sub-format", "vtt",
		"-o", filepath.Join(dir, "%(id)s.%(ext)s"), url); err != nil {
		return v, err
	}

	path, err := pickSubtitle(dir, v.ID)
	if err != nil {
		return v, err
	}
	f, err := os.Open(path)
	if err != nil {
		return v, err
	}
	defer f.Close()
	v.Transcript, err = vtt.Parse(f)
	return v, err
}

func pickSubtitle(dir, id string) (string, error) {
	for _, lang := range subLangPreference {
		p := filepath.Join(dir, id+"."+lang+".vtt")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.vtt"))
	if len(matches) > 0 {
		return matches[0], nil
	}
	return "", errors.New("no captions available for this video")
}

func (e Exec) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if len(msg) > 500 {
			msg = msg[len(msg)-500:] // yt-dlp puts the useful error last
		}
		return nil, fmt.Errorf("yt-dlp: %w: %s", err, msg)
	}
	return stdout.Bytes(), nil
}
