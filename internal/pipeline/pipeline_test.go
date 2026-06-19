package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
)

// --- fakes & helpers ------------------------------------------------------

// vecFor maps text to a deterministic 3-dim vector by keyword, so retrieval
// rankings are predictable: "go" -> x axis, "kafka" -> y axis, "rust" -> z axis.
// Order matters: check the more specific keywords before "go".
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

// fakeEmbedder returns vecFor(text) for each input, in order.
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = vecFor(t)
	}
	return out, nil
}

// panicEmbedder fails the test if Embed is ever called (used to prove the cache
// served every lookup).
type panicEmbedder struct{ t *testing.T }

func (p panicEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	p.t.Fatalf("Embed should not be called; the cache should have served all vectors")
	return nil, nil
}

// reqLLM returns a mock LLM whose single response is the given requirements JSON.
func reqLLM(reqsJSON string) gantry.LLMClient {
	return eval.NewMockLLMClient(gantry.LLMResponse{Content: reqsJSON, StopReason: gantry.StopReasonEnd})
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

const sampleContent = `# Ada Lovelace

## Contact
Email: ada@example.com

## Acme Corp

### Billing
- Built a Go billing service
- Scaled Kafka pipelines
`

// baseCfg writes a content file and a JD file under a temp dir and returns a
// Config + Deps wired to a fake embedder and a mock LLM scripted with reqsJSON.
func baseCfg(t *testing.T, content, reqsJSON string) (Config, Deps) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		ContentPath: writeFile(t, dir, "content.md", content),
		JDURL:       "https://example.com/job",
		JDFile:      writeFile(t, dir, "jd.txt", "We need a backend engineer."),
		MinScore:    0.5,
		JDCacheDir:  filepath.Join(dir, "jdcache"),
	}
	deps := Deps{LLM: reqLLM(reqsJSON), Embedder: fakeEmbedder{}}
	return cfg, deps
}

// --- tests ----------------------------------------------------------------

func TestAcquireHappyPath(t *testing.T) {
	reqs := `[{"text":"Go","must_have":true},{"text":"Kafka","must_have":false}]`
	cfg, deps := baseCfg(t, sampleContent, reqs)
	res, err := Acquire(context.Background(), cfg, deps)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if len(res.Requirements) != 2 {
		t.Fatalf("requirements = %d, want 2", len(res.Requirements))
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(res.Candidates))
	}
	if len(res.Gaps) != 0 {
		t.Errorf("gaps = %+v, want none", res.Gaps)
	}
	if res.Store == nil || res.Store.Profile.Name != "Ada Lovelace" {
		t.Errorf("store not parsed: %+v", res.Store)
	}
	if res.Posting == nil || res.Posting.URL != "https://example.com/job" {
		t.Errorf("posting: %+v", res.Posting)
	}
	if res.Candidates[0].Score < 0.99 {
		t.Errorf("top candidate score = %v, want ~1.0", res.Candidates[0].Score)
	}
}

func TestAcquireReportsCoverageGap(t *testing.T) {
	reqs := `[{"text":"Go","must_have":true},{"text":"Rust","must_have":true}]`
	cfg, deps := baseCfg(t, sampleContent, reqs)
	res, err := Acquire(context.Background(), cfg, deps)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if len(res.Gaps) != 1 || res.Gaps[0].Text != "Rust" {
		t.Fatalf("gaps = %+v, want exactly [Rust]", res.Gaps)
	}
}

func TestAcquireAppliesTopKDefault(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Ada Lovelace\n\n## Contact\nEmail: ada@example.com\n\n## Acme Corp\n\n### Work\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, "- Built Go service number %d\n", i)
	}
	reqs := `[{"text":"Go","must_have":true}]`
	cfg, deps := baseCfg(t, b.String(), reqs)
	res, err := Acquire(context.Background(), cfg, deps)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 10 distinct Go achievements all score 1.0; default TopK=8 caps the union.
	if len(res.Candidates) != 8 {
		t.Fatalf("candidates = %d, want 8 (default TopK)", len(res.Candidates))
	}
}

func TestAcquireErrorsOnNoRequirements(t *testing.T) {
	cfg, deps := baseCfg(t, sampleContent, `[]`)
	_, err := Acquire(context.Background(), cfg, deps)
	if err == nil || !strings.Contains(err.Error(), "no requirements") {
		t.Fatalf("err = %v, want a no-requirements error", err)
	}
}

func TestAcquireErrorsWithoutJDURL(t *testing.T) {
	cfg, deps := baseCfg(t, sampleContent, `[{"text":"Go","must_have":true}]`)
	cfg.JDURL = ""
	_, err := Acquire(context.Background(), cfg, deps)
	if err == nil || !strings.Contains(err.Error(), "JD URL is required") {
		t.Fatalf("err = %v, want a JD-URL-required error", err)
	}
}

func TestAcquireReusesEmbedCache(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		ContentPath:    writeFile(t, dir, "content.md", sampleContent),
		JDURL:          "https://example.com/job",
		JDFile:         writeFile(t, dir, "jd.txt", "We need a backend engineer."),
		MinScore:       0.5,
		EmbedCachePath: filepath.Join(dir, "embed-cache.json"),
		JDCacheDir:     filepath.Join(dir, "jdcache"),
	}
	reqs := `[{"text":"Go","must_have":true},{"text":"Kafka","must_have":false}]`

	// First run populates both the embedding cache and the JD cache.
	if _, err := Acquire(context.Background(), cfg, Deps{LLM: reqLLM(reqs), Embedder: fakeEmbedder{}}); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if _, err := os.Stat(cfg.EmbedCachePath); err != nil {
		t.Fatalf("embed cache not written: %v", err)
	}

	// Second run: a panicking embedder and a no-response LLM. The embed cache
	// must serve every vector (no Embed call) and the JD cache must serve the
	// posting (no LLM call), so neither fake is invoked.
	deps2 := Deps{LLM: eval.NewMockLLMClient(), Embedder: panicEmbedder{t}}
	res, err := Acquire(context.Background(), cfg, deps2)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if len(res.Candidates) != 2 {
		t.Errorf("candidates = %d, want 2", len(res.Candidates))
	}
}

func TestAcquireErrorsOnBadContentPath(t *testing.T) {
	cfg, deps := baseCfg(t, sampleContent, `[{"text":"Go","must_have":true}]`)
	cfg.ContentPath = filepath.Join(t.TempDir(), "does-not-exist.md")
	_, err := Acquire(context.Background(), cfg, deps)
	if err == nil || !strings.Contains(err.Error(), "parse content") {
		t.Fatalf("err = %v, want a parse-content error", err)
	}
}
