package vtt

import (
	"strings"
	"testing"
)

// auto-captions roll: every cue repeats the previous line and adds one.
const autoCaptionFixture = `WEBVTT
Kind: captions
Language: en

00:00:00.000 --> 00:00:02.000
<00:00:00.500><c>hello</c><00:00:01.000><c> world</c>

00:00:02.000 --> 00:00:04.000
hello world

00:00:02.000 --> 00:00:04.000
this is a test

00:00:04.000 --> 00:00:06.000
this is a test
`

func TestParseDedupesRollingCaptions(t *testing.T) {
	got, err := Parse(strings.NewReader(autoCaptionFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := "hello world this is a test"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseStripsCueNumbersAndNotes(t *testing.T) {
	in := "WEBVTT\n\nNOTE something\n\n1\n00:00:00.000 --> 00:00:01.000\nfirst line\n\n2\n00:00:01.000 --> 00:00:02.000\nsecond line\n"
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if got != "first line second line" {
		t.Errorf("got %q", got)
	}
}
