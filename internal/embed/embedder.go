package embed

import (
	"context"
	"fmt"

	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// Embedder turns content-store achievements into vectors, reusing an on-disk
// cache so unchanged text is never re-sent to the provider. Achievement IDs are
// content hashes (see store.DeriveID), so a changed bullet yields a new key and
// a natural cache miss.
type Embedder struct {
	client embeddings.Embeddings
	cache  *Cache
}

// NewEmbedder wraps an embeddings client and a cache.
func NewEmbedder(client embeddings.Embeddings, cache *Cache) *Embedder {
	return &Embedder{client: client, cache: cache}
}

// Cache returns the underlying cache so callers can persist it after embedding.
func (e *Embedder) Cache() *Cache { return e.cache }

// EmbedStore returns a vector for every achievement in s, keyed by achievement
// ID. Only achievements missing from the cache are sent to the provider; the
// cache is updated in memory with any new vectors. Call Cache().Save to persist.
func (e *Embedder) EmbedStore(ctx context.Context, s *store.Store) (map[string][]float32, error) {
	achs := s.Achievements()

	// Collect unique cache misses, preserving first-seen order so the provider
	// response (indexed by position) maps back to the right ID.
	var missIDs []string
	var missText []string
	seen := map[string]bool{}
	for _, a := range achs {
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		if _, ok := e.cache.Get(a.ID); !ok {
			missIDs = append(missIDs, a.ID)
			missText = append(missText, a.Text)
		}
	}

	if len(missText) > 0 {
		vecs, err := e.client.Embed(ctx, missText)
		if err != nil {
			return nil, fmt.Errorf("embed: provider: %w", err)
		}
		if len(vecs) != len(missIDs) {
			return nil, fmt.Errorf("embed: got %d vectors for %d inputs", len(vecs), len(missIDs))
		}
		for i, id := range missIDs {
			e.cache.Put(id, vecs[i])
		}
	}

	out := make(map[string][]float32, len(seen))
	for id := range seen {
		v, ok := e.cache.Get(id)
		if !ok {
			return nil, fmt.Errorf("embed: cache miss after fill for %s", id)
		}
		out[id] = v
	}
	return out, nil
}
