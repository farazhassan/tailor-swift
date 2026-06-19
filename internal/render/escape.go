package render

import "strings"

// texReplacer escapes the LaTeX special characters in a single left-to-right
// pass. strings.Replacer does not re-scan replacement output, so mapping `\`
// to `\textbackslash{}` is safe — the braces it introduces are not re-escaped.
// Backslash is listed first for clarity; replacement is single-pass regardless.
var texReplacer = strings.NewReplacer(
	`\`, `\textbackslash{}`,
	`{`, `\{`,
	`}`, `\}`,
	`$`, `\$`,
	`&`, `\&`,
	`#`, `\#`,
	`_`, `\_`,
	`%`, `\%`,
	`~`, `\textasciitilde{}`,
	`^`, `\textasciicircum{}`,
)

// escapeTeX makes an arbitrary string safe to drop into a LaTeX document body.
func escapeTeX(s string) string {
	return texReplacer.Replace(s)
}
