# Pipeline Acquisition Implementation Plan (Plan 10a)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `internal/pipeline` package whose single `Acquire` function chains the existing `store`, `embed`, `retrieve`, and `jd` packages into one pre-loop acquisition step, returning exactly the inputs `orchestrate.Run` consumes (parsed store, requirements, retrieved candidates) plus coverage gaps and the raw posting.

**Architecture:** One package, one source file `pipeline.go` holding `Config` (static inputs), `Deps` (injected `gantry.LLMClient` + `embeddings.Embeddings`), `Result` (the orchestrate inputs + `Gaps` + `Posting`), and `Acquire`. `Acquire` runs: parse store → build a model-scoped embedding cache → embed store → build the cosine index → acquire the JD (LLM requirement extraction, cached) → embed the requirement texts → select candidates → persist the cache. Fully unit-testable with a deterministic fake embedder and gantry's mock LLM — no API keys, no network, no `pdflatex`.

**Tech Stack:** Go 1.26, stdlib (`context`, `fmt`, `os`, `path/filepath`, `strings`), gantry (`github.com/farazhassan/gantry` for `LLMClient`/`Document`; `components/embeddings` for the `Embeddings` interface; `eval` for the mock LLM in tests), and the repo's `internal/{store,embed,retrieve,jd}`.

**Branch:** `feat/pipeline` in `/Users/fhassan-mac/Dev/tailor-swift`.

## Prerequisites

All collaborators are already merged on `main`:
- `internal/store` — `Parse(path) (*Store, error)`; `Store{Profile{Name},...}`.
- `internal/embed` — `NewCache(model) *Cache`, `LoadCache(path, model) (*Cache, error)`, `NewEmbedder(client embeddings.Embeddings, cache *Cache) *Embedder`, `(*Embedder).EmbedStore(ctx, *store.Store) (map[string][]float32, error)`, `(*Embedder).EmbedTexts(ctx, []string) ([][]float32, error)`, `(*Embedder).Cache() *Cache`, `(*Cache).Save(path) error`. A miss-free `ensure` means: if every key is already cached, the underlying `embeddings.Embeddings.Embed` is never called.
- `internal/retrieve` — `NewIndex(*store.Store, map[string][]float32) (*Index, error)` (errors if an achievement has no vector); `Select(ix *Index, reqs []jd.Requirement, reqVecs [][]float32, k int, minScore float64) (*Selection, error)` (errors if `len(reqs) != len(reqVecs)`); `Selection{Documents []gantry.Document; Gaps []jd.Requirement}`. A must-have whose best match scores below `minScore` is a gap.
- `internal/jd` — `Acquire(ctx, llm gantry.LLMClient, opts Options) (*Posting, error)` where `Options{URL, FilePath, CacheDir, HTTPClient}`. **`URL` is mandatory** (it is the cache key); `FilePath` only supplies the raw text offline. `Posting{URL, FetchedAt, RawText, Requirements []Requirement}`; `Requirement{Text string; MustHave bool}`. On a cache hit (same `CacheDir`+`URL`) it returns the cached posting **without calling the LLM**.
- gantry — `gantry.LLMClient`, `gantry.Document{ID, Content, Score, Metadata}`, `components/embeddings.Embeddings` (`Embed(ctx, []string) ([][]float32, error)`), `eval.NewMockLLMClient(...gantry.LLMResponse)` (no-arg = a client that errors if called).

The JD LLM extraction expects the model to return a JSON array `[{"text": string, "must_have": bool}]` (a leading ```` ```json ```` fence is tolerated).

**Environment:** No API key, no network, no `pdflatex` — every test uses a fake `embeddings.Embeddings` and gantry's mock LLM, with the JD supplied via a temp `FilePath`.

**Sandbox note:** `go` and `git` commands may fail under the harness sandbox with "operation not permitted" on the build cache (`~/Library/Caches/go-build`) or module-proxy "context deadline exceeded". This is expected and pre-authorized — if any step fails this way, retry the same command with the sandbox disabled.

## File structure

- `internal/pipeline/pipeline.go` — `Config`, `Deps`, `Result`, the `defaultModel`/`defaultTopK` consts, and `Acquire`. One responsibility: assemble the acquisition chain.
- `internal/pipeline/pipeline_test.go` — the deterministic `fakeEmbedder` and `panicEmbedder`, small helpers, and the seven behavior tests.

## Key design decisions

- **Injected deps, not internal client construction.** `Deps` carries a `gantry.LLMClient` and an `embeddings.Embeddings`; the CLI (Plan 10b) builds the real Anthropic/Voyage clients and passes them in. Tests pass fakes. This keeps the package unit-testable.
- **`Config.Model` scopes the embedding cache.** `embed`'s cache is model-scoped but the model id is not exposed on the `embeddings.Embeddings` interface, so `Config.Model` (default `"voyage-3"`) is threaded into `NewCache`/`LoadCache`.
- **Error classification.** Bad content path, missing `JDURL`, `jd.Acquire` failure, empty requirements, embedder errors, and index/select errors are fatal. `Gaps` are returned (non-fatal). The cache save is best-effort (error swallowed).

---

### Task 1: Branch and confirm dependencies resolve

**Files:**
- None modified (branch + verification only)

- [ ] **Step 1: Create the feature branch**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git checkout main
git checkout -b feat/pipeline
```

- [ ] **Step 2: Confirm the prerequisite packages resolve**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go list ./internal/store/... ./internal/embed/... ./internal/retrieve/... ./internal/jd/...
```
Expected: prints all four package paths. If any is missing, you based off the wrong branch — all must be present on `main`.

- [ ] **Step 3: Confirm the tree builds and tests pass before changes**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS across all packages (no behavior change yet).

No commit for this task — it only establishes the branch.

---

### Task 2: The acquisition pipeline

**Files:**
- Create: `internal/pipeline/pipeline.go`
- Test: `internal/pipeline/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/pipeline/pipeline_test.go` with exactly this content:

```go
package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
)

// --- fakes & helpers ------------------------------------------------------

// vecFor maps text to a deterministic 3-dim vector by keyword, so retrieval
// rankings are predictable: "go" -> x axis, "kafka" -> y axis, "rust" -> z axis.
// Order matters: check the more specific keywords before "go".
func vecFor(text string) []float32 {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "kafka"):
		return []float32{0, 1, 0}
	case strings.Contains(t, "rust"):
		return []float32{0, 0, 1}
	case strings.Contains(t, "go"):
		return []float32{1, 0, 0}
	default:
		return []float32{1, 1, 1}
	}
}

// fakeEmbedder returns vecFor(text) for each input, in order.
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = vecFor(t)
	}
	return out, nil
}

// panicEmbedder fails the test if Embed is ever called (used to prove the cache
// served every lookup).
type panicEmbedder struct{ t *testing.T }

func (p panicEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	p.t.Fatalf("Embed should not be called; the cache should have served all vectors")
	return nil, nil
}

// reqLLM returns a mock LLM whose single response is the given requirements JSON.
func reqLLM(reqsJSON string) gantry.LLMClient {
	return eval.NewMockLLMClient(gantry.LLMResponse{Content: reqsJSON, StopReason: gantry.StopReasonEnd})
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

const sampleContent = `# Ada Lovelace

## Contact
Email: ada@example.com

## Acme Corp

### Billing
- Built a Go billing service
- Scaled Kafka pipelines
`

// baseCfg writes a content file and a JD file under a temp dir and returns a
// Config + Deps wired to a fake embedder and a mock LLM scripted with reqsJSON.
func baseCfg(t *testing.T, content, reqsJSON string) (Config, Deps) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		ContentPath: writeFile(t, dir, "content.md", content),
		JDURL:       "https://example.com/job",
		JDFile:      writeFile(t, dir, "jd.txt", "We need a backend engineer."),
		MinScore:    0.5,
		JDCacheDir:  filepath.Join(dir, "jdcache"),
	}
	deps := Deps{LLM: reqLLM(reqsJSON), Embedder: fakeEmbedder{}}
	return cfg, deps
}

// --- tests ----------------------------------------------------------------

func TestAcquireHappyPath(t *testing.T) {
	reqs := `[{"text":"Go","must_have":true},{"text":"Kafka","must_have":false}]`
	cfg, deps := baseCfg(t, sampleContent, reqs)
	res, err := Acquire(context.Background(), cfg, deps)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if len(res.Requirements) != 2 {
		t.Fatalf("requirements = %d, want 2", len(res.Requirements))
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(res.Candidates))
	}
	if len(res.Gaps) != 0 {
		t.Errorf("gaps = %+v, want none", res.Gaps)
	}
	if res.Store == nil || res.Store.Profile.Name != "Ada Lovelace" {
		t.Errorf("store not parsed: %+v", res.Store)
	}
	if res.Posting == nil || res.Posting.URL != "https://example.com/job" {
		t.Errorf("posting: %+v", res.Posting)
	}
	if res.Candidates[0].Score < 0.99 {
		t.Errorf("top candidate score = %v, want ~1.0", res.Candidates[0].Score)
	}
}

func TestAcquireReportsCoverageGap(t *testing.T) {
	reqs := `[{"text":"Go","must_have":true},{"text":"Rust","must_have":true}]`
	cfg, deps := baseCfg(t, sampleContent, reqs)
	res, err := Acquire(context.Background(), cfg, deps)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if len(res.Gaps) != 1 || res.Gaps[0].Text != "Rust" {
		t.Fatalf("gaps = %+v, want exactly [Rust]", res.Gaps)
	}
}

func TestAcquireAppliesTopKDefault(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Ada Lovelace\n\n## Contact\nEmail: ada@example.com\n\n## Acme Corp\n\n### Work\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, "- Built Go service number %d\n", i)
	}
	reqs := `[{"text":"Go","must_have":true}]`
	cfg, deps := baseCfg(t, b.String(), reqs)
	res, err := Acquire(context.Background(), cfg, deps)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 10 distinct Go achievements all score 1.0; default TopK=8 caps the union.
	if len(res.Candidates) != 8 {
		t.Fatalf("candidates = %d, want 8 (default TopK)", len(res.Candidates))
	}
}

func TestAcquireErrorsOnNoRequirements(t *testing.T) {
	cfg, deps := baseCfg(t, sampleContent, `[]`)
	_, err := Acquire(context.Background(), cfg, deps)
	if err == nil || !strings.Contains(err.Error(), "no requirements") {
		t.Fatalf("err = %v, want a no-requirements error", err)
	}
}

func TestAcquireErrorsWithoutJDURL(t *testing.T) {
	cfg, deps := baseCfg(t, sampleContent, `[{"text":"Go","must_have":true}]`)
	cfg.JDURL = ""
	_, err := Acquire(context.Background(), cfg, deps)
	if err == nil || !strings.Contains(err.Error(), "JD URL is required") {
		t.Fatalf("err = %v, want a JD-URL-required error", err)
	}
}

func TestAcquireReusesEmbedCache(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		ContentPath:    writeFile(t, dir, "content.md", sampleContent),
		JDURL:          "https://example.com/job",
		JDFile:         writeFile(t, dir, "jd.txt", "We need a backend engineer."),
		MinScore:       0.5,
		EmbedCachePath: filepath.Join(dir, "embed-cache.json"),
		JDCacheDir:     filepath.Join(dir, "jdcache"),
	}
	reqs := `[{"text":"Go","must_have":true},{"text":"Kafka","must_have":false}]`

	// First run populates both the embedding cache and the JD cache.
	if _, err := Acquire(context.Background(), cfg, Deps{LLM: reqLLM(reqs), Embedder: fakeEmbedder{}}); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if _, err := os.Stat(cfg.EmbedCachePath); err != nil {
		t.Fatalf("embed cache not written: %v", err)
	}

	// Second run: a panicking embedder and a no-response LLM. The embed cache
	// must serve every vector (no Embed call) and the JD cache must serve the
	// posting (no LLM call), so neither fake is invoked.
	deps2 := Deps{LLM: eval.NewMockLLMClient(), Embedder: panicEmbedder{t}}
	res, err := Acquire(context.Background(), cfg, deps2)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if len(res.Candidates) != 2 {
		t.Errorf("candidates = %d, want 2", len(res.Candidates))
	}
}

func TestAcquireErrorsOnBadContentPath(t *testing.T) {
	cfg, deps := baseCfg(t, sampleContent, `[{"text":"Go","must_have":true}]`)
	cfg.ContentPath = filepath.Join(t.TempDir(), "does-not-exist.md")
	_, err := Acquire(context.Background(), cfg, deps)
	if err == nil || !strings.Contains(err.Error(), "parse content") {
		t.Fatalf("err = %v, want a parse-content error", err)
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/pipeline/...
```
Expected: build failure — `Config`, `Deps`, `Result`, `Acquire` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/pipeline/pipeline.go` with exactly this content:

```go
// Package pipeline performs the pre-loop acquisition for a tailoring run: it
// parses the content store, embeds and indexes the achievements, acquires the
// job posting, retrieves the candidate set for the posting's requirements, and
// returns exactly the inputs orchestrate.Run consumes (plus the coverage gaps
// and the raw posting for downstream visibility).
package pipeline

import (
	"context"
	"fmt"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/tailor-swift/internal/embed"
	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/retrieve"
	"github.com/farazhassan/tailor-swift/internal/store"
)

const (
	defaultModel = "voyage-3"
	defaultTopK  = 8
)

// Config is the static input to an acquisition (the CLI fills it from flags).
type Config struct {
	ContentPath    string  // path to the content store markdown (store.Parse)
	JDURL          string  // job posting URL (required: jd.Acquire's cache key + posting identity)
	JDFile         string  // optional local JD text file (offline source; URL still required)
	Model          string  // embedding model id for cache scoping (default "voyage-3")
	TopK           int     // top-K candidates per requirement (default 8 when 0)
	MinScore       float64 // min similarity for a must-have to count as covered
	EmbedCachePath string  // on-disk embedding cache file; "" disables caching
	JDCacheDir     string  // directory for cached postings (jd.Options.CacheDir)
}

// Deps are the injected collaborators: production passes real clients; tests
// pass mocks.
type Deps struct {
	LLM      gantry.LLMClient      // for jd requirement extraction
	Embedder embeddings.Embeddings // Voyage in production; a fake in tests
}

// Result is exactly what orchestrate.Config needs, plus visibility extras.
type Result struct {
	Store        *store.Store      // parsed content store        (-> orchestrate Config.Store)
	Posting      *jd.Posting       // the acquired JD (URL, raw text, requirements)
	Requirements []jd.Requirement  // == Posting.Requirements     (-> orchestrate Config.Requirements)
	Candidates   []gantry.Document // retrieved, ranked, deduped  (-> orchestrate Config.Candidates)
	Gaps         []jd.Requirement  // must-haves with no match above MinScore (warn, non-fatal)
}

// Acquire runs the pre-loop chain: parse the content store, embed and index the
// achievements, acquire the job posting, embed its requirements, and select the
// candidate set. It returns the assembled orchestrate inputs plus any coverage
// gaps. JDURL is required. An empty requirement set is an error (generating from
// nothing is never useful). The embedding cache save is best-effort.
func Acquire(ctx context.Context, cfg Config, deps Deps) (*Result, error) {
	if cfg.JDURL == "" {
		return nil, fmt.Errorf("pipeline: JD URL is required")
	}
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	topK := cfg.TopK
	if topK == 0 {
		topK = defaultTopK
	}

	s, err := store.Parse(cfg.ContentPath)
	if err != nil {
		return nil, fmt.Errorf("pipeline: parse content: %w", err)
	}

	cache := embed.NewCache(model)
	if cfg.EmbedCachePath != "" {
		cache, err = embed.LoadCache(cfg.EmbedCachePath, model)
		if err != nil {
			return nil, fmt.Errorf("pipeline: load embed cache: %w", err)
		}
	}
	emb := embed.NewEmbedder(deps.Embedder, cache)

	vectors, err := emb.EmbedStore(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("pipeline: embed store: %w", err)
	}

	ix, err := retrieve.NewIndex(s, vectors)
	if err != nil {
		return nil, fmt.Errorf("pipeline: build index: %w", err)
	}

	posting, err := jd.Acquire(ctx, deps.LLM, jd.Options{
		URL:      cfg.JDURL,
		FilePath: cfg.JDFile,
		CacheDir: cfg.JDCacheDir,
	})
	if err != nil {
		return nil, fmt.Errorf("pipeline: acquire jd: %w", err)
	}
	if len(posting.Requirements) == 0 {
		return nil, fmt.Errorf("pipeline: job posting has no requirements")
	}

	reqTexts := make([]string, len(posting.Requirements))
	for i, r := range posting.Requirements {
		reqTexts[i] = r.Text
	}
	reqVecs, err := emb.EmbedTexts(ctx, reqTexts)
	if err != nil {
		return nil, fmt.Errorf("pipeline: embed requirements: %w", err)
	}

	sel, err := retrieve.Select(ix, posting.Requirements, reqVecs, topK, cfg.MinScore)
	if err != nil {
		return nil, fmt.Errorf("pipeline: select candidates: %w", err)
	}

	if cfg.EmbedCachePath != "" {
		_ = emb.Cache().Save(cfg.EmbedCachePath) // best-effort: vectors already usable this run
	}

	return &Result{
		Store:        s,
		Posting:      posting,
		Requirements: posting.Requirements,
		Candidates:   sel.Documents,
		Gaps:         sel.Gaps,
	}, nil
}
```

- [ ] **Step 4: Run the tests + vet, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/pipeline/... && go vet ./internal/pipeline/...
```
Expected: PASS (7 tests), vet clean.

- [ ] **Step 5: Final whole-tree check**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS across all packages (`cmd/tailor`, `internal/embed`, `internal/evaluate`, `internal/fence`, `internal/generate`, `internal/jd`, `internal/orchestrate`, `internal/pipeline`, `internal/render`, `internal/retrieve`, `internal/store`).

- [ ] **Step 6: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/pipeline/pipeline.go internal/pipeline/pipeline_test.go
git commit -m "feat(pipeline): add pre-loop acquisition chaining store, embed, retrieve, and jd"
```

---

## Out of scope (deferred to Plan 10b)

- The `tailor generate` CLI command, flag parsing, and exit codes.
- Constructing the real `anthropic.New(...)` LLM client, the Voyage embedder (`embed.NewVoyageClient`), and the `render.PDFLaTeX` compiler.
- Calling `orchestrate.Run` with the `Result` and emitting artifacts (`out/<slug>-<date>/`: `resume.pdf`, `resume.tex`, `critique.json`, `run.log`), plus surfacing `Result.Gaps` as a user warning.

## Self-Review

- **Spec coverage:** Every spec section maps to Task 2. The `Config`/`Deps`/`Result` types match the spec's "Types" section field-for-field (including `Config.Model` for cache scoping). `Acquire`'s body implements the spec's 12-step "Data flow" in order: JDURL validation, default Model/TopK, `store.Parse`, model-scoped `NewCache`/`LoadCache`, `EmbedStore`, `NewIndex`, `jd.Acquire`, empty-requirements guard, `EmbedTexts`, `Select`, best-effort `Cache().Save`, and the `Result`. The spec's error model is realized exactly: fatal for bad content path (`TestAcquireErrorsOnBadContentPath`), missing JDURL (`TestAcquireErrorsWithoutJDURL`), empty requirements (`TestAcquireErrorsOnNoRequirements`); non-fatal `Gaps` (`TestAcquireReportsCoverageGap`); best-effort cache save (exercised by `TestAcquireReusesEmbedCache`). The spec's seven test cases are all present (happy path, coverage gap, TopK default, no requirements, no JD URL, cache round-trip, parse failure). The fakes match the spec's testing strategy (deterministic keyword embedder + gantry mock LLM + temp JD file).
- **Placeholders:** none — every step is complete code, an exact command with expected output, or a commit.
- **Type consistency:** `Acquire(ctx, cfg Config, deps Deps) (*Result, error)` and the `Config`/`Deps`/`Result` field names are used identically in `pipeline.go` and `pipeline_test.go`. Collaborator calls match the merged packages exactly: `store.Parse(path) (*store.Store, error)`; `embed.NewCache(model)`, `embed.LoadCache(path, model)`, `embed.NewEmbedder(client, cache)`, `(*Embedder).EmbedStore`/`EmbedTexts`/`Cache`, `(*Cache).Save`; `retrieve.NewIndex(s, vectors)`, `retrieve.Select(ix, reqs, reqVecs, k, minScore)` → `*retrieve.Selection{Documents, Gaps}`; `jd.Acquire(ctx, llm, jd.Options{URL, FilePath, CacheDir})` → `*jd.Posting{URL, Requirements}`, `jd.Requirement{Text, MustHave}`. The fake embedder satisfies `embeddings.Embeddings` (single `Embed(ctx, []string) ([][]float32, error)` method). The mock LLM returns the `[{"text",...,"must_have"}]` JSON that `jd.ExtractRequirements` parses.
- **Retrieval arithmetic sanity:** with the keyword vectors, requirement "Go" `[1,0,0]` scores cosine 1.0 against the Go achievement and 0.0 against Kafka; "Kafka" `[0,1,0]` the reverse; the union dedupes to both at score 1.0 (happy path: 2 candidates, no gaps). "Rust" `[0,0,1]` scores 0.0 against both Go/Kafka achievements, below `MinScore=0.5`, and being must-have becomes the single gap. Ten distinct Go achievements all score 1.0, so the default `TopK=8` caps the union at 8.
