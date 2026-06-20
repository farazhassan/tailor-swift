# OpenRouter gantry Client Implementation Plan (Phase 1 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a standalone `components/llm/openrouter` client to the gantry repo that implements `gantry.StreamingLLMClient` against OpenRouter's OpenAI-compatible API, then release it as `v0.0.4-beta`.

**Architecture:** OpenRouter speaks the OpenAI `/v1/chat/completions` protocol, so this package is a faithful, self-contained copy of `components/llm/openai` with three differences: default base URL (`https://openrouter.ai/api`), API-key env var (`OPENROUTER_API_KEY`), and error/panic message prefixes (`openrouter:`). It does **not** import the `openai` package — the wire types there are unexported, so a standalone client carries its own copy. Correctness is proven by mirroring openai's test suite plus the shared conformance suites.

**Tech Stack:** Go 1.22+, gantry (`github.com/farazhassan/gantry`), stdlib (`net/http`, `bufio`, `bytes`, `encoding/json`), `httptest` for tests.

**Repo / working directory:** `/Users/fhassan-mac/Dev/gantry` (this is a SEPARATE repo from tailor-swift). All commands below run there.

**Sandbox note:** `go` and `git` commands in this repo frequently fail under the command sandbox with "operation not permitted" (build cache) or network/proxy errors. When that happens, re-run the exact same command with `dangerouslyDisableSandbox: true`. This is pre-authorized for this work.

---

## File Structure

All new, under `/Users/fhassan-mac/Dev/gantry/components/llm/openrouter/`:

- `doc.go` — package documentation.
- `wire.go` — private request/response structs + mapping helpers (`toChatRequest`, `toChatTools`, `assembleResponse`, `toToolCalls`, `stopReason`). Verbatim copy of openai's wire mapping (OpenRouter uses the identical wire format).
- `openrouter.go` — `Client`, `Option` funcs, `New` (panics on empty model / missing key), `Generate`, `GenerateStream`, `BaseURL`, `toolAccumulator`, transport helpers. Copy of openai's `openai.go` with the three differences above.
- `openrouter_test.go` — constructor behavior tests.
- `chat_test.go` — non-streaming `Generate` tests + shared test helpers (`newServerClient`, `decodeJSON`).
- `stream_test.go` — `GenerateStream` tests + `sse` helper.
- `conformance_test.go` — the two conformance suites + `conformanceHandler`.

---

## Task 1: Write the test suite (red)

**Files:**
- Create: `components/llm/openrouter/openrouter_test.go`
- Create: `components/llm/openrouter/chat_test.go`
- Create: `components/llm/openrouter/stream_test.go`
- Create: `components/llm/openrouter/conformance_test.go`

These tests reference `openrouter.Client`/`openrouter.New`, which do not exist yet, so the package will fail to compile — that is the intended red state.

- [ ] **Step 1: Create a feature branch**

Run (in `/Users/fhassan-mac/Dev/gantry`):
```bash
git checkout main && git pull && git checkout -b feat/openrouter-client
```
Expected: now on `feat/openrouter-client`.

- [ ] **Step 2: Write `openrouter_test.go`**

Create `components/llm/openrouter/openrouter_test.go`:
```go
package openrouter_test

import (
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/llm/openrouter"
)

// Compile-time guarantee the client satisfies the streaming interface.
var _ gantry.StreamingLLMClient = (*openrouter.Client)(nil)

func TestNewPanicsOnEmptyModel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New(\"\"): want panic on empty model, got none")
		}
	}()
	openrouter.New("", openrouter.WithAPIKey("k"))
}

func TestNewPanicsOnMissingAPIKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	defer func() {
		if recover() == nil {
			t.Error("New without key: want panic, got none")
		}
	}()
	openrouter.New("anthropic/claude-sonnet-4-6")
}

func TestNewReadsAPIKeyFromEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "env-key")
	// Should not panic: key supplied via environment.
	openrouter.New("anthropic/claude-sonnet-4-6")
}

func TestNewAppliesOptions(t *testing.T) {
	c := openrouter.New("anthropic/claude-sonnet-4-6", openrouter.WithAPIKey("k"), openrouter.WithBaseURL("http://example:1234/"))
	// WithBaseURL must trim the trailing slash so path joins are clean.
	if got := c.BaseURL(); got != "http://example:1234" {
		t.Errorf("BaseURL = %q, want %q", got, "http://example:1234")
	}
}

func TestNewDefaultBaseURL(t *testing.T) {
	c := openrouter.New("anthropic/claude-sonnet-4-6", openrouter.WithAPIKey("k"))
	if got := c.BaseURL(); got != "https://openrouter.ai/api" {
		t.Errorf("default BaseURL = %q, want %q", got, "https://openrouter.ai/api")
	}
}
```

- [ ] **Step 3: Write `chat_test.go`**

Create `components/llm/openrouter/chat_test.go`:
```go
package openrouter_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/llm/openrouter"
)

// newServerClient spins up an httptest server with the given handler and returns
// a Client pointed at it. The server is closed via t.Cleanup.
func newServerClient(t *testing.T, handler http.HandlerFunc) *openrouter.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return openrouter.New("test-model",
		openrouter.WithAPIKey("test-key"),
		openrouter.WithBaseURL(srv.URL),
		openrouter.WithHTTPClient(srv.Client()),
	)
}

// decodeJSON reads the request body into v.
func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func TestGenerateMapsRequestAndResponse(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = decodeJSON(r, &gotBody)
		_, _ = io.WriteString(w, `{
			"choices":[{"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":11,"completion_tokens":7}
		}`)
	})

	resp, err := c.Generate(context.Background(), gantry.LLMRequest{
		System:   "be brief",
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
	if gotBody["model"] != "test-model" {
		t.Errorf("model = %v, want test-model", gotBody["model"])
	}
	if gotBody["stream"] != false {
		t.Errorf("stream = %v, want false", gotBody["stream"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2 (system + user)", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "be brief" {
		t.Errorf("first message = %v, want system/be brief", first)
	}

	if resp.Content != "hi there" {
		t.Errorf("Content = %q, want %q", resp.Content, "hi there")
	}
	if resp.StopReason != gantry.StopReasonEnd {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, gantry.StopReasonEnd)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 7 {
		t.Errorf("Usage = %+v, want In=11 Out=7", resp.Usage)
	}
}

func TestGenerateLengthFinishReasonMapsToMaxTokens(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"trunc"},"finish_reason":"length"}]}`)
	})
	resp, err := c.Generate(context.Background(), gantry.LLMRequest{
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.StopReason != gantry.StopReasonMaxTokens {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, gantry.StopReasonMaxTokens)
	}
}

func TestGenerateMapsToolCallsPreservingIDs(t *testing.T) {
	var gotBody map[string]any
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = decodeJSON(r, &gotBody)
		_, _ = io.WriteString(w, `{
			"choices":[{"message":{"role":"assistant","content":"",
				"tool_calls":[{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}}]},
				"finish_reason":"tool_calls"}]
		}`)
	})

	resp, err := c.Generate(context.Background(), gantry.LLMRequest{
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "weather?"}},
		Tools: []gantry.ToolDef{{
			Name:        "get_weather",
			Description: "look up weather",
			Schema:      json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	tools, _ := gotBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	tool0, _ := tools[0].(map[string]any)
	if tool0["type"] != "function" {
		t.Errorf("tool type = %v, want function", tool0["type"])
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q, want call_abc", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q, want get_weather", tc.Name)
	}
	if strings.TrimSpace(string(tc.Input)) != `{"city":"SF"}` {
		t.Errorf("ToolCall.Input = %s, want {\"city\":\"SF\"}", tc.Input)
	}
	if resp.StopReason != gantry.StopReasonToolUse {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, gantry.StopReasonToolUse)
	}
}

func TestGenerateNon2xxReturnsError(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"model not found"}`)
	})
	_, err := c.Generate(context.Background(), gantry.LLMRequest{
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "x"}},
	})
	if err == nil {
		t.Fatal("want error on 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error = %v, want status + body included", err)
	}
}

func TestGenerateForwardsAssistantToolCallsAndToolResults(t *testing.T) {
	var gotBody map[string]any
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = decodeJSON(r, &gotBody)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)
	})

	_, err := c.Generate(context.Background(), gantry.LLMRequest{
		Messages: []gantry.Message{
			{Role: gantry.RoleUser, Content: "weather?"},
			{Role: gantry.RoleAssistant, ToolCalls: []gantry.ToolCall{
				{ID: "call_abc", Name: "get_weather", Input: json.RawMessage(`{"city":"SF"}`)},
			}},
			{Role: gantry.RoleTool, ToolCallID: "call_abc", Content: "72F"},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages len = %d, want 3", len(msgs))
	}
	asst, _ := msgs[1].(map[string]any)
	tcs, _ := asst["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("assistant tool_calls len = %d, want 1", len(tcs))
	}
	tc0, _ := tcs[0].(map[string]any)
	fn, _ := tc0["function"].(map[string]any)
	if tc0["id"] != "call_abc" || fn["arguments"] != `{"city":"SF"}` {
		t.Errorf("assistant tool_call = %v, want id=call_abc arguments={\"city\":\"SF\"}", tc0)
	}
	toolMsg, _ := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_abc" {
		t.Errorf("tool message = %v, want role=tool tool_call_id=call_abc", toolMsg)
	}
}

func TestGeneratePropagatesContextCancellation(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"x"},"finish_reason":"stop"}]}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Generate(ctx, gantry.LLMRequest{
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "x"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
```

- [ ] **Step 4: Write `stream_test.go`**

Create `components/llm/openrouter/stream_test.go`:
```go
package openrouter_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
)

// sse writes lines as a Server-Sent Events body (each event is "data: <p>\n\n").
func sse(w http.ResponseWriter, payloads ...string) {
	for _, p := range payloads {
		_, _ = io.WriteString(w, "data: "+p+"\n\n")
	}
}

func TestGenerateStreamAggregatesDeltas(t *testing.T) {
	var gotStream any
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = decodeJSON(r, &body)
		gotStream = body["stream"]
		sse(w,
			`{"choices":[{"delta":{"role":"assistant","content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo "}}]}`,
			`{"choices":[{"delta":{"content":"world"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3}}`,
			"[DONE]",
		)
	})

	var deltas []string
	resp, err := c.GenerateStream(context.Background(), gantry.LLMRequest{
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "hi"}},
	}, func(ch gantry.StreamChunk) error {
		if ch.TextDelta != "" {
			deltas = append(deltas, ch.TextDelta)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	if gotStream != true {
		t.Errorf("stream flag = %v, want true", gotStream)
	}
	if got := strings.Join(deltas, ""); got != "Hello world" {
		t.Errorf("concatenated deltas = %q, want %q", got, "Hello world")
	}
	if resp.Content != "Hello world" {
		t.Errorf("resp.Content = %q, want %q", resp.Content, "Hello world")
	}
	if resp.StopReason != gantry.StopReasonEnd {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, gantry.StopReasonEnd)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 3 {
		t.Errorf("Usage = %+v, want In=5 Out=3", resp.Usage)
	}
}

func TestGenerateStreamYieldsTerminalUsageChunk(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"content":"hi"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
			"[DONE]",
		)
	})

	var terminal *gantry.StreamChunk
	_, err := c.GenerateStream(context.Background(), gantry.LLMRequest{
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "hi"}},
	}, func(ch gantry.StreamChunk) error {
		if ch.TextDelta == "" {
			cp := ch
			terminal = &cp
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected a terminal (empty-delta) chunk carrying StopReason + Usage")
	}
	if terminal.StopReason != gantry.StopReasonEnd {
		t.Errorf("terminal StopReason = %q, want %q", terminal.StopReason, gantry.StopReasonEnd)
	}
	if terminal.Usage == nil || terminal.Usage.OutputTokens != 1 {
		t.Errorf("terminal Usage = %+v, want OutputTokens=1", terminal.Usage)
	}
}

func TestGenerateStreamAccumulatesToolCallFragments(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"calc","arguments":"{\"a\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"2}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			"[DONE]",
		)
	})

	resp, err := c.GenerateStream(context.Background(), gantry.LLMRequest{
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "calc"}},
	}, func(gantry.StreamChunk) error { return nil })
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v, want 1", resp.ToolCalls)
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "calc" {
		t.Errorf("ToolCall = %+v, want id=call_1 name=calc", tc)
	}
	if string(tc.Input) != `{"a":2}` {
		t.Errorf("ToolCall.Input = %s, want {\"a\":2}", tc.Input)
	}
	if resp.StopReason != gantry.StopReasonToolUse {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, gantry.StopReasonToolUse)
	}
}

func TestGenerateStreamYieldErrorPropagates(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"content":"one"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			"[DONE]",
		)
	})

	boom := errors.New("yield boom")
	_, err := c.GenerateStream(context.Background(), gantry.LLMRequest{
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "hi"}},
	}, func(gantry.StreamChunk) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want yield error propagated", err)
	}
}

func TestGenerateStreamPropagatesContextCancellation(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"x"},"finish_reason":"stop"}]}`, "[DONE]")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.GenerateStream(ctx, gantry.LLMRequest{
		Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "x"}},
	}, func(gantry.StreamChunk) error { return nil })
	if err == nil {
		t.Error("want error on canceled context, got nil")
	}
}
```

- [ ] **Step 5: Write `conformance_test.go`**

Create `components/llm/openrouter/conformance_test.go`:
```go
package openrouter_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/conformance"
)

// conformanceHandler answers /v1/chat/completions for both stream=false (single
// JSON object) and stream=true (SSE), always with a valid "stop" reply so the
// conformance assertions (non-empty StopReason, delta/content parity) hold.
func conformanceHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Stream bool `json:"stream"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Stream {
		sse(w,
			`{"choices":[{"delta":{"role":"assistant","content":"hello world"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":2}}`,
			"[DONE]",
		)
		return
	}
	_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"hello world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":2}}`)
}

func TestOpenRouterConformsToLLMClient(t *testing.T) {
	conformance.LLMClientSuite(t, func() gantry.LLMClient {
		return newServerClient(t, conformanceHandler)
	})
}

func TestOpenRouterConformsToStreamingLLMClient(t *testing.T) {
	conformance.StreamingLLMClientSuite(t, func() gantry.StreamingLLMClient {
		return newServerClient(t, conformanceHandler)
	})
}
```

- [ ] **Step 6: Verify the package fails to build (red)**

Run (in `/Users/fhassan-mac/Dev/gantry`):
```bash
go test ./components/llm/openrouter/ 2>&1 | head
```
Expected: build/compile failure such as `package github.com/farazhassan/gantry/components/llm/openrouter: no Go files` becoming an undefined-symbol error — i.e. it does NOT pass. (If the sandbox blocks `go`, re-run with `dangerouslyDisableSandbox: true`.) Do NOT commit yet — the implementation in Task 2 makes this green.

---

## Task 2: Implement the client (green)

**Files:**
- Create: `components/llm/openrouter/doc.go`
- Create: `components/llm/openrouter/wire.go`
- Create: `components/llm/openrouter/openrouter.go`

- [ ] **Step 1: Write `doc.go`**

Create `components/llm/openrouter/doc.go`:
```go
// Package openrouter provides a gantry.StreamingLLMClient backed by
// OpenRouter's OpenAI-compatible /v1/chat/completions endpoint. It maps gantry
// request/response types to that wire format, supports tool calling, and
// streams Server-Sent Events.
//
// Construct a client with New. The API key comes from WithAPIKey or, failing
// that, the OPENROUTER_API_KEY environment variable. Model ids are namespaced
// by provider (e.g. "anthropic/claude-sonnet-4-6", "openai/gpt-4o"):
//
//	client := openrouter.New("anthropic/claude-sonnet-4-6", openrouter.WithAPIKey(key))
//	resp, err := client.Generate(ctx, gantry.LLMRequest{
//	    Messages: []gantry.Message{{Role: gantry.RoleUser, Content: "hi"}},
//	})
//
// Like OpenAI, OpenRouter carries tool-call arguments as a JSON-encoded string
// and splits them across streamed deltas keyed by index; the client reassembles
// them and preserves the per-call IDs.
package openrouter
```

- [ ] **Step 2: Write `wire.go`**

Create `components/llm/openrouter/wire.go` (identical mapping to the openai package; OpenRouter uses the same wire format):
```go
package openrouter

import (
	"encoding/json"

	"github.com/farazhassan/gantry"
)

// The structs below mirror OpenRouter's OpenAI-compatible
// /v1/chat/completions wire format. They are private: callers only ever see
// gantry types. Mapping lives here so the client code in openrouter.go stays
// focused on transport.

type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []chatMessage  `json:"messages"`
	Tools         []chatTool     `json:"tools,omitempty"`
	Temperature   float64        `json:"temperature,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

// streamOptions asks the API to emit a terminal chunk carrying usage when
// streaming (otherwise usage is omitted from streamed responses).
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// wireToolCall is the request-side assistant tool call. Arguments are carried
// as a JSON-encoded string, so gantry ToolCall.Input (already raw JSON) is
// forwarded verbatim as that string.
type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireToolCallFunc `json:"function"`
}

type wireToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// chatResponse is one /v1/chat/completions reply. For stream=false it is the
// whole reply; for stream=true it is one SSE data payload (the incremental
// content/tool-call deltas live under choices[].delta, and a terminal payload
// carries usage when stream_options.include_usage is set).
type chatResponse struct {
	Choices []choice `json:"choices"`
	Usage   *usage   `json:"usage"`
}

type choice struct {
	Message      respMessage `json:"message"`
	Delta        respMessage `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type respMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []respToolCall `json:"tool_calls"`
}

// respToolCall is the response-side tool call. In streaming, Index identifies
// the call across chunks and Function.Arguments accumulates partial fragments.
type respToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireToolCallFunc `json:"function"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// toChatRequest maps a gantry request to the wire format. System is carried as
// a leading system-role message.
func toChatRequest(model string, req gantry.LLMRequest, stream bool) chatRequest {
	var msgs []chatMessage
	if req.System != "" {
		msgs = append(msgs, chatMessage{Role: string(gantry.RoleSystem), Content: req.System})
	}
	for _, m := range req.Messages {
		cm := chatMessage{Role: string(m.Role), Content: m.Content}
		if m.Role == gantry.RoleTool {
			cm.ToolCallID = m.ToolCallID
		}
		for _, tc := range m.ToolCalls {
			cm.ToolCalls = append(cm.ToolCalls, wireToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: wireToolCallFunc{Name: tc.Name, Arguments: string(tc.Input)},
			})
		}
		msgs = append(msgs, cm)
	}

	cr := chatRequest{
		Model:       model,
		Messages:    msgs,
		Tools:       toChatTools(req.Tools),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      stream,
	}
	if stream {
		cr.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	return cr
}

func toChatTools(defs []gantry.ToolDef) []chatTool {
	if len(defs) == 0 {
		return nil
	}
	out := make([]chatTool, len(defs))
	for i, d := range defs {
		out[i] = chatTool{
			Type: "function",
			Function: chatFunction{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  d.Schema,
			},
		}
	}
	return out
}

// assembleResponse builds the gantry response from aggregated stream/non-stream
// fields. It is the single place stop-reason and tool-call mapping live.
func assembleResponse(content string, calls []respToolCall, finishReason string, u gantry.Usage) gantry.LLMResponse {
	return gantry.LLMResponse{
		Content:    content,
		ToolCalls:  toToolCalls(calls),
		StopReason: stopReason(finishReason, len(calls) > 0),
		Usage:      u,
	}
}

// toToolCalls preserves per-call IDs and forwards the arguments string as raw
// JSON (it is already a serialized JSON object).
func toToolCalls(calls []respToolCall) []gantry.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]gantry.ToolCall, len(calls))
	for i, c := range calls {
		out[i] = gantry.ToolCall{
			ID:    c.ID,
			Name:  c.Function.Name,
			Input: json.RawMessage(c.Function.Arguments),
		}
	}
	return out
}

func stopReason(finishReason string, hasTools bool) gantry.StopReason {
	switch {
	case finishReason == "tool_calls" || hasTools:
		return gantry.StopReasonToolUse
	case finishReason == "length":
		return gantry.StopReasonMaxTokens
	default:
		return gantry.StopReasonEnd
	}
}
```

- [ ] **Step 3: Write `openrouter.go`**

Create `components/llm/openrouter/openrouter.go` (copy of openai.go with base URL `https://openrouter.ai/api`, env `OPENROUTER_API_KEY`, and `openrouter:` message prefixes):
```go
package openrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/farazhassan/gantry"
)

const (
	defaultBaseURL = "https://openrouter.ai/api"
	chatPath       = "/v1/chat/completions"
	apiKeyEnv      = "OPENROUTER_API_KEY"
	dataPrefix     = "data: "
	doneSentinel   = "[DONE]"
	maxLineBytes   = 1 << 20 // 1 MiB; one SSE data line can hold a whole tool-call payload
)

// Client is a gantry.StreamingLLMClient backed by OpenRouter's
// OpenAI-compatible /v1/chat/completions endpoint. It is safe for concurrent
// use: it holds no per-call state and the underlying *http.Client is
// concurrency-safe.
type Client struct {
	model   string
	baseURL string
	apiKey  string
	httpc   *http.Client
}

var _ gantry.StreamingLLMClient = (*Client)(nil)

// Option configures a Client at construction.
type Option func(*Client)

// New returns a Client for the given OpenRouter model (e.g.
// "anthropic/claude-sonnet-4-6"). The API key is taken from WithAPIKey, or
// falls back to the OPENROUTER_API_KEY environment variable. It panics on an
// empty model or a missing key — both are programmer errors, not runtime
// conditions.
func New(model string, opts ...Option) *Client {
	if model == "" {
		panic("openrouter: New requires a non-empty model")
	}
	c := &Client{
		model:   model,
		baseURL: defaultBaseURL,
		apiKey:  os.Getenv(apiKeyEnv),
		httpc:   &http.Client{},
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.apiKey == "" {
		panic("openrouter: New requires an API key (WithAPIKey or " + apiKeyEnv + ")")
	}
	return c
}

// WithAPIKey sets the bearer token, overriding the OPENROUTER_API_KEY
// environment variable. An empty key is ignored so the env fallback still
// applies.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		if key != "" {
			c.apiKey = key
		}
	}
}

// WithBaseURL points the client at a non-default endpoint (e.g. a proxy). A
// trailing slash is trimmed so path joins stay clean.
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(url, "/") }
}

// WithHTTPClient supplies the *http.Client used for requests — set this to
// configure timeouts/transport, or to point tests at an httptest server. A nil
// client is ignored.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpc = h
		}
	}
}

// BaseURL returns the endpoint the client posts to (trailing slash trimmed).
func (c *Client) BaseURL() string { return c.baseURL }

// Generate sends a non-streaming chat-completions request and returns the
// assembled reply.
func (c *Client) Generate(ctx context.Context, req gantry.LLMRequest) (gantry.LLMResponse, error) {
	resp, err := c.post(ctx, req, false)
	if err != nil {
		return gantry.LLMResponse{}, err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return gantry.LLMResponse{}, err
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return gantry.LLMResponse{}, fmt.Errorf("openrouter: decode response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return gantry.LLMResponse{}, fmt.Errorf("openrouter: response had no choices")
	}
	ch := cr.Choices[0]
	return assembleResponse(ch.Message.Content, ch.Message.ToolCalls, ch.FinishReason, toUsage(cr.Usage)), nil
}

// GenerateStream sends a streaming chat-completions request, invoking yield
// once per non-empty text delta as SSE chunks arrive, and returns the fully
// aggregated reply. A yield error stops reading and is returned as-is so
// callers can match it with errors.Is.
func (c *Client) GenerateStream(ctx context.Context, req gantry.LLMRequest, yield func(gantry.StreamChunk) error) (gantry.LLMResponse, error) {
	resp, err := c.post(ctx, req, true)
	if err != nil {
		return gantry.LLMResponse{}, err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return gantry.LLMResponse{}, err
	}

	var (
		content      strings.Builder
		calls        toolAccumulator
		finishReason string
		u            gantry.Usage
	)

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return gantry.LLMResponse{}, err
		}
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || !bytes.HasPrefix(line, []byte(dataPrefix)) {
			continue
		}
		payload := bytes.TrimSpace(line[len(dataPrefix):])
		if string(payload) == doneSentinel {
			break
		}
		var chunk chatResponse
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return gantry.LLMResponse{}, fmt.Errorf("openrouter: decode stream chunk: %w", err)
		}
		if chunk.Usage != nil {
			u = toUsage(chunk.Usage)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		if delta := ch.Delta.Content; delta != "" {
			content.WriteString(delta)
			if err := yield(gantry.StreamChunk{TextDelta: delta}); err != nil {
				return gantry.LLMResponse{}, err
			}
		}
		for _, tc := range ch.Delta.ToolCalls {
			calls.add(tc)
		}
		if ch.FinishReason != "" {
			finishReason = ch.FinishReason
		}
	}
	if err := sc.Err(); err != nil {
		return gantry.LLMResponse{}, fmt.Errorf("openrouter: read stream: %w", err)
	}

	out := assembleResponse(content.String(), calls.calls(), finishReason, u)
	// Terminal metadata chunk (empty delta) for parity with the in-repo mock;
	// the default LLM handler ignores empty-delta chunks, so this is harmless.
	usage := out.Usage
	if err := yield(gantry.StreamChunk{StopReason: out.StopReason, Usage: &usage}); err != nil {
		return gantry.LLMResponse{}, err
	}
	return out, nil
}

// toolAccumulator stitches streamed tool-call fragments back together. The API
// splits one call across many deltas keyed by Index: the first carries id/name,
// later ones append argument fragments.
type toolAccumulator struct {
	order []int
	byIdx map[int]*respToolCall
}

func (a *toolAccumulator) add(frag respToolCall) {
	if a.byIdx == nil {
		a.byIdx = make(map[int]*respToolCall)
	}
	cur, ok := a.byIdx[frag.Index]
	if !ok {
		cp := frag
		a.byIdx[frag.Index] = &cp
		a.order = append(a.order, frag.Index)
		return
	}
	if frag.ID != "" {
		cur.ID = frag.ID
	}
	if frag.Function.Name != "" {
		cur.Function.Name = frag.Function.Name
	}
	cur.Function.Arguments += frag.Function.Arguments
}

func (a *toolAccumulator) calls() []respToolCall {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]respToolCall, 0, len(a.order))
	for _, idx := range a.order {
		out = append(out, *a.byIdx[idx])
	}
	return out
}

func (c *Client) post(ctx context.Context, req gantry.LLMRequest, stream bool) (*http.Response, error) {
	body, err := json.Marshal(toChatRequest(c.model, req, stream))
	if err != nil {
		return nil, fmt.Errorf("openrouter: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+chatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openrouter: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter: chat request: %w", err)
	}
	return resp, nil
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode/100 == 2 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("openrouter: chat: status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
}

func toUsage(u *usage) gantry.Usage {
	if u == nil {
		return gantry.Usage{}
	}
	return gantry.Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens}
}
```

- [ ] **Step 4: Run the package tests (green)**

Run (in `/Users/fhassan-mac/Dev/gantry`):
```bash
go test ./components/llm/openrouter/ -v 2>&1 | tail -40
```
Expected: all tests PASS, including `TestOpenRouterConformsToLLMClient` and `TestOpenRouterConformsToStreamingLLMClient`. (If the sandbox blocks `go`, re-run with `dangerouslyDisableSandbox: true`.)

- [ ] **Step 5: Run full validation gates**

Run (in `/Users/fhassan-mac/Dev/gantry`):
```bash
gofmt -l components/llm/openrouter/ && go vet ./components/llm/openrouter/ && go test -race ./...
```
Expected: `gofmt -l` prints nothing (all files formatted); `go vet` prints nothing; `go test -race ./...` passes for every package. (If the sandbox blocks `go`/network, re-run with `dangerouslyDisableSandbox: true`.)

- [ ] **Step 6: Commit**

Run (in `/Users/fhassan-mac/Dev/gantry`):
```bash
git add components/llm/openrouter/
git commit -m "$(cat <<'EOF'
feat(llm): add standalone OpenRouter client

OpenRouter speaks the OpenAI /v1/chat/completions protocol, so this is a
self-contained copy of components/llm/openai with OpenRouter's base URL
(https://openrouter.ai/api) and OPENROUTER_API_KEY env var. Passes the
LLMClient and StreamingLLMClient conformance suites.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Release `v0.0.4-beta`

**Files:** none (git tag only).

This task ships the client so tailor-swift (Phase 2) can require it. The branch must be merged to gantry `main` first.

- [ ] **Step 1: Push the branch and open a PR**

Run (in `/Users/fhassan-mac/Dev/gantry`):
```bash
git push -u origin feat/openrouter-client
gh pr create --title "feat(llm): add standalone OpenRouter client" --body "$(cat <<'EOF'
## Summary
- Adds components/llm/openrouter, a standalone gantry.StreamingLLMClient for OpenRouter's OpenAI-compatible API.
- Mirrors components/llm/openai; differs only in base URL, API-key env var, and message prefixes.

## Test Plan
- [ ] go test -race ./... passes
- [ ] go vet ./... clean
- [ ] gofmt -l . clean
- [ ] openrouter conformance suites pass
EOF
)"
```
Expected: PR URL printed. (If `gh`/network is sandboxed, re-run with `dangerouslyDisableSandbox: true`.)

- [ ] **Step 2: Merge the PR**

Merge via the normal review/merge process (squash or merge per repo convention). Confirm `main` now contains `components/llm/openrouter/`.

- [ ] **Step 3: Tag and push the release**

Run (in `/Users/fhassan-mac/Dev/gantry`, after the merge):
```bash
git checkout main && git pull
git tag v0.0.4-beta
git push origin v0.0.4-beta
```
Expected: tag `v0.0.4-beta` pushed. This unblocks Phase 2 (the tailor-swift plan).

- [ ] **Step 4: Verify the tag is fetchable**

Run (in `/Users/fhassan-mac/Dev/gantry`):
```bash
git ls-remote --tags origin | grep v0.0.4-beta
```
Expected: a line containing `refs/tags/v0.0.4-beta`.

---

## Self-Review

**Spec coverage:**
- Standalone package mirroring openai → Tasks 1–2 (no openai import; own wire.go). ✓
- `New` panics on empty model / missing key → `openrouter_test.go` + `New` impl. ✓
- Base URL `https://openrouter.ai/api`, env `OPENROUTER_API_KEY`, `Authorization: Bearer`, path `/v1/chat/completions` → constants in `openrouter.go`, asserted in `chat_test.go`/`openrouter_test.go`. ✓
- Generate, GenerateStream, tool-call accumulation, usage mapping, stop-reason → covered by chat/stream tests. ✓
- Conformance suites → `conformance_test.go`. ✓
- `go test -race` / `go vet` / `gofmt` clean + Conventional Commit → Task 2 steps 5–6. ✓
- Tag `v0.0.4-beta` → Task 3. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete file content. ✓

**Type consistency:** `Client`, `New`, `WithAPIKey`/`WithBaseURL`/`WithHTTPClient`, `BaseURL()`, `Generate`, `GenerateStream`, and the private wire types/funcs (`chatResponse`, `respToolCall`, `toChatRequest`, `assembleResponse`, `toUsage`, `toolAccumulator`) are named identically across tests and implementation. ✓
