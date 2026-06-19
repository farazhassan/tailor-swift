// Package pipeline performs the pre-loop acquisition for a tailoring run: it
// parses the content store, embeds and indexes the achievements, acquires the
// job posting, retrieves the candidate set for the posting's requirements, and
// returns exactly the inputs orchestrate.Run consumes (plus the coverage gaps
// and the raw posting for downstream visibility).
package pipeline

import (
	"context"
	"fmt"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/tailor-swift/internal/embed"
	"github.com/farazhassan/tailor-swift/internal/jd"
	"github.com/farazhassan/tailor-swift/internal/retrieve"
	"github.com/farazhassan/tailor-swift/internal/store"
)

const (
	defaultModel = "voyage-3"
	defaultTopK  = 8
)

// Config is the static input to an acquisition (the CLI fills it from flags).
type Config struct {
	ContentPath    string  // path to the content store markdown (store.Parse)
	JDURL          string  // job posting URL (required: jd.Acquire's cache key + posting identity)
	JDFile         string  // optional local JD text file (offline source; URL still required)
	Model          string  // embedding model id for cache scoping (default "voyage-3")
	TopK           int     // top-K candidates per requirement (default 8 when 0)
	MinScore       float64 // min similarity for a must-have to count as covered
	EmbedCachePath string  // on-disk embedding cache file; "" disables caching
	JDCacheDir     string  // directory for cached postings (jd.Options.CacheDir)
}

// Deps are the injected collaborators: production passes real clients; tests
// pass mocks.
type Deps struct {
	LLM      gantry.LLMClient      // for jd requirement extraction
	Embedder embeddings.Embeddings // Voyage in production; a fake in tests
}

// Result is exactly what orchestrate.Config needs, plus visibility extras.
type Result struct {
	Store        *store.Store      // parsed content store        (-> orchestrate Config.Store)
	Posting      *jd.Posting       // the acquired JD (URL, raw text, requirements)
	Requirements []jd.Requirement  // == Posting.Requirements     (-> orchestrate Config.Requirements)
	Candidates   []gantry.Document // retrieved, ranked, deduped  (-> orchestrate Config.Candidates)
	Gaps         []jd.Requirement  // must-haves with no match above MinScore (warn, non-fatal)
}

// Acquire runs the pre-loop chain: parse the content store, embed and index the
// achievements, acquire the job posting, embed its requirements, and select the
// candidate set. It returns the assembled orchestrate inputs plus any coverage
// gaps. JDURL is required. An empty requirement set is an error (generating from
// nothing is never useful). The embedding cache save is best-effort.
func Acquire(ctx context.Context, cfg Config, deps Deps) (*Result, error) {
	if cfg.JDURL == "" {
		return nil, fmt.Errorf("pipeline: JD URL is required")
	}
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	topK := cfg.TopK
	if topK == 0 {
		topK = defaultTopK
	}

	s, err := store.Parse(cfg.ContentPath)
	if err != nil {
		return nil, fmt.Errorf("pipeline: parse content: %w", err)
	}

	cache := embed.NewCache(model)
	if cfg.EmbedCachePath != "" {
		cache, err = embed.LoadCache(cfg.EmbedCachePath, model)
		if err != nil {
			return nil, fmt.Errorf("pipeline: load embed cache: %w", err)
		}
	}
	emb := embed.NewEmbedder(deps.Embedder, cache)

	vectors, err := emb.EmbedStore(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("pipeline: embed store: %w", err)
	}

	ix, err := retrieve.NewIndex(s, vectors)
	if err != nil {
		return nil, fmt.Errorf("pipeline: build index: %w", err)
	}

	posting, err := jd.Acquire(ctx, deps.LLM, jd.Options{
		URL:      cfg.JDURL,
		FilePath: cfg.JDFile,
		CacheDir: cfg.JDCacheDir,
	})
	if err != nil {
		return nil, fmt.Errorf("pipeline: acquire jd: %w", err)
	}
	if len(posting.Requirements) == 0 {
		return nil, fmt.Errorf("pipeline: job posting has no requirements")
	}

	reqTexts := make([]string, len(posting.Requirements))
	for i, r := range posting.Requirements {
		reqTexts[i] = r.Text
	}
	reqVecs, err := emb.EmbedTexts(ctx, reqTexts)
	if err != nil {
		return nil, fmt.Errorf("pipeline: embed requirements: %w", err)
	}

	sel, err := retrieve.Select(ix, posting.Requirements, reqVecs, topK, cfg.MinScore)
	if err != nil {
		return nil, fmt.Errorf("pipeline: select candidates: %w", err)
	}

	if cfg.EmbedCachePath != "" {
		_ = emb.Cache().Save(cfg.EmbedCachePath) // best-effort: vectors already usable this run
	}

	return &Result{
		Store:        s,
		Posting:      posting,
		Requirements: posting.Requirements,
		Candidates:   sel.Documents,
		Gaps:         sel.Gaps,
	}, nil
}
