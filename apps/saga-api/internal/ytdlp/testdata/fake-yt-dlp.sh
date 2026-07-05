#!/bin/sh
# Fake yt-dlp for tests: -J prints metadata JSON; otherwise it "downloads"
# subtitles by copying the fixture next to the -o output template.
here=$(dirname "$0")
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-o" ]; then out="$a"; fi
  if [ "$a" = "-J" ]; then cat "$here/meta.json"; exit 0; fi
  prev="$a"
done
dir=$(dirname "$out")
cp "$here/fixture.en.vtt" "$dir/abc123.en.vtt"
