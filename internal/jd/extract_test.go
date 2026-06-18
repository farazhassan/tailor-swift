package jd

import (
	"strings"
	"testing"
)

func TestExtractTextDropsScriptAndStyle(t *testing.T) {
	doc := `<html><head><style>.a{color:red}</style>
		<script>var x = 1;</script></head>
		<body><h1>Senior Go Engineer</h1>
		<p>Build   distributed systems.</p>
		<script>track();</script></body></html>`

	got, err := ExtractText(doc)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if strings.Contains(got, "color:red") || strings.Contains(got, "var x") || strings.Contains(got, "track()") {
		t.Errorf("script/style leaked into text: %q", got)
	}
	if !strings.Contains(got, "Senior Go Engineer") || !strings.Contains(got, "Build distributed systems.") {
		t.Errorf("body text missing or whitespace not collapsed: %q", got)
	}
}

func TestExtractTextCollapsesWhitespace(t *testing.T) {
	got, err := ExtractText("<p>a\n\n   b\t c</p>")
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got != "a b c" {
		t.Errorf("got %q, want %q", got, "a b c")
	}
}
