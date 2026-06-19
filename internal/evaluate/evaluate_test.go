package evaluate

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
	"github.com/farazhassan/tailor-swift/internal/jd"
)

func validInput() EvalInput {
	return EvalInput{
		Requirements: []jd.Requirement{{Text: "Go", MustHave: true}},
		Candidates:   []gantry.Document{{ID: "u1", Content: "Built a Go billing service"}},
		Resume:       "Ada Lovelace — Built a Go billing platform",
		Compiled:     true,
	}
}

// allHighJSON: every dimension 0.9 → composite 0.9 (weights sum to 1.0), truthful.
const allHighJSON = `{"scores":{"jd_coverage":0.9,"relevance":0.9,"evidence_quality":0.9,"truthfulness":0.9,"format":0.9},"truthful":true,"critique":{"jd_coverage":"good"},"summary":"ship it"}`

func TestEvaluateParsesAndComputesComposite(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: allHighJSON, StopReason: gantry.StopReasonEnd})
	ev, err := Evaluate(context.Background(), mock, validInput())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if math.Abs(ev.Composite-0.9) > 1e-9 {
		t.Errorf("composite = %g, want 0.9", ev.Composite)
	}
	if !ev.Pass {
		t.Errorf("expected Pass=true; got %+v", ev)
	}
	if ev.Summary != "ship it" {
		t.Errorf("summary = %q, want %q", ev.Summary, "ship it")
	}
	// The candidate evidence and the resume under review must reach the model.
	reqs := mock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("llm requests = %d, want 1", len(reqs))
	}
	msg := reqs[0].Messages[0].Content
	if !strings.Contains(msg, "Built a Go billing service") || !strings.Contains(msg, "Built a Go billing platform") {
		t.Errorf("context not sent to model:\n%s", msg)
	}
}

func TestEvaluateComputesWeightedComposite(t *testing.T) {
	// 0.9*0.30 + 0.8*0.20 + 0.7*0.20 + 1.0*0.15 + 0.6*0.15 = 0.81
	js := `{"scores":{"jd_coverage":0.9,"relevance":0.8,"evidence_quality":0.7,"truthfulness":1.0,"format":0.6},"truthful":true,"critique":{},"summary":""}`
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: js, StopReason: gantry.StopReasonEnd})
	ev, err := Evaluate(context.Background(), mock, validInput())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if math.Abs(ev.Composite-0.81) > 1e-9 {
		t.Errorf("composite = %g, want 0.81", ev.Composite)
	}
	if ev.Pass {
		t.Errorf("composite 0.81 < 0.85 must not pass; got %+v", ev)
	}
}

func TestEvaluateStripsCodeFence(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: "```json\n" + allHighJSON + "\n```", StopReason: gantry.StopReasonEnd})
	ev, err := Evaluate(context.Background(), mock, validInput())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !ev.Pass {
		t.Errorf("expected Pass=true; got %+v", ev)
	}
}

func TestEvaluateTruthfulnessHardGate(t *testing.T) {
	// Every score 1.0 (composite 1.0 >= threshold) but truthful=false must fail.
	js := `{"scores":{"jd_coverage":1.0,"relevance":1.0,"evidence_quality":1.0,"truthfulness":1.0,"format":1.0},"truthful":false,"critique":{},"summary":"fabricated bullet"}`
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: js, StopReason: gantry.StopReasonEnd})
	ev, err := Evaluate(context.Background(), mock, validInput())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if math.Abs(ev.Composite-1.0) > 1e-9 {
		t.Errorf("composite = %g, want 1.0", ev.Composite)
	}
	if ev.Pass {
		t.Error("truthfulness gate failed must force Pass=false regardless of composite")
	}
}

func TestEvaluateErrorsOnMissingDimension(t *testing.T) {
	js := `{"scores":{"jd_coverage":0.9,"relevance":0.9,"evidence_quality":0.9,"truthfulness":0.9},"truthful":true}`
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: js, StopReason: gantry.StopReasonEnd})
	if _, err := Evaluate(context.Background(), mock, validInput()); err == nil {
		t.Error("want error when a dimension score is missing, got nil")
	}
}

func TestEvaluateErrorsOnScoreOutOfRange(t *testing.T) {
	js := `{"scores":{"jd_coverage":1.5,"relevance":0.9,"evidence_quality":0.9,"truthfulness":0.9,"format":0.9},"truthful":true}`
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: js, StopReason: gantry.StopReasonEnd})
	if _, err := Evaluate(context.Background(), mock, validInput()); err == nil {
		t.Error("want error when a score is out of [0,1], got nil")
	}
}

func TestEvaluateErrorsOnBadJSON(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: "not json", StopReason: gantry.StopReasonEnd})
	if _, err := Evaluate(context.Background(), mock, validInput()); err == nil {
		t.Error("want error on non-JSON content, got nil")
	}
}

func TestEvaluateErrorsOnEmptyResume(t *testing.T) {
	mock := eval.NewMockLLMClient() // must not be called
	in := validInput()
	in.Resume = "   "
	if _, err := Evaluate(context.Background(), mock, in); err == nil {
		t.Error("want error on empty resume, got nil")
	}
	if len(mock.Requests()) != 0 {
		t.Error("llm should not be called when the resume is empty")
	}
}
