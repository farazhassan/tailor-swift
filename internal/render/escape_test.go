package render

import "testing"

func TestEscapeTeXSpecialChars(t *testing.T) {
	cases := map[string]string{
		"50% off":      `50\% off`,
		"A & B":        `A \& B`,
		"cost $5":      `cost \$5`,
		"C#":           `C\#`,
		"snake_case":   `snake\_case`,
		"a{b}c":        `a\{b\}c`,
		"x~y":          `x\textasciitilde{}y`,
		"2^10":         `2\textasciicircum{}10`,
		`path\to\file`: `path\textbackslash{}to\textbackslash{}file`,
		"plain text":   "plain text",
	}
	for in, want := range cases {
		if got := escapeTeX(in); got != want {
			t.Errorf("escapeTeX(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeTeXBackslashDoesNotDoubleEscapeItsBraces(t *testing.T) {
	// `\` expands to `\textbackslash{}`; the braces it introduces must NOT
	// themselves be escaped (single-pass replacer guarantees this).
	if got := escapeTeX(`\`); got != `\textbackslash{}` {
		t.Errorf("escapeTeX(backslash) = %q, want %q", got, `\textbackslash{}`)
	}
}
