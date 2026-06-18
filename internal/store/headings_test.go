package store

import (
	"reflect"
	"testing"
)

func TestParseTags(t *testing.T) {
	clean, tags := parseTags("Cut latency 40% `#performance #kafka`")
	if clean != "Cut latency 40%" {
		t.Fatalf("clean = %q", clean)
	}
	if !reflect.DeepEqual(tags, []string{"performance", "kafka"}) {
		t.Fatalf("tags = %v", tags)
	}
}

func TestParseTags_NoTags(t *testing.T) {
	clean, tags := parseTags("Just a plain bullet")
	if clean != "Just a plain bullet" {
		t.Fatalf("clean = %q", clean)
	}
	if len(tags) != 0 {
		t.Fatalf("tags = %v, want empty", tags)
	}
}

func TestParseRoleHeading(t *testing.T) {
	c, ti, s, e := parseRoleHeading("Acme — Senior Engineer (2021-03 – present)")
	if c != "Acme" || ti != "Senior Engineer" || s != "2021-03" || e != "present" {
		t.Fatalf("got %q / %q / %q / %q", c, ti, s, e)
	}
}

func TestParseRoleHeading_HyphenSeparators(t *testing.T) {
	c, ti, s, e := parseRoleHeading("Globex - Staff Engineer (2019-01 - 2021-02)")
	if c != "Globex" || ti != "Staff Engineer" || s != "2019-01" || e != "2021-02" {
		t.Fatalf("got %q / %q / %q / %q", c, ti, s, e)
	}
}

func TestParseRoleHeading_NoDates(t *testing.T) {
	c, ti, s, e := parseRoleHeading("Initech — Engineer")
	if c != "Initech" || ti != "Engineer" || s != "" || e != "" {
		t.Fatalf("got %q / %q / %q / %q", c, ti, s, e)
	}
}
