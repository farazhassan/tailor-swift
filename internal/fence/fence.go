// Package fence strips markdown code fences from LLM output so the wrapped
// payload (JSON, LaTeX, etc.) can be parsed or used directly.
package fence

import "strings"

// Strip removes a surrounding markdown code fence if present. Input wrapped in
// ```lang ... ``` (or plain ``` ... ```) returns the inner content; input with
// no fence is returned trimmed of surrounding whitespace.
func Strip(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(s)
	return strings.TrimSpace(strings.TrimSuffix(s, "```"))
}
