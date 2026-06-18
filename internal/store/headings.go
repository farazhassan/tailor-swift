package store

import (
	"regexp"
	"strings"
)

var (
	backtickSpan = regexp.MustCompile("`[^`]*`")
	tagToken     = regexp.MustCompile(`#[A-Za-z0-9_-]+`)
	dateParen    = regexp.MustCompile(`\s*\(([^)]*)\)\s*$`)
	// A range separator is a dash surrounded by whitespace, so internal
	// hyphens in dates like "2021-03" are never split.
	spacedDash = regexp.MustCompile(`\s+[–—-]\s+`)
)

// parseTags extracts #tags from backtick spans on a line and returns the
// line with those spans removed and trimmed.
func parseTags(line string) (string, []string) {
	var tags []string
	for _, span := range backtickSpan.FindAllString(line, -1) {
		for _, tok := range tagToken.FindAllString(span, -1) {
			tags = append(tags, strings.TrimPrefix(tok, "#"))
		}
	}
	clean := strings.TrimSpace(backtickSpan.ReplaceAllString(line, ""))
	return clean, tags
}

// parseRoleHeading parses "Company — Title (start – end)" into its parts.
// Title and dates are optional.
func parseRoleHeading(h string) (company, title, start, end string) {
	h = strings.TrimSpace(h)
	if m := dateParen.FindStringSubmatch(h); m != nil {
		dates := strings.TrimSpace(m[1])
		h = strings.TrimSpace(dateParen.ReplaceAllString(h, ""))
		dp := spacedDash.Split(dates, 2)
		start = strings.TrimSpace(dp[0])
		if len(dp) > 1 {
			end = strings.TrimSpace(dp[1])
		}
	}
	p := spacedDash.Split(h, 2)
	company = strings.TrimSpace(p[0])
	if len(p) > 1 {
		title = strings.TrimSpace(p[1])
	}
	return
}
