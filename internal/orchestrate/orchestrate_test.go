package orchestrate

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/limiter"
	"github.com/farazhassan/gantry/eval"
	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/render"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// --- fixtures -------------------------------------------------------------

const testTemplate = `\documentclass{article}\begin{document}{{.Name}}{{range .Roles}}{{range .Bullets}} {{.Text}}{{end}}{{end}}\end{document}`

func testStore() *store.Store {
	return &store.Store{
		Profile: store.Profile{Name: "Ada Lovelace", Contact: store.Contact{Email: "ada@example.com"}},
		Roles: []store.Role{{
			Company: "Acme", Title: "Engineer", Start: "2020", End: "2022",
			Projects: []store.Project{{
				Name: "Billing",
				Achievements: []store.Achievement{
					{ID: "u1", Text: "Built a Go billing service"},
					{ID: "u2", Text: "Scaled Kafka"},
				},
			}},
		}},
		Skills: []store.Skill{{Raw: "Go (expert)"}},
	}
}

func testCandidates() []gantry.Document {
	return []gantry.Document{
		{ID: "u1", Content: "Built a Go billing service"},
		{ID: "u2", Content: "Scaled Kafka"},
	}
}

func baseConfig() Config {
	return Config{
		Template:      testTemplate,
		Store:         testStore(),
		Requirements:  []jd.Requirement{{Text: "Go", MustHave: true}},
		Candidates:    testCandidates(),
		MaxIterations: 3,
		MaxRepairs:    1,
	}
}

func okCompiler() render.CompileFunc {
	return func(ctx context.Context, tex string) (render.CompileResult, error) {
		return render.CompileResult{OK: true, PDF: []byte("%PDF-1"), Log: "ok"}, nil
	}
}

func bullets(text string) gantry.LLMResponse {
	return gantry.LLMResponse{Content: `[{"unit_id":"u1","text":"` + text + `"}]`, StopReason: gantry.StopReasonEnd}
}

// evalResp builds a rubric reply with every dimension set to score (so the
// composite equals score, since the weights sum to 1.0) and the given truthful flag.
func evalResp(score float64, truthful bool) gantry.LLMResponse {
	js := fmt.Sprintf(`{"scores":{"jd_coverage":%[1]g,"relevance":%[1]g,"evidence_quality":%[1]g,"truthfulness":%[1]g,"format":%[1]g},"truthful":%[2]t,"critique":{},"summary":""}`, score, truthful)
	return gantry.LLMResponse{Content: js, StopReason: gantry.StopReasonEnd}
}

func withUsage(r gantry.LLMResponse, tokens int) gantry.LLMResponse {
	r.Usage = gantry.Usage{InputTokens: tokens}
	return r
}

const passEvalJSON = `{"scores":{"jd_coverage":0.9,"relevance":0.9,"evidence_quality":0.9,"truthfulness":0.9,"format":0.9},"truthful":true,"critique":{},"summary":"ship it"}`

func noLLM() gantry.LLMClient { return eval.NewMockLLMClient() }

// --- tests ----------------------------------------------------------------

func TestRunPassesFirstIteration(t *testing.T) {
	deps := Deps{
		GenLLM:    eval.NewMockLLMClient(bullets("Built a Go billing platform")),
		EvalLLM:   eval.NewMockLLMClient(gantry.LLMResponse{Content: passEvalJSON, StopReason: gantry.StopReasonEnd}),
		RepairLLM: noLLM(),
		Compile:   okCompiler(),
	}
	res, err := Run(context.Background(), baseConfig(), deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopPassed || !res.Passed {
		t.Errorf("StopReason=%q Passed=%v, want passed/true", res.StopReason, res.Passed)
	}
	if len(res.Iterations) != 1 {
		t.Fatalf("iterations = %d, want 1", len(res.Iterations))
	}
	if res.Best == nil || !res.Best.Compiled || len(res.Best.PDF) == 0 {
		t.Errorf("best = %+v", res.Best)
	}
}

func TestRunFailsThenPassesFeedingCritique(t *testing.T) {
	genLLM := eval.NewMockLLMClient(bullets("v1"), bullets("v2"))
	failJSON := `{"scores":{"jd_coverage":0.5,"relevance":0.5,"evidence_quality":0.5,"truthfulness":0.5,"format":0.5},"truthful":true,"critique":{"jd_coverage":"add Go depth"},"summary":"needs work"}`
	evalLLM := eval.NewMockLLMClient(
		gantry.LLMResponse{Content: failJSON, StopReason: gantry.StopReasonEnd},
		gantry.LLMResponse{Content: passEvalJSON, StopReason: gantry.StopReasonEnd},
	)
	res, err := Run(context.Background(), baseConfig(), Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: noLLM(), Compile: okCompiler()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopPassed || !res.Passed {
		t.Errorf("got StopReason=%q Passed=%v", res.StopReason, res.Passed)
	}
	if len(res.Iterations) != 2 {
		t.Fatalf("iterations = %d, want 2", len(res.Iterations))
	}
	greqs := genLLM.Requests()
	if len(greqs) != 2 {
		t.Fatalf("gen requests = %d, want 2", len(greqs))
	}
	msg := greqs[1].Messages[0].Content
	if !strings.Contains(msg, "needs work") || !strings.Contains(msg, "add Go depth") {
		t.Errorf("2nd generate missing prior critique:\n%s", msg)
	}
}

func TestRunMaxIterationsSelectsHighestComposite(t *testing.T) {
	genLLM := eval.NewMockLLMClient(bullets("a"), bullets("b"), bullets("c"))
	// composites 0.5, 0.7, 0.6 (all truthful, none pass) → best is index 1.
	evalLLM := eval.NewMockLLMClient(evalResp(0.5, true), evalResp(0.7, true), evalResp(0.6, true))
	res, err := Run(context.Background(), baseConfig(), Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: noLLM(), Compile: okCompiler()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopMaxIterations || res.Passed {
		t.Errorf("got StopReason=%q Passed=%v", res.StopReason, res.Passed)
	}
	if len(res.Iterations) != 3 {
		t.Fatalf("iterations = %d, want 3", len(res.Iterations))
	}
	if res.Best == nil || res.Best.Index != 1 {
		t.Errorf("best index = %v, want 1", res.Best)
	}
}

func TestRunPrefersTruthfulIterationForBest(t *testing.T) {
	genLLM := eval.NewMockLLMClient(bullets("a"), bullets("b"))
	// index 0: composite 0.9 but NOT truthful (Pass=false); index 1: 0.6 truthful.
	evalLLM := eval.NewMockLLMClient(evalResp(0.9, false), evalResp(0.6, true))
	cfg := baseConfig()
	cfg.MaxIterations = 2
	res, err := Run(context.Background(), cfg, Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: noLLM(), Compile: okCompiler()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Best == nil || res.Best.Index != 1 {
		t.Errorf("best index = %v, want 1 (truthful preferred over higher untruthful composite)", res.Best)
	}
}

func TestRunHandlesCompileFailureNonFatally(t *testing.T) {
	genLLM := eval.NewMockLLMClient(bullets("a"))
	evalLLM := eval.NewMockLLMClient(evalResp(0.6, true))
	repairLLM := eval.NewMockLLMClient(gantry.LLMResponse{Content: "still broken", StopReason: gantry.StopReasonEnd})
	failCompile := func(ctx context.Context, tex string) (render.CompileResult, error) {
		return render.CompileResult{OK: false, Log: "! broken"}, nil
	}
	cfg := baseConfig()
	cfg.MaxIterations = 1
	cfg.MaxRepairs = 1
	res, err := Run(context.Background(), cfg, Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: repairLLM, Compile: failCompile})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Iterations) != 1 {
		t.Fatalf("iterations = %d, want 1", len(res.Iterations))
	}
	if res.Iterations[0].Compiled {
		t.Error("iteration should be marked not compiled")
	}
	if res.Best == nil {
		t.Fatal("best should still be selected from a non-compiling iteration")
	}
	ereqs := evalLLM.Requests()
	if len(ereqs) != 1 || !strings.Contains(ereqs[0].Messages[0].Content, "FAILED to compile") {
		t.Errorf("evaluator should be told the document failed to compile")
	}
}

func TestRunStopsOnBudgetAndEmitsBestSoFar(t *testing.T) {
	genLLM := eval.NewMockLLMClient(withUsage(bullets("a"), 60), withUsage(bullets("b"), 60))
	evalLLM := eval.NewMockLLMClient(withUsage(evalResp(0.6, true), 60), withUsage(evalResp(0.6, true), 60))
	cfg := baseConfig()
	cfg.MaxIterations = 3
	cfg.Budget = limiter.Limits{MaxTokens: 100}
	res, err := Run(context.Background(), cfg, Deps{GenLLM: genLLM, EvalLLM: evalLLM, RepairLLM: noLLM(), Compile: okCompiler()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopBudget {
		t.Errorf("StopReason = %q, want budget", res.StopReason)
	}
	// Iter 0: gen records 60, eval records 60 (total 120). Iter 1's generate is
	// refused (120 > 100), so only one full iteration was recorded.
	if len(res.Iterations) != 1 {
		t.Errorf("iterations = %d, want 1", len(res.Iterations))
	}
	if res.Best == nil || res.Best.Index != 0 {
		t.Errorf("best = %+v, want index 0", res.Best)
	}
}

func TestRunReturnsErrorOnFatalGenerateFailure(t *testing.T) {
	genLLM := eval.NewMockLLMClient(gantry.LLMResponse{Content: "not json", StopReason: gantry.StopReasonEnd})
	_, err := Run(context.Background(), baseConfig(), Deps{GenLLM: genLLM, EvalLLM: noLLM(), RepairLLM: noLLM(), Compile: okCompiler()})
	if err == nil {
		t.Error("want a fatal error when generate returns unparseable content")
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	cfg := baseConfig()
	cfg.MaxIterations = 0
	_, err := Run(context.Background(), cfg, Deps{GenLLM: noLLM(), EvalLLM: noLLM(), RepairLLM: noLLM(), Compile: okCompiler()})
	if err == nil {
		t.Error("want error when MaxIterations < 1")
	}
}
