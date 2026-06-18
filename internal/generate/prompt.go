package generate

import (
	"fmt"
	"strings"
)

// systemPrompt instructs the model to select and rephrase candidate achievements
// without fabricating facts or unit IDs, and to return a bare JSON array.
const systemPrompt = `You tailor a resume to a job description.
From the provided candidate achievements, select those most relevant to the job's
requirements and rephrase each to emphasize the match.

Rules:
- Use ONLY the provided unit IDs. Never invent achievements or unit IDs.
- Each selected bullet must set "unit_id" to the candidate it was derived from.
- Rephrase for impact and relevance, but never state facts absent from the
  candidate's original text.
- Prefer covering must-have requirements.
- Return ONLY a JSON array, with no prose and no markdown code fences. Each
  element is an object: {"unit_id": string, "text": string}.`

// buildUserMessage renders the per-iteration context: the job requirements, the
// candidate achievements (each with its unit ID), and any prior critique.
func buildUserMessage(in Input) string {
	var b strings.Builder
	b.WriteString("# Job requirements\n")
	for _, r := range in.Requirements {
		tag := "nice-to-have"
		if r.MustHave {
			tag = "must-have"
		}
		fmt.Fprintf(&b, "- (%s) %s\n", tag, r.Text)
	}
	b.WriteString("\n# Candidate achievements\n")
	for _, d := range in.Candidates {
		fmt.Fprintf(&b, "- [%s] %s\n", d.ID, d.Content)
	}
	if strings.TrimSpace(in.PriorCritique) != "" {
		b.WriteString("\n# Reviewer critique of the previous attempt\n")
		b.WriteString(in.PriorCritique)
		b.WriteString("\n")
	}
	return b.String()
}
