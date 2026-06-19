// Package evaluate scores a tailored resume revision against a weighted rubric
// with a truthfulness hard-gate, returning a structured verdict the orchestrator
// uses as its loop stop-condition and as feedback for the next revision.
package evaluate

import (
	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/jd"
)

// Dimension is one scored rubric axis and its weight in the composite score.
type Dimension struct {
	Key    string
	Weight float64
}

// Dimensions is the weighted rubric. Weights sum to 1.0 so the composite is a
// weighted average on the same 0–1 scale as the individual dimensions. The keys
// match the JSON the evaluator model is asked to return.
var Dimensions = []Dimension{
	{Key: "jd_coverage", Weight: 0.30},
	{Key: "relevance", Weight: 0.20},
	{Key: "evidence_quality", Weight: 0.20},
	{Key: "truthfulness", Weight: 0.15},
	{Key: "format", Weight: 0.15},
}

// Threshold is the minimum composite score for an iteration to be accepted —
// and only when the truthfulness hard-gate has also passed.
const Threshold = 0.85

// EvalInput is everything the evaluator needs to score one resume revision.
type EvalInput struct {
	Requirements []jd.Requirement  // the job's requirements (must-haves flagged)
	Candidates   []gantry.Document // ground-truth achievements the resume must trace to
	Resume       string            // the generated resume content under review
	Compiled     bool              // whether the rendered .tex compiled to PDF cleanly
}

// Evaluation is the evaluator's structured verdict for one revision. Scores and
// Critique are keyed by dimension. Composite and Pass are computed by Evaluate
// (not parsed from the model), so they carry the `json:"-"` tag.
type Evaluation struct {
	Scores    map[string]float64 `json:"scores"`
	Truthful  bool               `json:"truthful"`
	Critique  map[string]string  `json:"critique"`
	Summary   string             `json:"summary"`
	Composite float64            `json:"-"`
	Pass      bool               `json:"-"`
}
