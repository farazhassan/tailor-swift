package jd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/farazhassan/gantry"
)

// Options configures a single Acquire call.
type Options struct {
	URL        string       // required: the JD URL, also the cache key
	FilePath   string       // optional: read JD text from this file instead of fetching (--jd-file fallback)
	CacheDir   string       // directory for cached postings
	HTTPClient *http.Client // optional; nil uses http.DefaultClient
}

// Acquire returns the parsed job posting for opts.URL. On a cache hit it returns
// the cached Posting without fetching or calling the LLM. On a miss it obtains
// the raw text (from opts.FilePath if set, otherwise by fetching opts.URL and
// extracting text from HTML), splits it into requirements via the LLM, caches
// the result, and returns it.
func Acquire(ctx context.Context, llm gantry.LLMClient, opts Options) (*Posting, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("jd: URL is required")
	}
	if p, ok, err := LoadPosting(opts.CacheDir, opts.URL); err != nil {
		return nil, err
	} else if ok {
		return p, nil
	}

	var raw string
	if opts.FilePath != "" {
		data, err := os.ReadFile(opts.FilePath)
		if err != nil {
			return nil, fmt.Errorf("jd: read jd file: %w", err)
		}
		raw = string(data)
	} else {
		body, err := Fetch(ctx, opts.HTTPClient, opts.URL)
		if err != nil {
			return nil, err
		}
		raw, err = ExtractText(body)
		if err != nil {
			return nil, err
		}
	}

	reqs, err := ExtractRequirements(ctx, llm, raw)
	if err != nil {
		return nil, err
	}

	p := &Posting{
		URL:          opts.URL,
		FetchedAt:    time.Now().UTC(),
		RawText:      raw,
		Requirements: reqs,
	}
	if err := SavePosting(opts.CacheDir, p); err != nil {
		return nil, err
	}
	return p, nil
}
