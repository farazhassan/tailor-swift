package evaluate

import (
	"math"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/jd"
)

func TestDimensionWeightsSumToOne(t *testing.T) {
	var sum float64
	for _, d := range Dimensions {
		sum += d.Weight
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("dimension weights sum = %g, want 1.0", sum)
	}
}

func TestBuildUserMessageIncludesAllContext(t *testing.T) {
	in := EvalInput{
		Requirements: []jd.Requirement{{Text: "Go expertise", MustHave: true}, {Text: "Kafka", MustHave: false}},
		Candidates:   []gantry.Document{{ID: "u1", Content: "Built a Go billing service"}},
		Resume:       "Ada Lovelace — Built a Go billing platform",
		Compiled:     true,
	}
	msg := buildUserMessage(in)
	for _, want := range []string{
		"Go expertise", "must-have", "Kafka", "nice-to-have",
		"u1", "Built a Go billing service", "Built a Go billing platform", "compiled",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestBuildUserMessageMarksCompileFailure(t *testing.T) {
	msg := buildUserMessage(EvalInput{Resume: "x", Compiled: false})
	if !strings.Contains(msg, "FAILED to compile") {
		t.Errorf("message should note compile failure:\n%s", msg)
	}
}
