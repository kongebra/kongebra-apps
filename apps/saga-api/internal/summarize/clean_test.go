package summarize

import "testing"

func TestCleanMath(t *testing.T) {
	cases := []struct{ in, want string }{
		{`Action $\rightarrow$ Failure`, "Action → Failure"},         // the real bug
		{`bare \to next`, "bare → next"},                             // unwrapped command
		{`$\Rightarrow$ done`, "⇒ done"},
		{`a $\leftrightarrow$ b`, "a ↔ b"},                           // longest-first alternation
		{`It cost $5 to $10 total`, "It cost $5 to $10 total"},        // word "to" and prices untouched (no backslash)
		{`\total is a word`, `\total is a word`},                     // \b guards against \to inside \total
		{`plain text`, "plain text"},
	}
	for _, c := range cases {
		if got := CleanMath(c.in); got != c.want {
			t.Errorf("CleanMath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
