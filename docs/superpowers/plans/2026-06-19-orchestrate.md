# Orchestrator Implementation Plan (Plan 9)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drive the bounded generate→render→compile→evaluate loop: for each iteration, generate a tailored selection, render+compile it (with the Plan 7 repair loop), and score it (Plan 8); stop as soon as an iteration passes the rubric (composite ≥ 0.85 with the truthfulness gate), feeding each critique back into the next generation, and cap iterations and total token/cost budget — returning an in-memory `Result` with the full per-iteration history and the selected best iteration.

**Architecture:** A new `internal/orchestrate` package with three small files. `budget.go` wraps a `gantry.LLMClient` in a `budgetedClient` that consults a single shared `limiter.BudgetLimiter` before each call and records usage after, so any LLM call (generate, evaluate, or repair) refuses once the budget is crossed. `types.go` holds the `Config` (static inputs), `Deps` (injected collaborators — real in production, fakes in tests), and the `Iteration`/`Result` records. `orchestrate.go` holds `Run`, the explicit loop that calls the already-built `generate`, `render`, and `evaluate` packages, classifies errors (budget → stop and keep best-so-far; compile-failed → non-fatal, record and continue; anything else → fatal), and selects the best iteration to emit.

**Tech Stack:** Go 1.26, stdlib (`context`, `errors`, `fmt`, `strings`), gantry (`github.com/farazhassan/gantry` for `LLMClient`/`LLMRequest`/`LLMResponse`/`State`/`Usage`/`Document`/`ErrLimitExceeded`; `github.com/farazhassan/gantry/components/limiter` for `BudgetLimiter`/`Limits`; `github.com/farazhassan/gantry/eval` for the mock in tests), and the repo's `internal/{store,jd,generate,render,evaluate}`.

**Branch:** `feat/orchestrate` in `/Users/fhassan-mac/Dev/tailor-swift`.

## Prerequisites

All collaborators are already merged on `main`: `internal/generate` (`Generate`, `Input`, `Result`, `Bullet`), `internal/render` (`Render`, `CompileWithRepair`, `CompileFunc`, `CompileResult`, `RepairResult`, `ErrCompileFailed`), `internal/evaluate` (`Evaluate`, `EvalInput`, `Evaluation`, `Dimensions`, `Threshold`), `internal/store` (the `Store` type), `internal/jd` (`Requirement`), and gantry's `limiter`.

- Branch `feat/orchestrate` off `main` (Task 1 below).

**Environment:** No API key and no `pdflatex` needed — every test uses gantry's mock LLM (`eval.NewMockLLMClient`) and an injected fake `render.CompileFunc`. (The real `render.PDFLaTeX` and `anthropic.New(...)` clients are wired by the CLI, a later plan.)

**Sandbox note:** `go` and `git` commands may fail with "operation not permitted" on the build cache (`~/Library/Caches/go-build`) or git transport under the harness sandbox; some `go` runs may also fail with module-proxy "context deadline exceeded". All of this is expected and pre-authorized — if any step fails this way, retry the same command with the sandbox disabled.

## Key design decisions

- **Budget via a wrapping `LLMClient`, not gantry middleware.** gantry's `limiter.WithLimiter` installs middleware into a gantry *Agent's* loop. Our orchestrator owns an explicit Go loop (the design's Approach 2: "thin Go orchestrator over focused gantry calls"), so instead we wrap each injected `gantry.LLMClient` in a `budgetedClient` that shares one `*limiter.BudgetLimiter`. Before each `Generate` it calls `lim.Check(ctx, &gantry.State{Usage: lim.Total()})`; if that returns an `ErrLimitExceeded`-wrapped error the call is refused (the LLM is never invoked); otherwise it calls through and `lim.Record`s the response usage. This mirrors gantry's own semantics: the call that *crosses* the limit still completes, and the *next* call is refused. The generate, evaluate, and repair clients share the one limiter, so the cap is over the whole run.
- **Error classification drives loop control.** A `gantry.ErrLimitExceeded` surfacing from any stage → stop the loop with `StopBudget` and emit the best already-completed iteration. A `render.ErrCompileFailed` (document never compiled after repairs) is *not* fatal — the iteration is recorded with `Compiled=false` (which the evaluator is told about) and the loop continues, per the design's "if all iterations fail → emit highest-truthful-scoring attempt + loud warning." Any other error (bad JSON from generate/evaluate, a template error, a missing-`pdflatex` environment error) is fatal and returned.
- **What the evaluator scores.** The orchestrator passes the rendered `.tex` string as `evaluate.EvalInput.Resume` and `rr.OK` as `Compiled`. The `.tex` is the actual artifact under review and contains all content and structure; this avoids inventing a separate plaintext renderer (YAGNI).
- **Best-iteration selection.** First a passing iteration; else the truthful iteration with the highest composite; else (no truthful iteration) the highest composite overall.

**Out of scope (later plans):**
- **Pre-loop acquisition** — fetching/caching the JD, embedding, and retrieving the candidate set. `Run` takes the already-retrieved `Requirements` and `Candidates`; the CLI/wiring plan fills them via the existing `jd`/`embed`/`retrieve` packages.
- **Artifact emission and exit codes** — writing `out/<job-slug>-<date>/` (`resume.pdf`, `resume.tex`, `critique.json` with the final + per-iteration history, `run.log`) and choosing the process exit code. `Result` already carries everything emission needs (`Best.PDF`, `Best.TeX`, `Best.CompileLog`, and every `Iteration.Evaluation`); the CLI/wiring plan serializes them.
- **Model selection / constructing `anthropic.New(...)` and `render.PDFLaTeX`** — the CLI builds the strong generator client, the cheaper evaluator client, the repair client, and the real compiler, then passes them in `Deps`.

---

### Task 1: Branch and confirm dependencies resolve

**Files:**
- None modified (branch + verification only)

- [ ] **Step 1: Create the feature branch**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git checkout main
git checkout -b feat/orchestrate
```

- [ ] **Step 2: Confirm the prerequisite packages resolve**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go list ./internal/generate/... ./internal/render/... ./internal/evaluate/... ./internal/store/... ./internal/jd/...
```
Expected: prints all five package paths. If any is missing, you based off the wrong branch — all must be present on `main`.

- [ ] **Step 3: Confirm the tree builds and tests pass before changes**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS (no behavior change yet).

No commit for this task — it only establishes the branch.

---

### Task 2: The budgeted LLM client

**Files:**
- Create: `internal/orchestrate/budget.go`
- Test: `internal/orchestrate/budget_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/orchestrate/budget_test.go`:

```go
package orchestrate

import (
	"context"
	"errors"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/limiter"
	"github.com/farazhassan/gantry/eval"
)

func TestBudgetedClientRecordsThenBlocks(t *testing.T) {
	mock := eval.NewMockLLMClient(
		gantry.LLMResponse{Content: "a", StopReason: gantry.StopReasonEnd, Usage: gantry.Usage{InputTokens: 80}},
		gantry.LLMResponse{Content: "b", StopReason: gantry.StopReasonEnd, Usage: gantry.Usage{InputTokens: 80}},
	)
	lim := limiter.NewBudget(limiter.Limits{MaxTokens: 100})
	c := newBudgetedClient(mock, lim)

	// First call: accumulated total is 0, allowed; records 80.
	if _, err := c.Generate(context.Background(), gantry.LLMRequest{}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call: total 80 <= 100, still allowed; records → 160.
	if _, err := c.Generate(context.Background(), gantry.LLMRequest{}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	// Third call: total 160 > 100, refused before invoking the inner client.
	if _, err := c.Generate(context.Background(), gantry.LLMRequest{}); !errors.Is(err, gantry.ErrLimitExceeded) {
		t.Fatalf("third call err = %v, want ErrLimitExceeded", err)
	}
	// The inner mock was only invoked twice (third was refused).
	if len(mock.Requests()) != 2 {
		t.Errorf("inner calls = %d, want 2", len(mock.Requests()))
	}
}

func TestBudgetedClientUnlimitedPassesThrough(t *testing.T) {
	mock := eval.NewMockLLMClient(
		gantry.LLMResponse{Content: "a", StopReason: gantry.StopReasonEnd, Usage: gantry.Usage{InputTokens: 1000}},
		gantry.LLMResponse{Content: "b", StopReason: gantry.StopReasonEnd, Usage: gantry.Usage{InputTokens: 1000}},
	)
	c := newBudgetedClient(mock, limiter.NewBudget(limiter.Limits{})) // zero limits = unlimited
	for i := 0; i < 2; i++ {
		if _, err := c.Generate(context.Background(), gantry.LLMRequest{}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/orchestrate/...
```
Expected: build failure — package `orchestrate` / `newBudgetedClient` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/orchestrate/budget.go`:

```go
package orchestrate

import (
	"context"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/limiter"
)

// budgetedClient wraps a gantry.LLMClient and enforces a shared budget across
// every call: it refuses a call once the accumulated usage has crossed a
// configured limit (returning a gantry.ErrLimitExceeded-wrapped error without
// invoking the inner client) and records each response's usage afterward. A
// single BudgetLimiter is shared by the generate, evaluate, and repair clients
// so the cap covers the whole run.
type budgetedClient struct {
	inner gantry.LLMClient
	lim   *limiter.BudgetLimiter
}

// newBudgetedClient wraps inner so its usage counts against lim.
func newBudgetedClient(inner gantry.LLMClient, lim *limiter.BudgetLimiter) *budgetedClient {
	return &budgetedClient{inner: inner, lim: lim}
}

// Generate refuses the call if the accumulated budget is already exceeded;
// otherwise it calls the inner client and records the response usage. The
// limit-crossing call still completes (its usage is recorded); the next call is
// the one refused — matching gantry's own limiter middleware semantics.
func (c *budgetedClient) Generate(ctx context.Context, req gantry.LLMRequest) (gantry.LLMResponse, error) {
	if err := c.lim.Check(ctx, &gantry.State{Usage: c.lim.Total()}); err != nil {
		return gantry.LLMResponse{}, err
	}
	resp, err := c.inner.Generate(ctx, req)
	if err != nil {
		return resp, err
	}
	c.lim.Record(ctx, resp.Usage)
	return resp, nil
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/orchestrate/... && go vet ./internal/orchestrate/...
```
Expected: PASS (2 tests), vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/orchestrate/budget.go internal/orchestrate/budget_test.go
git commit -m "feat(orchestrate): add budgeted LLM client wrapping a shared limiter"
```

---

### Task 3: The orchestration loop

**Files:**
- Create: `internal/orchestrate/types.go`
- Create: `internal/orchestrate/orchestrate.go`
- Test: `internal/orchestrate/orchestrate_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/orchestrate/orchestrate_test.go`:

```go
package orchestrate

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/limiter"
	"github.com/farazhassan/gantry/eval"
	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/render"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// --- fixtures -------------------------------------------------------------

const testTemplate = `\documentclass{article}\begin{document}{{.Name}}{{range .Roles}}{{range .Bullets}} {{.Text}}{{end}}{{end}}\end{document}`

func testStore() *store.Store {
	return &store.Store{
		Profile: store.Profile{Name: "Ada Lovelace", Contact: store.Contact{Email: "ada@example.com"}},
		Roles: []store.Role{{
			Company: "Acme", Title: "Engineer", Start: "2020", End: "2022",
			Projects: []store.Project{{
				Name: "Billing",
				Achievements: []store.Achievement{
					{ID: "u1", Text: "Built a Go billing service"},
					{ID: "u2", Text: "Scaled Kafka"},
				},
			}},
		}},
		Skills: []store.Skill{{Raw: "Go (expert)"}},
	}
}

func testCandidates() []gantry.Document {
	return []gantry.Document{
		{ID: "u1", Content: "Built a Go billing service"},
		{ID: "u2", Content: "Scaled Kafka"},
	}
}

func baseConfig() Config {
	return Config{
		Template:      testTemplate,
		Store:         testStore(),
		Requirements:  []jd.Requirement{{Text: "Go", MustHave: true}},
		Candidates:    testCandidates(),
		MaxIterations: 3,
		MaxRepairs:    1,
	}
}

func okCompiler() render.CompileFunc {
	return func(ctx context.Context, tex string) (render.CompileResult, error) {
		return render.CompileResult{OK: true, PDF: []byte("%PDF-1"), Log: "ok"}, nil
	}
}

func bullets(text string) gantry.LLMResponse {
	return gantry.LLMResponse{Content: `[{"unit_id":"u1","text":"` + text + `"}]`, StopReason: gantry.StopReasonEnd}
}

// evalResp builds a rubric reply with every dimension set to score (so the
// composite equals score, since the weights sum to 1.0) and the given truthful flag.
func evalResp(score float64, truthful bool) gantry.LLMResponse {
	js := fmt.Sprintf(`{"scores":{"jd_coverage":%[1]g,"relevance":%[1]g,"evidence_quality":%[1]g,"truthfulness":%[1]g,"format":%[1]g},"truthful":%[2]t,"critique":{},"summary":""}`, score, truthful)
	return gantry.LLMResponse{Content: js, StopReason: gantry.StopReasonEnd}
}

func withUsage(r gantry.LLMResponse, tokens int) gantry.LLMResponse {
	r.Usage = gantry.Usage{InputTokens: tokens}
	return r
}

const passEvalJSON = `{"scores":{"jd_coverage":0.9,"relevance":0.9,"evidence_quality":0.9,"truthfulness":0.9,"format":0.9},"truthful":true,"critique":{},"summary":"ship it"}`

func noLLM() gantry.LLMClient { return eval.NewMockLLMClient() }

// --- tests ----------------------------------------------------------------

func TestRunPassesFirstIteration(t *testing.T) {
	deps := Deps{
		GenLLM:    eval.NewMockLLMClient(bullets("Built a Go billing platform")),
		EvalLLM:   eval.NewMockLLMClient(gantry.LLMResponse{Content: passEvalJSON, StopReason: gantry.StopReasonEnd}),
		RepairLLM: noLLM(),
		Compile:   okCompiler(),
	}
	res, err := Run(context.Background(), baseConfig(), deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopPassed || !res.Passed {
		t.Errorf("StopReason=%q Passed=%v, want passed/true", res.StopReason, res.Passed)
	}
	if len(res.Iterations) != 1 {
		t.Fatalf("iterations = %d, want 1", len(res.Iterations))
	}
	if res.Best == nil || !res.Best.Compiled || len(res.Best.PDF) == 0 {
		t.Errorf("best = %+v", res.Best)
	}
}

func TestRunFailsThenPassesFeedingCritique(t *testing.T) {
	genLLM := eval.NewMockLLMClient(bullets("v1"), bullets("v2"))
	failJSON := `{"scores":{"jd_coverage":0.5,"relevance":0.5,"evidence_quality":0.5,"truthfulness":0.5,"format":0.5},"truthful":true,"critique":{"jd_coverage":"add Go depth"},"summary":"needs work"}`
	evalLLM := eval.NewMockLLMClient(
		gantry.LLMResponse{Content: failJSON, StopReason: gantry.StopReasonEnd},
		gantry.LLMResponse{Content: passEvalJSON, StopReason: gantry.StopReasonEnd},
	)
	res, err := Run(context.Background(), baseConfig(), Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: noLLM(), Compile: okCompiler()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopPassed || !res.Passed {
		t.Errorf("got StopReason=%q Passed=%v", res.StopReason, res.Passed)
	}
	if len(res.Iterations) != 2 {
		t.Fatalf("iterations = %d, want 2", len(res.Iterations))
	}
	greqs := genLLM.Requests()
	if len(greqs) != 2 {
		t.Fatalf("gen requests = %d, want 2", len(greqs))
	}
	msg := greqs[1].Messages[0].Content
	if !strings.Contains(msg, "needs work") || !strings.Contains(msg, "add Go depth") {
		t.Errorf("2nd generate missing prior critique:\n%s", msg)
	}
}

func TestRunMaxIterationsSelectsHighestComposite(t *testing.T) {
	genLLM := eval.NewMockLLMClient(bullets("a"), bullets("b"), bullets("c"))
	// composites 0.5, 0.7, 0.6 (all truthful, none pass) → best is index 1.
	evalLLM := eval.NewMockLLMClient(evalResp(0.5, true), evalResp(0.7, true), evalResp(0.6, true))
	res, err := Run(context.Background(), baseConfig(), Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: noLLM(), Compile: okCompiler()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopMaxIterations || res.Passed {
		t.Errorf("got StopReason=%q Passed=%v", res.StopReason, res.Passed)
	}
	if len(res.Iterations) != 3 {
		t.Fatalf("iterations = %d, want 3", len(res.Iterations))
	}
	if res.Best == nil || res.Best.Index != 1 {
		t.Errorf("best index = %v, want 1", res.Best)
	}
}

func TestRunPrefersTruthfulIterationForBest(t *testing.T) {
	genLLM := eval.NewMockLLMClient(bullets("a"), bullets("b"))
	// index 0: composite 0.9 but NOT truthful (Pass=false); index 1: 0.6 truthful.
	evalLLM := eval.NewMockLLMClient(evalResp(0.9, false), evalResp(0.6, true))
	cfg := baseConfig()
	cfg.MaxIterations = 2
	res, err := Run(context.Background(), cfg, Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: noLLM(), Compile: okCompiler()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Best == nil || res.Best.Index != 1 {
		t.Errorf("best index = %v, want 1 (truthful preferred over higher untruthful composite)", res.Best)
	}
}

func TestRunHandlesCompileFailureNonFatally(t *testing.T) {
	genLLM := eval.NewMockLLMClient(bullets("a"))
	evalLLM := eval.NewMockLLMClient(evalResp(0.6, true))
	repairLLM := eval.NewMockLLMClient(gantry.LLMResponse{Content: "still broken", StopReason: gantry.StopReasonEnd})
	failCompile := func(ctx context.Context, tex string) (render.CompileResult, error) {
		return render.CompileResult{OK: false, Log: "! broken"}, nil
	}
	cfg := baseConfig()
	cfg.MaxIterations = 1
	cfg.MaxRepairs = 1
	res, err := Run(context.Background(), cfg, Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: repairLLM, Compile: failCompile})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Iterations) != 1 {
		t.Fatalf("iterations = %d, want 1", len(res.Iterations))
	}
	if res.Iterations[0].Compiled {
		t.Error("iteration should be marked not compiled")
	}
	if res.Best == nil {
		t.Fatal("best should still be selected from a non-compiling iteration")
	}
	ereqs := evalLLM.Requests()
	if len(ereqs) != 1 || !strings.Contains(ereqs[0].Messages[0].Content, "FAILED to compile") {
		t.Errorf("evaluator should be told the document failed to compile")
	}
}

func TestRunStopsOnBudgetAndEmitsBestSoFar(t *testing.T) {
	genLLM := eval.NewMockLLMClient(withUsage(bullets("a"), 60), withUsage(bullets("b"), 60))
	evalLLM := eval.NewMockLLMClient(withUsage(evalResp(0.6, true), 60), withUsage(evalResp(0.6, true), 60))
	cfg := baseConfig()
	cfg.MaxIterations = 3
	cfg.Budget = limiter.Limits{MaxTokens: 100}
	res, err := Run(context.Background(), cfg, Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: noLLM(), Compile: okCompiler()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopBudget {
		t.Errorf("StopReason = %q, want budget", res.StopReason)
	}
	// Iter 0: gen records 60, eval records 60 (total 120). Iter 1's generate is
	// refused (120 > 100), so only one full iteration was recorded.
	if len(res.Iterations) != 1 {
		t.Errorf("iterations = %d, want 1", len(res.Iterations))
	}
	if res.Best == nil || res.Best.Index != 0 {
		t.Errorf("best = %+v, want index 0", res.Best)
	}
}

func TestRunReturnsErrorOnFatalGenerateFailure(t *testing.T) {
	genLLM := eval.NewMockLLMClient(gantry.LLMResponse{Content: "not json", StopReason: gantry.StopReasonEnd})
	_, err := Run(context.Background(), baseConfig(), Deps{GenLLM: genLLM, EvalLLM: noLLM(), RepairLLM: noLLM(), Compile: okCompiler()})
	if err == nil {
		t.Error("want a fatal error when generate returns unparseable content")
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	cfg := baseConfig()
	cfg.MaxIterations = 0
	_, err := Run(context.Background(), cfg, Deps{GenLLM: noLLM(), EvalLLM: noLLM(), RepairLLM: noLLM(), Compile: okCompiler()})
	if err == nil {
		t.Error("want error when MaxIterations < 1")
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/orchestrate/...
```
Expected: build failure — `Config`, `Deps`, `Iteration`, `Result`, `Run`, `StopPassed`, `StopMaxIterations`, `StopBudget` undefined.

- [ ] **Step 3: Write the types**

Create `internal/orchestrate/types.go`:

```go
// Package orchestrate drives the bounded generate→render→compile→evaluate loop:
// it refines a tailored resume until an iteration passes the rubric or a limit
// (iteration count or token/cost budget) is hit, returning the full per-iteration
// history and the selected best iteration.
package orchestrate

import (
	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/limiter"
	"github.com/farazhassan/tailor-swift/internal/evaluate"
	"github.com/farazhassan/tailor-swift/internal/generate"
	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/render"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// Stop reasons recorded on a finished run.
const (
	StopPassed        = "passed"         // an iteration met the rubric
	StopMaxIterations = "max-iterations" // the iteration cap was reached
	StopBudget        = "budget"         // the token/cost budget was exceeded
)

// Config is the static input to a run. Requirements and Candidates are the
// already-retrieved JD requirements and content set (the pre-loop acquisition is
// the CLI's job).
type Config struct {
	Template      string            // LaTeX template text
	Store         *store.Store      // content store, for rendering
	Requirements  []jd.Requirement  // the job's requirements (must-haves flagged)
	Candidates    []gantry.Document // retrieved candidate achievements (ID + text)
	MaxIterations int               // hard cap on loop iterations (>= 1)
	MaxRepairs    int               // passed to render.CompileWithRepair
	Budget        limiter.Limits    // token/cost ceiling; zero value = unlimited
}

// Deps are the injected collaborators: production passes real clients and the
// real compiler; tests pass mocks and a fake CompileFunc.
type Deps struct {
	GenLLM    gantry.LLMClient   // generator (strong model)
	EvalLLM   gantry.LLMClient   // evaluator (cheaper model)
	RepairLLM gantry.LLMClient   // LaTeX repair model
	Compile   render.CompileFunc // PDF compiler (render.PDFLaTeX in production)
}

// Iteration records the outcome of one generate→render→compile→evaluate pass.
type Iteration struct {
	Index      int
	Bullets    []generate.Bullet
	TeX        string
	Compiled   bool
	CompileLog string
	PDF        []byte
	Evaluation *evaluate.Evaluation
}

// Result is the outcome of a run. Best is the iteration the caller should emit;
// it is nil only when no iteration completed (e.g. the budget tripped on the
// very first generate).
type Result struct {
	Iterations []Iteration
	Best       *Iteration
	Passed     bool
	StopReason string
}
```

- [ ] **Step 4: Write the loop**

Create `internal/orchestrate/orchestrate.go`:

```go
package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/limiter"
	"github.com/farazhassan/tailor-swift/internal/evaluate"
	"github.com/farazhassan/tailor-swift/internal/generate"
	"github.com/farazhassan/tailor-swift/internal/render"
)

// Run drives the refinement loop. Each iteration generates a selection, renders
// and compiles it (with the repair loop), and scores it; the loop stops on the
// first passing iteration, on the iteration cap, or when the shared budget is
// exceeded. A compile that never succeeds after repairs is non-fatal: the
// iteration is recorded with Compiled=false and the loop continues. Any other
// error (unparseable model output, a bad template, a missing compiler) is fatal.
func Run(ctx context.Context, cfg Config, deps Deps) (*Result, error) {
	if cfg.MaxIterations < 1 {
		return nil, fmt.Errorf("orchestrate: MaxIterations must be >= 1, got %d", cfg.MaxIterations)
	}

	lim := limiter.NewBudget(cfg.Budget)
	genLLM := newBudgetedClient(deps.GenLLM, lim)
	evalLLM := newBudgetedClient(deps.EvalLLM, lim)
	repairLLM := newBudgetedClient(deps.RepairLLM, lim)

	res := &Result{StopReason: StopMaxIterations}
	critique := ""

	for i := 0; i < cfg.MaxIterations; i++ {
		gen, err := generate.Generate(ctx, genLLM, generate.Input{
			Requirements:  cfg.Requirements,
			Candidates:    cfg.Candidates,
			PriorCritique: critique,
		})
		if errors.Is(err, gantry.ErrLimitExceeded) {
			res.StopReason = StopBudget
			break
		}
		if err != nil {
			return nil, fmt.Errorf("orchestrate: generate (iteration %d): %w", i, err)
		}

		tex, err := render.Render(cfg.Template, cfg.Store, gen)
		if err != nil {
			return nil, fmt.Errorf("orchestrate: render (iteration %d): %w", i, err)
		}

		rr, cerr := render.CompileWithRepair(ctx, deps.Compile, repairLLM, tex, cfg.MaxRepairs)
		if cerr != nil {
			if errors.Is(cerr, gantry.ErrLimitExceeded) {
				res.StopReason = StopBudget
				break
			}
			if !errors.Is(cerr, render.ErrCompileFailed) {
				return nil, fmt.Errorf("orchestrate: compile (iteration %d): %w", i, cerr)
			}
			// ErrCompileFailed: non-fatal. rr holds the last attempted TeX and the
			// final log; fall through to evaluate this non-compiling iteration.
		}

		ev, eerr := evaluate.Evaluate(ctx, evalLLM, evaluate.EvalInput{
			Requirements: cfg.Requirements,
			Candidates:   cfg.Candidates,
			Resume:       rr.TeX,
			Compiled:     rr.OK,
		})
		if errors.Is(eerr, gantry.ErrLimitExceeded) {
			res.StopReason = StopBudget
			break
		}
		if eerr != nil {
			return nil, fmt.Errorf("orchestrate: evaluate (iteration %d): %w", i, eerr)
		}

		res.Iterations = append(res.Iterations, Iteration{
			Index:      i,
			Bullets:    gen.Bullets,
			TeX:        rr.TeX,
			Compiled:   rr.OK,
			CompileLog: rr.Log,
			PDF:        rr.PDF,
			Evaluation: ev,
		})

		if ev.Pass {
			res.StopReason = StopPassed
			break
		}
		critique = formatCritique(ev)
	}

	res.Best = selectBest(res.Iterations)
	if res.Best != nil && res.Best.Evaluation != nil {
		res.Passed = res.Best.Evaluation.Pass
	}
	return res, nil
}

// formatCritique renders an evaluation into the prior-critique text fed to the
// next generation: the summary followed by each non-empty per-dimension note.
func formatCritique(ev *evaluate.Evaluation) string {
	var b strings.Builder
	if ev.Summary != "" {
		b.WriteString(ev.Summary)
		b.WriteString("\n")
	}
	for _, d := range evaluate.Dimensions {
		if note, ok := ev.Critique[d.Key]; ok && note != "" {
			fmt.Fprintf(&b, "- %s: %s\n", d.Key, note)
		}
	}
	return b.String()
}

// selectBest picks the iteration to emit: a passing iteration if any; otherwise
// the truthful iteration with the highest composite; otherwise the highest
// composite overall; nil when there are no iterations.
func selectBest(its []Iteration) *Iteration {
	for i := range its {
		if its[i].Evaluation != nil && its[i].Evaluation.Pass {
			return &its[i]
		}
	}
	var best *Iteration
	for i := range its {
		ev := its[i].Evaluation
		if ev == nil || !ev.Truthful {
			continue
		}
		if best == nil || ev.Composite > best.Evaluation.Composite {
			best = &its[i]
		}
	}
	if best != nil {
		return best
	}
	for i := range its {
		if its[i].Evaluation == nil {
			continue
		}
		if best == nil || its[i].Evaluation.Composite > best.Evaluation.Composite {
			best = &its[i]
		}
	}
	return best
}
```

- [ ] **Step 5: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/orchestrate/... && go vet ./internal/orchestrate/...
```
Expected: PASS (2 budget tests + 8 loop tests = 10), vet clean.

- [ ] **Step 6: Final whole-tree check**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS across all packages (`cmd/tailor`, `internal/embed`, `internal/evaluate`, `internal/fence`, `internal/generate`, `internal/jd`, `internal/orchestrate`, `internal/render`, `internal/retrieve`, `internal/store`).

- [ ] **Step 7: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/orchestrate/types.go internal/orchestrate/orchestrate.go internal/orchestrate/orchestrate_test.go
git commit -m "feat(orchestrate): add generate-evaluate refinement loop with stop conditions"
```

---

## Out of scope (deferred to later plans)

- **Pre-loop acquisition** (JD fetch/cache, embed, retrieve): the CLI fills `Config.Requirements` and `Config.Candidates` via the existing `jd`/`embed`/`retrieve` packages before calling `Run`.
- **Artifact emission and exit code**: the CLI writes `out/<job-slug>-<date>/` from the `Result` — `Best.PDF` → `resume.pdf`, `Best.TeX` → `resume.tex`, `Best.CompileLog` (plus per-iteration evaluations) → `run.log`, the evaluations → `critique.json` — and exits non-zero when `Result.Passed` is false or the best iteration did not compile.
- **Model selection / constructing `anthropic.New(...)` and `render.PDFLaTeX`**: the CLI builds the three clients and the real compiler and passes them in `Deps`.

## Self-Review

- **Spec coverage:** Implements the design's "Pipeline (the `generate` flow)" loop section and the stop condition. The loop (`Run`) runs steps 5–8 per iteration: **generate** (`generate.Generate` with the prior critique), **render** (`render.Render`), compile + repair (`render.CompileWithRepair`, the design's step 6 repair sub-step), **evaluate** (`evaluate.Evaluate`), and **decide** (`ev.Pass` → stop). "score ≥ 0.85 **and** truthfulness passed → done" is `evaluate`'s `Pass` (composite ≥ `Threshold` && `Truthful`), surfaced here as `StopPassed`. "Cap hit → emit best-scoring iteration" is `StopMaxIterations` + `selectBest`. The "budget cap (limiter) → stop, emit best-so-far" edge case is the shared `BudgetLimiter` via `budgetedClient` → `StopBudget` (`TestRunStopsOnBudgetAndEmitsBestSoFar`). "LaTeX compile fails after repair cap → keep best `.tex`" and "if all iterations fail → emit highest-truthful-scoring attempt" are the non-fatal `ErrCompileFailed` path + `selectBest`'s truthful-first ordering (`TestRunHandlesCompileFailureNonFatally`, `TestRunPrefersTruthfulIterationForBest`). The critique-feedback requirement ("feed critique to step 5 and loop") is `formatCritique` threaded into `generate.Input.PriorCritique` (`TestRunFailsThenPassesFeedingCritique`). The design's testing-strategy note that `orchestrate/` loop logic is "the highest-value tests" is met by the eight scripted mock-generator/critic + scripted-compiler tests, with no API key or `pdflatex`. Emission (step 9) and pre-loop acquisition (steps 1–4) are explicitly deferred to the CLI plan, with `Result` carrying everything emission needs.
- **Placeholders:** none — every step is full code, an exact command with expected output, or a commit. No "TBD"/"handle errors"/"similar to Task N".
- **Type consistency:** `Config`/`Deps`/`Iteration`/`Result` and the `StopPassed`/`StopMaxIterations`/`StopBudget` constants are defined once in `types.go` (Task 3) and consumed by `Run`, `selectBest`, and every Task 3 test. `newBudgetedClient(inner gantry.LLMClient, lim *limiter.BudgetLimiter) *budgetedClient` and its `Generate` method (Task 2) are used by `Run` (wrapping all three `Deps` clients against one `limiter.NewBudget(cfg.Budget)`) and tested directly in Task 2. Collaborator calls match the merged packages exactly: `generate.Generate(ctx, llm, generate.Input{Requirements, Candidates, PriorCritique})` → `*generate.Result{Bullets}`; `render.Render(cfg.Template, cfg.Store, gen)` → `(string, error)`; `render.CompileWithRepair(ctx, deps.Compile, repairLLM, tex, cfg.MaxRepairs)` → `(render.RepairResult{OK, PDF, TeX, Log, Attempts}, error)` with the `render.ErrCompileFailed` sentinel; `evaluate.Evaluate(ctx, llm, evaluate.EvalInput{Requirements, Candidates, Resume, Compiled})` → `*evaluate.Evaluation{Pass, Truthful, Composite, Summary, Critique}`; `evaluate.Dimensions` for the critique ordering. gantry types (`LLMClient`, `LLMRequest`, `LLMResponse{Content, Usage, StopReason}`, `State{Usage}`, `Usage{InputTokens}`, `Document{ID, Content}`, `ErrLimitExceeded`) and `limiter.NewBudget`/`Limits{MaxTokens}`/`BudgetLimiter.{Check,Record,Total}` are used exactly as defined in gantry v0.0.3-beta. `store.Store`/`Profile`/`Contact`/`Role`/`Project`/`Achievement`/`Skill` in the test fixture match `internal/store/types.go`.
- **Budget arithmetic sanity:** with one shared limiter and `MaxTokens=100`, iteration 0 records 60 (generate) then 60 (evaluate) = 120; iteration 1's generate `Check` sees 120 > 100 and is refused — so exactly one iteration is recorded before `StopBudget`, as asserted.
