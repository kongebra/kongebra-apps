package summarize

import "regexp"

// Small local models sprinkle LaTeX math markup (e.g. $\rightarrow$) into their
// output; the web renderer shows it raw. We normalize deterministically here
// instead of trusting a weak model to obey a "no LaTeX" prompt instruction.
var mathCmd = map[string]string{
	"rightarrow":     "→", // ->
	"to":             "→",
	"leftarrow":      "←", // <-
	"gets":           "←",
	"leftrightarrow": "↔", // <->
	"Rightarrow":     "⇒",
	"Leftarrow":      "⇐",
	"Leftrightarrow": "⇔",
	"uparrow":        "↑",
	"downarrow":      "↓",
	"times":          "×",
	"cdot":           "·",
	"pm":             "±",
	"approx":         "≈",
	"leq":            "≤",
	"geq":            "≥",
	"neq":            "≠",
	"ldots":          "…",
	"dots":           "…",
}

// Longest alternatives first: Go regexp is leftmost-first, so \leftrightarrow
// must be tried before \leftarrow. \b keeps \total from matching \to.
var cmdRe = regexp.MustCompile(`\\(rightarrow|leftrightarrow|Leftrightarrow|Rightarrow|Leftarrow|leftarrow|uparrow|downarrow|times|approx|ldots|cdot|dots|gets|neq|leq|geq|pm|to)\b`)

// $ (or $$) wrapping a single already-converted symbol; leaves real dollar
// signs (prices) untouched because the class requires a math symbol between.
var wrapRe = regexp.MustCompile(`\${1,2}\s*([\x{2190}-\x{21ff}\x{00d7}\x{00b7}\x{00b1}\x{2248}\x{2264}\x{2265}\x{2260}\x{2026}])\s*\${1,2}`)

// Reasoning models may inline <think>...</think> before the answer. Thinking is
// disabled at the LiteLLM gateway for local models, but a cloud reasoning model
// could still emit a stray block - strip it (including an unterminated trailing
// one) before math normalization.
var thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)
var thinkOpenRe = regexp.MustCompile(`(?s)<think>.*$`)

// CleanMath strips <think> blocks, replaces LaTeX math commands with Unicode,
// and unwraps the leftover $...$ delimiters around single symbols.
func CleanMath(s string) string {
	s = thinkRe.ReplaceAllString(s, "")
	s = thinkOpenRe.ReplaceAllString(s, "")
	s = cmdRe.ReplaceAllStringFunc(s, func(m string) string {
		return mathCmd[m[1:]] // drop leading backslash
	})
	return wrapRe.ReplaceAllString(s, "$1")
}
