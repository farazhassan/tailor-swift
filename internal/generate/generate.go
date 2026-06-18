package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/farazhassan/gantry"
)

// Generate asks the LLM to select and rephrase candidate achievements for the
// job described in in, returning the structured selection. Every returned bullet
// is validated to reference a real candidate unit ID (the truthfulness
// contract); an unknown ID is an error. MaxTokens is left at 0 so the provider
// adapter supplies its default.
func Generate(ctx context.Context, llm gantry.LLMClient, in Input) (*Result, error) {
	if len(in.Candidates) == 0 {
		return nil, fmt.Errorf("generate: no candidate achievements")
	}
	resp, err := llm.Generate(ctx, gantry.LLMRequest{
		System:   systemPrompt,
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: buildUserMessage(in)}},
	})
	if err != nil {
		return nil, fmt.Errorf("generate: llm: %w", err)
	}
	var bullets []Bullet
	if err := json.Unmarshal([]byte(stripCodeFence(resp.Content)), &bullets); err != nil {
		return nil, fmt.Errorf("generate: parse bullets json: %w", err)
	}
	valid := make(map[string]bool, len(in.Candidates))
	for _, d := range in.Candidates {
		valid[d.ID] = true
	}
	for _, bl := range bullets {
		if !valid[bl.UnitID] {
			return nil, fmt.Errorf("generate: bullet references unknown unit id %q", bl.UnitID)
		}
	}
	return &Result{Bullets: bullets}, nil
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
