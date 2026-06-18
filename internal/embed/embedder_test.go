package embed

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/farazhassan/tailor-swift/internal/store"
)

// fakeEmb records how many times Embed is called and returns a deterministic
// vector per input so tests can assert cache behavior without a network.
type fakeEmb struct {
	calls  int
	inputs [][]string
}

func (f *fakeEmb) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.calls++
	f.inputs = append(f.inputs, append([]string(nil), texts...))
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{float32(len(t))}
	}
	return out, nil
}

func twoAchStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.ParseReader([]byte("## Acme — Eng\n### P\n- alpha\n- beta\n"), "mem")
	if err != nil {
		t.Fatalf("ParseReader: %v", err)
	}
	if got := len(s.Achievements()); got != 2 {
		t.Fatalf("fixture has %d achievements, want 2", got)
	}
	return s
}

func TestEmbedStoreEmbedsEachAchievementOnce(t *testing.T) {
	s := twoAchStore(t)
	f := &fakeEmb{}
	e := NewEmbedder(f, NewCache("voyage-3"))

	got, err := e.EmbedStore(context.Background(), s)
	if err != nil {
		t.Fatalf("EmbedStore: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d vectors, want 2", len(got))
	}
	if f.calls != 1 {
		t.Errorf("provider calls = %d, want 1", f.calls)
	}
	for _, a := range s.Achievements() {
		if _, ok := got[a.ID]; !ok {
			t.Errorf("missing vector for %s", a.ID)
		}
	}
}

func TestEmbedStoreUsesCacheOnSecondCall(t *testing.T) {
	s := twoAchStore(t)
	f := &fakeEmb{}
	e := NewEmbedder(f, NewCache("voyage-3"))

	if _, err := e.EmbedStore(context.Background(), s); err != nil {
		t.Fatalf("first EmbedStore: %v", err)
	}
	if _, err := e.EmbedStore(context.Background(), s); err != nil {
		t.Fatalf("second EmbedStore: %v", err)
	}
	if f.calls != 1 {
		t.Errorf("provider calls = %d after warm cache, want 1", f.calls)
	}
}

func TestEmbedStorePersistedCacheSkipsProvider(t *testing.T) {
	s := twoAchStore(t)
	path := filepath.Join(t.TempDir(), "cache.json")

	first := NewEmbedder(&fakeEmb{}, NewCache("voyage-3"))
	if _, err := first.EmbedStore(context.Background(), s); err != nil {
		t.Fatalf("EmbedStore: %v", err)
	}
	if err := first.Cache().Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadCache(path, "voyage-3")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	f2 := &fakeEmb{}
	second := NewEmbedder(f2, loaded)
	if _, err := second.EmbedStore(context.Background(), s); err != nil {
		t.Fatalf("second EmbedStore: %v", err)
	}
	if f2.calls != 0 {
		t.Errorf("provider calls = %d with persisted cache, want 0", f2.calls)
	}
}

// TestEmbedStorePartiallyWarmCache pre-warms the cache with one achievement and
// asserts the provider receives only the misses, in order — the path where an
// index-alignment bug between miss IDs and miss text would surface.
func TestEmbedStorePartiallyWarmCache(t *testing.T) {
	s, err := store.ParseReader([]byte("## Acme — Eng\n### P\n- alpha\n- beta\n- gamma\n"), "mem")
	if err != nil {
		t.Fatalf("ParseReader: %v", err)
	}
	betaID := store.DeriveID("beta")
	sentinel := []float32{42}

	cache := NewCache("voyage-3")
	cache.Put(betaID, sentinel)
	f := &fakeEmb{}
	e := NewEmbedder(f, cache)

	got, err := e.EmbedStore(context.Background(), s)
	if err != nil {
		t.Fatalf("EmbedStore: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", f.calls)
	}
	want := []string{"alpha", "gamma"}
	if len(f.inputs[0]) != len(want) || f.inputs[0][0] != want[0] || f.inputs[0][1] != want[1] {
		t.Errorf("provider input = %v, want %v (misses only, in order)", f.inputs[0], want)
	}
	if v, ok := got[betaID]; !ok || len(v) != 1 || v[0] != sentinel[0] {
		t.Errorf("beta vector = %v, %v, want cached sentinel %v", v, ok, sentinel)
	}
	if len(got) != 3 {
		t.Errorf("got %d vectors, want 3", len(got))
	}
}
