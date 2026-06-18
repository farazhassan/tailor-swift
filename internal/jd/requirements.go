package jd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
)

const requirementsSystemPrompt = `You extract structured hiring requirements from a job description.
Return ONLY a JSON array, with no prose and no markdown code fences.
Each element is an object: {"text": string, "must_have": boolean}.
"text" is a single concrete requirement or skill, stated concisely.
"must_have" is true for required/mandatory items and false for nice-to-haves.`

// ExtractRequirements asks the LLM to split a job description into discrete
// requirements. The model is instructed to return a JSON array; a leading
// markdown code fence (```json ... ```) is tolerated.
func ExtractRequirements(ctx context.Context, llm gantry.LLMClient, jobText string) ([]Requirement, error) {
	if strings.TrimSpace(jobText) == "" {
		return nil, fmt.Errorf("jd: empty job text")
	}
	resp, err := llm.Generate(ctx, gantry.LLMRequest{
		System:   requirementsSystemPrompt,
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: jobText}},
	})
	if err != nil {
		return nil, fmt.Errorf("jd: extract requirements: %w", err)
	}
	var reqs []Requirement
	if err := json.Unmarshal([]byte(stripCodeFence(resp.Content)), &reqs); err != nil {
		return nil, fmt.Errorf("jd: parse requirements json: %w", err)
	}
	return reqs, nil
}

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
