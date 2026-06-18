package generate

import (
	"context"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
	"github.com/farazhassan/tailor-swift/internal/jd"
)

func twoCandidates() []gantry.Document {
	return []gantry.Document{
		{ID: "u1", Content: "Built a Go billing service"},
		{ID: "u2", Content: "Scaled Kafka to 1M msgs/s"},
	}
}

func TestGenerateParsesBulletsAndSendsContext(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    `[{"unit_id":"u1","text":"Built a Go billing platform"},{"unit_id":"u2","text":"Scaled Kafka"}]`,
		StopReason: gantry.StopReasonEnd,
	})
	in := Input{
		Requirements: []jd.Requirement{{Text: "Go", MustHave: true}},
		Candidates:   twoCandidates(),
	}
	got, err := Generate(context.Background(), mock, in)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(got.Bullets) != 2 || got.Bullets[0].UnitID != "u1" || got.Bullets[0].Text != "Built a Go billing platform" {
		t.Errorf("bullets = %+v", got.Bullets)
	}
	// The candidate text must reach the model.
	reqs := mock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("llm requests = %d, want 1", len(reqs))
	}
	if !strings.Contains(reqs[0].Messages[0].Content, "Built a Go billing service") {
		t.Errorf("candidate text not sent to model:\n%s", reqs[0].Messages[0].Content)
	}
}

func TestGenerateStripsCodeFence(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    "```json\n[{\"unit_id\":\"u1\",\"text\":\"Go work\"}]\n```",
		StopReason: gantry.StopReasonEnd,
	})
	got, err := Generate(context.Background(), mock, Input{Candidates: twoCandidates()})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(got.Bullets) != 1 || got.Bullets[0].UnitID != "u1" {
		t.Errorf("bullets = %+v", got.Bullets)
	}
}
