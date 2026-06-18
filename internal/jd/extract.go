package jd

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// nonTextTags hold content that is never human-readable JD text.
var nonTextTags = map[string]bool{"script": true, "style": true, "noscript": true}

// ExtractText pulls human-readable text out of an HTML document: it drops
// script/style/noscript content and collapses all runs of whitespace to single
// spaces. Input that is already plain text passes through (collapsed).
func ExtractText(doc string) (string, error) {
	z := html.NewTokenizer(strings.NewReader(doc))
	var b strings.Builder
	skipDepth := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			if err := z.Err(); err == io.EOF {
				return strings.Join(strings.Fields(b.String()), " "), nil
			} else {
				return "", fmt.Errorf("jd: parse html: %w", err)
			}
		case html.StartTagToken:
			name, _ := z.TagName()
			if nonTextTags[string(name)] {
				skipDepth++
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if nonTextTags[string(name)] && skipDepth > 0 {
				skipDepth--
			}
		case html.TextToken:
			if skipDepth == 0 {
				b.Write(z.Text())
				b.WriteByte(' ')
			}
		}
	}
}
