package retrieve

import (
	"testing"

	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/store"
)

func TestSelectUnionsAndReportsGaps(t *testing.T) {
	s := threeAchStore(t)
	ix, err := NewIndex(s, threeVecs())
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	reqs := []jd.Requirement{
		{Text: "needs alpha", MustHave: true},   // matches alpha (cosine 1.0)
		{Text: "needs nothing", MustHave: true}, // best cosine negative → gap
	}
	reqVecs := [][]float32{
		{1, 0},   // top match: alpha = 1.0
		{-1, -1}, // best cosine is negative, below minScore
	}

	sel, err := Select(ix, reqs, reqVecs, 1, 0.5)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	// The unmet must-have is reported as a gap; the met one is not.
	if len(sel.Gaps) != 1 || sel.Gaps[0].Text != "needs nothing" {
		t.Errorf("gaps = %+v, want [needs nothing]", sel.Gaps)
	}

	// alpha is in the candidate set with its perfect score.
	var foundAlpha bool
	for _, d := range sel.Documents {
		if d.ID == store.DeriveID("alpha") {
			foundAlpha = true
			if d.Score < 0.999 {
				t.Errorf("alpha score = %v, want ~1.0", d.Score)
			}
		}
	}
	if !foundAlpha {
		t.Error("alpha missing from candidate set")
	}

	// Documents are sorted by descending score.
	for i := 1; i < len(sel.Documents); i++ {
		if sel.Documents[i-1].Score < sel.Documents[i].Score {
			t.Errorf("documents not sorted by descending score: %+v", sel.Documents)
		}
	}
}

func TestSelectNoGapWhenMustHaveCovered(t *testing.T) {
	s := threeAchStore(t)
	ix, _ := NewIndex(s, threeVecs())

	reqs := []jd.Requirement{{Text: "needs beta", MustHave: true}}
	reqVecs := [][]float32{{0, 1}} // matches beta exactly (cosine 1.0)

	sel, err := Select(ix, reqs, reqVecs, 1, 0.5)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(sel.Gaps) != 0 {
		t.Errorf("gaps = %+v, want none", sel.Gaps)
	}
}

func TestSelectErrorsOnLengthMismatch(t *testing.T) {
	s := threeAchStore(t)
	ix, _ := NewIndex(s, threeVecs())
	reqs := []jd.Requirement{{Text: "x", MustHave: true}}
	if _, err := Select(ix, reqs, [][]float32{}, 1, 0.5); err == nil {
		t.Error("Select: want error on reqs/vecs length mismatch, got nil")
	}
}
