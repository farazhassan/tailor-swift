# Resume Tailoring Agent — Design

**Date:** 2026-06-17
**Status:** Approved (design phase)
**Project:** `tailor-swift`

## Summary

A Go CLI tool (`tailor`) that generates a job-tailored resume from a personal
content store, a chosen LaTeX template, and a target job description (JD) fetched
from a URL. A closed generate→evaluate loop refines the resume until an evaluator
score clears a threshold or an iteration cap is hit, then emits a compiled PDF plus
a critique report.

Built on the [gantry](https://github.com/farazhassan/gantry) Go agent runtime.
Every LLM interaction goes through gantry components. Where gantry lacks something
we need (Voyage embeddings), we contribute it upstream and depend on it rather than
reimplementing locally.

## Goals

- One command turns "JD URL + template" into a tailored, compiled PDF resume.
- Content (facts) is separated from presentation (LaTeX template).
- The evaluator's feedback automatically improves the output across iterations.
- No fabrication: every generated bullet traces to a real claim in the content store.
- Reusable: same JD is never re-fetched or re-embedded; unchanged content is never re-embedded.
- Fully testable without an API key (gantry mock LLM).

## Non-Goals

- No web/GUI interface. CLI only.
- No multi-user / hosted service.
- No ATS scraping beyond reading the JD text from a URL.

## Key Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Loop shape | Closed agentic loop, max 3 iterations, final report always emitted | Hands-off refinement + visibility into why it converged |
| Content model | Lightly-structured markdown content store; `.tex` = pure presentation | Atomic, addressable units for retrieval + truthfulness; markdown is low-friction to author/review |
| LLM provider | Anthropic via gantry `components/llm/anthropic/`; strong model generates, cheaper model evaluates | Best rewriting + structured critique; separate evaluator reduces self-grading bias and cost |
| Embeddings | Voyage AI (new gantry component, contributed upstream) | Anthropic has no embeddings API; Voyage is Anthropic's recommended partner |
| Vector store | Local on-disk cosine index | Personal store is small; no external service needed |
| JD ingestion | Fetched up front (deterministic), cached on disk by URL hash | JD is always-required input; no LLM decision needed |
| Output | Compile to PDF with compile-error repair loop | LLM-generated LaTeX occasionally breaks; repair keeps it hands-off |
| Evaluator | gantry `components/critic/` with weighted rubric + truthfulness hard-gate | Already wired into the loop and tested |
| Orchestration | Thin Go orchestrator over focused gantry calls (Approach 2) | Isolated, testable units; explicit loop matches gantry's "see what runs" philosophy |
| Ingest | LLM-assisted extraction of base resume(s) → `content.md`, human-reviewed | Fast bootstrap; review step protects the truthfulness ground truth |

## Architecture

```
cmd/tailor/            CLI entrypoint (ingest | generate | evaluate)
internal/
  store/               parse/validate/query the markdown content store into units
  jd/                  fetch + extract + cache job descriptions (keyed by URL hash)
  embed/               Voyage embedding wrapper + on-disk vector index (cosine)
  retrieve/            top-k content units for a JD (gantry retriever interface)
  ingest/              LLM-assisted base-resume -> structured units extraction
  generate/            generator: gantry agent (Anthropic) -> selection + rephrased bullets
  render/              units + selection -> .tex via template; pdflatex compile + repair
  evaluate/            evaluator: gantry critic (separate model) -> score + critique
  orchestrate/         the generate<->evaluate loop, stop conditions, report emission
templates/             user LaTeX templates (.tex with placeholders)
cache/                 jd/ + embeddings/ (gitignored)
out/                   generated artifacts (gitignored)
```

**gantry dependencies:** `generate/` uses `components/llm/anthropic/`; `evaluate/`
uses `components/critic/` + anthropic; `retrieve/` uses `components/retriever/`;
`orchestrate/` uses `components/limiter/` (budget cap). `embed/` depends on a new
`components/embeddings/voyage/` contributed to gantry upstream.

Each unit has one job, a clear interface, and is testable with a mock LLM.

## Data Model & Storage

### Content store — `content.md` (ground truth)

Lightly-structured markdown. The parser derives atomic units from structure:

```markdown
## Acme — Senior Engineer (2021-03 – present)

### Billing platform revamp  `#go #payments #distributed-systems`
- Cut billing settlement latency 40% by … `#performance #kafka`
- Migrated 200 services to … `#go`

## Skills
Go (expert), Kafka, Postgres …
```

Parsing conventions:
- `##` heading = a **role** (company — title (dates)).
- `###` heading = a **project** under the current role.
- `-` bullet = an **achievement** — the atomic unit (own embedding + provenance).
- Inline `` `#tag` `` tokens = tags on the nearest project/achievement (optional).
- A `## Skills` section lists skills.

Each achievement carries **provenance** = content-store file path + line number. A
stable unit id is derived from the bullet (content hash), so editing a bullet
re-embeds only that unit.

The generator may **rephrase** an achievement for a JD but may never emit a bullet
that does not reference a real unit id. This is enforced by the truthfulness gate.

### On-disk layout

```
content.md                              # ground truth (ingest writes it, user reviews)
cache/jd/<sha256(url)>.json             # {url, fetched_at, raw_text, extracted_requirements[]}
cache/embeddings/content_<hash>.json    # vec per achievement id; hash = content fingerprint
cache/embeddings/jd_<sha256>.json       # vec per JD requirement chunk
out/<job-slug>-<date>/                  # resume.tex, resume.pdf, critique.json, run.log
```

Embedding caches are keyed by a content fingerprint, so re-embedding happens only
when a bullet's text changes or a new JD appears. The same JD is never re-fetched.

## Pipeline (the `generate` flow)

The outer orchestrator owns iteration. Generator and evaluator are the only LLM
stages; each is a gantry agent. A gantry `limiter` (budget cap) wraps LLM calls so a
runaway loop cannot burn unbounded tokens.

**Pre-loop (deterministic, once):**
1. **Load & parse** `content.md` → in-memory units (roles, projects, achievements w/ provenance, skills).
2. **JD acquire** — cache hit? use it. Else fetch URL → extract text → LLM splits into discrete requirements/skills → cache.
3. **Embed** — ensure content unit vectors (Voyage) cached (re-embed only changed bullets); embed JD requirement chunks (cached per JD).
4. **Retrieve** — cosine top-k achievements + must-have skill coverage → candidate content set.

**Loop (max 3 iterations):**
5. **Generate** (gantry agent, Anthropic, strong model) — given JD + retrieved units + prior critique, produce a structured *selection + rephrased bullets*. Every bullet references a real unit id.
6. **Render** — fill chosen `.tex` template → `pdflatex`. On LaTeX error → repair sub-step (feed error back, fix escaping) up to a small cap.
7. **Evaluate** (gantry `critic`, separate cheaper model) — score on the rubric + per-dimension critique.
8. **Decide** — score ≥ 0.85 **and** truthfulness passed → done. Else feed critique to step 5 and loop. Cap hit → emit best-scoring iteration.

**Post-loop:**
9. **Emit** `out/<job-slug>-<date>/`: `resume.pdf`, `resume.tex`, `critique.json` (final + per-iteration history), `run.log`.

## Evaluator Rubric

Each dimension scored 0–1, weighted into a composite:

- **JD coverage** — JD's key requirements/skills represented by real content (penalize missing must-haves).
- **Relevance / signal-to-noise** — included content is relevant, not filler.
- **Evidence quality** — bullets quantified / impact-oriented vs. vague.
- **Truthfulness / no fabrication** — every claim traces to a real unit. **Hard gate**: failure = automatic iteration rejection regardless of other scores.
- **Format / ATS sanity** — length, section completeness, compiles cleanly.

Stop condition: composite ≥ 0.85 **and** truthfulness gate passed, or iteration cap (3).

## CLI Surface

- **`tailor ingest`** — LLM-assisted: parse base resume(s) + notes → write `content.md` for user review → embed each unit (Voyage) into the local index. Re-run when achievements change.
- **`tailor generate --jd-url <url> --template <name> [--out dir] [--jd-file <path>]`** — main flow (steps 1–9). `--jd-file` is the manual fallback when a URL can't be fetched.
- **`tailor evaluate --resume <file> --jd-url <url>`** — steps 1–2 (load content for provenance) + 7 + emit report only.

## Error Handling & Edge Cases

- **JD fetch fails / paywalled / JS-heavy** → error with the URL; suggest `--jd-file` fallback. Never proceed without a JD.
- **Voyage API error** → fail with a clear message (embeddings required). Cached vectors mean transient failures only affect new content/JDs.
- **LaTeX compile fails after repair cap** → keep best `.tex`, emit it with the compile error in `run.log`, exit non-zero.
- **Truthfulness gate trips** → reject that iteration; critique tells generator to drop/replace the unsupported bullet. If all iterations fail → emit highest-truthful-scoring attempt + loud warning.
- **No relevant content for a must-have skill** → surface a coverage-gap warning in the report rather than letting the LLM fabricate.
- **Budget cap hit** (limiter) → stop, emit best-so-far.

## Testing Strategy

- Every LLM stage tested with gantry's **mock LLM** — no API key in CI (generator, evaluator, ingest).
- `store/` parser: table tests for markdown → units (headings, bullets, tags, provenance line numbers, malformed input).
- `embed/` index: cosine math + cache hit/miss with fake vectors.
- `render/`: golden-file test (units + template → expected `.tex`); real `pdflatex` compile test gated behind a build tag so LaTeX-less CI still passes.
- `orchestrate/`: loop logic (threshold met, cap hit, truthfulness fail, best-iteration selection) with scripted mock generator + critic — highest-value tests.
- New gantry **Voyage embeddings** provider ships with a conformance test matching gantry's existing embedding-provider pattern (contributed upstream).

## Open Items / Sequencing Notes

- **Upstream first:** the Voyage embeddings provider must land in gantry (or a fork pinned via `go.mod replace` as an interim) before `embed/` can compile against it.
- **Compliance:** wiring a live Anthropic and Voyage API key is new SDK/API-key setup; per League policy this routes through vendor procurement before running against live keys. Does not block design or mock-based development.
- Environment: Go 1.26.4, `pdflatex` present locally, `tectonic` absent.
```
