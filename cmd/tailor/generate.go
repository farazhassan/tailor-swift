package main

import (
	"net/url"
	"strings"
)

// slugify derives a filesystem-friendly slug from a job posting URL: the last
// non-empty path segment (or the host when there is no path), lowercased, with
// each run of non-alphanumerics collapsed to a single dash and the ends
// trimmed. Falls back to "job" when nothing usable remains.
func slugify(rawURL string) string {
	seg := ""
	if u, err := url.Parse(rawURL); err == nil {
		parts := strings.Split(u.Path, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				seg = parts[i]
				break
			}
		}
		if seg == "" && u.Path == "" {
			seg = u.Host
		}
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(seg) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "job"
	}
	return s
}
