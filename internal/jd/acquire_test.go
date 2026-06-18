package jd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
)

const reqJSON = `[{"text":"Go","must_have":true}]`

func TestAcquireFetchesExtractsAndCaches(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("<html><body><h1>Go Engineer</h1><script>x()</script></body></html>"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: reqJSON, StopReason: gantry.StopReasonEnd})
	p, err := Acquire(context.Background(), mock, Options{
		URL: srv.URL, CacheDir: dir, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if hits != 1 {
		t.Errorf("server hits = %d, want 1", hits)
	}
	if len(p.Requirements) != 1 || p.Requirements[0].Text != "Go" {
		t.Errorf("requirements = %+v", p.Requirements)
	}
	if p.RawText == "" || contains(p.RawText, "x()") {
		t.Errorf("RawText not extracted cleanly: %q", p.RawText)
	}
	if _, err := os.Stat(filepath.Join(dir, CacheKey(srv.URL)+".json")); err != nil {
		t.Errorf("cache file not written: %v", err)
	}
}

func TestAcquireUsesCacheOnSecondCall(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("<html><body>Go Engineer</body></html>"))
	}))
	defer srv.Close()
	dir := t.TempDir()

	first := eval.NewMockLLMClient(gantry.LLMResponse{Content: reqJSON, StopReason: gantry.StopReasonEnd})
	if _, err := Acquire(context.Background(), first, Options{URL: srv.URL, CacheDir: dir, HTTPClient: srv.Client()}); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	// Second call: empty mock (would error if Generate is called) and a server
	// hit counter that must not advance.
	second := eval.NewMockLLMClient()
	if _, err := Acquire(context.Background(), second, Options{URL: srv.URL, CacheDir: dir, HTTPClient: srv.Client()}); err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if hits != 1 {
		t.Errorf("server hits = %d after cache hit, want 1", hits)
	}
	if len(second.Requests()) != 0 {
		t.Errorf("LLM called on cache hit: %d requests", len(second.Requests()))
	}
}

func TestAcquireFileFallbackSkipsFetch(t *testing.T) {
	dir := t.TempDir()
	jdFile := filepath.Join(dir, "jd.txt")
	if err := os.WriteFile(jdFile, []byte("Senior Go Engineer, must know Kafka."), 0o644); err != nil {
		t.Fatalf("write jd file: %v", err)
	}
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: reqJSON, StopReason: gantry.StopReasonEnd})

	p, err := Acquire(context.Background(), mock, Options{
		URL:      "https://example.com/jobs/99", // used only for the cache key
		FilePath: jdFile,
		CacheDir: filepath.Join(dir, "cache"),
		// no HTTPClient: fetch must not happen
	})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if p.RawText != "Senior Go Engineer, must know Kafka." {
		t.Errorf("RawText = %q", p.RawText)
	}
	if len(p.Requirements) != 1 {
		t.Errorf("requirements = %+v", p.Requirements)
	}
}

func TestAcquireErrorsWithoutURL(t *testing.T) {
	mock := eval.NewMockLLMClient()
	if _, err := Acquire(context.Background(), mock, Options{CacheDir: t.TempDir()}); err == nil {
		t.Error("Acquire: want error when URL is empty, got nil")
	}
}

func contains(s, sub string) bool { return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0 }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
