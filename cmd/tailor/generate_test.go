package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
	"github.com/farazhassan/tailor-swift/internal/evaluate"
	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/orchestrate"
	"github.com/farazhassan/tailor-swift/internal/pipeline"
	"github.com/farazhassan/tailor-swift/internal/render"
	"github.com/farazhassan/tailor-swift/internal/store"
)

func TestSlugify(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://acme.com/jobs/senior-go-engineer", "senior-go-engineer"},
		{"https://acme.com/jobs/123?ref=x", "123"},
		{"https://acme.com/", "job"},
		{"https://acme.com", "acme-com"},
		{"https://acme.com/Jobs/Staff_Engineer!", "staff-engineer"},
		{"", "job"},
	}
	for _, c := range cases {
		if got := slugify(c.url); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestResolveProvider(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"anthropic", "anthropic", false},
		{"openrouter", "openrouter", false},
		{"bogus", "", true},
	} {
		got, err := resolveProvider(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("resolveProvider(%q): want error, got nil", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveProvider(%q): unexpected error %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("resolveProvider(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDefaultModel(t *testing.T) {
	if got := defaultModel("anthropic"); got != "claude-sonnet-4-6" {
		t.Errorf("defaultModel(anthropic) = %q, want claude-sonnet-4-6", got)
	}
	if got := defaultModel("openrouter"); got != "anthropic/claude-sonnet-4-6" {
		t.Errorf("defaultModel(openrouter) = %q, want anthropic/claude-sonnet-4-6", got)
	}
}

// acqResult builds a minimal pipeline.Result for artifact tests.
func acqResult() *pipeline.Result {
	return &pipeline.Result{
		Posting:      &jd.Posting{URL: "https://acme.com/jobs/go", FetchedAt: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)},
		Requirements: []jd.Requirement{{Text: "Go", MustHave: true}},
		Candidates:   []gantry.Document{{ID: "u1", Content: "Built a Go service"}},
	}
}

func TestWriteArtifactsHappy(t *testing.T) {
	dir := t.TempDir()
	run := &orchestrate.Result{
		StopReason: orchestrate.StopPassed,
		Passed:     true,
		Iterations: []orchestrate.Iteration{{Index: 0, Compiled: true, Evaluation: &evaluate.Evaluation{Pass: true, Composite: 0.9}}},
	}
	run.Best = &run.Iterations[0]
	run.Best.TeX = `\documentclass{article}`
	run.Best.PDF = []byte("%PDF-1")
	run.Best.Evaluation = &evaluate.Evaluation{
		Pass: true, Composite: 0.9, Truthful: true,
		Scores:   map[string]float64{"jd_coverage": 0.9},
		Critique: map[string]string{}, Summary: "ship it",
	}

	var errb strings.Builder
	if err := writeArtifacts(dir, run, acqResult(), &errb); err != nil {
		t.Fatalf("writeArtifacts: %v", err)
	}
	for _, name := range []string{"resume.tex", "resume.pdf", "critique.json", "run.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing artifact %s: %v", name, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "critique.json"))
	var co critiqueOut
	if err := json.Unmarshal(data, &co); err != nil {
		t.Fatalf("critique.json invalid: %v", err)
	}
	if !co.Pass || co.Composite != 0.9 || co.StopReason != orchestrate.StopPassed {
		t.Errorf("critiqueOut = %+v", co)
	}
}

func TestWriteArtifactsSkipsPDFWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	run := &orchestrate.Result{
		StopReason: orchestrate.StopMaxIterations,
		Iterations: []orchestrate.Iteration{{Index: 0, Compiled: false, Evaluation: &evaluate.Evaluation{}}},
	}
	run.Best = &run.Iterations[0]
	run.Best.TeX = `\documentclass{article}`
	run.Best.PDF = nil

	var errb strings.Builder
	if err := writeArtifacts(dir, run, acqResult(), &errb); err != nil {
		t.Fatalf("writeArtifacts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "resume.pdf")); !os.IsNotExist(err) {
		t.Errorf("resume.pdf should be skipped, stat err = %v", err)
	}
	if !strings.Contains(errb.String(), "did not compile") {
		t.Errorf("expected stderr warning, got %q", errb.String())
	}
	log, _ := os.ReadFile(filepath.Join(dir, "run.log"))
	if !strings.Contains(string(log), "resume.pdf skipped") {
		t.Errorf("run.log missing skip note:\n%s", log)
	}
}

// --- fixtures for genRun ---------------------------------------------------

func vecFor(text string) []float32 {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "kafka"):
		return []float32{0, 1, 0}
	case strings.Contains(t, "rust"):
		return []float32{0, 0, 1}
	case strings.Contains(t, "go"):
		return []float32{1, 0, 0}
	default:
		return []float32{1, 1, 1}
	}
}

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, tx := range texts {
		out[i] = vecFor(tx)
	}
	return out, nil
}

func writeFileT(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

const cliContent = `# Ada Lovelace

## Contact
Email: ada@example.com

## Acme Corp

### Billing
- Built a Go billing service
- Scaled Kafka pipelines
`

const testTemplate = `\documentclass{article}\begin{document}{{.Name}}{{range .Roles}}{{range .Bullets}} {{.Text}}{{end}}{{end}}\end{document}`

const passEvalJSON = `{"scores":{"jd_coverage":0.9,"relevance":0.9,"evidence_quality":0.9,"truthfulness":0.9,"format":0.9},"truthful":true,"critique":{},"summary":"ship it"}`

const failEvalJSON = `{"scores":{"jd_coverage":0.5,"relevance":0.5,"evidence_quality":0.5,"truthfulness":0.5,"format":0.5},"truthful":true,"critique":{"jd_coverage":"more depth"},"summary":"needs work"}`

func resp(s string) gantry.LLMResponse {
	return gantry.LLMResponse{Content: s, StopReason: gantry.StopReasonEnd}
}

func okCompiler() render.CompileFunc {
	return func(ctx context.Context, tex string) (render.CompileResult, error) {
		return render.CompileResult{OK: true, PDF: []byte("%PDF-1"), Log: "ok"}, nil
	}
}

func failCompiler() render.CompileFunc {
	return func(ctx context.Context, tex string) (render.CompileResult, error) {
		return render.CompileResult{OK: false, Log: "! Undefined control sequence"}, nil
	}
}

// cliBase writes a content file + JD file under a temp dir and returns a
// genConfig (MaxIterations=1) plus the temp dir.
func cliBase(t *testing.T) (genConfig, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := genConfig{
		ContentPath:   writeFileT(t, dir, "content.md", cliContent),
		JDURL:         "https://acme.com/jobs/senior-go-engineer",
		JDFile:        writeFileT(t, dir, "jd.txt", "We need a backend engineer."),
		OutDir:        filepath.Join(dir, "out"),
		EmbedModel:    "voyage-3",
		TopK:          8,
		MinScore:      0.5,
		MaxIterations: 1,
		JDCacheDir:    filepath.Join(dir, "jdcache"),
		Today:         time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC),
	}
	return cfg, dir
}

// cliDeps builds genDeps with per-role scripted mocks and the given compiler.
// repair may be nil to mean "must not be called".
func cliDeps(extractJSON, genJSON, evalJSON string, repair gantry.LLMClient, compile render.CompileFunc) genDeps {
	if repair == nil {
		repair = eval.NewMockLLMClient()
	}
	return genDeps{
		ExtractLLM: eval.NewMockLLMClient(resp(extractJSON)),
		GenLLM:     eval.NewMockLLMClient(resp(genJSON)),
		EvalLLM:    eval.NewMockLLMClient(resp(evalJSON)),
		RepairLLM:  repair,
		Embedder:   fakeEmbedder{},
		Compile:    compile,
		Template:   testTemplate,
	}
}

// goBullets returns a generation response selecting the Go achievement by its
// derived id (the id orchestrate validates bullets against).
func goBullets() string {
	id := store.DeriveID("Built a Go billing service")
	return `[{"unit_id":"` + id + `","text":"Built billing in Go"}]`
}

const reqsGoKafka = `[{"text":"Go","must_have":true},{"text":"Kafka","must_have":false}]`

// --- genRun tests ----------------------------------------------------------

func TestGenRunHappyPath(t *testing.T) {
	cfg, _ := cliBase(t)
	deps := cliDeps(reqsGoKafka, goBullets(), passEvalJSON, nil, okCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, errb.String())
	}
	dir := filepath.Join(cfg.OutDir, "senior-go-engineer-2026-06-19")
	for _, name := range []string{"resume.tex", "resume.pdf", "critique.json", "run.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "critique.json"))
	if !strings.Contains(string(data), `"pass": true`) {
		t.Errorf("critique.json not passing:\n%s", data)
	}
}

func TestGenRunNotPassedExits3(t *testing.T) {
	cfg, _ := cliBase(t)
	deps := cliDeps(reqsGoKafka, goBullets(), failEvalJSON, nil, okCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 3 {
		t.Fatalf("exit = %d, want 3; stderr=%s", code, errb.String())
	}
	dir := filepath.Join(cfg.OutDir, "senior-go-engineer-2026-06-19")
	data, _ := os.ReadFile(filepath.Join(dir, "critique.json"))
	if !strings.Contains(string(data), `"pass": false`) {
		t.Errorf("critique.json should be failing:\n%s", data)
	}
}

func TestGenRunWarnsOnGaps(t *testing.T) {
	cfg, _ := cliBase(t)
	reqs := `[{"text":"Go","must_have":true},{"text":"Rust","must_have":true}]`
	deps := cliDeps(reqs, goBullets(), passEvalJSON, nil, okCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "Rust") || !strings.Contains(errb.String(), "unmatched") {
		t.Errorf("expected gap warning mentioning Rust, got %q", errb.String())
	}
}

func TestGenRunSkipsPDFOnCompileFailure(t *testing.T) {
	cfg, _ := cliBase(t)
	// maxRepairs=2 => exactly 2 repair-LLM calls before ErrCompileFailed.
	repair := eval.NewMockLLMClient(resp(`\documentclass{article}`), resp(`\documentclass{article}`))
	deps := cliDeps(reqsGoKafka, goBullets(), failEvalJSON, repair, failCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 3 {
		t.Fatalf("exit = %d, want 3; stderr=%s", code, errb.String())
	}
	dir := filepath.Join(cfg.OutDir, "senior-go-engineer-2026-06-19")
	if _, err := os.Stat(filepath.Join(dir, "resume.pdf")); !os.IsNotExist(err) {
		t.Errorf("resume.pdf should be skipped, stat err = %v", err)
	}
}

func TestGenRunFatalOnBadContent(t *testing.T) {
	cfg, _ := cliBase(t)
	cfg.ContentPath = filepath.Join(t.TempDir(), "does-not-exist.md")
	deps := cliDeps(reqsGoKafka, goBullets(), passEvalJSON, nil, okCompiler())

	var out, errb strings.Builder
	code := genRun(context.Background(), cfg, deps, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(cfg.OutDir); !os.IsNotExist(err) {
		t.Errorf("no output dir should be created on fatal error")
	}
}

func TestGenRunOverwritesSameDay(t *testing.T) {
	cfg, _ := cliBase(t)
	deps1 := cliDeps(reqsGoKafka, goBullets(), passEvalJSON, nil, okCompiler())
	var o1, e1 strings.Builder
	if code := genRun(context.Background(), cfg, deps1, &o1, &e1); code != 0 {
		t.Fatalf("first run exit = %d; stderr=%s", code, e1.String())
	}
	// Second run with fresh mocks (mock clients are single-use) overwrites.
	deps2 := cliDeps(reqsGoKafka, goBullets(), passEvalJSON, nil, okCompiler())
	var o2, e2 strings.Builder
	if code := genRun(context.Background(), cfg, deps2, &o2, &e2); code != 0 {
		t.Fatalf("second run exit = %d; stderr=%s", code, e2.String())
	}
}

func TestRunGenerateMissingContent(t *testing.T) {
	code, _, errOut := runCapture("generate", "--jd-url", "https://acme.com/job")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "required") {
		t.Errorf("stderr = %q, want 'required'", errOut)
	}
}

func TestRunGenerateMissingJDURL(t *testing.T) {
	code, _, errOut := runCapture("generate", "--content", "some.md")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "required") {
		t.Errorf("stderr = %q, want 'required'", errOut)
	}
}

func TestRunGenerateUnknownProvider(t *testing.T) {
	code, _, errOut := runCapture("generate", "--content", "some.md", "--jd-url", "https://acme.com/job", "--provider", "bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "unknown provider") {
		t.Errorf("stderr = %q, want 'unknown provider'", errOut)
	}
}

func TestRunGenerateMissingAPIKeyIsFatal(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	dir := t.TempDir()
	content := writeFileT(t, dir, "content.md", cliContent)
	code, _, errOut := runCapture("generate", "--content", content, "--jd-url", "https://acme.com/job")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%s", code, errOut)
	}
}
