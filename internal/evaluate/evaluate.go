package evaluate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/fence"
)

// Evaluate scores one resume revision. It sends the rubric (system prompt) and
// the revision context (user message) to the LLM, parses the structured JSON
// verdict, validates that every rubric dimension has an in-range score, computes
// the weighted composite, and sets Pass = composite >= Threshold AND the
// truthfulness hard-gate passed. MaxTokens is left at 0 so the provider adapter
// supplies its default.
func Evaluate(ctx context.Context, llm gantry.LLMClient, in EvalInput) (*Evaluation, error) {
	if strings.TrimSpace(in.Resume) == "" {
		return nil, fmt.Errorf("evaluate: empty resume under review")
	}
	resp, err := llm.Generate(ctx, gantry.LLMRequest{
		System:   systemPrompt,
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: buildUserMessage(in)}},
	})
	if err != nil {
		return nil, fmt.Errorf("evaluate: llm: %w", err)
	}
	var ev Evaluation
	if err := json.Unmarshal([]byte(fence.Strip(resp.Content)), &ev); err != nil {
		return nil, fmt.Errorf("evaluate: parse evaluation json: %w", err)
	}
	var composite float64
	for _, d := range Dimensions {
		s, ok := ev.Scores[d.Key]
		if !ok {
			return nil, fmt.Errorf("evaluate: missing score for dimension %q", d.Key)
		}
		if s < 0 || s > 1 {
			return nil, fmt.Errorf("evaluate: score for %q out of range [0,1]: %g", d.Key, s)
		}
		composite += d.Weight * s
	}
	ev.Composite = composite
	ev.Pass = composite >= Threshold && ev.Truthful
	return &ev, nil
}
