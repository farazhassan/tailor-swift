# Generate CLI Design (Plan 10b)

**Status:** Approved (design)
**Date:** 2026-06-19
**Author:** fhassan@league.com

## Context

`pipeline.Acquire` (Plan 10a) turns a content file + a JD source into the inputs
`orchestrate.Run` (Plan 9) consumes, and `orchestrate.Run` drives the bounded
generate→render→compile→evaluate loop. Nothing yet wires them together behind a
command. `tailor generate` is that wiring: the user-facing command that parses
flags, constructs the real clients, runs acquisition then the loop, emits
artifacts, and chooses the process exit code.

This is the second of the two CLI sub-projects:

- **Plan 10a (done):** `internal/pipeline` — acquisition.
- **Plan 10b (this spec):** the `tailor generate` command.

## Goal

A working `tailor generate` command that, given a content store and a job posting
URL, produces a tailored resume (`.pdf`/`.tex`), a critique, and a run log in an
output directory — chaining `pipeline.Acquire` into `orchestrate.Run` with real
Anthropic/Voyage clients and the `pdflatex` compiler, and surfacing the outcome
through a meaningful exit code.

## Non-goals

- The `ingest` and `evaluate` commands (still stubs).
- Streaming/progress UI beyond a gap warning and the run log.
- Configurable per-role models, budget flags, or repair-count flags (hardcoded
  for v1; see Flag surface).

## Architecture

### Core / wiring split

The command splits into two layers so the orchestration logic is unit-testable
without API keys or network:

- **Wiring** (`runGenerate`): parses flags, constructs the real clients
  (`anthropic.New`, `embed.NewVoyageClient`, `render.PDFLaTeX`), resolves the
  template text, assembles the core deps, and calls the core. Integration-only.
- **Core** (`genRun`): takes a plain config struct + an injected `genDeps`
  struct, runs `pipeline.Acquire` → warn on gaps → `orchestrate.Run` → write
  artifacts → return the exit code. Fully unit-testable with fakes.

### File structure

- `templates/embed.go` *(new)*: `package templates` with `//go:embed default.tex`
  → `var Default string`. `//go:embed` cannot reference parent directories, so
  the embed declaration must live in the `templates/` directory beside the file;
  this keeps `default.tex` where it is and lets `cmd/tailor` import it.
- `cmd/tailor/generate.go` *(new)*: `runGenerate` (wiring), `genRun` (core),
  helpers `slugify`, `writeArtifacts`, and the exit-code mapping.
- `cmd/tailor/generate_test.go` *(new)*: core + helper unit tests with fakes.
- `cmd/tailor/run.go` *(modify)*: dispatch `case "generate"` →
  `runGenerate(args[1:], stdout, stderr)`; remove `generate` from the
  "not implemented" branch and update the usage text.
- `cmd/tailor/run_test.go` *(modify)*: `TestRun_KnownStubs` asserts `generate`
  prints "not implemented"; narrow it to `ingest`, `evaluate`.

### Types

```go
// genConfig is the resolved, validated input to the core (filled by the wiring
// from flags; tests construct it directly).
type genConfig struct {
    ContentPath    string
    JDURL          string
    JDFile         string
    OutDir         string    // base output dir (--out)
    EmbedModel     string    // Voyage model for cache scoping (VOYAGE_MODEL)
    TopK           int
    MinScore       float64
    MaxIterations  int
    EmbedCachePath string
    JDCacheDir     string
    Today          time.Time // injected; wiring passes time.Now()
}

// genDeps are the injected collaborators. The wiring assigns the single
// constructed Anthropic client to all four LLM role slots; keeping them separate
// lets tests script a per-role mock (matching orchestrate's test pattern).
type genDeps struct {
    ExtractLLM gantry.LLMClient      // jd requirement extraction
    GenLLM     gantry.LLMClient      // resume generation
    EvalLLM    gantry.LLMClient      // evaluation
    RepairLLM  gantry.LLMClient      // LaTeX repair
    Embedder   embeddings.Embeddings // Voyage in production; fake in tests
    Compile    render.CompileFunc    // render.PDFLaTeX in production
    Template   string                // resolved template text
}

func runGenerate(args []string, stdout, stderr io.Writer) int
func genRun(ctx context.Context, cfg genConfig, deps genDeps, stdout, stderr io.Writer) int
```

## Flag surface

`tailor generate` builds a `flag.FlagSet` whose output is routed to the passed
`stderr`.

| Flag | Default | Maps to |
|------|---------|---------|
| `--content` | *(required)* | `genConfig.ContentPath` → `pipeline.Config.ContentPath` |
| `--jd-url` | *(required)* | `genConfig.JDURL` → `pipeline.Config.JDURL`; slug source |
| `--jd-file` | `""` | `pipeline.Config.JDFile` (offline JD; URL still required) |
| `--model` | `claude-sonnet-4-6` | Anthropic model for all four roles |
| `--out` | `out` | `genConfig.OutDir` |
| `--template` | `""` | path override; empty → embedded default |
| `--max-iterations` | `3` | `orchestrate.Config.MaxIterations` |
| `--top-k` | `8` | `pipeline.Config.TopK` |
| `--min-score` | `0` | `pipeline.Config.MinScore` |
| `--embed-cache` | `""` | `pipeline.Config.EmbedCachePath` (`""` disables) |
| `--jd-cache` | `""` | `pipeline.Config.JDCacheDir` |

**Hardcoded constants:** `MaxRepairs = 2`; `Budget = limiter.Limits{}` (unlimited).
**Embedding model:** `EmbedModel` is read from `VOYAGE_MODEL` (default `voyage-3`)
— the same source `embed.NewVoyageClient` uses — so the embed cache scopes to the
model the Voyage client actually produces vectors with. The `--model` flag is the
Anthropic model and is unrelated to the embedding cache scope.

## Data flow inside `genRun`

```
1. res, err := pipeline.Acquire(ctx, pipeline.Config{
       ContentPath, JDURL, JDFile, Model: cfg.EmbedModel,
       TopK, MinScore, EmbedCachePath, JDCacheDir,
   }, pipeline.Deps{LLM: deps.ExtractLLM, Embedder: deps.Embedder})
   → fatal (exit 1) on error.
2. If len(res.Gaps) > 0: warn to stderr
   ("warning: N must-have requirement(s) unmatched: <texts>"). Non-fatal.
3. run, err := orchestrate.Run(ctx, orchestrate.Config{
       Template: deps.Template, Store: res.Store,
       Requirements: res.Requirements, Candidates: res.Candidates,
       MaxIterations: cfg.MaxIterations, MaxRepairs: 2, Budget: limiter.Limits{},
   }, orchestrate.Deps{
       GenLLM: deps.GenLLM, EvalLLM: deps.EvalLLM,
       RepairLLM: deps.RepairLLM, Compile: deps.Compile,
   })
   → fatal (exit 1) on error.
4. If run.Best == nil → fatal (exit 1; no artifacts). (Happens only when no
   iteration completed, e.g. the budget tripped on the first generate.)
5. dir := filepath.Join(cfg.OutDir, slugify(cfg.JDURL)+"-"+cfg.Today.Format("2006-01-02"))
   os.MkdirAll(dir, 0o755) → fatal on error.
6. writeArtifacts(dir, run, res.Posting, stderr) → fatal on error.
7. Print the output dir to stdout. Return 0 if run.Passed else 3.
```

### Slug derivation

`slugify(url)`:
- Parse the URL; take the last non-empty path segment (split on `/`).
- Lowercase; replace each run of non-`[a-z0-9]` characters with a single `-`;
  trim leading/trailing `-`.
- If the result is empty, return `"job"`.

Examples: `https://acme.com/jobs/senior-go-engineer` → `senior-go-engineer`;
`https://acme.com/jobs/123?ref=x` → `123`; `https://acme.com/` → `job`.

## Artifacts

Written into `<out>/<slug>-<YYYY-MM-DD>/`, overwriting on a same-day re-run:

- **`resume.tex`** ← `run.Best.TeX`.
- **`resume.pdf`** ← `run.Best.PDF`. **Skipped** when `len(PDF) == 0` (the best
  iteration never compiled); a note is written to the run log and a warning to
  stderr. This is not an error here (exit code already reflects non-pass).
- **`critique.json`** ← an explicit struct (the `Evaluation` type carries
  `json:"-"` on `Composite`/`Pass`, so it cannot be marshaled directly):

  ```go
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
  ```

  Built from `run.Best.Evaluation` plus `run.StopReason` and
  `len(run.Iterations)`. Marshaled with `json.MarshalIndent`.
- **`run.log`** ← plaintext lines: JD URL; `FetchedAt`; requirement count;
  candidate count; the gap list (or "none"); one line per iteration
  (`iteration N: compiled=<bool> pass=<bool> composite=<float>`); the stop
  reason; the chosen best iteration index; and, when the PDF was skipped, a note.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | A best resume was emitted **and** it passed the rubric (`run.Passed`). |
| `3` | A best resume was emitted but did **not** pass (max-iterations/budget). Artifacts still written. |
| `1` | Fatal: acquisition error, orchestrate error, `run.Best == nil`, missing API key / client construction panic, or an mkdir/write failure. |
| `2` | Usage error: missing required flag or flag parse error. |

## Error handling

- **Usage (exit 2):** missing `--content` or `--jd-url`, or a `flag` parse error
  → print usage to stderr, return 2. (Validate required flags after parsing.)
- **Voyage client (exit 1):** `embed.NewVoyageClient()` returns an error when
  `VOYAGE_API_KEY` is unset → print to stderr, return 1.
- **Anthropic client (exit 1):** `anthropic.New` **panics** on a missing key or
  empty model. The wiring wraps construction in a helper that `recover()`s the
  panic and converts it to a clean stderr message + exit 1 — the panic never
  escapes the command.
- **Core (exit 1):** any error from `pipeline.Acquire`, `orchestrate.Run`,
  `os.MkdirAll`, or artifact writing → `generate: %v` to stderr, return 1.

## Testing strategy

All core tests are pure unit tests — no API keys, no network, no `pdflatex` —
using: a temp content file; the deterministic keyword-vector `fakeEmbedder`
(same approach as the pipeline tests); per-role scripted mock LLMs
(`eval.NewMockLLMClient`); a fake `render.CompileFunc`; and a local `JDFile` for
the offline JD (with a `JDURL` cache key and a temp `JDCacheDir`).

Per-role mocks: `ExtractLLM` returns the requirements JSON; `GenLLM` returns the
generation JSON; `EvalLLM` returns the evaluation JSON (pass or fail per test);
`RepairLLM` returns repaired TeX (only exercised when the fake compiler fails).

Test cases (core, via `genRun`):

1. **Happy path** — evaluator returns a passing evaluation, fake compiler
   succeeds (non-empty PDF) → exit 0; `resume.tex`, `resume.pdf`,
   `critique.json`, `run.log` all exist; `critique.json` has `"pass": true`.
2. **Not passed** — evaluator never passes → exit 3; all artifacts written;
   `critique.json` `"pass": false`.
3. **Coverage gap warning** — a must-have whose fake vector matches nothing above
   `MinScore` → stderr contains the gap warning; the run still proceeds to write
   artifacts.
4. **Non-compiling best** — fake compiler always fails (returns
   `render.ErrCompileFailed` via `CompileWithRepair`) → `resume.pdf` is **not**
   written; `run.log` notes the skip; exit 3.
5. **Fatal acquisition** — `ContentPath` points at a missing file → exit 1; no
   output directory created.
6. **slugify** — table test: trailing slash, query string, multi-segment path,
   empty path → expected slugs (including the `job` fallback).
7. **Flags** — `runGenerate` with `--jd-url` but no `--content` → exit 2;
   with `--content` but no `--jd-url` → exit 2.
8. **Output dir naming + idempotence** — `genRun` with a fixed `cfg.Today`
   writes `<out>/<slug>-2026-06-19/`; a second identical run overwrites the same
   directory without error.

Wiring test (light): `runGenerate` with valid flags but `ANTHROPIC_API_KEY` and
`VOYAGE_API_KEY` unset → exit 1 with a clean message and no panic escaping.

## Dependencies (all on `main`)

- `internal/pipeline` — `Acquire(ctx, Config, Deps) (*Result, error)`;
  `Config{ContentPath, JDURL, JDFile, Model, TopK, MinScore, EmbedCachePath, JDCacheDir}`;
  `Deps{LLM, Embedder}`; `Result{Store, Posting, Requirements, Candidates, Gaps}`.
- `internal/orchestrate` — `Run(ctx, Config, Deps) (*Result, error)`;
  `Config{Template, Store, Requirements, Candidates, MaxIterations, MaxRepairs, Budget}`;
  `Deps{GenLLM, EvalLLM, RepairLLM, Compile}`;
  `Result{Iterations, Best, Passed, StopReason}`;
  `Iteration{Index, Bullets, TeX, Compiled, CompileLog, PDF, Evaluation}`.
- `internal/render` — `PDFLaTeX` (a `CompileFunc`), `CompileFunc`, `ErrCompileFailed`.
- `internal/embed` — `NewVoyageClient() (embeddings.Embeddings, error)`.
- `internal/store` — `Store`, used via `pipeline.Result.Store`.
- `internal/jd` — `Posting{URL, FetchedAt, RawText, Requirements}`, `Requirement{Text, MustHave}`.
- `internal/evaluate` — `Evaluation{Scores, Truthful, Critique, Summary, Composite, Pass}`.
- gantry — `gantry.LLMClient`; `components/llm/anthropic.New(model, opts...)`;
  `components/embeddings.Embeddings`; `components/limiter.Limits`;
  `eval.NewMockLLMClient` (tests).
- `templates` *(new)* — `Default` (embedded `default.tex`).
```
