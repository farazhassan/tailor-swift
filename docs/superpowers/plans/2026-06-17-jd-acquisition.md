# JD Acquisition Implementation Plan (Plan 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Acquire a job description from a URL (or a manual file fallback), extract readable text, use an LLM to split it into discrete requirements/skills, and cache the result keyed by URL hash so the same JD is never re-fetched or re-processed.

**Architecture:** A new `internal/jd` package with focused units: `Fetch` (HTTP GET → raw body), `ExtractText` (HTML → plain text via `golang.org/x/net/html`), `ExtractRequirements` (raw text → structured `[]Requirement` via a one-shot `gantry.LLMClient.Generate` call, JSON-parsed), an on-disk JSON cache (`Posting` keyed by `sha256(url)`), and an `Acquire` orchestrator that ties them together with a cache-hit short-circuit and a `--jd-file` manual override. All LLM work goes through the `gantry.LLMClient` interface so production injects `anthropic.New(...)` and tests inject `eval.NewMockLLMClient(...)` — no API key in CI.

**Tech Stack:** Go 1.26, stdlib (`net/http`, `encoding/json`, `crypto/sha256`, `net/http/httptest` for tests), `golang.org/x/net/html` for HTML text extraction, gantry (`github.com/farazhassan/gantry` for `LLMClient`/`LLMRequest`/`Message`; `github.com/farazhassan/gantry/eval` for the mock in tests).

**Branch:** `feat/jd-acquisition` in `/Users/fhassan-mac/Dev/tailor-swift`.

## Prerequisites

This plan depends on the gantry dependency already being wired into `go.mod` (added in PR #2 / the `feat/embeddings-cache` branch with `require github.com/farazhassan/gantry v0.0.3-beta`).

- **If PR #2 is merged to `main`:** branch `feat/jd-acquisition` off `main` (Task 1 below).
- **If PR #2 is NOT yet merged:** branch `feat/jd-acquisition` off `feat/embeddings-cache` instead, so gantry is present. Task 1's verification step will confirm gantry resolves.

This plan does NOT depend on the `internal/embed` package — JD acquisition is independent of embeddings.

---

### Task 1: Branch and add the HTML dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Create the feature branch**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
# Base off main if PR #2 is merged; otherwise: git checkout feat/embeddings-cache
git checkout main
git checkout -b feat/jd-acquisition
```

- [ ] **Step 2: Verify gantry resolves**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go list -m github.com/farazhassan/gantry
```
Expected: prints `github.com/farazhassan/gantry v0.0.3-beta` (or the local replace path if based on a branch that still uses it). If it errors with "not a known dependency", you based off the wrong branch — see Prerequisites. (Disable the sandbox if the build cache is blocked.)

- [ ] **Step 3: Add the x/net dependency**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && GOFLAGS=-mod=mod go get golang.org/x/net/html
```
Expected: `go.mod` gains a `require golang.org/x/net vX.Y.Z` line; `go.sum` updated.

- [ ] **Step 4: Verify the tree still builds**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS (no behavior change yet).

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add go.mod go.sum
git commit -m "build: add golang.org/x/net for JD HTML extraction"
```

---

### Task 2: Posting & Requirement types + on-disk cache

**Files:**
- Create: `internal/jd/types.go`
- Create: `internal/jd/cache.go`
- Test: `internal/jd/cache_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/jd/cache_test.go`:

```go
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
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/...
```
Expected: build failure — package `jd` does not exist.

- [ ] **Step 3: Write the types**

Create `internal/jd/types.go`:

```go
package jd

import "time"

// Requirement is one discrete hiring need extracted from a job description.
type Requirement struct {
	Text     string `json:"text"`
	MustHave bool   `json:"must_have"`
}

// Posting is the cached result of acquiring one job description. It is keyed on
// disk by sha256(URL) so the same JD is never re-fetched or re-processed.
type Posting struct {
	URL          string        `json:"url"`
	FetchedAt    time.Time     `json:"fetched_at"`
	RawText      string        `json:"raw_text"`
	Requirements []Requirement `json:"requirements"`
}
```

- [ ] **Step 4: Write the cache**

Create `internal/jd/cache.go`:

```go
package jd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CacheKey returns the stable on-disk key for a JD URL: hex sha256.
func CacheKey(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

func cachePath(dir, url string) string {
	return filepath.Join(dir, CacheKey(url)+".json")
}

// LoadPosting reads a cached posting for url. A missing file is a cache miss
// (ok=false), not an error.
func LoadPosting(dir, url string) (*Posting, bool, error) {
	data, err := os.ReadFile(cachePath(dir, url))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("jd: read cache: %w", err)
	}
	var p Posting
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, false, fmt.Errorf("jd: parse cache: %w", err)
	}
	return &p, true, nil
}

// SavePosting writes p to the cache for p.URL, atomically (temp file + rename),
// creating the cache directory as needed.
func SavePosting(dir string, p *Posting) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("jd: create cache dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("jd: marshal posting: %w", err)
	}
	path := cachePath(dir, p.URL)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("jd: write cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("jd: replace cache: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/... && go vet ./internal/jd/...
```
Expected: PASS (3 tests), vet clean.

- [ ] **Step 6: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/jd/types.go internal/jd/cache.go internal/jd/cache_test.go
git commit -m "feat(jd): add Posting/Requirement types and on-disk cache"
```

---

### Task 3: HTTP fetch

**Files:**
- Create: `internal/jd/fetch.go`
- Test: `internal/jd/fetch_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/jd/fetch_test.go`:

```go
package jd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("Fetch sent no User-Agent")
		}
		_, _ = w.Write([]byte("<html><body>hello</body></html>"))
	}))
	defer srv.Close()

	got, err := Fetch(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("body = %q, want it to contain 'hello'", got)
	}
}

func TestFetchErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	if _, err := Fetch(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("Fetch: want error on 403, got nil")
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/...
```
Expected: build failure — `Fetch` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/jd/fetch.go`:

```go
package jd

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// maxFetchBytes caps a fetched JD body; real postings are far smaller.
const maxFetchBytes = 5 << 20 // 5 MiB

const userAgent = "tailor-swift/0.1 (+https://github.com/farazhassan/tailor-swift)"

// Fetch GETs url and returns the response body as a string. A nil client uses
// http.DefaultClient. Non-2xx responses are errors.
func Fetch(ctx context.Context, client *http.Client, url string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("jd: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("jd: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("jd: fetch %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return "", fmt.Errorf("jd: read body: %w", err)
	}
	return string(body), nil
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/... && go vet ./internal/jd/...
```
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/jd/fetch.go internal/jd/fetch_test.go
git commit -m "feat(jd): add HTTP fetch with size limit and non-2xx errors"
```

---

### Task 4: HTML → text extraction

**Files:**
- Create: `internal/jd/extract.go`
- Test: `internal/jd/extract_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/jd/extract_test.go`:

```go
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
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/...
```
Expected: build failure — `ExtractText` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/jd/extract.go`:

```go
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
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/... && go vet ./internal/jd/...
```
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/jd/extract.go internal/jd/extract_test.go
git commit -m "feat(jd): extract readable text from HTML, dropping script/style"
```

---

### Task 5: LLM requirement extraction

**Files:**
- Create: `internal/jd/requirements.go`
- Test: `internal/jd/requirements_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/jd/requirements_test.go`:

```go
package jd

import (
	"context"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
)

func TestExtractRequirementsParsesJSON(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    `[{"text":"Go","must_have":true},{"text":"Kafka","must_have":false}]`,
		StopReason: gantry.StopReasonEnd,
	})
	got, err := ExtractRequirements(context.Background(), mock, "we need a Go engineer")
	if err != nil {
		t.Fatalf("ExtractRequirements: %v", err)
	}
	if len(got) != 2 || got[0].Text != "Go" || !got[0].MustHave || got[1].MustHave {
		t.Errorf("requirements = %+v", got)
	}
	// The job text must be sent to the model.
	reqs := mock.Requests()
	if len(reqs) != 1 || !strings.Contains(reqs[0].Messages[0].Content, "Go engineer") {
		t.Errorf("LLM request = %+v", reqs)
	}
}

func TestExtractRequirementsStripsCodeFence(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    "```json\n[{\"text\":\"Go\",\"must_have\":true}]\n```",
		StopReason: gantry.StopReasonEnd,
	})
	got, err := ExtractRequirements(context.Background(), mock, "jd text")
	if err != nil {
		t.Fatalf("ExtractRequirements: %v", err)
	}
	if len(got) != 1 || got[0].Text != "Go" {
		t.Errorf("requirements = %+v", got)
	}
}

func TestExtractRequirementsErrorsOnBadJSON(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    "not json at all",
		StopReason: gantry.StopReasonEnd,
	})
	if _, err := ExtractRequirements(context.Background(), mock, "jd text"); err == nil {
		t.Error("ExtractRequirements: want error on non-JSON content, got nil")
	}
}

func TestExtractRequirementsErrorsOnEmptyText(t *testing.T) {
	mock := eval.NewMockLLMClient()
	if _, err := ExtractRequirements(context.Background(), mock, "   "); err == nil {
		t.Error("ExtractRequirements: want error on empty job text, got nil")
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/...
```
Expected: build failure — `ExtractRequirements` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/jd/requirements.go`:

```go
package jd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
)

const requirementsSystemPrompt = `You extract structured hiring requirements from a job description.
Return ONLY a JSON array, with no prose and no markdown code fences.
Each element is an object: {"text": string, "must_have": boolean}.
"text" is a single concrete requirement or skill, stated concisely.
"must_have" is true for required/mandatory items and false for nice-to-haves.`

// ExtractRequirements asks the LLM to split a job description into discrete
// requirements. The model is instructed to return a JSON array; a leading
// markdown code fence (```json ... ```) is tolerated.
func ExtractRequirements(ctx context.Context, llm gantry.LLMClient, jobText string) ([]Requirement, error) {
	if strings.TrimSpace(jobText) == "" {
		return nil, fmt.Errorf("jd: empty job text")
	}
	resp, err := llm.Generate(ctx, gantry.LLMRequest{
		System:   requirementsSystemPrompt,
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: jobText}},
	})
	if err != nil {
		return nil, fmt.Errorf("jd: extract requirements: %w", err)
	}
	var reqs []Requirement
	if err := json.Unmarshal([]byte(stripCodeFence(resp.Content)), &reqs); err != nil {
		return nil, fmt.Errorf("jd: parse requirements json: %w", err)
	}
	return reqs, nil
}

// stripCodeFence removes a surrounding markdown code fence if present, so JSON
// wrapped in ```json ... ``` still parses.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(s)
	return strings.TrimSpace(strings.TrimSuffix(s, "```"))
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/... && go vet ./internal/jd/...
```
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/jd/requirements.go internal/jd/requirements_test.go
git commit -m "feat(jd): extract structured requirements via gantry LLM"
```

---

### Task 6: Acquire orchestrator

**Files:**
- Create: `internal/jd/acquire.go`
- Test: `internal/jd/acquire_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/jd/acquire_test.go`:

```go
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
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/...
```
Expected: build failure — `Acquire`/`Options` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/jd/acquire.go`:

```go
package jd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/farazhassan/gantry"
)

// Options configures a single Acquire call.
type Options struct {
	URL        string       // required: the JD URL, also the cache key
	FilePath   string       // optional: read JD text from this file instead of fetching (--jd-file fallback)
	CacheDir   string       // directory for cached postings
	HTTPClient *http.Client // optional; nil uses http.DefaultClient
}

// Acquire returns the parsed job posting for opts.URL. On a cache hit it returns
// the cached Posting without fetching or calling the LLM. On a miss it obtains
// the raw text (from opts.FilePath if set, otherwise by fetching opts.URL and
// extracting text from HTML), splits it into requirements via the LLM, caches
// the result, and returns it.
func Acquire(ctx context.Context, llm gantry.LLMClient, opts Options) (*Posting, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("jd: URL is required")
	}
	if p, ok, err := LoadPosting(opts.CacheDir, opts.URL); err != nil {
		return nil, err
	} else if ok {
		return p, nil
	}

	var raw string
	if opts.FilePath != "" {
		data, err := os.ReadFile(opts.FilePath)
		if err != nil {
			return nil, fmt.Errorf("jd: read jd file: %w", err)
		}
		raw = string(data)
	} else {
		body, err := Fetch(ctx, opts.HTTPClient, opts.URL)
		if err != nil {
			return nil, err
		}
		raw, err = ExtractText(body)
		if err != nil {
			return nil, err
		}
	}

	reqs, err := ExtractRequirements(ctx, llm, raw)
	if err != nil {
		return nil, err
	}

	p := &Posting{
		URL:          opts.URL,
		FetchedAt:    time.Now().UTC(),
		RawText:      raw,
		Requirements: reqs,
	}
	if err := SavePosting(opts.CacheDir, p); err != nil {
		return nil, err
	}
	return p, nil
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/jd/... && go vet ./internal/jd/...
```
Expected: PASS (all jd tests), vet clean.

- [ ] **Step 5: Final whole-tree check**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS across all packages.

- [ ] **Step 6: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/jd/acquire.go internal/jd/acquire_test.go
git commit -m "feat(jd): add Acquire orchestrator with cache and file fallback"
```

---

## Out of scope (deferred to later plans)

- **CLI wiring** (`tailor generate --jd-url`): the `generate` command that calls `jd.Acquire` is wired in Plan 6 (CLI). Plan 3 delivers the package only.
- **Embedding JD requirement chunks:** the pipeline embeds requirements for retrieval (spec step 3). That belongs to Plan 4 (Retrieval), which consumes `Posting.Requirements`.
- **Model selection:** Acquire takes an already-constructed `gantry.LLMClient`; choosing the model (cheap vs strong) and constructing `anthropic.New(...)` happens at the CLI layer (Plan 6).

## Self-Review

- **Spec coverage:** Implements design step 2 ("JD acquire — cache hit? use it. Else fetch URL → extract text → LLM splits into discrete requirements/skills → cache") and the `cache/jd/<sha256(url)>.json` layout (`{url, fetched_at, raw_text, extracted_requirements[]}` → `Posting{URL, FetchedAt, RawText, Requirements}`). Covers the `--jd-file` manual fallback (CLI surface) and the "never re-fetch the same JD" guarantee. The "JD fetch fails / paywalled" error path is covered by `Fetch`'s non-2xx error; the LLM-stage-tested-with-mock testing requirement is met via `eval.NewMockLLMClient`.
- **Placeholders:** none — every step has full code or an exact command with expected output.
- **Type consistency:** `Posting`/`Requirement` field names and JSON tags are identical across `types.go`, `cache.go`, `requirements.go`, and `acquire.go`. `Acquire`'s `Options` fields (`URL`, `FilePath`, `CacheDir`, `HTTPClient`) match their uses. `ExtractRequirements`, `Fetch`, `ExtractText`, `LoadPosting`, `SavePosting`, `CacheKey` signatures match their call sites in `acquire.go` and the tests.
