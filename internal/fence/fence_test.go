package fence

import "testing"

func TestStripNoFenceTrims(t *testing.T) {
	if got := Strip("  hello  "); got != "hello" {
		t.Errorf("Strip = %q, want %q", got, "hello")
	}
}

func TestStripJSONFence(t *testing.T) {
	in := "```json\n[{\"a\":1}]\n```"
	if got := Strip(in); got != `[{"a":1}]` {
		t.Errorf("Strip = %q", got)
	}
}

func TestStripPlainFence(t *testing.T) {
	in := "```\n\\documentclass\n```"
	if got := Strip(in); got != `\documentclass` {
		t.Errorf("Strip = %q", got)
	}
}

func TestStripMultiLineFence(t *testing.T) {
	in := "```latex\nline1\nline2\n```"
	if got := Strip(in); got != "line1\nline2" {
		t.Errorf("Strip = %q", got)
	}
}
