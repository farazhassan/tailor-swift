package embed

import (
	"context"
	"fmt"

	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// Embedder turns text into vectors, reusing an on-disk cache so unchanged text
// is never re-sent to the provider. Keys are content hashes (see
// store.DeriveID), so changed text yields a new key and a natural cache miss.
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

// ensure embeds any (ids[i], texts[i]) pair not already cached and stores the
// result. ids and texts are parallel slices; duplicate ids are embedded once.
// First-seen order is preserved so the provider response (indexed by position)
// maps back to the right id.
func (e *Embedder) ensure(ctx context.Context, ids, texts []string) error {
	var missIDs []string
	var missText []string
	seen := map[string]bool{}
	for i, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := e.cache.Get(id); !ok {
			missIDs = append(missIDs, id)
			missText = append(missText, texts[i])
		}
	}
	if len(missText) == 0 {
		return nil
	}
	vecs, err := e.client.Embed(ctx, missText)
	if err != nil {
		return fmt.Errorf("embed: provider: %w", err)
	}
	if len(vecs) != len(missIDs) {
		return fmt.Errorf("embed: got %d vectors for %d inputs", len(vecs), len(missIDs))
	}
	for i, id := range missIDs {
		e.cache.Put(id, vecs[i])
	}
	return nil
}

// EmbedStore returns a vector for every achievement in s, keyed by achievement
// ID. Only achievements missing from the cache are sent to the provider; the
// cache is updated in memory with any new vectors. Call Cache().Save to persist.
func (e *Embedder) EmbedStore(ctx context.Context, s *store.Store) (map[string][]float32, error) {
	achs := s.Achievements()
	ids := make([]string, len(achs))
	texts := make([]string, len(achs))
	for i, a := range achs {
		ids[i] = a.ID
		texts[i] = a.Text
	}
	if err := e.ensure(ctx, ids, texts); err != nil {
		return nil, err
	}
	out := make(map[string][]float32, len(achs))
	for _, a := range achs {
		v, ok := e.cache.Get(a.ID)
		if !ok {
			return nil, fmt.Errorf("embed: cache miss after fill for %s", a.ID)
		}
		out[a.ID] = v
	}
	return out, nil
}

// EmbedTexts returns a vector for each text, in input order, reusing the cache.
// Texts are keyed by content hash (store.DeriveID), so identical text — within
// one call or across calls/JDs — is embedded only once. Used for JD requirement
// chunks.
func (e *Embedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	ids := make([]string, len(texts))
	for i, t := range texts {
		ids[i] = store.DeriveID(t)
	}
	if err := e.ensure(ctx, ids, texts); err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		v, ok := e.cache.Get(ids[i])
		if !ok {
			return nil, fmt.Errorf("embed: cache miss after fill for %s", ids[i])
		}
		out[i] = v
	}
	return out, nil
}
