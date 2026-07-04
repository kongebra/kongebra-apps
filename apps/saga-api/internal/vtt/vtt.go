// Package vtt turns WebVTT subtitle files into plain transcript text.
// YouTube auto-captions repeat lines across cues (rolling captions);
// consecutive-duplicate filtering flattens that.
// ponytail: [Music]-style cue markers are left in; harmless noise for an LLM.
package vtt

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

var (
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	cueIDRe = regexp.MustCompile(`^\d+$`)
)

func Parse(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var out []string
	prev := ""
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "",
			strings.HasPrefix(line, "WEBVTT"),
			strings.HasPrefix(line, "Kind:"),
			strings.HasPrefix(line, "Language:"),
			strings.HasPrefix(line, "NOTE"),
			strings.HasPrefix(line, "STYLE"),
			strings.Contains(line, "-->"),
			cueIDRe.MatchString(line):
			continue
		}
		line = strings.TrimSpace(tagRe.ReplaceAllString(line, ""))
		if line == "" || line == prev {
			continue
		}
		out = append(out, line)
		prev = line
	}
	return strings.Join(out, " "), sc.Err()
}
