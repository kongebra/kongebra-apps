// Package summarize holds the map-reduce building blocks for transcript
// summarization: word-based chunking and the prompt set.
package summarize

import "strings"

// Split cuts text into word-based chunks with overlap, so sentences cut at
// a boundary still appear whole in one of the two neighboring chunks.
func Split(text string, chunkWords, overlapWords int) []string {
	w := strings.Fields(text)
	if len(w) <= chunkWords {
		return []string{text}
	}
	step := chunkWords - overlapWords
	var chunks []string
	for i := 0; i < len(w); i += step {
		end := min(i+chunkWords, len(w))
		chunks = append(chunks, strings.Join(w[i:end], " "))
		if end == len(w) {
			break
		}
	}
	return chunks
}
