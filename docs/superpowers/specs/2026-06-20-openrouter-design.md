# OpenRouter Support — Design

**Date:** 2026-06-20
**Status:** Approved (design); pending spec review

## Goal

Let `tailor generate` use OpenRouter as its LLM backend instead of (or in
addition to) calling Anthropic directly, with OpenRouter as the default
provider. OpenRouter exposes an OpenAI-compatible API, so the work splits into
two phases across two repositories.

## Context

- tailor-swift consumes gantry via a published tag (`require
  github.com/farazhassan/gantry`, currently `v0.0.3-beta`, no `replace`
  directive). A new gantry client therefore requires a release before
  tailor-swift can import it.
- gantry already ships per-provider clients under `components/llm/`:
  `anthropic`, `openai`, `ollama`. Each is a self-contained package exposing
  `New(model string, ...Option) *Client` and implementing
  `gantry.StreamingLLMClient`.
- OpenRouter is **not** Anthropic-protocol. It is OpenAI-API-compatible:
  `POST {base}/v1/chat/completions`, `Authorization: Bearer <key>`, SSE
  streaming with `data: ` framing and a `[DONE]` sentinel. Model ids are
  namespaced (e.g. `anthropic/claude-sonnet-4-6`, `openai/gpt-4o`).

## Phase 1 — gantry `components/llm/openrouter`

A **standalone** package that does not depend on the `openai` package, but
mirrors its structure since the wire protocol is identical. (The `openai`
package's wire types are unexported, so a standalone client must carry its own
copy — duplication is accepted as the cost of independence.)

### Files (all new, under `components/llm/openrouter/`)

- `openrouter.go` — `Client` struct (`model`, `baseURL`, `apiKey`, `httpc
  *http.Client`); `Option` funcs `WithAPIKey`, `WithBaseURL` (trims trailing
  slash), `WithHTTPClient`; `New(model string, ...Option) *Client` that
  **panics** on empty model or a missing key; `Generate`,
  `GenerateStream`, `BaseURL()`. Compile-time assertion `var _
  gantry.StreamingLLMClient = (*Client)(nil)`.
- `wire.go` — private request/response structs and mapping helpers
  (`toChatRequest`, `toChatTools`, `assembleResponse`, `toToolCalls`,
  `stopReason`), copied from the `openai` package.
- `doc.go` — package documentation.
- `openrouter_test.go` — constructor behavior (panic on empty model, panic on
  missing key, reads key from env, applies options, default base URL).
- `chat_test.go` — non-streaming `Generate`: request/response mapping, tool
  calls preserving ids, `length` → `StopReasonMaxTokens`, non-2xx error
  surfaces status + body, assistant/tool message forwarding, context
  cancellation.
- `stream_test.go` — `GenerateStream`: delta aggregation, terminal usage chunk,
  tool-call fragment accumulation, yield-error propagation, context
  cancellation.
- `conformance_test.go` — `conformance.LLMClientSuite` and
  `conformance.StreamingLLMClientSuite`.

These mirror the existing `components/llm/openai/*` files; the test bodies are
adapted to assert the OpenRouter defaults.

### The only meaningful differences from `openai`

| Constant        | `openai`                   | `openrouter`                  |
| --------------- | -------------------------- | ----------------------------- |
| `defaultBaseURL`| `https://api.openai.com`   | `https://openrouter.ai/api`   |
| `apiKeyEnv`     | `OPENAI_API_KEY`           | `OPENROUTER_API_KEY`          |
| chat path       | `/v1/chat/completions`     | `/v1/chat/completions` (same) |
| auth header     | `Authorization: Bearer`    | same                          |

SSE handling, tool-call accumulation (keyed by index), usage mapping, and stop
reason translation are identical.

### Validation & release

- `go test -race ./...`, `go vet ./...`, `gofmt -l .` all clean.
- Conventional Commit message.
- Merge to gantry main, then tag **`v0.0.4-beta`**.

## Phase 2 — tailor-swift wiring

Sequenced **after** Phase 1's tag exists (otherwise tailor-swift cannot
compile).

- Bump `require github.com/farazhassan/gantry` to `v0.0.4-beta` (no `replace`).
- Add a `--provider` flag accepting `anthropic` | `openrouter`, **defaulting to
  `openrouter`**. An unknown value is a usage error (exit 2).
- Provider-aware client construction in `runGenerate`:
  - `openrouter` → `openrouter.New(model, ...)`, key from `OPENROUTER_API_KEY`.
  - `anthropic` → existing `newAnthropic(model)` path, key from
    `ANTHROPIC_API_KEY`.
  - Both wrap the constructor's panic into a clean exit-1 error, as the current
    anthropic path already does.
- **Provider-aware default model.** When `--model` is not set:
  - `openrouter` → `anthropic/claude-sonnet-4-6` (namespaced).
  - `anthropic` → `claude-sonnet-4-6` (unchanged).
  An explicitly passed `--model` is used verbatim for either provider. An empty
  explicit `--model` remains a usage error (exit 2), as today.
- Update `README.md`: document `--provider`, the `OPENROUTER_API_KEY` env var,
  and namespaced model ids; note OpenRouter is now the default.
- Add wiring tests covering: default provider is openrouter, provider-specific
  default model resolution, explicit `--model` override, unknown provider → 2.
- Push branch and open a PR (standing preference: always open a PR when work is
  done).

## Out of scope (YAGNI)

- No OpenRouter-specific headers (`HTTP-Referer`, `X-Title`) — not required for
  function.
- No provider auto-detection from model id.
- No changes to the embedding provider (Voyage stays as-is).
- No reuse/refactor of the `openai` package to share wire code — standalone was
  explicitly chosen.

## Risks

- **Wire duplication drift:** the copied wire code could diverge from `openai`'s
  over time. Accepted; the protocol is stable and both are conformance-tested.
- **Cross-repo ordering:** Phase 2 is blocked until `v0.0.4-beta` is tagged and
  fetchable. Handled by strict sequencing.
