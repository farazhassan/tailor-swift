package retrieve

import (
	"fmt"
	"sort"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/jd"
)

// Selection is the candidate content set for a job plus the must-have
// requirements that no achievement covers well enough.
type Selection struct {
	Documents []gantry.Document // unioned top-k across requirements, deduped, ranked
	Gaps      []jd.Requirement  // must-have requirements whose best match scored below minScore
}

// Select ranks achievements against each requirement vector (reqVecs[i] is the
// embedding of reqs[i].Text) and unions the per-requirement top-k into one
// candidate set, keeping the highest score when an achievement matches several
// requirements. A must-have requirement whose best-matching achievement scores
// below minScore is reported as a coverage gap. reqs and reqVecs must be the
// same length.
func Select(ix *Index, reqs []jd.Requirement, reqVecs [][]float32, k int, minScore float64) (*Selection, error) {
	if len(reqs) != len(reqVecs) {
		return nil, fmt.Errorf("retrieve: %d requirements but %d vectors", len(reqs), len(reqVecs))
	}
	best := map[string]gantry.Document{}
	var gaps []jd.Requirement
	for i, req := range reqs {
		docs := ix.TopK(reqVecs[i], k)
		var topScore float64
		if len(docs) > 0 {
			topScore = docs[0].Score
		}
		for _, d := range docs {
			if cur, ok := best[d.ID]; !ok || d.Score > cur.Score {
				best[d.ID] = d
			}
		}
		if req.MustHave && topScore < minScore {
			gaps = append(gaps, req)
		}
	}
	docs := make([]gantry.Document, 0, len(best))
	for _, d := range best {
		docs = append(docs, d)
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Score != docs[j].Score {
			return docs[i].Score > docs[j].Score
		}
		return docs[i].ID < docs[j].ID
	})
	return &Selection{Documents: docs, Gaps: gaps}, nil
}
