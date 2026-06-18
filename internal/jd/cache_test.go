package jd

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCacheKeyStable(t *testing.T) {
	a := CacheKey("https://example.com/jobs/42")
	b := CacheKey("https://example.com/jobs/42")
	c := CacheKey("https://example.com/jobs/43")
	if a != b {
		t.Errorf("CacheKey not stable: %q vs %q", a, b)
	}
	if a == c {
		t.Error("CacheKey collision for different URLs")
	}
	if len(a) != 64 {
		t.Errorf("CacheKey len = %d, want 64 hex chars", len(a))
	}
}

func TestPostingRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jd")
	url := "https://example.com/jobs/42"
	want := &Posting{
		URL:       url,
		FetchedAt: time.Now().UTC().Truncate(time.Second),
		RawText:   "we need a go engineer",
		Requirements: []Requirement{
			{Text: "Go", MustHave: true},
			{Text: "Kafka", MustHave: false},
		},
	}
	if err := SavePosting(dir, want); err != nil {
		t.Fatalf("SavePosting: %v", err)
	}

	got, ok, err := LoadPosting(dir, url)
	if err != nil {
		t.Fatalf("LoadPosting: %v", err)
	}
	if !ok {
		t.Fatal("LoadPosting: want hit, got miss")
	}
	if got.URL != want.URL || got.RawText != want.RawText {
		t.Errorf("posting = %+v, want %+v", got, want)
	}
	if len(got.Requirements) != 2 || !got.Requirements[0].MustHave || got.Requirements[0].Text != "Go" {
		t.Errorf("requirements = %+v", got.Requirements)
	}
}

func TestLoadPostingMissIsNotError(t *testing.T) {
	dir := t.TempDir()
	_, ok, err := LoadPosting(dir, "https://example.com/never-cached")
	if err != nil {
		t.Fatalf("LoadPosting: %v", err)
	}
	if ok {
		t.Error("want cache miss, got hit")
	}
}
