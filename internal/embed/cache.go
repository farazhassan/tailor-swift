package embed

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Cache is an on-disk store of embedding vectors keyed by a stable content key
// (an achievement ID). It is scoped to one embedding model: vectors from a
// different model are not comparable, so a model mismatch on load is treated as
// a cold cache.
type Cache struct {
	Model   string               `json:"model"`
	Vectors map[string][]float32 `json:"vectors"`
}

// NewCache returns an empty cache for the given model.
func NewCache(model string) *Cache {
	return &Cache{Model: model, Vectors: map[string][]float32{}}
}

// LoadCache reads a cache file. A missing file yields an empty cache (cold
// start, not an error). If the stored model differs from model, the on-disk
// vectors are discarded and an empty cache for model is returned.
func LoadCache(path, model string) (*Cache, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewCache(model), nil
	}
	if err != nil {
		return nil, fmt.Errorf("embed: read cache %s: %w", path, err)
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("embed: parse cache %s: %w", path, err)
	}
	if c.Model != model || c.Vectors == nil {
		return NewCache(model), nil
	}
	return &c, nil
}

// Get returns the cached vector for key and whether it was present.
func (c *Cache) Get(key string) ([]float32, bool) {
	v, ok := c.Vectors[key]
	return v, ok
}

// Put stores vec under key.
func (c *Cache) Put(key string, vec []float32) {
	c.Vectors[key] = vec
}

// Save writes the cache to path atomically (temp file + rename), creating
// parent directories as needed.
func (c *Cache) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("embed: create cache dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("embed: marshal cache: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("embed: write cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("embed: replace cache: %w", err)
	}
	return nil
}
