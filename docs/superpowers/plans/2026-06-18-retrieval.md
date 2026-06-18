# Retrieval Implementation Plan (Plan 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Embed a job description's requirement chunks (cached), then rank a content store's already-embedded achievements against those requirements by cosine similarity, producing a candidate content set plus a list of must-have requirements no achievement covers (coverage gaps).

**Architecture:** Two pieces. (1) Extend the existing `internal/embed` package with `EmbedTexts` — a cached, order-preserving embed call for arbitrary strings (JD requirement chunks), sharing one private miss/fill helper with the existing `EmbedStore` (DRY). (2) A new `internal/retrieve` package: pure `cosine` math, an in-memory `Index` over `(achievement, vector)` pairs that yields `gantry.Document`s ranked by cosine (`TopK`), and a `Select` orchestrator that unions per-requirement top-k results and reports must-have coverage gaps. Retrieval produces `gantry.Document` (gantry's native retrieved-chunk type) so downstream stages speak gantry's currency.

**Tech Stack:** Go 1.26, stdlib (`math`, `sort`), gantry (`github.com/farazhassan/gantry` for `Document`), `internal/store` (achievements + `DeriveID`), `internal/jd` (`Requirement`). No new external dependencies. Tests use deterministic hand-written vectors and the existing `fakeEmb` test double — no API key.

**Branch:** `feat/retrieval` in `/Users/fhassan-mac/Dev/tailor-swift`.

## Prerequisites

This plan's `internal/retrieve` package imports `internal/jd` (the `Requirement` type, added in Plan 3 / PR #3 on branch `feat/jd-acquisition`) and `internal/embed` + `internal/store` (already on `main`).

- **If PR #3 is merged to `main`:** branch `feat/retrieval` off `main` (Task 1 below).
- **If PR #3 is NOT yet merged:** branch `feat/retrieval` off `feat/jd-acquisition` instead, so the `jd` package is present. Task 1's verification step confirms both `jd` and `embed` resolve.

This plan does NOT fetch, render, generate, or evaluate — it only embeds JD requirements and ranks content. Wiring retrieval into the generate flow happens in Plan 6.

**Sandbox note:** `go` and `git` commands may fail with "operation not permitted" on the build cache (`~/Library/Caches/go-build`) or git transport under the harness sandbox. When that happens, retry the same command with the sandbox disabled. This is expected and pre-authorized for this project.

---

### Task 1: Branch and confirm dependencies resolve

**Files:**
- None modified (branch + verification only)

- [ ] **Step 1: Create the feature branch**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
# Base off main if PR #3 is merged; otherwise: git checkout feat/jd-acquisition
git checkout main
git checkout -b feat/retrieval
```

- [ ] **Step 2: Confirm the prerequisite packages resolve**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go list ./internal/jd/... ./internal/embed/... ./internal/store/...
```
Expected: prints the three package paths with no error. If `internal/jd` is "no Go files" or "cannot find package", you based off the wrong branch — see Prerequisites.

- [ ] **Step 3: Confirm the tree builds and tests pass before changes**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS (no behavior change yet).

No commit for this task — it only establishes the branch.

---

### Task 2: `EmbedTexts` — cached embedding for JD requirement chunks

**Files:**
- Modify: `internal/embed/embedder.go`
- Test: `internal/embed/embedder_test.go` (append one test)

- [ ] **Step 1: Write the failing test**

Append to `internal/embed/embedder_test.go`:

```go
func TestEmbedTextsReturnsVectorsInOrderAndCaches(t *testing.T) {
	f := &fakeEmb{}
	e := NewEmbedder(f, NewCache("voyage-3"))

	got, err := e.EmbedTexts(context.Background(), []string{"go", "kafka", "go"})
	if err != nil {
		t.Fatalf("EmbedTexts: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d vectors, want 3", len(got))
	}
	// fakeEmb returns {len(text)} so duplicates match and order is preserved.
	if got[0][0] != 2 || got[1][0] != 5 || got[2][0] != 2 {
		t.Errorf("vectors = %v, want first elems [2 5 2]", got)
	}
	// "go" appears twice but must be embedded once (dedup by content hash).
	if f.calls != 1 {
		t.Errorf("provider calls = %d, want 1", f.calls)
	}
	if len(f.inputs[0]) != 2 {
		t.Errorf("provider got %d unique texts, want 2", len(f.inputs[0]))
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/embed/...
```
Expected: build failure — `e.EmbedTexts` undefined.

- [ ] **Step 3: Refactor `EmbedStore` and add `EmbedTexts` sharing one helper**

Replace the entire body of `internal/embed/embedder.go` (keep the package clause and imports; the only import change is none — `store` and `embeddings` are already imported) with:

```go
package embed

import (
	"context"
	"fmt"

	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// Embedder turns text into vectors, reusing an on-disk cache so unchanged text
// is never re-sent to the provider. Keys are content hashes (see
// store.DeriveID), so changed text yields a new key and a natural cache miss.
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

// ensure embeds any (ids[i], texts[i]) pair not already cached and stores the
// result. ids and texts are parallel slices; duplicate ids are embedded once.
// First-seen order is preserved so the provider response (indexed by position)
// maps back to the right id.
func (e *Embedder) ensure(ctx context.Context, ids, texts []string) error {
	var missIDs []string
	var missText []string
	seen := map[string]bool{}
	for i, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := e.cache.Get(id); !ok {
			missIDs = append(missIDs, id)
			missText = append(missText, texts[i])
		}
	}
	if len(missText) == 0 {
		return nil
	}
	vecs, err := e.client.Embed(ctx, missText)
	if err != nil {
		return fmt.Errorf("embed: provider: %w", err)
	}
	if len(vecs) != len(missIDs) {
		return fmt.Errorf("embed: got %d vectors for %d inputs", len(vecs), len(missIDs))
	}
	for i, id := range missIDs {
		e.cache.Put(id, vecs[i])
	}
	return nil
}

// EmbedStore returns a vector for every achievement in s, keyed by achievement
// ID. Only achievements missing from the cache are sent to the provider; the
// cache is updated in memory with any new vectors. Call Cache().Save to persist.
func (e *Embedder) EmbedStore(ctx context.Context, s *store.Store) (map[string][]float32, error) {
	achs := s.Achievements()
	ids := make([]string, len(achs))
	texts := make([]string, len(achs))
	for i, a := range achs {
		ids[i] = a.ID
		texts[i] = a.Text
	}
	if err := e.ensure(ctx, ids, texts); err != nil {
		return nil, err
	}
	out := make(map[string][]float32, len(achs))
	for _, a := range achs {
		v, ok := e.cache.Get(a.ID)
		if !ok {
			return nil, fmt.Errorf("embed: cache miss after fill for %s", a.ID)
		}
		out[a.ID] = v
	}
	return out, nil
}

// EmbedTexts returns a vector for each text, in input order, reusing the cache.
// Texts are keyed by content hash (store.DeriveID), so identical text — within
// one call or across calls/JDs — is embedded only once. Used for JD requirement
// chunks.
func (e *Embedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	ids := make([]string, len(texts))
	for i, t := range texts {
		ids[i] = store.DeriveID(t)
	}
	if err := e.ensure(ctx, ids, texts); err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		v, ok := e.cache.Get(ids[i])
		if !ok {
			return nil, fmt.Errorf("embed: cache miss after fill for %s", ids[i])
		}
		out[i] = v
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/embed/... && go vet ./internal/embed/...
```
Expected: PASS — the new `TestEmbedTextsReturnsVectorsInOrderAndCaches` plus all four pre-existing `EmbedStore` tests (the refactor preserves their behavior), vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/embed/embedder.go internal/embed/embedder_test.go
git commit -m "feat(embed): add EmbedTexts for cached JD requirement embedding"
```

---

### Task 3: Cosine similarity

**Files:**
- Create: `internal/retrieve/cosine.go`
- Test: `internal/retrieve/cosine_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/retrieve/cosine_test.go`:

```go
package retrieve

import (
	"math"
	"testing"
)

func TestCosine(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0}, []float32{1, 0}, 1},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1},
		{"scaled", []float32{1, 1}, []float32{2, 2}, 1},
		{"zero vector", []float32{0, 0}, []float32{1, 1}, 0},
		{"length mismatch", []float32{1, 0, 0}, []float32{1, 0}, 0},
		{"empty", []float32{}, []float32{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cosine(tc.a, tc.b)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("cosine(%v,%v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/retrieve/...
```
Expected: build failure — package `retrieve` does not exist / `cosine` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/retrieve/cosine.go`:

```go
package retrieve

import "math"

// cosine returns the cosine similarity of a and b in [-1, 1]. It returns 0 when
// the vectors are empty, have differing lengths, or either has zero magnitude.
// Vectors from one embedding model are always the same length, so a length
// mismatch indicates a programming error rather than a meaningful comparison.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/retrieve/... && go vet ./internal/retrieve/...
```
Expected: PASS (7 sub-tests), vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/retrieve/cosine.go internal/retrieve/cosine_test.go
git commit -m "feat(retrieve): add cosine similarity"
```

---

### Task 4: Index and TopK

**Files:**
- Create: `internal/retrieve/index.go`
- Test: `internal/retrieve/index_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/retrieve/index_test.go`:

```go
package retrieve

import (
	"testing"

	"github.com/farazhassan/tailor-swift/internal/store"
)

// threeAchStore parses a fixture with achievements "alpha", "beta", "gamma".
func threeAchStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.ParseReader([]byte("## Acme — Eng\n### P\n- alpha\n- beta\n- gamma\n"), "mem")
	if err != nil {
		t.Fatalf("ParseReader: %v", err)
	}
	if got := len(s.Achievements()); got != 3 {
		t.Fatalf("fixture has %d achievements, want 3", got)
	}
	return s
}

func threeVecs() map[string][]float32 {
	return map[string][]float32{
		store.DeriveID("alpha"): {1, 0},
		store.DeriveID("beta"):  {0, 1},
		store.DeriveID("gamma"): {1, 1},
	}
}

func TestNewIndexErrorsOnMissingVector(t *testing.T) {
	s := threeAchStore(t)
	vecs := map[string][]float32{store.DeriveID("alpha"): {1, 0}} // beta, gamma missing
	if _, err := NewIndex(s, vecs); err == nil {
		t.Error("NewIndex: want error when a vector is missing, got nil")
	}
}

func TestTopKRanksByCosine(t *testing.T) {
	s := threeAchStore(t)
	ix, err := NewIndex(s, threeVecs())
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	got := ix.TopK([]float32{1, 0}, 2)
	if len(got) != 2 {
		t.Fatalf("TopK len = %d, want 2", len(got))
	}
	// alpha is identical to the query (cosine 1) → rank 1.
	if got[0].ID != store.DeriveID("alpha") || got[0].Content != "alpha" {
		t.Errorf("top doc = %+v, want alpha", got[0])
	}
	// gamma (cosine ~0.707) beats beta (cosine 0) → rank 2.
	if got[1].ID != store.DeriveID("gamma") {
		t.Errorf("second doc = %s, want gamma", got[1].ID)
	}
	if got[0].Score < got[1].Score {
		t.Errorf("not sorted by descending score: %v then %v", got[0].Score, got[1].Score)
	}
}

func TestTopKReturnsAllWhenKNonPositive(t *testing.T) {
	s := threeAchStore(t)
	ix, _ := NewIndex(s, threeVecs())
	if got := ix.TopK([]float32{1, 0}, 0); len(got) != 3 {
		t.Errorf("TopK(_,0) len = %d, want 3 (all)", len(got))
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/retrieve/...
```
Expected: build failure — `NewIndex` / `Index.TopK` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/retrieve/index.go`:

```go
package retrieve

import (
	"fmt"
	"sort"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// entry pairs an achievement with its embedding vector.
type entry struct {
	ach store.Achievement
	vec []float32
}

// Index is an in-memory cosine index over a content store's achievements. Build
// it from a parsed store plus the vectors produced by embed.Embedder.EmbedStore
// (keyed by achievement ID).
type Index struct {
	entries []entry
}

// NewIndex pairs each achievement in s with its vector. Achievement IDs are
// content hashes, so duplicate bullets collapse to a single entry. It errors if
// any achievement has no vector in vectors.
func NewIndex(s *store.Store, vectors map[string][]float32) (*Index, error) {
	ix := &Index{}
	seen := map[string]bool{}
	for _, a := range s.Achievements() {
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		v, ok := vectors[a.ID]
		if !ok {
			return nil, fmt.Errorf("retrieve: no vector for achievement %s", a.ID)
		}
		ix.entries = append(ix.entries, entry{ach: a, vec: v})
	}
	return ix, nil
}

// docFor builds a gantry.Document for an achievement and its similarity score.
func docFor(a store.Achievement, score float64) gantry.Document {
	return gantry.Document{
		ID:      a.ID,
		Content: a.Text,
		Score:   score,
		Metadata: map[string]any{
			"tags": a.Tags,
			"file": a.Provenance.File,
			"line": a.Provenance.Line,
		},
	}
}

// TopK returns the k achievements most similar to query as gantry Documents,
// sorted by descending cosine score (ties broken by ascending ID for
// determinism). k <= 0 or k beyond the index size returns all entries, ranked.
func (ix *Index) TopK(query []float32, k int) []gantry.Document {
	docs := make([]gantry.Document, 0, len(ix.entries))
	for _, e := range ix.entries {
		docs = append(docs, docFor(e.ach, cosine(query, e.vec)))
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Score != docs[j].Score {
			return docs[i].Score > docs[j].Score
		}
		return docs[i].ID < docs[j].ID
	})
	if k > 0 && k < len(docs) {
		docs = docs[:k]
	}
	return docs
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/retrieve/... && go vet ./internal/retrieve/...
```
Expected: PASS (cosine tests + 3 index tests), vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/retrieve/index.go internal/retrieve/index_test.go
git commit -m "feat(retrieve): add cosine index with TopK over achievements"
```

---

### Task 5: Select — candidate set + must-have coverage gaps

**Files:**
- Create: `internal/retrieve/select.go`
- Test: `internal/retrieve/select_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/retrieve/select_test.go`:

```go
package retrieve

import (
	"testing"

	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/store"
)

func TestSelectUnionsAndReportsGaps(t *testing.T) {
	s := threeAchStore(t)
	ix, err := NewIndex(s, threeVecs())
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	reqs := []jd.Requirement{
		{Text: "needs alpha", MustHave: true},   // matches alpha (cosine 1.0)
		{Text: "needs nothing", MustHave: true}, // best cosine negative → gap
	}
	reqVecs := [][]float32{
		{1, 0},   // top match: alpha = 1.0
		{-1, -1}, // best cosine is negative, below minScore
	}

	sel, err := Select(ix, reqs, reqVecs, 1, 0.5)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	// The unmet must-have is reported as a gap; the met one is not.
	if len(sel.Gaps) != 1 || sel.Gaps[0].Text != "needs nothing" {
		t.Errorf("gaps = %+v, want [needs nothing]", sel.Gaps)
	}

	// alpha is in the candidate set with its perfect score.
	var foundAlpha bool
	for _, d := range sel.Documents {
		if d.ID == store.DeriveID("alpha") {
			foundAlpha = true
			if d.Score < 0.999 {
				t.Errorf("alpha score = %v, want ~1.0", d.Score)
			}
		}
	}
	if !foundAlpha {
		t.Error("alpha missing from candidate set")
	}

	// Documents are sorted by descending score.
	for i := 1; i < len(sel.Documents); i++ {
		if sel.Documents[i-1].Score < sel.Documents[i].Score {
			t.Errorf("documents not sorted by descending score: %+v", sel.Documents)
		}
	}
}

func TestSelectNoGapWhenMustHaveCovered(t *testing.T) {
	s := threeAchStore(t)
	ix, _ := NewIndex(s, threeVecs())

	reqs := []jd.Requirement{{Text: "needs beta", MustHave: true}}
	reqVecs := [][]float32{{0, 1}} // matches beta exactly (cosine 1.0)

	sel, err := Select(ix, reqs, reqVecs, 1, 0.5)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(sel.Gaps) != 0 {
		t.Errorf("gaps = %+v, want none", sel.Gaps)
	}
}

func TestSelectErrorsOnLengthMismatch(t *testing.T) {
	s := threeAchStore(t)
	ix, _ := NewIndex(s, threeVecs())
	reqs := []jd.Requirement{{Text: "x", MustHave: true}}
	if _, err := Select(ix, reqs, [][]float32{}, 1, 0.5); err == nil {
		t.Error("Select: want error on reqs/vecs length mismatch, got nil")
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/retrieve/...
```
Expected: build failure — `Select` / `Selection` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/retrieve/select.go`:

```go
package retrieve

import (
	"fmt"
	"sort"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/jd"
)

// Selection is the candidate content set for a job plus the must-have
// requirements that no achievement covers well enough.
type Selection struct {
	Documents []gantry.Document // unioned top-k across requirements, deduped, ranked
	Gaps      []jd.Requirement  // must-have requirements whose best match scored below minScore
}

// Select ranks achievements against each requirement vector (reqVecs[i] is the
// embedding of reqs[i].Text) and unions the per-requirement top-k into one
// candidate set, keeping the highest score when an achievement matches several
// requirements. A must-have requirement whose best-matching achievement scores
// below minScore is reported as a coverage gap. reqs and reqVecs must be the
// same length.
func Select(ix *Index, reqs []jd.Requirement, reqVecs [][]float32, k int, minScore float64) (*Selection, error) {
	if len(reqs) != len(reqVecs) {
		return nil, fmt.Errorf("retrieve: %d requirements but %d vectors", len(reqs), len(reqVecs))
	}
	best := map[string]gantry.Document{}
	var gaps []jd.Requirement
	for i, req := range reqs {
		docs := ix.TopK(reqVecs[i], k)
		var topScore float64
		if len(docs) > 0 {
			topScore = docs[0].Score
		}
		for _, d := range docs {
			if cur, ok := best[d.ID]; !ok || d.Score > cur.Score {
				best[d.ID] = d
			}
		}
		if req.MustHave && topScore < minScore {
			gaps = append(gaps, req)
		}
	}
	docs := make([]gantry.Document, 0, len(best))
	for _, d := range best {
		docs = append(docs, d)
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Score != docs[j].Score {
			return docs[i].Score > docs[j].Score
		}
		return docs[i].ID < docs[j].ID
	})
	return &Selection{Documents: docs, Gaps: gaps}, nil
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/retrieve/... && go vet ./internal/retrieve/...
```
Expected: PASS (all retrieve tests), vet clean.

- [ ] **Step 5: Final whole-tree check**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS across all packages (`cmd/tailor`, `internal/embed`, `internal/jd`, `internal/retrieve`, `internal/store`).

- [ ] **Step 6: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/retrieve/select.go internal/retrieve/select_test.go
git commit -m "feat(retrieve): add Select with candidate union and coverage gaps"
```

---

## Out of scope (deferred to later plans)

- **JD acquisition** (`Fetch`/`ExtractText`/`ExtractRequirements`/`Acquire`): delivered in Plan 3; this plan consumes `jd.Requirement` only.
- **Choosing `k`, `minScore`, and persisting the JD requirement-vector cache file** (`cache/embeddings/jd_<sha256>.json`): the orchestrator/CLI (Plan 6) picks these knobs and owns cache file paths. `Select` takes `k`/`minScore` as parameters; `EmbedTexts` operates on whatever `Cache` the caller hands its `Embedder`.
- **Feeding the candidate set into a prompt / gantry `WithRetriever` middleware:** the generator (Plan 5) consumes `Selection.Documents`; whether retrieval is wired as gantry middleware or called directly is decided there. This plan deliberately does not build an unused query-embedding `retriever.Retriever` adapter (YAGNI).
- **Surfacing coverage-gap warnings in the report:** the report writer (Plan 6) renders `Selection.Gaps`; this plan only computes them.

## Self-Review

- **Spec coverage:** Implements design pre-loop step 3 ("embed JD requirement chunks (cached per JD)" → `embed.EmbedTexts`, cached by content hash) and step 4 ("cosine top-k achievements + must-have skill coverage → candidate content set" → `cosine` + `Index.TopK` + `Select`). The "coverage-gap warning" edge case ("No relevant content for a must-have skill → surface a coverage-gap warning rather than letting the LLM fabricate") is realized as `Selection.Gaps`. Uses gantry's `Document` type per the architecture note that `retrieve/` produces gantry documents. The "embed/ index: cosine math + cache hit/miss with fake vectors" testing-strategy item is covered by `cosine_test.go`, `index_test.go`, and the `fakeEmb`-based `EmbedTexts` test.
- **Placeholders:** none — every step is full code or an exact command with expected output.
- **Type consistency:** `gantry.Document{ID, Content, Score, Metadata}` is used identically in `index.go` (`docFor`, `TopK`) and `select.go` (`Select`). `Index`/`entry`/`NewIndex`/`TopK`/`cosine`/`docFor` signatures match their call sites across `index.go`, `select.go`, and the tests. `Selection{Documents, Gaps}` field names match the test assertions. `jd.Requirement{Text, MustHave}` (Plan 3) and `store.Achievement{ID, Text, Tags, Provenance}` + `store.DeriveID` are used exactly as defined. `Embedder.ensure`/`EmbedStore`/`EmbedTexts` share the parallel `ids`/`texts` contract; `EmbedStore`'s observable behavior is unchanged, so its four existing tests still pass.
