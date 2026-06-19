# Compile + Repair Implementation Plan (Plan 7)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compile a rendered `.tex` document to a PDF with `pdflatex`, and when compilation fails, feed the compiler error and the broken source to an LLM to repair the LaTeX and recompile — up to a small cap — returning the resulting PDF bytes (or the last attempt plus a sentinel error if it never compiles).

**Architecture:** Two new files in the existing `internal/render` package. `compile.go` defines a `CompileResult`, an injectable `CompileFunc` seam, and `PDFLaTeX` (the real `os/exec` compiler that runs in a throwaway temp dir and reads the PDF bytes back out). `repair.go` defines `repairTeX` (one LLM fix call) and `CompileWithRepair` (the loop over an injected `CompileFunc`). Injecting the compiler lets the loop's logic be tested with a scripted fake — no `pdflatex` needed in CI — while the real compiler is exercised by a build-tag-gated test. This plan also pays down a documented duplication: the `stripCodeFence` helper copied in `internal/jd` and `internal/generate` is extracted into a new `internal/fence` package (the repair loop is its third consumer), and both callers are refactored to use it.

**Tech Stack:** Go 1.26, stdlib (`os`, `os/exec`, `path/filepath`, `context`, `errors`, `strings`, `fmt`), gantry (`github.com/farazhassan/gantry` for `LLMClient`/`LLMRequest`/`Message`; `github.com/farazhassan/gantry/eval` for the mock in tests). `pdflatex` for the gated real-compile test only.

**Branch:** `feat/compile-repair` in `/Users/fhassan-mac/Dev/tailor-swift`.

## Prerequisites

This plan extends `internal/render` (merged via PR #6: `escape.go`, `model.go`, `render.go`, `templates/default.tex`). The deterministic `Render(templateText string, s *store.Store, res *generate.Result) (string, error)` produces the `.tex` string that this plan compiles; `Render` itself is unchanged here. The repair loop takes a `gantry.LLMClient` (production wires `anthropic.New(...)`, tests use `eval.NewMockLLMClient(...)`).

- Branch `feat/compile-repair` off `main` (Task 1 below).

**Environment:** `pdflatex` is available locally at `/Library/TeX/texbin/pdflatex`. The gated compile test (Task 3) runs only with `-tags pdflatex`; the default `go test ./...` excludes it so a LaTeX-less CI still passes.

**Out of scope (later plans):**
- **Artifact emission** — writing `out/<job-slug>-<date>/` (`resume.pdf`, `resume.tex`, `run.log`, `critique.json`) is the orchestrator's job (design step 9). This plan returns PDF bytes + the final source + the log in memory; the orchestrator persists them.
- **Evaluator, the generate↔evaluate loop, and budget limiter** — orchestrator plan.
- **Model selection / constructing `anthropic.New(...)`** — the CLI/orchestrator picks the repair model and builds the client; `CompileWithRepair` takes an already-constructed `gantry.LLMClient`.

**Sandbox note:** `go` and `git` commands may fail with "operation not permitted" on the build cache (`~/Library/Caches/go-build`) or git transport under the harness sandbox. `pdflatex` (Task 3) writes only to a temp dir under `$TMPDIR` and should run sandboxed, but if any step fails with "operation not permitted", retry the same command with the sandbox disabled. This is expected and pre-authorized for this project.

**A note on `stripCodeFence` (now being extracted):** `internal/jd/requirements.go` and `internal/generate/generate.go` each hold a byte-identical unexported `stripCodeFence`. The generate plan committed to extracting a shared helper when a third consumer appeared. The repair loop here is that third consumer, so Task 2 creates `internal/fence.Strip` and refactors both existing callers. Behavior is unchanged — their existing tests must still pass.

---

### Task 1: Branch and confirm dependencies resolve

**Files:**
- None modified (branch + verification only)

- [ ] **Step 1: Create the feature branch**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git checkout main
git checkout -b feat/compile-repair
```

- [ ] **Step 2: Confirm the prerequisite packages resolve and pdflatex is present**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go list ./internal/render/... ./internal/jd/... ./internal/generate/... && which pdflatex
```
Expected: prints the three package paths and a `pdflatex` path (e.g. `/Library/TeX/texbin/pdflatex`). If `internal/render` is missing, you based off the wrong branch — it must be present on `main`.

- [ ] **Step 3: Confirm the tree builds and tests pass before changes**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./...
```
Expected: PASS (no behavior change yet).

No commit for this task — it only establishes the branch.

---

### Task 2: Extract the shared `internal/fence` package

**Files:**
- Create: `internal/fence/fence.go`
- Test: `internal/fence/fence_test.go`
- Modify: `internal/jd/requirements.go` (use `fence.Strip`, delete local helper)
- Modify: `internal/generate/generate.go` (use `fence.Strip`, delete local helper)

- [ ] **Step 1: Write the failing test**

Create `internal/fence/fence_test.go`:

```go
package fence

import "testing"

func TestStripNoFenceTrims(t *testing.T) {
	if got := Strip("  hello  "); got != "hello" {
		t.Errorf("Strip = %q, want %q", got, "hello")
	}
}

func TestStripJSONFence(t *testing.T) {
	in := "```json\n[{\"a\":1}]\n```"
	if got := Strip(in); got != `[{"a":1}]` {
		t.Errorf("Strip = %q", got)
	}
}

func TestStripPlainFence(t *testing.T) {
	in := "```\n\\documentclass\n```"
	if got := Strip(in); got != `\documentclass` {
		t.Errorf("Strip = %q", got)
	}
}

func TestStripMultiLineFence(t *testing.T) {
	in := "```latex\nline1\nline2\n```"
	if got := Strip(in); got != "line1\nline2" {
		t.Errorf("Strip = %q", got)
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/fence/...
```
Expected: build failure — package `fence` / `Strip` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/fence/fence.go`:

```go
// Package fence strips markdown code fences from LLM output so the wrapped
// payload (JSON, LaTeX, etc.) can be parsed or used directly.
package fence

import "strings"

// Strip removes a surrounding markdown code fence if present. Input wrapped in
// ```lang ... ``` (or plain ``` ... ```) returns the inner content; input with
// no fence is returned trimmed of surrounding whitespace.
func Strip(s string) string {
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
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/fence/...
```
Expected: PASS (4 tests).

- [ ] **Step 5: Refactor `internal/jd/requirements.go` to use `fence.Strip`**

In `internal/jd/requirements.go`, replace the import block:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
)
```

with (add the `fence` import; `strings` stays — it is still used by `strings.TrimSpace`):

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/fence"
)
```

Replace the unmarshal call:

```go
	if err := json.Unmarshal([]byte(stripCodeFence(resp.Content)), &reqs); err != nil {
```

with:

```go
	if err := json.Unmarshal([]byte(fence.Strip(resp.Content)), &reqs); err != nil {
```

Delete the entire local helper (the comment and the function):

```go
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

- [ ] **Step 6: Refactor `internal/generate/generate.go` to use `fence.Strip`**

In `internal/generate/generate.go`, replace the import block:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
)
```

with (add `fence`; REMOVE `strings` — after deleting the local helper, `strings` is no longer used in this file):

```go
import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/fence"
)
```

Replace the unmarshal call:

```go
	if err := json.Unmarshal([]byte(stripCodeFence(resp.Content)), &bullets); err != nil {
```

with:

```go
	if err := json.Unmarshal([]byte(fence.Strip(resp.Content)), &bullets); err != nil {
```

Delete the entire local helper (the comment and the function):

```go
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

- [ ] **Step 7: Verify the refactor compiles and all existing tests still pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./internal/fence/... ./internal/jd/... ./internal/generate/... && go vet ./internal/fence/... ./internal/jd/... ./internal/generate/...
```
Expected: PASS. The jd and generate suites pass unchanged (behavior identical); `fence` passes. If `go build` reports `"strings" imported and not used` in `generate.go`, you left the `strings` import in — remove it.

- [ ] **Step 8: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/fence/fence.go internal/fence/fence_test.go internal/jd/requirements.go internal/generate/generate.go
git commit -m "refactor(fence): extract shared code-fence stripper from jd and generate"
```

---

### Task 3: The real `pdflatex` compiler

**Files:**
- Create: `internal/render/compile.go`
- Test: `internal/render/compile_pdflatex_test.go` (build-tag gated: `//go:build pdflatex`)

- [ ] **Step 1: Write the failing (gated) test**

Create `internal/render/compile_pdflatex_test.go`:

```go
//go:build pdflatex

package render

import (
	"context"
	"strings"
	"testing"
)

func TestPDFLaTeXCompilesValidDocument(t *testing.T) {
	tex := `\documentclass{article}
\begin{document}
Hello \textbf{world}.
\end{document}
`
	res, err := PDFLaTeX(context.Background(), tex)
	if err != nil {
		t.Fatalf("PDFLaTeX: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK; log:\n%s", res.Log)
	}
	if !strings.HasPrefix(string(res.PDF), "%PDF") {
		t.Errorf("PDF does not start with %%PDF magic; got %d bytes", len(res.PDF))
	}
}

func TestPDFLaTeXReportsErrorOnBrokenDocument(t *testing.T) {
	tex := `\documentclass{article}
\begin{document}
\thiscommanddoesnotexist
\end{document}
`
	res, err := PDFLaTeX(context.Background(), tex)
	if err != nil {
		t.Fatalf("PDFLaTeX returned an environment error: %v", err)
	}
	if res.OK {
		t.Error("expected OK=false for a broken document")
	}
	if res.Log == "" {
		t.Error("expected a non-empty log on failure")
	}
}
```

- [ ] **Step 2: Run the gated test, confirm it fails to build**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test -tags pdflatex ./internal/render/...
```
Expected: build failure — `PDFLaTeX` / `CompileResult` undefined. (Without `-tags pdflatex` the file is excluded entirely, so always use the tag for this task's test runs.)

- [ ] **Step 3: Write the implementation**

Create `internal/render/compile.go`:

```go
package render

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CompileResult is the outcome of one LaTeX compilation.
type CompileResult struct {
	OK  bool   // a PDF was produced
	PDF []byte // compiled PDF bytes; nil when OK is false
	Log string // compiler output and .log contents, for diagnostics and repair
}

// CompileFunc compiles LaTeX source into a PDF. Implementations return OK=false
// with a populated Log on a LaTeX error (so the repair loop can react), and a
// non-nil error only for environment failures (e.g. the compiler is missing).
type CompileFunc func(ctx context.Context, tex string) (CompileResult, error)

// PDFLaTeX compiles tex by invoking pdflatex in a fresh temporary directory. A
// single pass suffices for single-page resumes (no bibliography or
// cross-references). The PDF bytes are read into the result before the temp
// directory is removed. A LaTeX error yields OK=false with the log and a nil
// error; a missing pdflatex binary yields a non-nil error.
func PDFLaTeX(ctx context.Context, tex string) (CompileResult, error) {
	dir, err := os.MkdirTemp("", "tailor-render-*")
	if err != nil {
		return CompileResult{}, fmt.Errorf("render: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	texPath := filepath.Join(dir, "resume.tex")
	if err := os.WriteFile(texPath, []byte(tex), 0o644); err != nil {
		return CompileResult{}, fmt.Errorf("render: write tex: %w", err)
	}

	cmd := exec.CommandContext(ctx, "pdflatex",
		"-interaction=nonstopmode", "-halt-on-error",
		"-output-directory", dir, texPath)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()

	var log strings.Builder
	log.Write(out)
	if logBytes, rerr := os.ReadFile(filepath.Join(dir, "resume.log")); rerr == nil {
		log.WriteString("\n")
		log.Write(logBytes)
	}

	// A missing binary is an environment error, distinct from a LaTeX error.
	if runErr != nil && errors.Is(runErr, exec.ErrNotFound) {
		return CompileResult{}, fmt.Errorf("render: pdflatex not found: %w", runErr)
	}
	// A non-zero exit (LaTeX error) is reported via OK below, not as a Go error.

	pdf, perr := os.ReadFile(filepath.Join(dir, "resume.pdf"))
	if perr != nil {
		return CompileResult{OK: false, Log: log.String()}, nil
	}
	return CompileResult{OK: true, PDF: pdf, Log: log.String()}, nil
}
```

- [ ] **Step 4: Run the gated test, confirm it passes**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test -tags pdflatex ./internal/render/...
```
Expected: PASS (the two `PDFLaTeX` tests plus the existing render tests). If `pdflatex` runs but the temp-dir write is blocked under the sandbox, retry with the sandbox disabled.

- [ ] **Step 5: Confirm the default (untagged) build still excludes the gated test**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./internal/render/...
```
Expected: PASS — the untagged run compiles `compile.go` (so `PDFLaTeX`/`CompileResult` exist) but does not run the gated `pdflatex` tests.

- [ ] **Step 6: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/render/compile.go internal/render/compile_pdflatex_test.go
git commit -m "feat(render): add pdflatex compiler with build-tag-gated compile test"
```

---

### Task 4: Single-shot LaTeX repair via the LLM

**Files:**
- Create: `internal/render/repair.go`
- Test: `internal/render/repair_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/render/repair_test.go`:

```go
package render

import (
	"context"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
)

func TestRepairTeXReturnsFixedSourceStrippingFence(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    "```latex\n\\documentclass{article}\\begin{document}ok\\end{document}\n```",
		StopReason: gantry.StopReasonEnd,
	})
	got, err := repairTeX(context.Background(), mock, "broken source", "! Undefined control sequence")
	if err != nil {
		t.Fatalf("repairTeX: %v", err)
	}
	if strings.Contains(got, "```") {
		t.Errorf("code fence not stripped: %q", got)
	}
	if !strings.Contains(got, `\documentclass`) {
		t.Errorf("repaired source missing document: %q", got)
	}
	// The error log and the broken source must both reach the model.
	reqs := mock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("llm requests = %d, want 1", len(reqs))
	}
	msg := reqs[0].Messages[0].Content
	if !strings.Contains(msg, "Undefined control sequence") || !strings.Contains(msg, "broken source") {
		t.Errorf("repair message missing context:\n%s", msg)
	}
}

func TestRepairTeXErrorsOnEmptyReply(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: "   ", StopReason: gantry.StopReasonEnd})
	if _, err := repairTeX(context.Background(), mock, "x", "err"); err == nil {
		t.Error("repairTeX: want error on empty repair, got nil")
	}
}

func TestTailLines(t *testing.T) {
	if got := tailLines("a\nb\nc\nd", 2); got != "c\nd" {
		t.Errorf("tailLines = %q, want %q", got, "c\nd")
	}
	if got := tailLines("only", 5); got != "only" {
		t.Errorf("tailLines = %q, want %q", got, "only")
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/render/...
```
Expected: build failure — `repairTeX` / `tailLines` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/render/repair.go`:

```go
package render

import (
	"context"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/fence"
)

// repairSystemPrompt instructs the model to fix a LaTeX document that failed to
// compile, returning the corrected complete source with no prose or fences.
const repairSystemPrompt = `You fix LaTeX documents that fail to compile with pdflatex.
You are given the current LaTeX source and the compiler error output.
Return the corrected, COMPLETE LaTeX document that will compile.

Rules:
- Return ONLY the LaTeX source: no explanation, no commentary, no markdown code fences.
- Preserve the document's content and structure; change only what is needed to fix the error (commonly an unescaped special character or a malformed command).
- Do not invent new content.`

// repairTeX asks the LLM to fix a LaTeX document given the compiler error log,
// returning the corrected source. The error log is truncated to its tail (where
// pdflatex reports the failure) to bound token use. An empty reply is an error.
func repairTeX(ctx context.Context, llm gantry.LLMClient, brokenTeX, errorLog string) (string, error) {
	resp, err := llm.Generate(ctx, gantry.LLMRequest{
		System:   repairSystemPrompt,
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: buildRepairMessage(brokenTeX, errorLog)}},
	})
	if err != nil {
		return "", fmt.Errorf("render: repair llm: %w", err)
	}
	fixed := fence.Strip(resp.Content)
	if strings.TrimSpace(fixed) == "" {
		return "", fmt.Errorf("render: repair produced an empty document")
	}
	return fixed, nil
}

// buildRepairMessage renders the repair request: the compiler error (tail) and
// the full current LaTeX source.
func buildRepairMessage(brokenTeX, errorLog string) string {
	var b strings.Builder
	b.WriteString("# pdflatex error\n")
	b.WriteString(tailLines(errorLog, 40))
	b.WriteString("\n\n# current LaTeX source\n")
	b.WriteString(brokenTeX)
	b.WriteString("\n")
	return b.String()
}

// tailLines returns the last n lines of s (or all of s if it has fewer than n).
func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/render/... && go vet ./internal/render/...
```
Expected: PASS (existing render tests + 3 new repair tests), vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/render/repair.go internal/render/repair_test.go
git commit -m "feat(render): add single-shot LLM LaTeX repair"
```

---

### Task 5: The compile-and-repair loop

**Files:**
- Modify: `internal/render/repair.go` (add `RepairResult`, `ErrCompileFailed`, `CompileWithRepair`)
- Test: `internal/render/repair_loop_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/render/repair_loop_test.go`:

```go
package render

import (
	"context"
	"errors"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
)

// scriptedCompiler returns a CompileFunc that records each source it is given.
// If envErr is non-nil it always returns that error. Otherwise it returns the
// next result from results, repeating the last entry once exhausted.
func scriptedCompiler(results []CompileResult, envErr error) (CompileFunc, *[]string) {
	var seen []string
	i := 0
	fn := func(ctx context.Context, tex string) (CompileResult, error) {
		seen = append(seen, tex)
		if envErr != nil {
			return CompileResult{}, envErr
		}
		r := results[i]
		if i < len(results)-1 {
			i++
		}
		return r, nil
	}
	return fn, &seen
}

func TestCompileWithRepairSucceedsFirstTry(t *testing.T) {
	compile, seen := scriptedCompiler([]CompileResult{{OK: true, PDF: []byte("%PDF-1")}}, nil)
	mock := eval.NewMockLLMClient() // must not be called
	res, err := CompileWithRepair(context.Background(), compile, mock, "src", 2)
	if err != nil {
		t.Fatalf("CompileWithRepair: %v", err)
	}
	if !res.OK || res.Attempts != 1 || res.TeX != "src" {
		t.Errorf("res = %+v", res)
	}
	if len(*seen) != 1 {
		t.Errorf("compiles = %d, want 1", len(*seen))
	}
	if len(mock.Requests()) != 0 {
		t.Errorf("llm called %d times, want 0", len(mock.Requests()))
	}
}

func TestCompileWithRepairFixesThenSucceeds(t *testing.T) {
	compile, seen := scriptedCompiler([]CompileResult{
		{OK: false, Log: "! error one"},
		{OK: true, PDF: []byte("%PDF-1")},
	}, nil)
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: "fixed source", StopReason: gantry.StopReasonEnd})
	res, err := CompileWithRepair(context.Background(), compile, mock, "broken", 2)
	if err != nil {
		t.Fatalf("CompileWithRepair: %v", err)
	}
	if !res.OK || res.Attempts != 2 || res.TeX != "fixed source" {
		t.Errorf("res = %+v", res)
	}
	if len(*seen) != 2 || (*seen)[1] != "fixed source" {
		t.Errorf("seen = %v", *seen)
	}
	if len(mock.Requests()) != 1 {
		t.Errorf("llm calls = %d, want 1", len(mock.Requests()))
	}
}

func TestCompileWithRepairExhaustsAndReturnsErrCompileFailed(t *testing.T) {
	compile, seen := scriptedCompiler([]CompileResult{{OK: false, Log: "! always broken"}}, nil)
	mock := eval.NewMockLLMClient(
		gantry.LLMResponse{Content: "try1", StopReason: gantry.StopReasonEnd},
		gantry.LLMResponse{Content: "try2", StopReason: gantry.StopReasonEnd},
	)
	res, err := CompileWithRepair(context.Background(), compile, mock, "broken", 2)
	if !errors.Is(err, ErrCompileFailed) {
		t.Fatalf("err = %v, want ErrCompileFailed", err)
	}
	if res.OK || res.Attempts != 3 {
		t.Errorf("res = %+v", res)
	}
	if res.TeX != "try2" {
		t.Errorf("final tex = %q, want last repair", res.TeX)
	}
	if len(*seen) != 3 {
		t.Errorf("compiles = %d, want 3", len(*seen))
	}
	if len(mock.Requests()) != 2 {
		t.Errorf("llm calls = %d, want 2", len(mock.Requests()))
	}
}

func TestCompileWithRepairAbortsOnCompilerEnvError(t *testing.T) {
	compile, _ := scriptedCompiler(nil, errors.New("pdflatex not found"))
	mock := eval.NewMockLLMClient() // must not be called
	_, err := CompileWithRepair(context.Background(), compile, mock, "src", 2)
	if err == nil || errors.Is(err, ErrCompileFailed) {
		t.Fatalf("err = %v, want a wrapped environment error (not ErrCompileFailed)", err)
	}
	if len(mock.Requests()) != 0 {
		t.Errorf("llm should not be called on an environment error")
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/render/...
```
Expected: build failure — `CompileWithRepair`, `RepairResult`, `ErrCompileFailed` undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/render/repair.go`. First add `"errors"` to its import block so it reads:

```go
import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/fence"
)
```

Then append these declarations to the end of the file:

```go
// ErrCompileFailed reports that the document still did not compile after the
// allowed number of repair attempts. The returned RepairResult carries the last
// attempted source and the final compiler log so the caller can emit them.
var ErrCompileFailed = errors.New("render: document did not compile after repairs")

// RepairResult is the outcome of CompileWithRepair.
type RepairResult struct {
	OK       bool   // the document compiled
	PDF      []byte // compiled PDF bytes; nil when OK is false
	TeX      string // the (possibly repaired) source of the final attempt
	Log      string // the final compiler log
	Attempts int    // number of compile attempts made (1 = no repair needed)
}

// CompileWithRepair compiles tex and, on a LaTeX error, asks the LLM to fix it
// and recompiles, up to maxRepairs times. It returns as soon as a compile
// succeeds. If every attempt fails it returns the last attempt's RepairResult
// together with ErrCompileFailed. A compiler environment error (non-nil error
// from compile) aborts immediately and is returned wrapped, without ever calling
// the LLM.
func CompileWithRepair(ctx context.Context, compile CompileFunc, llm gantry.LLMClient, tex string, maxRepairs int) (RepairResult, error) {
	current := tex
	for attempt := 0; ; attempt++ {
		res, err := compile(ctx, current)
		if err != nil {
			return RepairResult{}, fmt.Errorf("render: compile: %w", err)
		}
		if res.OK {
			return RepairResult{OK: true, PDF: res.PDF, TeX: current, Log: res.Log, Attempts: attempt + 1}, nil
		}
		if attempt >= maxRepairs {
			return RepairResult{OK: false, TeX: current, Log: res.Log, Attempts: attempt + 1}, ErrCompileFailed
		}
		fixed, rerr := repairTeX(ctx, llm, current, res.Log)
		if rerr != nil {
			return RepairResult{OK: false, TeX: current, Log: res.Log, Attempts: attempt + 1}, rerr
		}
		current = fixed
	}
}
```

- [ ] **Step 4: Run the tests, confirm they pass**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go test ./internal/render/... && go vet ./internal/render/...
```
Expected: PASS (all render tests including the 4 loop tests), vet clean.

- [ ] **Step 5: Final whole-tree check (untagged and tagged)**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift && go build ./... && go test ./... && go test -tags pdflatex ./internal/render/...
```
Expected: both PASS. The untagged `go test ./...` passes across all packages (`cmd/tailor`, `internal/embed`, `internal/fence`, `internal/generate`, `internal/jd`, `internal/render`, `internal/retrieve`, `internal/store`); the tagged run additionally exercises the real `pdflatex` compile.

- [ ] **Step 6: Commit**

```bash
cd /Users/fhassan-mac/Dev/tailor-swift
git add internal/render/repair.go internal/render/repair_loop_test.go
git commit -m "feat(render): add compile-and-repair loop over injectable compiler"
```

---

## Out of scope (deferred to later plans)

- **Artifact emission** (`out/<job-slug>-<date>/`): the orchestrator persists `RepairResult.PDF` as `resume.pdf`, `RepairResult.TeX` as `resume.tex`, and `RepairResult.Log` into `run.log`, and exits non-zero when `CompileWithRepair` returns `ErrCompileFailed` (design error-handling: "keep best `.tex`, emit it with the compile error in `run.log`, exit non-zero").
- **Evaluator, the generate↔evaluate loop, and the budget limiter:** orchestrator plan.
- **Wiring the real `PDFLaTeX` and a real repair model into the CLI:** the CLI/orchestrator passes `PDFLaTeX` as the `CompileFunc` and an `anthropic.New(...)` client as the `gantry.LLMClient`.

## Self-Review

- **Spec coverage:** Implements the compile-and-repair half of design pipeline step 6 ("fill chosen `.tex` template → `pdflatex`. On LaTeX error → repair sub-step (feed error back, fix escaping) up to a small cap"). `PDFLaTeX` (Task 3) is the `pdflatex` invocation; `CompileWithRepair` (Task 5) is the capped repair loop feeding the compiler error back to the LLM via `repairTeX` (Task 4). The design's testing-strategy item "real `pdflatex` compile test gated behind a build tag so LaTeX-less CI still passes" is met by `compile_pdflatex_test.go` (`//go:build pdflatex`); the loop logic is tested without `pdflatex` via the injected `scriptedCompiler` + `eval.NewMockLLMClient`, satisfying "every LLM stage tested with gantry's mock LLM — no API key in CI". The error-handling requirement "LaTeX compile fails after repair cap → keep best `.tex`, emit it with the compile error in `run.log`, exit non-zero" is supported: `CompileWithRepair` returns the last attempted `TeX` + final `Log` alongside the `ErrCompileFailed` sentinel, leaving emission/exit-code to the orchestrator (explicitly deferred). The DRY cleanup (Task 2) honors the generate plan's documented commitment to extract `stripCodeFence` at its third consumer.
- **Placeholders:** none — every step is full code, an exact `Edit`-style before/after, or an exact command with expected output. Task 2's refactor shows the precise import blocks (including the subtle point that `generate.go` must drop the now-unused `strings` import while `jd/requirements.go` keeps it) and the exact functions to delete.
- **Type consistency:** `CompileResult{OK bool; PDF []byte; Log string}` is defined once in `compile.go` (Task 3) and consumed identically by the gated test, `scriptedCompiler`, and `CompileWithRepair` (Tasks 3/5). `CompileFunc func(ctx context.Context, tex string) (CompileResult, error)` is the seam defined in `compile.go` and used by `scriptedCompiler` and `CompileWithRepair`. `RepairResult{OK, PDF, TeX, Log, Attempts}` and the `ErrCompileFailed` sentinel are defined in `repair.go` (Task 5) and asserted in `repair_loop_test.go`. `repairTeX(ctx, llm, brokenTeX, errorLog) (string, error)`, `buildRepairMessage(brokenTeX, errorLog) string`, and `tailLines(s, n) string` (Task 4) are used consistently by Task 5's loop and the Task 4 tests. `fence.Strip(string) string` (Task 2) is used by `jd`, `generate`, and `repairTeX`. Upstream `gantry.LLMClient/LLMRequest/Message/RoleUser/StopReasonEnd/LLMResponse` and `eval.NewMockLLMClient(...).Requests()` are used exactly as in the merged `generate` package. `repair.go` accumulates imports across Tasks 4 and 5 — Task 5 Step 3 explicitly restates the full import block (adding `errors`) to avoid an "imported and not used" / "undefined: errors" mismatch.
