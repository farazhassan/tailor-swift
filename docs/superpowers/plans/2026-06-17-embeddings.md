# Embeddings Provider & Cache Implementation Plan (Plan 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Voyage AI embeddings provider to gantry (upstream-first) and an on-disk embedding cache in tailor-swift so content-store achievements are embedded once and reused.

**Architecture:** The Voyage provider is contributed to gantry as a new `components/embeddings/voyage` package that mirrors the existing OpenAI provider and satisfies the `embeddings.Embeddings` interface (`Embed(ctx, texts) ([][]float32, error)`). tailor-swift depends on gantry via a go.mod `replace` directive pointing at the local gantry checkout until the gantry PR merges. On the tailor-swift side, a model-scoped JSON cache keyed by achievement ID (which is already a content hash) means changed text yields a new key and a natural cache miss; an `Embedder` consults the cache and sends only misses to the provider.

**Tech Stack:** Go (gantry `go 1.22`, tailor-swift `go 1.26`), standard library only (`net/http`, `encoding/json`, `net/http/httptest` for tests), gantry `github.com/farazhassan/gantry`.

**Branches:**
- gantry repo (`/Users/fhassan-mac/Dev/gantry`): `feat/voyage-embeddings` (Task 1 only).
- tailor-swift repo (`/Users/fhassan-mac/Dev/tailor-swift`): `feat/embeddings-cache` (Tasks 2–5).

**Cross-repo note:** During development tailor-swift uses `replace github.com/farazhassan/gantry => /Users/fhassan-mac/Dev/gantry`, so it builds against whatever branch is checked out in that working tree. Keep `feat/voyage-embeddings` checked out in the gantry tree while executing Tasks 2–5. After the gantry PR merges and a tag is cut, drop the `replace` and pin the tag (see "After gantry PR merges" at the end).

---

### Task 1: Voyage embeddings provider (gantry repo)

Mirror gantry's OpenAI provider. Voyage's `/v1/embeddings` wire format is identical (`{model, input}` → `{data:[{index, embedding}]}`, `Authorization: Bearer`), so only the package name, the three endpoint/env constants, and error prefixes change.

**Files:**
- Create: `/Users/fhassan-mac/Dev/gantry/components/embeddings/voyage/doc.go`
- Create: `/Users/fhassan-mac/Dev/gantry/components/embeddings/voyage/wire.go`
- Create: `/Users/fhassan-mac/Dev/gantry/components/embeddings/voyage/voyage.go`
- Test: `/Users/fhassan-mac/Dev/gantry/components/embeddings/voyage/voyage_test.go`

- [ ] **Step 1: Create the gantry feature branch**

```bash
cd /Users/fhassan-mac/Dev/gantry
git checkout main
git checkout -b feat/voyage-embeddings
```

- [ ] **Step 2: Write the failing test**

Create `/Users/fhassan-mac/Dev/gantry/components/embeddings/voyage/voyage_test.go`:

```go
package voyage_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/gantry/components/embeddings/voyage"
)

// Compile-time guarantee the client satisfies the interface.
var _ embeddings.Embeddings = (*voyage.Client)(nil)

func TestNewPanicsOnEmptyModel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New(\"\"): want panic on empty model, got none")
		}
	}()
	voyage.New("", voyage.WithAPIKey("k"))
}

func TestNewPanicsOnMissingAPIKey(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	defer func() {
		if recover() == nil {
			t.Error("New without key: want panic, got none")
		}
	}()
	voyage.New("voyage-3")
}

func TestEmbedEmptyInputSkipsCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := voyage.New("m", voyage.WithAPIKey("k"), voyage.WithBaseURL(srv.URL),
		voyage.WithHTTPClient(srv.Client()))
	got, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d vectors, want 0", len(got))
	}
	if called {
		t.Error("Embed made an HTTP call for empty input")
	}
}

func TestEmbedReturnsVectorsInInputOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/embeddings" {
			t.Errorf("path = %q, want /v1/embeddings", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("auth = %q, want Bearer k", got)
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Input) != 2 {
			t.Errorf("input len = %d, want 2", len(req.Input))
		}
		// Return out of order to prove the client reorders by index.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 1, "embedding": []float32{0.3, 0.4}},
				{"index": 0, "embedding": []float32{0.1, 0.2}},
			},
		})
	}))
	defer srv.Close()

	c := voyage.New("m", voyage.WithAPIKey("k"), voyage.WithBaseURL(srv.URL),
		voyage.WithHTTPClient(srv.Client()))
	got, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	want := [][]float32{{0.1, 0.2}, {0.3, 0.4}}
	if len(got) != 2 || got[0][0] != want[0][0] || got[1][0] != want[1][0] {
		t.Errorf("Embed = %v, want %v", got, want)
	}
}

func TestEmbedErrorsOnDuplicateIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Same length as input (2) but index 0 is duplicated and 1 is missing.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float32{0.1, 0.2}},
				{"index": 0, "embedding": []float32{0.3, 0.4}},
			},
		})
	}))
	defer srv.Close()

	c := voyage.New("m", voyage.WithAPIKey("k"), voyage.WithBaseURL(srv.URL),
		voyage.WithHTTPClient(srv.Client()))
	if _, err := c.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Error("Embed: want error on duplicate/missing index, got nil")
	}
}

func TestEmbedErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := voyage.New("m", voyage.WithAPIKey("k"), voyage.WithBaseURL(srv.URL),
		voyage.WithHTTPClient(srv.Client()))
	if _, err := c.Embed(context.Background(), []string{"a"}); err == nil {
		t.Error("Embed: want error on 400, got nil")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
cd /Users/fhassan-mac/Dev/gantry && go test ./components/embeddings/voyage/...
```
Expected: build failure — package `voyage` does not exist yet.

(If the build cache is blocked by the sandbox — "Operation not permitted" touching `~/Library/Caches/go-build` — re-run with the sandbox disabled.)

- [ ] **Step 4: Write the wire format**

Create `/Users/fhassan-mac/Dev/gantry/components/embeddings/voyage/wire.go`:

```go
package voyage

// Mirrors Voyage's /v1/embeddings wire format. Private: callers only ever see
// [][]float32.

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []embedDatum `json:"data"`
}

type embedDatum struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}
```

- [ ] **Step 5: Write the package doc**

Create `/Users/fhassan-mac/Dev/gantry/components/embeddings/voyage/doc.go`:

```go
// Package voyage implements embeddings.Embeddings against the Voyage AI
// /v1/embeddings endpoint. Voyage has no Go SDK and its wire format matches the
// OpenAI embeddings shape, so this adapter is standard library only. A
// configurable base URL supports proxies and test servers.
package voyage
```

- [ ] **Step 6: Write the client**

Create `/Users/fhassan-mac/Dev/gantry/components/embeddings/voyage/voyage.go`:

```go
package voyage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	defaultBaseURL = "https://api.voyageai.com"
	embedPath      = "/v1/embeddings"
	apiKeyEnv      = "VOYAGE_API_KEY"
)

// Client implements embeddings.Embeddings over Voyage's /v1/embeddings. Safe
// for concurrent use: it holds no per-call state.
type Client struct {
	model   string
	baseURL string
	apiKey  string
	httpc   *http.Client
}

// Option configures a Client at construction.
type Option func(*Client)

// New returns a Client for the given embedding model (e.g. "voyage-3"). The API
// key comes from WithAPIKey or the VOYAGE_API_KEY environment variable. It
// panics on an empty model or missing key — both are programmer errors.
func New(model string, opts ...Option) *Client {
	if model == "" {
		panic("embeddings/voyage: New requires a non-empty model")
	}
	c := &Client{
		model:   model,
		baseURL: defaultBaseURL,
		apiKey:  os.Getenv(apiKeyEnv),
		httpc:   &http.Client{},
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.apiKey == "" {
		panic("embeddings/voyage: New requires an API key (WithAPIKey or " + apiKeyEnv + ")")
	}
	return c
}

// WithAPIKey sets the bearer token. An empty key is ignored so the env fallback
// still applies.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		if key != "" {
			c.apiKey = key
		}
	}
}

// WithBaseURL points the client at a non-default endpoint (e.g. a proxy or test
// server). A trailing slash is trimmed.
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(url, "/") }
}

// WithHTTPClient supplies the *http.Client used for requests. A nil client is
// ignored.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpc = h
		}
	}
}

// BaseURL returns the endpoint the client posts to (trailing slash trimmed).
func (c *Client) BaseURL() string { return c.baseURL }

// Embed returns one vector per input text, in input order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(embedRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("embeddings/voyage: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+embedPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embeddings/voyage: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings/voyage: do request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("embeddings/voyage: status %d: %s", resp.StatusCode, bytes.TrimSpace(b))
	}

	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("embeddings/voyage: decode response: %w", err)
	}
	if len(er.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings/voyage: got %d vectors for %d inputs", len(er.Data), len(texts))
	}

	out := make([][]float32, len(texts))
	for _, d := range er.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			return nil, fmt.Errorf("embeddings/voyage: response index %d out of range", d.Index)
		}
		if out[d.Index] != nil {
			return nil, fmt.Errorf("embeddings/voyage: duplicate response index %d", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	// A duplicate index (caught above) means another slot was never filled; the
	// matching length check can't catch that on its own.
	for i, v := range out {
		if v == nil {
			return nil, fmt.Errorf("embeddings/voyage: missing vector for input %d", i)
		}
	}
	return out, nil
}
```

- [ ] **Step 7: Run the tests to verify they pass**

```bash
cd /Users/fhassan-mac/Dev/gantry && go test ./components/embeddings/voyage/...
```
Expected: PASS (6 tests). (Disable the sandbox if the build cache is blocked.)

- [ ] **Step 8: Commit**

```bash
cd /Users/fhassan-mac/Dev/gantry
git add components/embeddings/voyage
git commit -m "feat(embeddings): add Voyage AI provider"
```

---

### Task 2: Create tailor-swift feature branch

**Files:** none (branch only).

- [ ] **Step 1: Branch from main**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git checkout main
git checkout -b feat/embeddings-cache
```

- [ ] **Step 2: Verify clean baseline**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./...
```
Expected: PASS (all existing tests green). (Disable the sandbox if the build cache is blocked.)

---

### Task 3: On-disk embedding cache (tailor-swift)

A model-scoped JSON cache keyed by achievement ID. No gantry dependency — pure stdlib.

**Files:**
- Create: `internal/embed/cache.go`
- Test: `internal/embed/cache_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/embed/cache_test.go`:

```go
package embed

import (
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "cache.json")

	c := NewCache("voyage-3")
	c.Put("ach_aaa", []float32{0.1, 0.2})
	c.Put("ach_bbb", []float32{0.3, 0.4})
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadCache(path, "voyage-3")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	v, ok := got.Get("ach_aaa")
	if !ok || len(v) != 2 || v[0] != 0.1 {
		t.Errorf("Get(ach_aaa) = %v, %v", v, ok)
	}
	if _, ok := got.Get("missing"); ok {
		t.Error("Get(missing) reported present")
	}
}

func TestLoadCacheMissingFileIsCold(t *testing.T) {
	dir := t.TempDir()
	c, err := LoadCache(filepath.Join(dir, "nope.json"), "voyage-3")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if c.Model != "voyage-3" || len(c.Vectors) != 0 {
		t.Errorf("cold cache = %+v, want empty voyage-3 cache", c)
	}
}

func TestLoadCacheModelMismatchIsCold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	old := NewCache("voyage-2")
	old.Put("ach_aaa", []float32{1})
	if err := old.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	c, err := LoadCache(path, "voyage-3")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if c.Model != "voyage-3" || len(c.Vectors) != 0 {
		t.Errorf("mismatch should yield empty voyage-3 cache, got %+v", c)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/embed/...
```
Expected: build failure — package `embed` does not exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/embed/cache.go`:

```go
package embed

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Cache is an on-disk store of embedding vectors keyed by a stable content key
// (an achievement ID). It is scoped to one embedding model: vectors from a
// different model are not comparable, so a model mismatch on load is treated as
// a cold cache.
type Cache struct {
	Model   string               `json:"model"`
	Vectors map[string][]float32 `json:"vectors"`
}

// NewCache returns an empty cache for the given model.
func NewCache(model string) *Cache {
	return &Cache{Model: model, Vectors: map[string][]float32{}}
}

// LoadCache reads a cache file. A missing file yields an empty cache (cold
// start, not an error). If the stored model differs from model, the on-disk
// vectors are discarded and an empty cache for model is returned.
func LoadCache(path, model string) (*Cache, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewCache(model), nil
	}
	if err != nil {
		return nil, fmt.Errorf("embed: read cache %s: %w", path, err)
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("embed: parse cache %s: %w", path, err)
	}
	if c.Model != model || c.Vectors == nil {
		return NewCache(model), nil
	}
	return &c, nil
}

// Get returns the cached vector for key and whether it was present.
func (c *Cache) Get(key string) ([]float32, bool) {
	v, ok := c.Vectors[key]
	return v, ok
}

// Put stores vec under key.
func (c *Cache) Put(key string, vec []float32) {
	c.Vectors[key] = vec
}

// Save writes the cache to path atomically (temp file + rename), creating
// parent directories as needed.
func (c *Cache) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("embed: create cache dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("embed: marshal cache: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("embed: write cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("embed: replace cache: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/embed/...
```
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/embed/cache.go internal/embed/cache_test.go
git commit -m "feat(embed): add on-disk embedding cache"
```

---

### Task 4: Wire gantry dependency (tailor-swift go.mod)

Add the gantry requirement and a local `replace` so imports resolve against the checked-out gantry tree. No code imports gantry yet, so this task is verified by module resolution alone.

**Files:**
- Modify: `go.mod` (and `go.sum` will be created/updated)

- [ ] **Step 1: Add the require and replace directives**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
go mod edit -require=github.com/farazhassan/gantry@v0.0.2-beta
go mod edit -replace=github.com/farazhassan/gantry=/Users/fhassan-mac/Dev/gantry
```

(The pinned version is a placeholder; the local `replace` governs resolution during development. Disable the sandbox if module/cache operations are blocked.)

- [ ] **Step 2: Verify the replace resolves to the local checkout**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go list -m -f '{{with .Replace}}{{.Path}}{{end}}' github.com/farazhassan/gantry
```
Expected output: `/Users/fhassan-mac/Dev/gantry`

- [ ] **Step 3: Verify the project still builds**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS (no behavior change; the require is present but unused).

- [ ] **Step 4: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add go.mod go.sum
git commit -m "build: depend on gantry via local replace for Voyage embeddings"
```

---

### Task 5: Store embedder with cache (tailor-swift)

An `Embedder` that embeds every achievement once, consulting the cache and sending only misses to the provider. Achievement IDs are content hashes, so changed text is a new key and a natural miss.

**Files:**
- Create: `internal/embed/embedder.go`
- Test: `internal/embed/embedder_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/embed/embedder_test.go`:

```go
package embed

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/farazhassan/tailor-swift/internal/store"
)

// fakeEmb records how many times Embed is called and returns a deterministic
// vector per input so tests can assert cache behavior without a network.
type fakeEmb struct {
	calls  int
	inputs [][]string
}

func (f *fakeEmb) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.calls++
	f.inputs = append(f.inputs, append([]string(nil), texts...))
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{float32(len(t))}
	}
	return out, nil
}

func twoAchStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.ParseReader([]byte("## Acme — Eng\n### P\n- alpha\n- beta\n"), "mem")
	if err != nil {
		t.Fatalf("ParseReader: %v", err)
	}
	if got := len(s.Achievements()); got != 2 {
		t.Fatalf("fixture has %d achievements, want 2", got)
	}
	return s
}

func TestEmbedStoreEmbedsEachAchievementOnce(t *testing.T) {
	s := twoAchStore(t)
	f := &fakeEmb{}
	e := NewEmbedder(f, NewCache("voyage-3"))

	got, err := e.EmbedStore(context.Background(), s)
	if err != nil {
		t.Fatalf("EmbedStore: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d vectors, want 2", len(got))
	}
	if f.calls != 1 {
		t.Errorf("provider calls = %d, want 1", f.calls)
	}
	for _, a := range s.Achievements() {
		if _, ok := got[a.ID]; !ok {
			t.Errorf("missing vector for %s", a.ID)
		}
	}
}

func TestEmbedStoreUsesCacheOnSecondCall(t *testing.T) {
	s := twoAchStore(t)
	f := &fakeEmb{}
	e := NewEmbedder(f, NewCache("voyage-3"))

	if _, err := e.EmbedStore(context.Background(), s); err != nil {
		t.Fatalf("first EmbedStore: %v", err)
	}
	if _, err := e.EmbedStore(context.Background(), s); err != nil {
		t.Fatalf("second EmbedStore: %v", err)
	}
	if f.calls != 1 {
		t.Errorf("provider calls = %d after warm cache, want 1", f.calls)
	}
}

func TestEmbedStorePersistedCacheSkipsProvider(t *testing.T) {
	s := twoAchStore(t)
	path := filepath.Join(t.TempDir(), "cache.json")

	first := NewEmbedder(&fakeEmb{}, NewCache("voyage-3"))
	if _, err := first.EmbedStore(context.Background(), s); err != nil {
		t.Fatalf("EmbedStore: %v", err)
	}
	if err := first.Cache().Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadCache(path, "voyage-3")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	f2 := &fakeEmb{}
	second := NewEmbedder(f2, loaded)
	if _, err := second.EmbedStore(context.Background(), s); err != nil {
		t.Fatalf("second EmbedStore: %v", err)
	}
	if f2.calls != 0 {
		t.Errorf("provider calls = %d with persisted cache, want 0", f2.calls)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/embed/...
```
Expected: build failure — `NewEmbedder`/`EmbedStore` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/embed/embedder.go`:

```go
package embed

import (
	"context"
	"fmt"

	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// Embedder turns content-store achievements into vectors, reusing an on-disk
// cache so unchanged text is never re-sent to the provider. Achievement IDs are
// content hashes (see store.DeriveID), so a changed bullet yields a new key and
// a natural cache miss.
type Embedder struct {
	client embeddings.Embeddings
	cache  *Cache
}

// NewEmbedder wraps an embeddings client and a cache.
func NewEmbedder(client embeddings.Embeddings, cache *Cache) *Embedder {
	return &Embedder{client: client, cache: cache}
}

// Cache returns the underlying cache so callers can persist it after embedding.
func (e *Embedder) Cache() *Cache { return e.cache }

// EmbedStore returns a vector for every achievement in s, keyed by achievement
// ID. Only achievements missing from the cache are sent to the provider; the
// cache is updated in memory with any new vectors. Call Cache().Save to persist.
func (e *Embedder) EmbedStore(ctx context.Context, s *store.Store) (map[string][]float32, error) {
	achs := s.Achievements()

	// Collect unique cache misses, preserving first-seen order so the provider
	// response (indexed by position) maps back to the right ID.
	var missIDs []string
	var missText []string
	seen := map[string]bool{}
	for _, a := range achs {
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		if _, ok := e.cache.Get(a.ID); !ok {
			missIDs = append(missIDs, a.ID)
			missText = append(missText, a.Text)
		}
	}

	if len(missText) > 0 {
		vecs, err := e.client.Embed(ctx, missText)
		if err != nil {
			return nil, fmt.Errorf("embed: provider: %w", err)
		}
		if len(vecs) != len(missIDs) {
			return nil, fmt.Errorf("embed: got %d vectors for %d inputs", len(vecs), len(missIDs))
		}
		for i, id := range missIDs {
			e.cache.Put(id, vecs[i])
		}
	}

	out := make(map[string][]float32, len(seen))
	for id := range seen {
		v, ok := e.cache.Get(id)
		if !ok {
			return nil, fmt.Errorf("embed: cache miss after fill for %s", id)
		}
		out[id] = v
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/embed/...
```
Expected: PASS (all cache + embedder tests).

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/embed/embedder.go internal/embed/embedder_test.go
git commit -m "feat(embed): embed store achievements with cache reuse"
```

---

### Task 6: Voyage client factory (tailor-swift)

A thin constructor that builds the real Voyage client from the environment, returning an error (not a panic) when the key is missing so the CLI can report cleanly. This task also concretely validates the cross-repo wiring (the `voyage` package resolves through the `replace`).

**Files:**
- Create: `internal/embed/voyage.go`
- Test: `internal/embed/voyage_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/embed/voyage_test.go`:

```go
package embed

import "testing"

func TestNewVoyageClientErrorsWithoutKey(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	if _, err := NewVoyageClient(); err == nil {
		t.Error("NewVoyageClient: want error when VOYAGE_API_KEY unset, got nil")
	}
}

func TestNewVoyageClientBuildsWithKey(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "k")
	c, err := NewVoyageClient()
	if err != nil {
		t.Fatalf("NewVoyageClient: %v", err)
	}
	if c == nil {
		t.Error("NewVoyageClient returned nil client")
	}
}

func TestNewVoyageClientHonorsModelEnv(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "k")
	t.Setenv("VOYAGE_MODEL", "voyage-3-large")
	if _, err := NewVoyageClient(); err != nil {
		t.Fatalf("NewVoyageClient: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/embed/...
```
Expected: build failure — `NewVoyageClient` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/embed/voyage.go`:

```go
package embed

import (
	"fmt"
	"os"

	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/gantry/components/embeddings/voyage"
)

const (
	defaultVoyageModel = "voyage-3"
	voyageModelEnv     = "VOYAGE_MODEL"
	voyageKeyEnv       = "VOYAGE_API_KEY"
)

// NewVoyageClient builds a Voyage embeddings client from the environment. The
// model comes from VOYAGE_MODEL (default "voyage-3"); the key from
// VOYAGE_API_KEY. It returns an error — rather than panicking — when the key is
// absent, so the CLI can report it cleanly.
func NewVoyageClient() (embeddings.Embeddings, error) {
	if os.Getenv(voyageKeyEnv) == "" {
		return nil, fmt.Errorf("embed: %s is not set", voyageKeyEnv)
	}
	model := os.Getenv(voyageModelEnv)
	if model == "" {
		model = defaultVoyageModel
	}
	return voyage.New(model), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/embed/...
```
Expected: PASS.

- [ ] **Step 5: Tidy modules and verify the whole tree**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go mod tidy && go build ./... && go test ./...
```
Expected: PASS. `go.sum` updated; the `replace` line for gantry remains in `go.mod`.

- [ ] **Step 6: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/embed/voyage.go internal/embed/voyage_test.go go.mod go.sum
git commit -m "feat(embed): add Voyage client factory from environment"
```

---

## After gantry PR merges (follow-up, not part of this plan's tasks)

The local `replace` is a development bridge. Once the gantry `feat/voyage-embeddings` branch is merged via PR and a new gantry version is tagged (e.g. `v0.0.3`):

1. In tailor-swift, pin the tag and drop the replace:
   ```bash
   go mod edit -dropreplace=github.com/farazhassan/gantry
   go get github.com/farazhassan/gantry@v0.0.3
   go mod tidy && go build ./... && go test ./...
   ```
2. Commit the go.mod/go.sum change.

This keeps the upstream-first contract: the provider lives in gantry; tailor-swift consumes a tagged release.

## Self-Review

- **Spec coverage:** Plan 2 covers the design's "Voyage embeddings (upstream to gantry)" and "local on-disk vector cache so the same content/JD is never re-embedded." Similarity search / ranking is deferred to Plan 3 (Retrieval), per the spec's plan sequencing.
- **Placeholders:** none — every step has full code or an exact command with expected output.
- **Type consistency:** `embeddings.Embeddings` interface (`Embed(ctx, []string) ([][]float32, error)`) is used consistently by the fake, the factory return type, and `Embedder.client`. `Cache` methods (`NewCache`, `LoadCache`, `Get`, `Put`, `Save`, `Cache()`) match across cache and embedder code and tests. Achievement keying uses `store.Achievement.ID` everywhere.
