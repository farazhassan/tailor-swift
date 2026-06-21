# tailor-swift

A resume-tailoring CLI. Given a content store of your achievements and a job
posting URL, it retrieves the most relevant achievements, generates a tailored
LaTeX resume, and refines it through a generate↔evaluate loop until it passes a
quality bar (or runs out of iterations).

Built on the [gantry](https://github.com/farazhassan/gantry) library. CLI only.

## Commands

```
tailor <command> [flags]

  generate    generate a tailored resume for a job description
  validate    parse a content store file and print a summary
  ingest      (not implemented)
  evaluate    (not implemented)
```

## Build

```bash
go build -o tailor ./cmd/tailor
```

Or run without building: `go run ./cmd/tailor <command> [flags]`.

---

## End-to-end testing

The unit suite (`go test ./...`) runs entirely against fakes and mock LLMs — no
keys or network. The steps below are for exercising the **real** `generate`
pipeline against live services.

### Prerequisites

1. **API keys** (exported in your shell):

   ```bash
   export VOYAGE_API_KEY=...
   # the generation key depends on --provider (default openrouter):
   export OPENROUTER_API_KEY=sk-or-...   # --provider openrouter (default)
   export ANTHROPIC_API_KEY=sk-ant-...   # --provider anthropic
   # optional, defaults shown:
   export VOYAGE_MODEL=voyage-3
   ```

   - Missing `VOYAGE_API_KEY` → the command exits 1 (the Voyage client is built
     first).
   - Missing the provider's key → exits 1 when that LLM client is built:
     `OPENROUTER_API_KEY` for the default `openrouter` provider, or
     `ANTHROPIC_API_KEY` when you pass `--provider anthropic`.

2. **`pdflatex`** on your `PATH` (from a TeX distribution such as MacTeX or
   TeX Live). Without it, compilation fails and `resume.pdf` is skipped (the run
   still emits `resume.tex`, `critique.json`, and `run.log`).

   ```bash
   pdflatex --version   # verify it's installed
   ```

3. **A content store** — a markdown file describing your background. Minimal
   shape (see `internal/store/testdata/sample.md` for a fuller example):

   ```markdown
   # Jane Doe

   ## Contact
   Email: jane@example.com

   ## Acme — Senior Engineer (2021 – present)

   ### Billing platform revamp  `#go #payments`
   - Cut billing settlement latency 40% by sharding the ledger `#performance`
   - Migrated 200 services off the monolith `#go`

   ## Skills
   Go (expert), Kafka, Postgres
   ```

   You can sanity-check it before running the full pipeline:

   ```bash
   ./tailor validate path/to/content.md
   ```

### Run the pipeline

```bash
./tailor generate \
  --content path/to/content.md \
  --jd-url https://example.com/jobs/senior-go-engineer
```

This uses the default `openrouter` provider (requires `OPENROUTER_API_KEY`).
To go through Anthropic directly instead:

```bash
./tailor generate \
  --content path/to/content.md \
  --jd-url https://example.com/jobs/senior-go-engineer \
  --provider anthropic
```

Model ids are provider-specific. OpenRouter uses namespaced ids
(`anthropic/claude-sonnet-4-6`, `openai/gpt-4o`); Anthropic uses bare ids
(`claude-sonnet-4-6`). Leave `--model` unset to get the provider's default.

To avoid live JD fetching (or when the posting is behind auth), supply the text
locally — the URL is still required as the cache key:

```bash
./tailor generate \
  --content path/to/content.md \
  --jd-url https://example.com/jobs/senior-go-engineer \
  --jd-file path/to/jd.txt
```

### Flags

| Flag               | Default              | Purpose                                    |
| ------------------ | -------------------- | ------------------------------------------ |
| `--content`        | _(required)_         | Content store markdown file                |
| `--jd-url`         | _(required)_         | Job posting URL (also the JD cache key)    |
| `--jd-file`        | _(none)_             | Local JD text (URL still required)         |
| `--provider`       | `openrouter`         | LLM provider: `anthropic` or `openrouter`  |
| `--model`          | _(provider default)_ | Model id (provider-specific; empty = default) |
| `--out`            | `out`                | Base output directory                      |
| `--template`       | _(built-in)_         | LaTeX template override                    |
| `--max-iterations` | `3`                  | Max refinement iterations                  |
| `--top-k`          | `8`                  | Candidate achievements per requirement     |
| `--min-score`      | `0`                  | Min similarity for a must-have requirement |
| `--embed-cache`    | _(disabled)_         | Embedding cache file (speeds up reruns)    |
| `--jd-cache`       | _(none)_             | Cached postings directory                  |

### Output

Artifacts land in `out/<slug>-<YYYY-MM-DD>/`, where `<slug>` is derived from the
JD URL. Re-running on the same day overwrites the directory.

```
out/senior-go-engineer-2026-06-19/
├── resume.tex        # the tailored LaTeX source (always written)
├── resume.pdf        # compiled PDF (skipped if pdflatex failed)
├── critique.json     # final evaluation: scores, pass/fail, summary
└── run.log           # per-iteration trace, coverage gaps, stop reason
```

### Exit codes

| Code | Meaning                                                            |
| ---- | ----------------------------------------------------------------- |
| `0`  | Resume produced **and** passed the quality bar                    |
| `3`  | Resume produced but did **not** pass (artifacts still written)    |
| `1`  | Fatal error (missing key, bad content, acquisition/loop failure)  |
| `2`  | Usage error (missing/invalid flags)                               |

### What to check

- Exit code matches expectation (`echo $?` after the run).
- All four artifacts exist; open `resume.pdf` and eyeball the result.
- `critique.json` `"pass"` and `"composite"` reflect quality; `"truthful"` is
  `true` (the evaluator flags fabricated claims).
- `run.log` lists any must-have requirements with no matching achievement under
  `gaps:` — these are also warned to stderr.

### Tips

- Pass `--embed-cache cache/embed.json` and `--jd-cache cache/jd` to make
  reruns cheap (embeddings and the parsed posting are reused). Both `cache/` and
  `out/` are gitignored.
- Use `--max-iterations 1` for a fast smoke test that costs fewer tokens.
- Use `--jd-file` to keep e2e runs deterministic and offline-ish (no live fetch).
