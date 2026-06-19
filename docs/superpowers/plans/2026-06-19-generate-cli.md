# Generate CLI Implementation Plan (Plan 10b)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `tailor generate` command that chains `pipeline.Acquire` into `orchestrate.Run` with real Anthropic/Voyage clients and the `pdflatex` compiler, emits artifacts to `out/<slug>-<date>/`, and returns a meaningful exit code.

**Architecture:** A core/wiring split. `runGenerate` (wiring) parses flags, builds real clients, resolves the template, and calls the testable `genRun` (core), which runs acquisition → warns on gaps → runs the loop → writes artifacts → returns an exit code. The default LaTeX template is embedded via a new `templates` package.

**Tech Stack:** Go 1.26 stdlib (`flag`, `net/url`, `os`, `encoding/json`, `embed`), gantry (`anthropic`, `eval` mocks, `limiter`), the existing `internal/pipeline`, `internal/orchestrate`, `internal/embed`, `internal/render`, `internal/store`, `internal/jd`, `internal/evaluate`, `internal/generate`.

---

## Background the engineer needs

**Collaborator signatures (already on `main`):**

- `pipeline.Acquire(ctx, pipeline.Config, pipeline.Deps) (*pipeline.Result, error)`
  - `pipeline.Config{ContentPath, JDURL, JDFile, Model, TopK, MinScore, EmbedCachePath, JDCacheDir string/int/float64}`
  - `pipeline.Deps{LLM gantry.LLMClient; Embedder embeddings.Embeddings}`
  - `pipeline.Result{Store *store.Store; Posting *jd.Posting; Requirements []jd.Requirement; Candidates []gantry.Document; Gaps []jd.Requirement}`
- `orchestrate.Run(ctx, orchestrate.Config, orchestrate.Deps) (*orchestrate.Result, error)`
  - `orchestrate.Config{Template string; Store *store.Store; Requirements []jd.Requirement; Candidates []gantry.Document; MaxIterations, MaxRepairs int; Budget limiter.Limits}`
  - `orchestrate.Deps{GenLLM, EvalLLM, RepairLLM gantry.LLMClient; Compile render.CompileFunc}`
  - `orchestrate.Result{Iterations []Iteration; Best *Iteration; Passed bool; StopReason string}`
  - `orchestrate.Iteration{Index int; Bullets []generate.Bullet; TeX string; Compiled bool; CompileLog string; PDF []byte; Evaluation *evaluate.Evaluation}`
- `evaluate.Evaluation{Scores map[string]float64; Truthful bool; Critique map[string]string; Summary string; Composite float64 (json:"-"); Pass bool (json:"-")}`
- `jd.Posting{URL string; FetchedAt time.Time; RawText string; Requirements []jd.Requirement}`; `jd.Requirement{Text string; MustHave bool}`
- `render.PDFLaTeX` is a `render.CompileFunc = func(ctx, tex string) (render.CompileResult, error)`.
- `embed.NewVoyageClient() (embeddings.Embeddings, error)` — errors (does not panic) when `VOYAGE_API_KEY` is unset. Reads `VOYAGE_MODEL` (default `voyage-3`).
- `anthropic.New(model string, opts ...Option) *anthropic.Client` — **panics** on empty model or missing `ANTHROPIC_API_KEY`. Import path `github.com/farazhassan/gantry/components/llm/anthropic`.
- `store.DeriveID(text string) string` returns the stable `ach_<hex>` id used as both the achievement ID and the retrieved `gantry.Document.ID`. Tests compute candidate ids with it.
- Mock LLM: `eval.NewMockLLMClient(responses ...gantry.LLMResponse) *eval.MockLLMClient` (import `github.com/farazhassan/gantry/eval`). No-arg returns a client that errors if called. Returns responses in order; errors if called more times than it has responses.
- `evaluate.Threshold = 0.85`; `Pass = composite >= Threshold && Truthful`. The five rubric dimensions are `jd_coverage`, `relevance`, `evidence_quality`, `truthfulness`, `format` with weights summing to 1.0, so an evaluation with every score `s` has composite `s`.
- `render.CompileWithRepair` with `maxRepairs=2` makes up to 3 compile attempts and, when every compile fails, exactly **2** repair-LLM calls before returning `render.ErrCompileFailed` with `OK=false, PDF=nil`.

**Sandbox note for the implementer:** `go`/`git` commands in this environment frequently fail with "operation not permitted" (build cache) or "context deadline exceeded" (module proxy). These are sandbox restrictions, not real failures — retry the exact command with `dangerouslyDisableSandbox: true`. This is pre-authorized for this repo.

**If any API signature here does not match the actual code, STOP and report the mismatch instead of guessing.**

---

## File structure

- `templates/embed.go` *(new)*: `package templates`; embeds `templates/default.tex` as `var Default string`.
- `templates/embed_test.go` *(new)*: asserts the embedded template is non-empty LaTeX.
- `cmd/tailor/generate.go` *(new)*: `genConfig`, `genDeps`, `critiqueOut` types; `slugify`, `writeArtifacts`, `genRun` (core); `runGenerate` + `newAnthropic` + `generateUsage` (wiring); `defaultMaxRepairs` const.
- `cmd/tailor/generate_test.go` *(new)*: unit tests + fakes for `slugify`, `writeArtifacts`, `genRun`, and flag handling.
- `cmd/tailor/run.go` *(modify)*: dispatch `generate` to `runGenerate`; update usage text.
- `cmd/tailor/run_test.go` *(modify)*: drop `generate` from the "not implemented" stub test.

---

## Task 1: Embed the default LaTeX template

**Files:**
- Create: `templates/embed.go`
- Test: `templates/embed_test.go`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout main && git pull
git checkout -b feat/generate-cli
```

Expected: on a new branch `feat/generate-cli` based on the latest `main`.

- [ ] **Step 2: Write the failing test**

Create `templates/embed_test.go`:

```go
package templates

import "strings"

import "testing"

func TestDefaultTemplateEmbedded(t *testing.T) {
	if !strings.Contains(Default, `\documentclass`) {
		t.Errorf("Default missing \\documentclass; got %d bytes", len(Default))
	}
	if !strings.Contains(Default, `\end{document}`) {
		t.Errorf("Default missing \\end{document}")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./templates/`
Expected: FAIL — `undefined: Default` (compile error).

- [ ] **Step 4: Create the embed file**

Create `templates/embed.go`:

```go
// Package templates provides the built-in LaTeX resume template, embedded into
// the binary so the CLI works from any working directory.
package templates

import _ "embed"

// Default is the built-in LaTeX template (templates/default.tex), used unless
// the caller supplies an override.
//
//go:embed default.tex
var Default string
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./templates/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add templates/embed.go templates/embed_test.go
git commit -m "feat(templates): embed default LaTeX template into the binary"
```

---

## Task 2: `slugify` helper

**Files:**
- Create: `cmd/tailor/generate.go`
- Test: `cmd/tailor/generate_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/tailor/generate_test.go`:

```go
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
```

Note: for `"https://acme.com"` the URL has no path, so the last non-empty segment falls back to the host `acme.com` → `acme-com`. For `"https://acme.com/"` the path is `/` (only empty segments) → fallback `job`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/tailor/ -run TestSlugify`
Expected: FAIL — `undefined: slugify`.

- [ ] **Step 3: Create `generate.go` with `slugify`**

Create `cmd/tailor/generate.go`:

```go
package main

import (
	"net/url"
	"strings"
)

// slugify derives a filesystem-friendly slug from a job posting URL: the last
// non-empty path segment (or the host when there is no path), lowercased, with
// each run of non-alphanumerics collapsed to a single dash and the ends
// trimmed. Falls back to "job" when nothing usable remains.
func slugify(rawURL string) string {
	seg := ""
	if u, err := url.Parse(rawURL); err == nil {
		parts := strings.Split(u.Path, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				seg = parts[i]
				break
			}
		}
		if seg == "" {
			seg = u.Host
		}
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(seg) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "job"
	}
	return s
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/tailor/ -run TestSlugify`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tailor/generate.go cmd/tailor/generate_test.go
git commit -m "feat(cli): add slugify for output directory naming"
```

---

## Task 3: `critiqueOut` + `writeArtifacts`

**Files:**
- Modify: `cmd/tailor/generate.go`
- Test: `cmd/tailor/generate_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/tailor/generate_test.go` (add the imports shown to the existing `import` — the file currently only imports `testing`):

```go
import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
	"github.com/farazhassan/tailor-swift/internal/evaluate"
	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/orchestrate"
	"github.com/farazhassan/tailor-swift/internal/pipeline"
	"github.com/farazhassan/tailor-swift/internal/render"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// acqResult builds a minimal pipeline.Result for artifact tests.
func acqResult() *pipeline.Result {
	return &pipeline.Result{
		Posting:      &jd.Posting{URL: "https://acme.com/jobs/go", FetchedAt: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)},
		Requirements: []jd.Requirement{{Text: "Go", MustHave: true}},
		Candidates:   []gantry.Document{{ID: "u1", Content: "Built a Go service"}},
	}
}

func TestWriteArtifactsHappy(t *testing.T) {
	dir := t.TempDir()
	run := &orchestrate.Result{
		StopReason: orchestrate.StopPassed,
		Passed:     true,
		Iterations: []orchestrate.Iteration{{Index: 0, Compiled: true, Evaluation: &evaluate.Evaluation{Pass: true, Composite: 0.9}}},
	}
	run.Best = &run.Iterations[0]
	run.Best.TeX = `\documentclass{article}`
	run.Best.PDF = []byte("%PDF-1")
	run.Best.Evaluation = &evaluate.Evaluation{
		Pass: true, Composite: 0.9, Truthful: true,
		Scores:   map[string]float64{"jd_coverage": 0.9},
		Critique: map[string]string{}, Summary: "ship it",
	}

	var errb strings.Builder
	if err := writeArtifacts(dir, run, acqResult(), &errb); err != nil {
		t.Fatalf("writeArtifacts: %v", err)
	}
	for _, name := range []string{"resume.tex", "resume.pdf", "critique.json", "run.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing artifact %s: %v", name, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "critique.json"))
	var co critiqueOut
	if err := json.Unmarshal(data, &co); err != nil {
		t.Fatalf("critique.json invalid: %v", err)
	}
	if !co.Pass || co.Composite != 0.9 || co.StopReason != orchestrate.StopPassed {
		t.Errorf("critiqueOut = %+v", co)
	}
}

func TestWriteArtifactsSkipsPDFWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	run := &orchestrate.Result{
		StopReason: orchestrate.StopMaxIterations,
		Iterations: []orchestrate.Iteration{{Index: 0, Compiled: false, Evaluation: &evaluate.Evaluation{}}},
	}
	run.Best = &run.Iterations[0]
	run.Best.TeX = `\documentclass{article}`
	run.Best.PDF = nil

	var errb strings.Builder
	if err := writeArtifacts(dir, run, acqResult(), &errb); err != nil {
		t.Fatalf("writeArtifacts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "resume.pdf")); !os.IsNotExist(err) {
		t.Errorf("resume.pdf should be skipped, stat err = %v", err)
	}
	if !strings.Contains(errb.String(), "did not compile") {
		t.Errorf("expected stderr warning, got %q", errb.String())
	}
	log, _ := os.ReadFile(filepath.Join(dir, "run.log"))
	if !strings.Contains(string(log), "resume.pdf skipped") {
		t.Errorf("run.log missing skip note:\n%s", log)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/tailor/ -run TestWriteArtifacts`
Expected: FAIL — `undefined: writeArtifacts` / `undefined: critiqueOut`.

- [ ] **Step 3: Implement `critiqueOut` + `writeArtifacts`**

Add to `cmd/tailor/generate.go` (extend the import block to include the new packages):

```go
import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/farazhassan/tailor-swift/internal/orchestrate"
	"github.com/farazhassan/tailor-swift/internal/pipeline"
)

// critiqueOut is the JSON shape written to critique.json. It mirrors
// evaluate.Evaluation but includes Composite and Pass (which carry json:"-" on
// the source type) plus run-level context.
type critiqueOut struct {
	Pass       bool               `json:"pass"`
	Composite  float64            `json:"composite"`
	Truthful   bool               `json:"truthful"`
	Scores     map[string]float64 `json:"scores"`
	Critique   map[string]string  `json:"critique"`
	Summary    string             `json:"summary"`
	StopReason string             `json:"stop_reason"`
	Iterations int                `json:"iterations"`
}

// writeArtifacts writes resume.tex, resume.pdf (skipped when the best iteration
// produced no PDF), critique.json, and run.log into dir. dir must already exist.
func writeArtifacts(dir string, run *orchestrate.Result, acq *pipeline.Result, stderr io.Writer) error {
	best := run.Best
	if err := os.WriteFile(filepath.Join(dir, "resume.tex"), []byte(best.TeX), 0o644); err != nil {
		return fmt.Errorf("write resume.tex: %w", err)
	}

	pdfSkipped := false
	if len(best.PDF) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "resume.pdf"), best.PDF, 0o644); err != nil {
			return fmt.Errorf("write resume.pdf: %w", err)
		}
	} else {
		pdfSkipped = true
		fmt.Fprintln(stderr, "warning: best iteration did not compile; resume.pdf not written")
	}

	co := critiqueOut{StopReason: run.StopReason, Iterations: len(run.Iterations)}
	if ev := best.Evaluation; ev != nil {
		co.Pass = ev.Pass
		co.Composite = ev.Composite
		co.Truthful = ev.Truthful
		co.Scores = ev.Scores
		co.Critique = ev.Critique
		co.Summary = ev.Summary
	}
	cj, err := json.MarshalIndent(co, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal critique: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "critique.json"), cj, 0o644); err != nil {
		return fmt.Errorf("write critique.json: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "jd_url: %s\n", acq.Posting.URL)
	fmt.Fprintf(&b, "fetched_at: %s\n", acq.Posting.FetchedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "requirements: %d\n", len(acq.Requirements))
	fmt.Fprintf(&b, "candidates: %d\n", len(acq.Candidates))
	if len(acq.Gaps) == 0 {
		fmt.Fprintln(&b, "gaps: none")
	} else {
		fmt.Fprintf(&b, "gaps: %d\n", len(acq.Gaps))
		for _, g := range acq.Gaps {
			fmt.Fprintf(&b, "  - %s\n", g.Text)
		}
	}
	for _, it := range run.Iterations {
		pass, comp := false, 0.0
		if it.Evaluation != nil {
			pass = it.Evaluation.Pass
			comp = it.Evaluation.Composite
		}
		fmt.Fprintf(&b, "iteration %d: compiled=%t pass=%t composite=%.3f\n", it.Index, it.Compiled, pass, comp)
	}
	fmt.Fprintf(&b, "stop_reason: %s\n", run.StopReason)
	fmt.Fprintf(&b, "best_iteration: %d\n", best.Index)
	if pdfSkipped {
		fmt.Fprintln(&b, "note: resume.pdf skipped (best iteration did not compile)")
	}
	if err := os.WriteFile(filepath.Join(dir, "run.log"), []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write run.log: %w", err)
	}
	return nil
}
```

Note: the `slugify` function from Task 2 already imports `net/url` and `strings`; merge the import lists so there are no duplicate or unused imports. `context`, `gantry`, `eval`, `evaluate`, `jd`, `render`, `store` are used by the *test* file (Task 3 tests and Task 4 tests), not yet by `generate.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/tailor/ -run TestWriteArtifacts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tailor/generate.go cmd/tailor/generate_test.go
git commit -m "feat(cli): write resume/critique/run-log artifacts"
```

---

## Task 4: `genRun` core (acquisition → loop → artifacts → exit code)

**Files:**
- Modify: `cmd/tailor/generate.go`
- Test: `cmd/tailor/generate_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/tailor/generate_test.go`. These add shared fixtures (`vecFor`, `fakeEmbedder`, `writeFileT`, `cliContent`, `testTemplate`, `okCompiler`, `failCompiler`, `passEvalJSON`, `failEvalJSON`, `resp`, `cliBase`, `cliDeps`) used across the `genRun` tests:

```go
// --- fixtures for genRun ---------------------------------------------------

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

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, tx := range texts {
		out[i] = vecFor(tx)
	}
	return out, nil
}

func writeFileT(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

const cliContent = `# Ada Lovelace

## Contact
Email: ada@example.com

## Acme Corp

### Billing
- Built a Go billing service
- Scaled Kafka pipelines
`

const testTemplate = `\documentclass{article}\begin{document}{{.Name}}{{range .Roles}}{{range .Bullets}} {{.Text}}{{end}}{{end}}\end{document}`

const passEvalJSON = `{"scores":{"jd_coverage":0.9,"relevance":0.9,"evidence_quality":0.9,"truthfulness":0.9,"format":0.9},"truthful":true,"critique":{},"summary":"ship it"}`

const failEvalJSON = `{"scores":{"jd_coverage":0.5,"relevance":0.5,"evidence_quality":0.5,"truthfulness":0.5,"format":0.5},"truthful":true,"critique":{"jd_coverage":"more depth"},"summary":"needs work"}`

func resp(s string) gantry.LLMResponse {
	return gantry.LLMResponse{Content: s, StopReason: gantry.StopReasonEnd}
}

func okCompiler() render.CompileFunc {
	return func(ctx context.Context, tex string) (render.CompileResult, error) {
		return render.CompileResult{OK: true, PDF: []byte("%PDF-1"), Log: "ok"}, nil
	}
}

func failCompiler() render.CompileFunc {
	return func(ctx context.Context, tex string) (render.CompileResult, error) {
		return render.CompileResult{OK: false, Log: "! Undefined control sequence"}, nil
	}
}

// cliBase writes a content file + JD file under a temp dir and returns a
// genConfig (MaxIterations=1) plus the temp dir.
func cliBase(t *testing.T) (genConfig, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := genConfig{
		ContentPath:   writeFileT(t, dir, "content.md", cliContent),
		JDURL:         "https://acme.com/jobs/senior-go-engineer",
		JDFile:        writeFileT(t, dir, "jd.txt", "We need a backend engineer."),
		OutDir:        filepath.Join(dir, "out"),
		EmbedModel:    "voyage-3",
		TopK:          8,
		MinScore:      0.5,
		MaxIterations: 1,
		JDCacheDir:    filepath.Join(dir, "jdcache"),
		Today:         time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC),
	}
	return cfg, dir
}

// cliDeps builds genDeps with per-role scripted mocks and the given compiler.
// repair may be nil to mean "must not be called".
func cliDeps(extractJSON, genJSON, evalJSON string, repair gantry.LLMClient, compile render.CompileFunc) genDeps {
	if repair == nil {
		repair = eval.NewMockLLMClient()
	}
	return genDeps{
		ExtractLLM: eval.NewMockLLMClient(resp(extractJSON)),
		GenLLM:     eval.NewMockLLMClient(resp(genJSON)),
		EvalLLM:    eval.NewMockLLMClient(resp(evalJSON)),
		RepairLLM:  repair,
		Embedder:   fakeEmbedder{},
		Compile:    compile,
		Template:   testTemplate,
	}
}

// goBullets returns a generation response selecting the Go achievement by its
// derived id (the id orchestrate validates bullets against).
func goBullets() string {
	id := store.DeriveID("Built a Go billing service")
	return `[{"unit_id":"` + id + `","text":"Built billing in Go"}]`
}

const reqsGoKafka = `[{"text":"Go","must_have":true},{"text":"Kafka","must_have":false}]`

// --- genRun tests ----------------------------------------------------------

func TestGenRunHappyPath(t *testing.T) {
	cfg, _ := cliBase(t)
	deps := cliDeps(reqsGoKafka, goBullets(), passEvalJSON, nil, okCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, errb.String())
	}
	dir := filepath.Join(cfg.OutDir, "senior-go-engineer-2026-06-19")
	for _, name := range []string{"resume.tex", "resume.pdf", "critique.json", "run.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "critique.json"))
	if !strings.Contains(string(data), `"pass": true`) {
		t.Errorf("critique.json not passing:\n%s", data)
	}
}

func TestGenRunNotPassedExits3(t *testing.T) {
	cfg, _ := cliBase(t)
	deps := cliDeps(reqsGoKafka, goBullets(), failEvalJSON, nil, okCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 3 {
		t.Fatalf("exit = %d, want 3; stderr=%s", code, errb.String())
	}
	dir := filepath.Join(cfg.OutDir, "senior-go-engineer-2026-06-19")
	data, _ := os.ReadFile(filepath.Join(dir, "critique.json"))
	if !strings.Contains(string(data), `"pass": false`) {
		t.Errorf("critique.json should be failing:\n%s", data)
	}
}

func TestGenRunWarnsOnGaps(t *testing.T) {
	cfg, _ := cliBase(t)
	reqs := `[{"text":"Go","must_have":true},{"text":"Rust","must_have":true}]`
	deps := cliDeps(reqs, goBullets(), passEvalJSON, nil, okCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "Rust") || !strings.Contains(errb.String(), "unmatched") {
		t.Errorf("expected gap warning mentioning Rust, got %q", errb.String())
	}
}

func TestGenRunSkipsPDFOnCompileFailure(t *testing.T) {
	cfg, _ := cliBase(t)
	// maxRepairs=2 => exactly 2 repair-LLM calls before ErrCompileFailed.
	repair := eval.NewMockLLMClient(resp(`\documentclass{article}`), resp(`\documentclass{article}`))
	deps := cliDeps(reqsGoKafka, goBullets(), failEvalJSON, repair, failCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 3 {
		t.Fatalf("exit = %d, want 3; stderr=%s", code, errb.String())
	}
	dir := filepath.Join(cfg.OutDir, "senior-go-engineer-2026-06-19")
	if _, err := os.Stat(filepath.Join(dir, "resume.pdf")); !os.IsNotExist(err) {
		t.Errorf("resume.pdf should be skipped, stat err = %v", err)
	}
}

func TestGenRunFatalOnBadContent(t *testing.T) {
	cfg, _ := cliBase(t)
	cfg.ContentPath = filepath.Join(t.TempDir(), "does-not-exist.md")
	deps := cliDeps(reqsGoKafka, goBullets(), passEvalJSON, nil, okCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(cfg.OutDir); !os.IsNotExist(err) {
		t.Errorf("no output dir should be created on fatal error")
	}
}

func TestGenRunOverwritesSameDay(t *testing.T) {
	cfg, _ := cliBase(t)
	deps1 := cliDeps(reqsGoKafka, goBullets(), passEvalJSON, nil, okCompiler())
	var o1, e1 strings.Builder
	if code := genRun(context.Background(), cfg, deps1, &o1, &e1); code != 0 {
		t.Fatalf("first run exit = %d; stderr=%s", code, e1.String())
	}
	// Second run with fresh mocks (mock clients are single-use) overwrites.
	deps2 := cliDeps(reqsGoKafka, goBullets(), passEvalJSON, nil, okCompiler())
	var o2, e2 strings.Builder
	if code := genRun(context.Background(), cfg, deps2, &o2, &e2); code != 0 {
		t.Fatalf("second run exit = %d; stderr=%s", code, e2.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/tailor/ -run TestGenRun`
Expected: FAIL — `undefined: genRun`, `undefined: genConfig`, `undefined: genDeps`.

- [ ] **Step 3: Implement the types and `genRun`**

Add to `cmd/tailor/generate.go`. Extend the import block with `context`, `github.com/farazhassan/gantry`, `github.com/farazhassan/gantry/components/embeddings`, `github.com/farazhassan/gantry/components/limiter`, and `github.com/farazhassan/tailor-swift/internal/render`:

```go
const defaultMaxRepairs = 2

// genConfig is the resolved, validated input to the core (the wiring fills it
// from flags; tests construct it directly).
type genConfig struct {
	ContentPath    string
	JDURL          string
	JDFile         string
	OutDir         string
	EmbedModel     string
	TopK           int
	MinScore       float64
	MaxIterations  int
	EmbedCachePath string
	JDCacheDir     string
	Today          time.Time
}

// genDeps are the injected collaborators. The wiring assigns one Anthropic
// client to all four LLM roles; keeping them separate lets tests script a
// per-role mock.
type genDeps struct {
	ExtractLLM gantry.LLMClient
	GenLLM     gantry.LLMClient
	EvalLLM    gantry.LLMClient
	RepairLLM  gantry.LLMClient
	Embedder   embeddings.Embeddings
	Compile    render.CompileFunc
	Template   string
}

// genRun is the testable core: acquire inputs, warn on coverage gaps, run the
// refinement loop, write artifacts, and return the process exit code
// (0 pass, 3 emitted-but-not-passed, 1 fatal).
func genRun(ctx context.Context, cfg genConfig, deps genDeps, stdout, stderr io.Writer) int {
	acq, err := pipeline.Acquire(ctx, pipeline.Config{
		ContentPath:    cfg.ContentPath,
		JDURL:          cfg.JDURL,
		JDFile:         cfg.JDFile,
		Model:          cfg.EmbedModel,
		TopK:           cfg.TopK,
		MinScore:       cfg.MinScore,
		EmbedCachePath: cfg.EmbedCachePath,
		JDCacheDir:     cfg.JDCacheDir,
	}, pipeline.Deps{LLM: deps.ExtractLLM, Embedder: deps.Embedder})
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}

	if len(acq.Gaps) > 0 {
		texts := make([]string, len(acq.Gaps))
		for i, g := range acq.Gaps {
			texts[i] = g.Text
		}
		fmt.Fprintf(stderr, "warning: %d must-have requirement(s) unmatched: %s\n", len(acq.Gaps), strings.Join(texts, ", "))
	}

	run, err := orchestrate.Run(ctx, orchestrate.Config{
		Template:      deps.Template,
		Store:         acq.Store,
		Requirements:  acq.Requirements,
		Candidates:    acq.Candidates,
		MaxIterations: cfg.MaxIterations,
		MaxRepairs:    defaultMaxRepairs,
		Budget:        limiter.Limits{},
	}, orchestrate.Deps{
		GenLLM:    deps.GenLLM,
		EvalLLM:   deps.EvalLLM,
		RepairLLM: deps.RepairLLM,
		Compile:   deps.Compile,
	})
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	if run.Best == nil {
		fmt.Fprintln(stderr, "generate: no resume produced")
		return 1
	}

	dir := filepath.Join(cfg.OutDir, slugify(cfg.JDURL)+"-"+cfg.Today.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	if err := writeArtifacts(dir, run, acq, stderr); err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "wrote %s\n", dir)
	if run.Passed {
		return 0
	}
	return 3
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/tailor/ -run TestGenRun`
Expected: PASS (all six `TestGenRun*` tests).

- [ ] **Step 5: Run the full package test + vet**

Run: `go test ./cmd/tailor/ && go vet ./cmd/tailor/`
Expected: PASS, no vet warnings.

- [ ] **Step 6: Commit**

```bash
git add cmd/tailor/generate.go cmd/tailor/generate_test.go
git commit -m "feat(cli): add genRun core wiring pipeline acquisition into the refinement loop"
```

---

## Task 5: `runGenerate` wiring, flags, and dispatch

**Files:**
- Modify: `cmd/tailor/generate.go`
- Modify: `cmd/tailor/run.go`
- Test: `cmd/tailor/generate_test.go`
- Test: `cmd/tailor/run_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/tailor/generate_test.go` (`runCapture` already exists in `run_test.go`, same package):

```go
func TestRunGenerateMissingContent(t *testing.T) {
	code, _, errOut := runCapture("generate", "--jd-url", "https://acme.com/job")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "required") {
		t.Errorf("stderr = %q, want 'required'", errOut)
	}
}

func TestRunGenerateMissingJDURL(t *testing.T) {
	code, _, errOut := runCapture("generate", "--content", "some.md")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%s", code, errOut)
	}
}

func TestRunGenerateMissingAPIKeyIsFatal(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	dir := t.TempDir()
	content := writeFileT(t, dir, "content.md", cliContent)
	code, _, errOut := runCapture("generate", "--content", content, "--jd-url", "https://acme.com/job")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%s", code, errOut)
	}
}
```

Note: `TestRunGenerateMissingAPIKeyIsFatal` reaches the Voyage client construction first (validation passes, template resolves), which errors cleanly on the missing key → exit 1, before the Anthropic client is built.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/tailor/ -run TestRunGenerate`
Expected: FAIL — `undefined: runGenerate` (and the dispatch still routes `generate` to the stub).

- [ ] **Step 3: Implement `runGenerate`, `newAnthropic`, and `generateUsage`**

Add to `cmd/tailor/generate.go`. Extend the import block with `flag`, `github.com/farazhassan/gantry/components/llm/anthropic`, `github.com/farazhassan/tailor-swift/internal/embed`, and `github.com/farazhassan/tailor-swift/templates`:

```go
const generateUsage = `usage: tailor generate --content <file> --jd-url <url> [flags]

required:
  --content <file>   content store markdown
  --jd-url <url>     job posting URL

optional:
  --jd-file <file>   local job description text (URL still required)
  --model <id>       Anthropic model (default claude-sonnet-4-6)
  --out <dir>        base output directory (default out)
  --template <file>  LaTeX template override (default: built-in)
  --max-iterations N refinement iterations (default 3)
  --top-k N          candidates per requirement (default 8)
  --min-score F      min similarity for a must-have (default 0)
  --embed-cache <f>  embedding cache file (default: disabled)
  --jd-cache <dir>   cached postings directory`

// runGenerate parses flags, constructs the real clients, resolves the template,
// and calls genRun. Returns the process exit code.
func runGenerate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", "", "content store markdown (required)")
	jdURL := fs.String("jd-url", "", "job posting URL (required)")
	jdFile := fs.String("jd-file", "", "local job description text file")
	model := fs.String("model", "claude-sonnet-4-6", "Anthropic model id")
	out := fs.String("out", "out", "base output directory")
	template := fs.String("template", "", "LaTeX template override")
	maxIter := fs.Int("max-iterations", 3, "max refinement iterations")
	topK := fs.Int("top-k", 8, "top-K candidates per requirement")
	minScore := fs.Float64("min-score", 0, "min similarity for a must-have")
	embedCache := fs.String("embed-cache", "", "embedding cache file")
	jdCache := fs.String("jd-cache", "", "cached postings directory")
	if err := fs.Parse(args); err != nil {
		return 2 // flag already printed the error to stderr
	}
	if *content == "" || *jdURL == "" {
		fmt.Fprintln(stderr, "generate: --content and --jd-url are required")
		fmt.Fprintln(stderr, generateUsage)
		return 2
	}

	tmpl := templates.Default
	if *template != "" {
		data, err := os.ReadFile(*template)
		if err != nil {
			fmt.Fprintf(stderr, "generate: read template: %v\n", err)
			return 1
		}
		tmpl = string(data)
	}

	embedder, err := embed.NewVoyageClient()
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	embedModel := os.Getenv("VOYAGE_MODEL")
	if embedModel == "" {
		embedModel = "voyage-3"
	}

	llm, err := newAnthropic(*model)
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}

	deps := genDeps{
		ExtractLLM: llm, GenLLM: llm, EvalLLM: llm, RepairLLM: llm,
		Embedder: embedder, Compile: render.PDFLaTeX, Template: tmpl,
	}
	cfg := genConfig{
		ContentPath: *content, JDURL: *jdURL, JDFile: *jdFile, OutDir: *out,
		EmbedModel: embedModel, TopK: *topK, MinScore: *minScore,
		MaxIterations: *maxIter, EmbedCachePath: *embedCache, JDCacheDir: *jdCache,
		Today: time.Now(),
	}
	return genRun(context.Background(), cfg, deps, stdout, stderr)
}

// newAnthropic constructs the Anthropic client, converting its panic (missing
// key or empty model) into an error so the command exits cleanly.
func newAnthropic(model string) (llm gantry.LLMClient, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return anthropic.New(model), nil
}
```

- [ ] **Step 4: Wire the dispatch in `run.go`**

In `cmd/tailor/run.go`, change the stub branch so it no longer handles `generate`, and add a `generate` case. Replace:

```go
	switch args[0] {
	case "ingest", "generate", "evaluate":
		fmt.Fprintf(stdout, "%s: not implemented yet\n", args[0])
		return 0
	case "validate":
		return runValidate(args[1:], stdout, stderr)
```

with:

```go
	switch args[0] {
	case "ingest", "evaluate":
		fmt.Fprintf(stdout, "%s: not implemented yet\n", args[0])
		return 0
	case "generate":
		return runGenerate(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
```

Also update the usage text line for `generate` in the `usage` constant, replacing:

```go
  generate    generate a tailored resume for a job description (not implemented)
```

with:

```go
  generate    generate a tailored resume for a job description
```

- [ ] **Step 5: Update the stub test in `run_test.go`**

In `cmd/tailor/run_test.go`, `TestRun_KnownStubs` currently iterates `{"ingest", "generate", "evaluate"}`. Remove `generate` (it is no longer a stub). Replace:

```go
	for _, cmd := range []string{"ingest", "generate", "evaluate"} {
```

with:

```go
	for _, cmd := range []string{"ingest", "evaluate"} {
```

- [ ] **Step 6: Run the package tests + vet**

Run: `go test ./cmd/tailor/ && go vet ./cmd/tailor/`
Expected: PASS, no vet warnings. (`TestRunGenerateMissingContent`, `TestRunGenerateMissingJDURL`, `TestRunGenerateMissingAPIKeyIsFatal`, and the updated `TestRun_KnownStubs` all pass.)

- [ ] **Step 7: Run the whole tree**

Run: `go build ./... && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 8: Commit**

```bash
git add cmd/tailor/generate.go cmd/tailor/run.go cmd/tailor/run_test.go cmd/tailor/generate_test.go
git commit -m "feat(cli): wire tailor generate command (flags, clients, dispatch)"
```

---

## Self-review (completed by plan author)

**Spec coverage:**
- Core/wiring split → Tasks 4 (core) + 5 (wiring). ✓
- `templates` embed package → Task 1. ✓
- Flag surface (all 11 flags + hardcoded MaxRepairs=2/unlimited budget + VOYAGE_MODEL embed model) → Task 5. ✓
- Data flow (acquire → gap warn → run → Best==nil guard → mkdir → artifacts → exit) → Task 4. ✓
- Slug derivation → Task 2. ✓
- Artifacts (resume.tex/.pdf with skip, critique.json via critiqueOut, run.log) → Task 3. ✓
- Exit codes 0/3/1/2 → Tasks 4 (0/3/1) + 5 (2, and 1 for client/template errors). ✓
- Error handling (usage, Voyage error, Anthropic panic→recover, core errors) → Tasks 4 + 5. ✓
- Testing strategy (happy, not-passed, gap, non-compiling, fatal, slugify, flags, idempotent, wiring no-key) → Tasks 2–5. ✓

**Placeholder scan:** No TBD/TODO; every code step has full code; every command has expected output.

**Type consistency:** `genConfig`/`genDeps`/`critiqueOut` field names are identical across the type definitions (Task 3/4) and their uses in tests and `genRun`. `slugify`, `writeArtifacts`, `genRun`, `runGenerate`, `newAnthropic` signatures match between definition and call sites. `defaultMaxRepairs` defined once (Task 4) and used in `genRun`. Import lists are called out as cumulative across tasks to avoid duplicate/unused imports.
