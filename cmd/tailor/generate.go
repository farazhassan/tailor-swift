package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/farazhassan/tailor-swift/internal/orchestrate"
	"github.com/farazhassan/tailor-swift/internal/pipeline"
)

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
