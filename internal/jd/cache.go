package jd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CacheKey returns the stable on-disk key for a JD URL: hex sha256.
func CacheKey(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

func cachePath(dir, url string) string {
	return filepath.Join(dir, CacheKey(url)+".json")
}

// LoadPosting reads a cached posting for url. A missing file is a cache miss
// (ok=false), not an error.
func LoadPosting(dir, url string) (*Posting, bool, error) {
	data, err := os.ReadFile(cachePath(dir, url))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("jd: read cache: %w", err)
	}
	var p Posting
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, false, fmt.Errorf("jd: parse cache: %w", err)
	}
	return &p, true, nil
}

// SavePosting writes p to the cache for p.URL, atomically (temp file + rename),
// creating the cache directory as needed.
func SavePosting(dir string, p *Posting) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("jd: create cache dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("jd: marshal posting: %w", err)
	}
	path := cachePath(dir, p.URL)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("jd: write cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("jd: replace cache: %w", err)
	}
	return nil
}
