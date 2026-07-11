package summarize

import (
	"fmt"
	"strings"
)

// PromptVersion identifies the current prompt wording. Bump this whenever any
// prompt in this file changes, so summaries can be traced back to the wording
// that produced them.
const PromptVersion = "2026-07-10"

// Prompts are written in English (small models follow English instructions
// best) with an explicit output-language directive.
func langName(lang string) string {
	if lang == "no" {
		return "Norwegian"
	}
	return "English"
}

func SinglePrompt(lang, title, transcript string) string {
	return fmt.Sprintf(`You are summarizing the transcript of a video titled %q.
Write the summary in %s, as Markdown with: a one-paragraph overview,
the key points as bullets, and a short list of concepts worth learning more about.
Use plain text and standard Markdown only - never LaTeX or math notation (write arrows as ->, not \rightarrow).

Transcript:
%s`, title, langName(lang), transcript)
}

func MapPrompt(lang, title, chunk string) string {
	return fmt.Sprintf(`You are summarizing one part of a longer video transcript titled %q.
Summarize the key points of this part in %s as concise bullets.
Do not add an introduction or conclusion; other parts exist.

Transcript part:
%s`, title, langName(lang), chunk)
}

func ReducePrompt(lang, title string, parts []string) string {
	return fmt.Sprintf(`You are writing the final summary of a video titled %q,
based on summaries of its parts, in order.
Write in %s, as Markdown with: a one-paragraph overview,
the key points as bullets, and a short list of concepts worth learning more about.
Merge duplicate points across parts.
Use plain text and standard Markdown only - never LaTeX or math notation (write arrows as ->, not \rightarrow).

Part summaries:
%s`, title, langName(lang), strings.Join(parts, "\n\n---\n\n"))
}

// TranslatePrompt asks the model to translate a Markdown summary into the target
// language while preserving the Markdown structure.
func TranslatePrompt(targetLang, markdown string) string {
	return fmt.Sprintf(`Translate the following Markdown text into %s.
Preserve the Markdown structure (headings, bullet lists, links) exactly.
Output only the translated Markdown, no preamble.

%s`, langName(targetLang), markdown)
}
