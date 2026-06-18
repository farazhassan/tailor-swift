package embed

import (
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "cache.json")

	c := NewCache("voyage-3")
	c.Put("ach_aaa", []float32{0.1, 0.2})
	c.Put("ach_bbb", []float32{0.3, 0.4})
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadCache(path, "voyage-3")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	v, ok := got.Get("ach_aaa")
	if !ok || len(v) != 2 || v[0] != 0.1 {
		t.Errorf("Get(ach_aaa) = %v, %v", v, ok)
	}
	if _, ok := got.Get("missing"); ok {
		t.Error("Get(missing) reported present")
	}
}

func TestLoadCacheMissingFileIsCold(t *testing.T) {
	dir := t.TempDir()
	c, err := LoadCache(filepath.Join(dir, "nope.json"), "voyage-3")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if c.Model != "voyage-3" || len(c.Vectors) != 0 {
		t.Errorf("cold cache = %+v, want empty voyage-3 cache", c)
	}
}

func TestLoadCacheModelMismatchIsCold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	old := NewCache("voyage-2")
	old.Put("ach_aaa", []float32{1})
	if err := old.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	c, err := LoadCache(path, "voyage-3")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if c.Model != "voyage-3" || len(c.Vectors) != 0 {
		t.Errorf("mismatch should yield empty voyage-3 cache, got %+v", c)
	}
}
