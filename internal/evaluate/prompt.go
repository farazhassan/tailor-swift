package evaluate

import (
	"fmt"
	"strings"
)

// systemPrompt defines the rubric and the exact JSON the evaluator must return.
const systemPrompt = `You are a hiring reviewer scoring a tailored resume against a job description.

Score each rubric dimension from 0.0 to 1.0:
- jd_coverage: are the job's key requirements (especially must-haves) represented by real content?
- relevance: is the included content relevant and free of filler?
- evidence_quality: are bullets quantified and impact-oriented rather than vague?
- truthfulness: does every claim trace to the provided candidate evidence (no fabrication)?
- format: section completeness, sensible length, and clean compilation.

Also set "truthful" to false if ANY claim in the resume is not supported by the
provided candidate evidence. This is a hard gate, independent of the numeric scores.

Return ONLY a JSON object, with no prose and no markdown code fences, of exactly this shape:
{
  "scores": {"jd_coverage": 0.0, "relevance": 0.0, "evidence_quality": 0.0, "truthfulness": 0.0, "format": 0.0},
  "truthful": true,
  "critique": {"jd_coverage": "...", "relevance": "...", "evidence_quality": "...", "truthfulness": "...", "format": "..."},
  "summary": "concise, actionable feedback for the next revision"
}`

// buildUserMessage renders the per-revision context: the job requirements, the
// ground-truth candidate achievements, the resume under review, and whether it
// compiled (which feeds the format dimension).
func buildUserMessage(in EvalInput) string {
	var b strings.Builder
	b.WriteString("# Job requirements\n")
	for _, r := range in.Requirements {
		tag := "nice-to-have"
		if r.MustHave {
			tag = "must-have"
		}
		fmt.Fprintf(&b, "- (%s) %s\n", tag, r.Text)
	}
	b.WriteString("\n# Candidate achievements (ground-truth evidence)\n")
	for _, d := range in.Candidates {
		fmt.Fprintf(&b, "- [%s] %s\n", d.ID, d.Content)
	}
	b.WriteString("\n# Resume under review\n")
	b.WriteString(in.Resume)
	b.WriteString("\n\n# Compilation\n")
	if in.Compiled {
		b.WriteString("The resume compiled to PDF without errors.\n")
	} else {
		b.WriteString("The resume FAILED to compile to PDF.\n")
	}
	return b.String()
}
