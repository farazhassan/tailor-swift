# OpenRouter tailor-swift Wiring Implementation Plan (Phase 2 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `tailor generate` choose its LLM backend via a `--provider` flag (`anthropic` | `openrouter`), defaulting to `openrouter`, with provider-aware default model resolution.

**Architecture:** Add small pure helpers (`resolveProvider`, `defaultModel`, `newOpenRouter`, `newLLM`) to `cmd/tailor/generate.go`, then wire a `--provider` flag into `runGenerate`. The flag selects which gantry client to build (`openrouter.New` or the existing `anthropic.New`). The `--model` flag defaults to empty and resolves to a provider-specific default when unset. Pure helpers are unit-tested directly; the flag plumbing is tested via the existing `runGenerate` harness.

**Tech Stack:** Go 1.26, gantry `v0.0.4-beta` (`github.com/farazhassan/gantry/components/llm/openrouter` and `.../anthropic`), stdlib `flag`.

**Repo / working directory:** `/Users/fhassan-mac/Dev/tailor-swift`.

**PREREQUISITES (must be true before starting):**
1. The gantry Phase-1 plan is complete and `v0.0.4-beta` is tagged and pushed (see `docs/superpowers/plans/2026-06-20-openrouter-gantry-client.md`). Verify: `git ls-remote --tags https://github.com/farazhassan/gantry | grep v0.0.4-beta`.
2. The `generate` CLI is on `main`. It currently lives in PR #11 (`feat/generate-cli`). Either merge PR #11 first and branch from `main`, or branch this work from `feat/generate-cli`. The steps below assume `cmd/tailor/generate.go` exists with the structure documented in the spec (the `runGenerate`/`newAnthropic`/`genConfig` shape).

**Sandbox note:** `go`/`git` commands here frequently fail under the command sandbox ("operation not permitted" / network). Re-run the same command with `dangerouslyDisableSandbox: true` when that happens. Pre-authorized.

---

## File Structure

- Modify: `cmd/tailor/generate.go` — add `resolveProvider`, `defaultModel`, `newOpenRouter`, `newLLM`; change `--model` default to empty; add `--provider` flag; resolve provider + default model in `runGenerate`; update `generateUsage`.
- Modify: `cmd/tailor/generate_test.go` — add unit tests for the new helpers; add a `runGenerate` unknown-provider test; replace the now-obsolete empty-model test.
- Modify: `go.mod` / `go.sum` — bump gantry to `v0.0.4-beta`.
- Modify: `README.md` — document `--provider`, `OPENROUTER_API_KEY`, namespaced model ids, default provider.

---

## Task 1: Bump gantry to v0.0.4-beta

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Create a feature branch**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`). If PR #11 is merged, branch from `main`; otherwise branch from `feat/generate-cli`:
```bash
git fetch origin
git checkout feat/generate-cli 2>/dev/null || git checkout main
git checkout -b feat/openrouter-provider
```
Expected: now on `feat/openrouter-provider`.

- [ ] **Step 2: Bump the dependency**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
go get github.com/farazhassan/gantry@v0.0.4-beta && go mod tidy
```
Expected: `go.mod` now shows `require github.com/farazhassan/gantry v0.0.4-beta`. (If the module proxy is sandboxed, re-run with `dangerouslyDisableSandbox: true`.)

- [ ] **Step 3: Verify it still builds and tests pass**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
go build ./... && go test ./...
```
Expected: build succeeds; all packages pass (unchanged behavior — this is just the version bump). (Re-run with `dangerouslyDisableSandbox: true` if sandboxed.)

- [ ] **Step 4: Commit**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
git add go.mod go.sum
git commit -m "$(cat <<'EOF'
chore(deps): bump gantry to v0.0.4-beta for openrouter client

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add provider helpers (pure, unit-tested)

**Files:**
- Modify: `cmd/tailor/generate.go`
- Test: `cmd/tailor/generate_test.go`

- [ ] **Step 1: Write failing tests for `resolveProvider` and `defaultModel`**

Add to `cmd/tailor/generate_test.go`:
```go
func TestResolveProvider(t *testing.T) {
	for _, p := range []string{"anthropic", "openrouter"} {
		got, err := resolveProvider(p)
		if err != nil {
			t.Errorf("resolveProvider(%q) error = %v, want nil", p, err)
		}
		if got != p {
			t.Errorf("resolveProvider(%q) = %q, want %q", p, got, p)
		}
	}
	if _, err := resolveProvider("bogus"); err == nil {
		t.Error("resolveProvider(\"bogus\"): want error, got nil")
	}
}

func TestDefaultModel(t *testing.T) {
	if got := defaultModel("anthropic"); got != "claude-sonnet-4-6" {
		t.Errorf("defaultModel(anthropic) = %q, want claude-sonnet-4-6", got)
	}
	if got := defaultModel("openrouter"); got != "anthropic/claude-sonnet-4-6" {
		t.Errorf("defaultModel(openrouter) = %q, want anthropic/claude-sonnet-4-6", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
go test ./cmd/tailor/ -run 'TestResolveProvider|TestDefaultModel' 2>&1 | tail
```
Expected: compile failure — `undefined: resolveProvider`, `undefined: defaultModel`.

- [ ] **Step 3: Implement the helpers**

In `cmd/tailor/generate.go`, add the `openrouter` import and the helpers. Add to the import block (alongside the existing `.../components/llm/anthropic` line):
```go
	"github.com/farazhassan/gantry/components/llm/openrouter"
```

Add these functions (place them just after `newAnthropic`):
```go
// resolveProvider validates the --provider flag value.
func resolveProvider(p string) (string, error) {
	switch p {
	case "anthropic", "openrouter":
		return p, nil
	default:
		return "", fmt.Errorf("unknown provider %q (want anthropic or openrouter)", p)
	}
}

// defaultModel returns the default model id for a provider. OpenRouter ids are
// namespaced by upstream provider; Anthropic ids are not.
func defaultModel(provider string) string {
	switch provider {
	case "openrouter":
		return "anthropic/claude-sonnet-4-6"
	default: // anthropic
		return "claude-sonnet-4-6"
	}
}

// newOpenRouter constructs the OpenRouter client, converting its panic (missing
// key or empty model) into an error so the command exits cleanly.
func newOpenRouter(model string) (llm gantry.LLMClient, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return openrouter.New(model), nil
}

// newLLM builds the LLM client for the chosen provider.
func newLLM(provider, model string) (gantry.LLMClient, error) {
	switch provider {
	case "openrouter":
		return newOpenRouter(model)
	default: // anthropic
		return newAnthropic(model)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
go test ./cmd/tailor/ -run 'TestResolveProvider|TestDefaultModel' -v 2>&1 | tail
```
Expected: `TestResolveProvider` and `TestDefaultModel` PASS.

- [ ] **Step 5: Commit**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
git add cmd/tailor/generate.go cmd/tailor/generate_test.go
git commit -m "$(cat <<'EOF'
feat(generate): add provider resolution helpers

resolveProvider/defaultModel/newOpenRouter/newLLM select and construct the
LLM backend. Pure helpers unit-tested; wiring follows.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Wire the `--provider` flag into `runGenerate`

**Files:**
- Modify: `cmd/tailor/generate.go`
- Test: `cmd/tailor/generate_test.go`

- [ ] **Step 1: Write the failing test for unknown provider**

Add to `cmd/tailor/generate_test.go`:
```go
func TestRunGenerateUnknownProvider(t *testing.T) {
	var out, errb bytes.Buffer
	code := runGenerate([]string{
		"--content", "x.md", "--jd-url", "https://e.com/j", "--provider", "bogus",
	}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "unknown provider") {
		t.Fatalf("stderr = %q, want unknown provider", errb.String())
	}
}
```

NOTE: this test file already imports `bytes` and `strings` (used by the existing `runGenerate` tests). If not, add them to the import block.

- [ ] **Step 2: Run the test to verify it fails**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
go test ./cmd/tailor/ -run TestRunGenerateUnknownProvider 2>&1 | tail
```
Expected: FAIL — `--provider` is not yet a flag, so parsing errors with `flag provided but not defined: -provider` (exit 2 but stderr won't contain "unknown provider"), or the assertion on stderr fails.

- [ ] **Step 3: Add the flag, provider validation, and default-model resolution**

In `cmd/tailor/generate.go`, inside `runGenerate`:

(a) Change the `--model` flag default from `"claude-sonnet-4-6"` to empty, and add a `--provider` flag. Replace this line:
```go
	model := fs.String("model", "claude-sonnet-4-6", "Anthropic model id")
```
with:
```go
	model := fs.String("model", "", "model id (default: provider-specific)")
	provider := fs.String("provider", "openrouter", "LLM provider: anthropic or openrouter")
```

(b) Replace the existing empty-model guard:
```go
	if *model == "" {
		fmt.Fprintln(stderr, "generate: --model must not be empty")
		return 2
	}
```
with provider validation + default-model resolution:
```go
	prov, err := resolveProvider(*provider)
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 2
	}
	if *model == "" {
		*model = defaultModel(prov)
	}
```

(c) Replace the Anthropic-specific client construction:
```go
	llm, err := newAnthropic(*model)
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
```
with provider-aware construction:
```go
	llm, err := newLLM(prov, *model)
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
```

NOTE: `err` is already declared later via `embedder, err := ...`. Because `prov, err := resolveProvider(...)` introduces `err` earlier in the same scope, change the later embedder line from `embedder, err := embed.NewVoyageClient()` to `embedder, err := embed.NewVoyageClient()` — this is still a valid `:=` since `embedder` is new. No change needed there; just ensure the `llm, err :=` and `embedder, err :=` lines keep `:=` (each introduces at least one new variable). Verify the file compiles in Step 5.

(d) Update `generateUsage` to document the new flags. Replace the `--model` line and add a `--provider` line:
```go
  --provider <name>  LLM provider: anthropic or openrouter (default openrouter)
  --model <id>       model id (default: provider-specific)
```
(Place `--provider` immediately above the `--model` line in the `optional:` block.)

- [ ] **Step 4: Replace the obsolete empty-model test**

The empty-model usage error no longer exists (`--model ""` now resolves to the provider default). Remove `TestRunGenerateEmptyModel` from `cmd/tailor/generate_test.go` (search for `func TestRunGenerateEmptyModel`) and delete its entire function body. Its concern (no cryptic panic on empty model) is now covered by `defaultModel` resolution + the `newLLM`/`newOpenRouter`/`newAnthropic` panic-to-error conversion.

If a separate `TestRunGenerateMissingJDURL` exists and asserts only the missing-flag path, leave it unchanged.

- [ ] **Step 5: Run the full cmd/tailor test suite**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
go build ./... && go test ./cmd/tailor/ -v 2>&1 | tail -40
```
Expected: build succeeds; `TestRunGenerateUnknownProvider`, `TestResolveProvider`, `TestDefaultModel`, and the existing `run`/`generate` tests all PASS; no reference to the deleted `TestRunGenerateEmptyModel`.

- [ ] **Step 6: Commit**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
git add cmd/tailor/generate.go cmd/tailor/generate_test.go
git commit -m "$(cat <<'EOF'
feat(generate): add --provider flag defaulting to openrouter

--model now defaults to a provider-specific id (anthropic/claude-sonnet-4-6
for openrouter, claude-sonnet-4-6 for anthropic). Unknown providers exit 2.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Update the README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update the prerequisites and flags sections**

In `README.md`:

(a) In the "API keys" prerequisite block, add OpenRouter and clarify the default provider. Replace the export block so it reads:
```bash
# Default provider is OpenRouter:
export OPENROUTER_API_KEY=sk-or-...
export VOYAGE_API_KEY=...
# optional, defaults shown:
export VOYAGE_MODEL=voyage-3

# To use Anthropic directly instead, pass --provider anthropic and set:
export ANTHROPIC_API_KEY=sk-ant-...
```
And update the bullets below it to:
```
   - Missing `VOYAGE_API_KEY` → the command exits 1 (the Voyage client is built first).
   - With `--provider openrouter` (default), a missing `OPENROUTER_API_KEY` → exits 1.
   - With `--provider anthropic`, a missing `ANTHROPIC_API_KEY` → exits 1.
```

(b) In the Flags table, add a `--provider` row and update the `--model` row. Replace the `--model` row with these two rows (keep table alignment):
```
| `--provider`       | `openrouter`         | LLM provider: `anthropic` or `openrouter`  |
| `--model`          | _(provider default)_ | Model id; OpenRouter ids are namespaced    |
```

(c) Add a short note after the Flags table:
```markdown
> **Model ids:** OpenRouter ids are namespaced by upstream provider (e.g.
> `anthropic/claude-sonnet-4-6`, `openai/gpt-4o`). The default when `--model`
> is omitted is `anthropic/claude-sonnet-4-6` for OpenRouter and
> `claude-sonnet-4-6` for `--provider anthropic`.
```

- [ ] **Step 2: Verify the README renders sensibly**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
grep -n 'provider\|OPENROUTER_API_KEY' README.md
```
Expected: matches in the prerequisites block, the flags table, and the model-ids note.

- [ ] **Step 3: Commit**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
git add README.md
git commit -m "$(cat <<'EOF'
docs: document --provider flag and OpenRouter setup

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Push and open the PR

**Files:** none.

- [ ] **Step 1: Run the full suite one last time**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
go build ./... && go test ./...
```
Expected: build succeeds; all packages pass. (Re-run with `dangerouslyDisableSandbox: true` if sandboxed.)

- [ ] **Step 2: Push and create the PR**

Run (in `/Users/fhassan-mac/Dev/tailor-swift`):
```bash
git push -u origin feat/openrouter-provider
gh pr create --title "feat: add --provider flag (OpenRouter default)" --body "$(cat <<'EOF'
## Summary
- Adds a `--provider` flag (`anthropic` | `openrouter`) to `tailor generate`, defaulting to `openrouter`.
- `--model` now resolves to a provider-specific default when omitted (namespaced ids for OpenRouter).
- Bumps gantry to v0.0.4-beta for the new openrouter client.

## Test Plan
- [ ] `go test ./...` passes
- [ ] `tailor generate --help` shows `--provider` and updated `--model`
- [ ] Unknown `--provider` exits 2
- [ ] e2e: `--provider openrouter` with `OPENROUTER_API_KEY` produces a resume
EOF
)"
```
Expected: PR URL printed. (If `gh`/network is sandboxed, re-run with `dangerouslyDisableSandbox: true`.)

NOTE: if this branch was based on `feat/generate-cli` (PR #11 not yet merged), set the PR base to `feat/generate-cli` or wait for #11 to merge so the diff is clean. Prefer merging #11 first, then rebasing this branch onto `main`.

---

## Self-Review

**Spec coverage:**
- Bump gantry to v0.0.4-beta (no replace) → Task 1. ✓
- `--provider` flag (anthropic|openrouter), default openrouter, unknown → exit 2 → Task 3 (flag + `resolveProvider`), `TestRunGenerateUnknownProvider`. ✓
- Provider-aware client construction (openrouter via `OPENROUTER_API_KEY`, anthropic path retained) → `newLLM`/`newOpenRouter`/`newAnthropic` (Task 2), wired in Task 3. ✓
- Provider-aware default model (openrouter → `anthropic/claude-sonnet-4-6`; anthropic → `claude-sonnet-4-6`); explicit `--model` used verbatim → `defaultModel` (Task 2) + resolution in Task 3; `TestDefaultModel`. ✓
- Panic→error conversion for clean exit → `newOpenRouter` mirrors `newAnthropic`. ✓
- README updates (provider, OPENROUTER_API_KEY, namespaced ids, default) → Task 4. ✓
- Wiring tests (default provider, default-model resolution, explicit override, unknown provider) → `TestResolveProvider`/`TestDefaultModel`/`TestRunGenerateUnknownProvider`. ✓
- Push + PR → Task 5. ✓

**Placeholder scan:** No TBD/TODO; every code/edit step shows the exact before/after. ✓

**Type consistency:** `resolveProvider(string) (string, error)`, `defaultModel(string) string`, `newOpenRouter(string) (gantry.LLMClient, error)`, `newLLM(string, string) (gantry.LLMClient, error)` are used identically in tests and implementation. The `prov` variable feeds both `defaultModel(prov)` and `newLLM(prov, *model)`. The empty-`--model` semantics are consistent: empty → `defaultModel(prov)`; the obsolete empty-model usage error is removed in Task 3 Step 4 to match. ✓

**Note on out-of-scope items (YAGNI, per spec):** no OpenRouter-specific headers, no provider auto-detection, no Voyage/embedding changes, no openai-package refactor.
