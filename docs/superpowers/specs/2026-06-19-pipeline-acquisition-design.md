# Pipeline Acquisition Design (Plan 10a)

**Status:** Approved (design)
**Date:** 2026-06-19
**Author:** fhassan@league.com

## Context

`orchestrate.Run(ctx, Config, Deps)` already drives the bounded
generate→render→compile→evaluate loop, but it deliberately takes *already-retrieved*
inputs: a `*store.Store`, the JD `[]jd.Requirement`, and the candidate
`[]gantry.Document`. Something has to produce those inputs. That "pre-loop
acquisition" is this project.

This is the first of two sub-projects that together make a working `tailor generate`
command:

- **Plan 10a (this spec):** `internal/pipeline` — turn a content file + a JD source
  into the inputs `orchestrate.Run` needs.
- **Plan 10b (later):** the `tailor generate` CLI command — parse flags, construct the
  real Anthropic/Voyage clients and the `pdflatex` compiler, call
  `pipeline.Acquire` then `orchestrate.Run`, emit artifacts (`out/<slug>-<date>/`),
  and choose the process exit code.

## Goal

A new `internal/pipeline` package with a single entry point that chains the existing
`store`, `embed`, `retrieve`, and `jd` packages into one acquisition step, returning
exactly what `orchestrate.Config` consumes — plus the coverage gaps and the raw
posting for downstream visibility. Fully unit-testable with injected clients (no API
keys, no network, no `pdflatex`).

## Non-goals (deferred to Plan 10b)

- The `tailor generate` CLI command, flag parsing, and exit codes.
- Constructing the real `anthropic.New(...)` LLM client, the Voyage embedder, and the
  `render.PDFLaTeX` compiler.
- Calling `orchestrate.Run` and emitting artifacts (`resume.pdf`, `resume.tex`,
  `critique.json`, `run.log`).

## Architecture

One package, `internal/pipeline`, with one file `pipeline.go` (plus
`pipeline_test.go`). A single exported function `Acquire` runs the whole chain
internally; unexported helpers keep each stage focused. This mirrors
`orchestrate.Run`'s `Config`/`Deps`/`Result` shape so Plan 10b just constructs the
real clients and calls it.

### Types

```go
// Config is the static input to an acquisition (the CLI fills it from flags).
type Config struct {
    ContentPath    string  // path to the content store markdown (store.Parse)
    JDURL          string  // job posting URL (required: cache key + posting identity)
    JDFile         string  // optional local JD text file (offline source; URL still required as cache key)
    Model          string  // embedding model id for cache scoping (default "voyage-3")
    TopK           int     // top-K candidates per requirement (default 8 when 0)
    MinScore       float64 // min similarity for a must-have to count as covered (default 0.0)
    EmbedCachePath string  // on-disk embedding cache file; "" disables caching
    JDCacheDir     string  // dir for cached postings (passed to jd.Options.CacheDir)
}

// Deps are the injected collaborators: production passes real clients; tests pass mocks.
type Deps struct {
    LLM      gantry.LLMClient      // for jd requirement extraction
    Embedder embeddings.Embeddings // Voyage in production; a fake in tests
}

// Result is exactly what orchestrate.Config needs, plus visibility extras.
type Result struct {
    Store        *store.Store      // parsed content store        (→ orchestrate Config.Store)
    Posting      *jd.Posting       // the acquired JD (URL, raw text, requirements)
    Requirements []jd.Requirement  // == Posting.Requirements     (→ orchestrate Config.Requirements)
    Candidates   []gantry.Document // retrieved, ranked, deduped  (→ orchestrate Config.Candidates)
    Gaps         []jd.Requirement  // must-haves with no match above MinScore (warn, non-fatal)
}

func Acquire(ctx context.Context, cfg Config, deps Deps) (*Result, error)
```

`Result.{Store,Requirements,Candidates}` map 1:1 onto `orchestrate.Config`. `Posting`
is carried for the JD URL / raw text (Plan 10b uses it for the output-dir slug and
`run.log`). `Gaps` is surfaced as a non-fatal warning.

## Data flow inside `Acquire`

```
1. Apply defaults: Model ← "voyage-3" if ""; TopK ← 8 if 0. (MinScore left at 0.0.)
2. Validate: JDURL is set (it is jd.Acquire's mandatory cache key), else fatal config error.
3. store.Parse(cfg.ContentPath)                          → *store.Store
4. Build the embedder with a model-scoped cache:
     cache := embed.NewCache(cfg.Model)                  // or embed.LoadCache(EmbedCachePath, Model) if path set
     emb   := embed.NewEmbedder(deps.Embedder, cache)
5. vectors := emb.EmbedStore(ctx, store)                 → map[achievementID][]float32
6. ix := retrieve.NewIndex(store, vectors)               → *retrieve.Index
7. posting := jd.Acquire(ctx, deps.LLM, jd.Options{
        URL: cfg.JDURL, FilePath: cfg.JDFile, CacheDir: cfg.JDCacheDir,
   })                                                     → *jd.Posting
8. If posting.Requirements is empty → fatal error (generating from nothing is useless).
9. reqTexts := texts of posting.Requirements
   reqVecs  := emb.EmbedTexts(ctx, reqTexts)             → [][]float32 (same length/order)
10. sel := retrieve.Select(ix, posting.Requirements, reqVecs, cfg.TopK, cfg.MinScore)  → *retrieve.Selection
11. If EmbedCachePath set: emb.Cache().Save(EmbedCachePath)  // best-effort; save error swallowed
12. return &Result{Store, Posting, posting.Requirements, sel.Documents, sel.Gaps}, nil
```

### Why `Config.Model`

`embed.NewCache(model)` / `embed.LoadCache(path, model)` are model-scoped: a cache
records its model and is treated as empty on mismatch (so vectors from a different
model are never reused). The model string lives inside the Voyage client and is *not*
exposed on the `embeddings.Embeddings` interface (`Embed(ctx, texts) ([][]float32, error)`).
Adding `Config.Model` (default `"voyage-3"`, matching `embed.NewVoyageClient`) lets the
pipeline and the embedder client agree on the cache scope. Plan 10b sets it alongside
constructing the Voyage client.

## Error handling

Each stage wraps its error with a `pipeline:` prefix and returns immediately; no
partial `Result` is returned.

**Fatal (return error):**
- `store.Parse` failure (bad/missing content file).
- Config: `JDURL` empty (`pipeline: JD URL is required`).
- `jd.Acquire` failure (fetch error, unparseable JD, LLM extraction error).
- Empty `posting.Requirements` (`pipeline: job posting has no requirements`).
- Any embedder error (`EmbedStore` / `EmbedTexts`).
- `retrieve.NewIndex` or `retrieve.Select` error.

**Non-fatal (carried in `Result`):**
- `sel.Gaps` — must-haves with no match above `MinScore`. Returned, not errored;
  Plan 10b warns and proceeds.

**Best-effort (swallowed):**
- Embedding cache save (step 11). The vectors are already computed and usable this
  run; a failed cache write must not fail an otherwise-successful acquisition, and the
  CLI can't do anything useful with the error.

**Edge cases:**
- *Empty candidate set with non-empty requirements* (nothing matched at all): not an
  error here — `Gaps` lists the unmet must-haves and `Candidates` may be empty. The
  downstream `generate` package already errors on zero candidates, so the CLI surfaces
  that; we don't double-guard.

## Testing strategy

All pure unit tests — no API keys, no network, no `pdflatex`. Two fakes:

- **`fakeEmbedder`** implements `embeddings.Embeddings` (single `Embed` method):
  returns a deterministic vector per input text, seeded so a requirement's vector is
  closest to its matching achievement's vector. Makes `retrieve.Select` rankings and
  gaps predictable.
- **gantry mock LLM** (`eval.NewMockLLMClient`) for `jd.Acquire`'s requirement
  extraction, scripted to return a JSON requirements list. JD source is a local temp
  file (`JDFile`) so there is no HTTP.

Test cases:

1. **Happy path** — content store + JD file with two requirements, both matching
   achievements → `Result` has the expected `Store`, `Requirements`, ranked
   `Candidates`, empty `Gaps`.
2. **Coverage gap** — a must-have whose fake vector matches nothing above `MinScore`
   → that requirement appears in `Result.Gaps`; acquisition still succeeds.
3. **Defaults applied** — `TopK=0` → behaves as `TopK=8` (store with >8 achievements;
   assert candidate cap/ranking).
4. **No requirements** — mock LLM returns an empty list → `Acquire` returns the
   "no requirements" error.
5. **No JD URL** — `JDURL` empty → config error (validated before any work).
6. **Embed cache round-trip** — set `EmbedCachePath` to a temp file; first `Acquire`
   writes it; a second run whose fake embedder panics-on-call still succeeds (proving
   cached vectors were loaded, not re-embedded), confirming the model-scoped cache
   wiring.
7. **store.Parse failure** — bad content path → fatal error.

## Dependencies (all already on `main`)

- `internal/store` — `Parse(path) (*Store, error)`, the `Store` type.
- `internal/embed` — `NewCache`, `LoadCache`, `NewEmbedder`, `(*Embedder).EmbedStore`,
  `(*Embedder).EmbedTexts`, `(*Embedder).Cache`, `(*Cache).Save`.
- `internal/retrieve` — `NewIndex(store, vectors)`, `Select(ix, reqs, reqVecs, k, minScore) (*Selection, error)`,
  `Selection{Documents, Gaps}`.
- `internal/jd` — `Acquire(ctx, llm, Options{URL, FilePath, CacheDir}) (*Posting, error)`,
  `Posting{URL, FetchedAt, RawText, Requirements}`, `Requirement{Text, MustHave}`.
- gantry — `gantry.LLMClient`, `gantry.Document`,
  `components/embeddings.Embeddings`, `eval.NewMockLLMClient` (tests).
