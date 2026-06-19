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
