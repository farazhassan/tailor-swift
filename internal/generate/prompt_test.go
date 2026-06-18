package generate

import (
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/jd"
)

func TestBuildUserMessageIncludesRequirementsCandidatesAndCritique(t *testing.T) {
	in := Input{
		Requirements: []jd.Requirement{
			{Text: "Go", MustHave: true},
			{Text: "Kafka", MustHave: false},
		},
		Candidates: []gantry.Document{
			{ID: "u1", Content: "Built a Go service"},
		},
		PriorCritique: "add more metrics",
	}
	msg := buildUserMessage(in)

	for _, want := range []string{"must-have", "Go", "nice-to-have", "Kafka", "u1", "Built a Go service", "add more metrics"} {
		if !strings.Contains(msg, want) {
			t.Errorf("user message missing %q\n---\n%s", want, msg)
		}
	}
}

func TestBuildUserMessageOmitsCritiqueWhenEmpty(t *testing.T) {
	in := Input{
		Candidates: []gantry.Document{{ID: "u1", Content: "x"}},
	}
	if strings.Contains(buildUserMessage(in), "critique") {
		t.Error("empty critique should not produce a critique section")
	}
}
