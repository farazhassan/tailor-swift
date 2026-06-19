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
