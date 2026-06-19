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
