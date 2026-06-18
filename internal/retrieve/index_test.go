package retrieve

import (
	"testing"

	"github.com/farazhassan/tailor-swift/internal/store"
)

// threeAchStore parses a fixture with achievements "alpha", "beta", "gamma".
func threeAchStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.ParseReader([]byte("## Acme — Eng\n### P\n- alpha\n- beta\n- gamma\n"), "mem")
	if err != nil {
		t.Fatalf("ParseReader: %v", err)
	}
	if got := len(s.Achievements()); got != 3 {
		t.Fatalf("fixture has %d achievements, want 3", got)
	}
	return s
}

func threeVecs() map[string][]float32 {
	return map[string][]float32{
		store.DeriveID("alpha"): {1, 0},
		store.DeriveID("beta"):  {0, 1},
		store.DeriveID("gamma"): {1, 1},
	}
}

func TestNewIndexErrorsOnMissingVector(t *testing.T) {
	s := threeAchStore(t)
	vecs := map[string][]float32{store.DeriveID("alpha"): {1, 0}} // beta, gamma missing
	if _, err := NewIndex(s, vecs); err == nil {
		t.Error("NewIndex: want error when a vector is missing, got nil")
	}
}

func TestTopKRanksByCosine(t *testing.T) {
	s := threeAchStore(t)
	ix, err := NewIndex(s, threeVecs())
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	got := ix.TopK([]float32{1, 0}, 2)
	if len(got) != 2 {
		t.Fatalf("TopK len = %d, want 2", len(got))
	}
	// alpha is identical to the query (cosine 1) → rank 1.
	if got[0].ID != store.DeriveID("alpha") || got[0].Content != "alpha" {
		t.Errorf("top doc = %+v, want alpha", got[0])
	}
	// gamma (cosine ~0.707) beats beta (cosine 0) → rank 2.
	if got[1].ID != store.DeriveID("gamma") {
		t.Errorf("second doc = %s, want gamma", got[1].ID)
	}
	if got[0].Score < got[1].Score {
		t.Errorf("not sorted by descending score: %v then %v", got[0].Score, got[1].Score)
	}
}

func TestTopKReturnsAllWhenKNonPositive(t *testing.T) {
	s := threeAchStore(t)
	ix, _ := NewIndex(s, threeVecs())
	if got := ix.TopK([]float32{1, 0}, 0); len(got) != 3 {
		t.Errorf("TopK(_,0) len = %d, want 3 (all)", len(got))
	}
}
