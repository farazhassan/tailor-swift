package main

import "testing"

func TestSlugify(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://acme.com/jobs/senior-go-engineer", "senior-go-engineer"},
		{"https://acme.com/jobs/123?ref=x", "123"},
		{"https://acme.com/", "job"},
		{"https://acme.com", "acme-com"},
		{"https://acme.com/Jobs/Staff_Engineer!", "staff-engineer"},
		{"", "job"},
	}
	for _, c := range cases {
		if got := slugify(c.url); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}
