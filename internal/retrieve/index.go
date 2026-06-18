package retrieve

import (
	"fmt"
	"sort"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// entry pairs an achievement with its embedding vector.
type entry struct {
	ach store.Achievement
	vec []float32
}

// Index is an in-memory cosine index over a content store's achievements. Build
// it from a parsed store plus the vectors produced by embed.Embedder.EmbedStore
// (keyed by achievement ID).
type Index struct {
	entries []entry
}

// NewIndex pairs each achievement in s with its vector. Achievement IDs are
// content hashes, so duplicate bullets collapse to a single entry. It errors if
// any achievement has no vector in vectors.
func NewIndex(s *store.Store, vectors map[string][]float32) (*Index, error) {
	ix := &Index{}
	seen := map[string]bool{}
	for _, a := range s.Achievements() {
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		v, ok := vectors[a.ID]
		if !ok {
			return nil, fmt.Errorf("retrieve: no vector for achievement %s", a.ID)
		}
		ix.entries = append(ix.entries, entry{ach: a, vec: v})
	}
	return ix, nil
}

// docFor builds a gantry.Document for an achievement and its similarity score.
func docFor(a store.Achievement, score float64) gantry.Document {
	return gantry.Document{
		ID:      a.ID,
		Content: a.Text,
		Score:   score,
		Metadata: map[string]any{
			"tags": a.Tags,
			"file": a.Provenance.File,
			"line": a.Provenance.Line,
		},
	}
}

// TopK returns the k achievements most similar to query as gantry Documents,
// sorted by descending cosine score (ties broken by ascending ID for
// determinism). k <= 0 or k beyond the index size returns all entries, ranked.
func (ix *Index) TopK(query []float32, k int) []gantry.Document {
	docs := make([]gantry.Document, 0, len(ix.entries))
	for _, e := range ix.entries {
		docs = append(docs, docFor(e.ach, cosine(query, e.vec)))
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Score != docs[j].Score {
			return docs[i].Score > docs[j].Score
		}
		return docs[i].ID < docs[j].ID
	})
	if k > 0 && k < len(docs) {
		docs = docs[:k]
	}
	return docs
}
