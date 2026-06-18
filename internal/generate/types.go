package generate

import (
	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/jd"
)

// Bullet is one selected, rephrased achievement. UnitID references the candidate
// achievement it was derived from; the truthfulness contract requires every
// bullet to reference a real candidate ID.
type Bullet struct {
	UnitID string `json:"unit_id"`
	Text   string `json:"text"`
}

// Result is the generator's structured output for one iteration.
type Result struct {
	Bullets []Bullet
}

// Input is everything the generator needs for one iteration.
type Input struct {
	Requirements  []jd.Requirement  // the job's requirements (must-haves flagged)
	Candidates    []gantry.Document // retrieved candidate achievements (ID + original text)
	PriorCritique string            // evaluator feedback from the previous iteration; empty on the first
}
