package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/evaluate"
	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/orchestrate"
	"github.com/farazhassan/tailor-swift/internal/pipeline"
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
