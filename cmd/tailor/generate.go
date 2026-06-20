package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/gantry/components/limiter"
	"github.com/farazhassan/gantry/components/llm/anthropic"
	"github.com/farazhassan/tailor-swift/internal/embed"
	"github.com/farazhassan/tailor-swift/internal/orchestrate"
	"github.com/farazhassan/tailor-swift/internal/pipeline"
	"github.com/farazhassan/tailor-swift/internal/render"
	"github.com/farazhassan/tailor-swift/templates"
)

const defaultMaxRepairs = 2

const generateUsage = `usage: tailor generate --content <file> --jd-url <url> [flags]

required:
  --content <file>   content store markdown
  --jd-url <url>     job posting URL

optional:
  --jd-file <file>   local job description text (URL still required)
  --model <id>       Anthropic model (default claude-sonnet-4-6)
  --out <dir>        base output directory (default out)
  --template <file>  LaTeX template override (default: built-in)
  --max-iterations N refinement iterations (default 3)
  --top-k N          candidates per requirement (default 8)
  --min-score F      min similarity for a must-have (default 0)
  --embed-cache <f>  embedding cache file (default: disabled)
  --jd-cache <dir>   cached postings directory`

// runGenerate parses flags, constructs the real clients, resolves the template,
// and calls genRun. Returns the process exit code.
func runGenerate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", "", "content store markdown (required)")
	jdURL := fs.String("jd-url", "", "job posting URL (required)")
	jdFile := fs.String("jd-file", "", "local job description text file")
	model := fs.String("model", "claude-sonnet-4-6", "Anthropic model id")
	out := fs.String("out", "out", "base output directory")
	template := fs.String("template", "", "LaTeX template override")
	maxIter := fs.Int("max-iterations", 3, "max refinement iterations")
	topK := fs.Int("top-k", 8, "top-K candidates per requirement")
	minScore := fs.Float64("min-score", 0, "min similarity for a must-have")
	embedCache := fs.String("embed-cache", "", "embedding cache file")
	jdCache := fs.String("jd-cache", "", "cached postings directory")
	if err := fs.Parse(args); err != nil {
		return 2 // flag already printed the error to stderr
	}
	if *content == "" || *jdURL == "" {
		fmt.Fprintln(stderr, "generate: --content and --jd-url are required")
		fmt.Fprintln(stderr, generateUsage)
		return 2
	}
	if *model == "" {
		fmt.Fprintln(stderr, "generate: --model must not be empty")
		return 2
	}

	tmpl := templates.Default
	if *template != "" {
		data, err := os.ReadFile(*template)
		if err != nil {
			fmt.Fprintf(stderr, "generate: read template: %v\n", err)
			return 1
		}
		tmpl = string(data)
	}

	embedder, err := embed.NewVoyageClient()
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	embedModel := os.Getenv("VOYAGE_MODEL")
	if embedModel == "" {
		embedModel = "voyage-3"
	}

	llm, err := newAnthropic(*model)
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}

	deps := genDeps{
		ExtractLLM: llm, GenLLM: llm, EvalLLM: llm, RepairLLM: llm,
		Embedder: embedder, Compile: render.PDFLaTeX, Template: tmpl,
	}
	cfg := genConfig{
		ContentPath: *content, JDURL: *jdURL, JDFile: *jdFile, OutDir: *out,
		EmbedModel: embedModel, TopK: *topK, MinScore: *minScore,
		MaxIterations: *maxIter, EmbedCachePath: *embedCache, JDCacheDir: *jdCache,
		Today: time.Now(),
	}
	return genRun(context.Background(), cfg, deps, stdout, stderr)
}

// newAnthropic constructs the Anthropic client, converting its panic (missing
// key or empty model) into an error so the command exits cleanly.
func newAnthropic(model string) (llm gantry.LLMClient, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return anthropic.New(model), nil
}

// genConfig is the resolved, validated input to the core (the wiring fills it
// from flags; tests construct it directly).
type genConfig struct {
	ContentPath    string
	JDURL          string
	JDFile         string
	OutDir         string
	EmbedModel     string
	TopK           int
	MinScore       float64
	MaxIterations  int
	EmbedCachePath string
	JDCacheDir     string
	Today          time.Time
}

// genDeps are the injected collaborators. The wiring assigns one Anthropic
// client to all four LLM roles; keeping them separate lets tests script a
// per-role mock.
type genDeps struct {
	ExtractLLM gantry.LLMClient
	GenLLM     gantry.LLMClient
	EvalLLM    gantry.LLMClient
	RepairLLM  gantry.LLMClient
	Embedder   embeddings.Embeddings
	Compile    render.CompileFunc
	Template   string
}

// genRun is the testable core: acquire inputs, warn on coverage gaps, run the
// refinement loop, write artifacts, and return the process exit code
// (0 pass, 3 emitted-but-not-passed, 1 fatal).
func genRun(ctx context.Context, cfg genConfig, deps genDeps, stdout, stderr io.Writer) int {
	acq, err := pipeline.Acquire(ctx, pipeline.Config{
		ContentPath:    cfg.ContentPath,
		JDURL:          cfg.JDURL,
		JDFile:         cfg.JDFile,
		Model:          cfg.EmbedModel,
		TopK:           cfg.TopK,
		MinScore:       cfg.MinScore,
		EmbedCachePath: cfg.EmbedCachePath,
		JDCacheDir:     cfg.JDCacheDir,
	}, pipeline.Deps{LLM: deps.ExtractLLM, Embedder: deps.Embedder})
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}

	if len(acq.Gaps) > 0 {
		texts := make([]string, len(acq.Gaps))
		for i, g := range acq.Gaps {
			texts[i] = g.Text
		}
		fmt.Fprintf(stderr, "warning: %d must-have requirement(s) unmatched: %s\n", len(acq.Gaps), strings.Join(texts, ", "))
	}

	run, err := orchestrate.Run(ctx, orchestrate.Config{
		Template:      deps.Template,
		Store:         acq.Store,
		Requirements:  acq.Requirements,
		Candidates:    acq.Candidates,
		MaxIterations: cfg.MaxIterations,
		MaxRepairs:    defaultMaxRepairs,
		Budget:        limiter.Limits{},
	}, orchestrate.Deps{
		GenLLM:    deps.GenLLM,
		EvalLLM:   deps.EvalLLM,
		RepairLLM: deps.RepairLLM,
		Compile:   deps.Compile,
	})
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	if run.Best == nil {
		fmt.Fprintln(stderr, "generate: no resume produced")
		return 1
	}

	dir := filepath.Join(cfg.OutDir, slugify(cfg.JDURL)+"-"+cfg.Today.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	if err := writeArtifacts(dir, run, acq, stderr); err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "wrote %s\n", dir)
	if run.Passed {
		return 0
	}
	return 3
}

// slugify derives a filesystem-friendly slug from a job posting URL: the last
// non-empty path segment (or the host when there is no path), lowercased, with
// each run of non-alphanumerics collapsed to a single dash and the ends
// trimmed. Falls back to "job" when nothing usable remains.
func slugify(rawURL string) string {
	seg := ""
	if u, err := url.Parse(rawURL); err == nil {
		parts := strings.Split(u.Path, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				seg = parts[i]
				break
			}
		}
		if seg == "" && u.Path == "" {
			seg = u.Host
		}
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(seg) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "job"
	}
	return s
}

// critiqueOut is the JSON shape written to critique.json. It mirrors
// evaluate.Evaluation but includes Composite and Pass (which carry json:"-" on
// the source type) plus run-level context.
type critiqueOut struct {
	Pass       bool               `json:"pass"`
	Composite  float64            `json:"composite"`
	Truthful   bool               `json:"truthful"`
	Scores     map[string]float64 `json:"scores"`
	Critique   map[string]string  `json:"critique"`
	Summary    string             `json:"summary"`
	StopReason string             `json:"stop_reason"`
	Iterations int                `json:"iterations"`
}

// writeArtifacts writes resume.tex, resume.pdf (skipped when the best iteration
// produced no PDF), critique.json, and run.log into dir. dir must already exist.
//
// Precondition: run.Best must be non-nil. orchestrate.Result.Best can be nil
// (e.g. the budget tripped on the very first generate); the caller is
// responsible for handling that case before calling writeArtifacts.
func writeArtifacts(dir string, run *orchestrate.Result, acq *pipeline.Result, stderr io.Writer) error {
	best := run.Best
	if err := os.WriteFile(filepath.Join(dir, "resume.tex"), []byte(best.TeX), 0o644); err != nil {
		return fmt.Errorf("write resume.tex: %w", err)
	}

	pdfSkipped := false
	if len(best.PDF) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "resume.pdf"), best.PDF, 0o644); err != nil {
			return fmt.Errorf("write resume.pdf: %w", err)
		}
	} else {
		pdfSkipped = true
		fmt.Fprintln(stderr, "warning: best iteration did not compile; resume.pdf not written")
	}

	co := critiqueOut{StopReason: run.StopReason, Iterations: len(run.Iterations)}
	if ev := best.Evaluation; ev != nil {
		co.Pass = ev.Pass
		co.Composite = ev.Composite
		co.Truthful = ev.Truthful
		co.Scores = ev.Scores
		co.Critique = ev.Critique
		co.Summary = ev.Summary
	}
	cj, err := json.MarshalIndent(co, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal critique: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "critique.json"), cj, 0o644); err != nil {
		return fmt.Errorf("write critique.json: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "jd_url: %s\n", acq.Posting.URL)
	fmt.Fprintf(&b, "fetched_at: %s\n", acq.Posting.FetchedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "requirements: %d\n", len(acq.Requirements))
	fmt.Fprintf(&b, "candidates: %d\n", len(acq.Candidates))
	if len(acq.Gaps) == 0 {
		fmt.Fprintln(&b, "gaps: none")
	} else {
		fmt.Fprintf(&b, "gaps: %d\n", len(acq.Gaps))
		for _, g := range acq.Gaps {
			fmt.Fprintf(&b, "  - %s\n", g.Text)
		}
	}
	for _, it := range run.Iterations {
		pass, comp := false, 0.0
		if it.Evaluation != nil {
			pass = it.Evaluation.Pass
			comp = it.Evaluation.Composite
		}
		fmt.Fprintf(&b, "iteration %d: compiled=%t pass=%t composite=%.3f\n", it.Index, it.Compiled, pass, comp)
	}
	fmt.Fprintf(&b, "stop_reason: %s\n", run.StopReason)
	fmt.Fprintf(&b, "best_iteration: %d\n", best.Index)
	if pdfSkipped {
		fmt.Fprintln(&b, "note: resume.pdf skipped (best iteration did not compile)")
	}
	if err := os.WriteFile(filepath.Join(dir, "run.log"), []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write run.log: %w", err)
	}
	return nil
}
