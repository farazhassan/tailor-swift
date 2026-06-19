package render

import (
	"context"
	"errors"
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
