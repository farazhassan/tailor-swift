# Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the `tailor` Go project — a CLI skeleton plus a fully-tested markdown content-store parser that turns `content.md` into an in-memory unit model with provenance.

**Architecture:** A stdlib-only Go CLI (`cmd/tailor`) dispatches to subcommands. The `internal/store` package parses lightly-structured markdown (H1 = name, `## Contact`/`## Skills` = special sections, other `##` = roles, `###` = projects, `-` bullets = achievements) into typed units. Each achievement gets a stable derived id and file+line provenance, which later plans rely on for embedding and the truthfulness gate. No LLM and no external dependencies in this plan — everything is locally testable with `go test`.

**Tech Stack:** Go 1.26, standard library only (`flag`, `bufio`, `regexp`, `crypto/sha256`). gantry is NOT a dependency yet (introduced in Plan 2).

**Branch:** Create and work on `feat/foundations` (never commit code to `main`).

---

## File Structure

- `go.mod` — module `github.com/farazhassan/tailor-swift`, Go 1.26.
- `cmd/tailor/main.go` — entrypoint; thin `main()` calling testable `run(args, stdout, stderr) int`.
- `cmd/tailor/run.go` — subcommand dispatch (`ingest`, `generate`, `evaluate`, `validate`).
- `cmd/tailor/run_test.go` — dispatch/usage/exit-code tests.
- `internal/store/types.go` — `Store`, `Profile`, `Contact`, `Role`, `Project`, `Achievement`, `Skill`, `Provenance`.
- `internal/store/id.go` — `DeriveID(text string) string`.
- `internal/store/id_test.go`.
- `internal/store/headings.go` — `parseTags`, `parseRoleHeading` helpers.
- `internal/store/headings_test.go`.
- `internal/store/parse.go` — `Parse(path)` / `ParseReader(r, path)`.
- `internal/store/parse_test.go`.
- `internal/store/testdata/sample.md` — fixture content store.

`validate` subcommand is included because the spec requires the user to review the LLM-written `content.md`; a parse-and-summarize command makes that review concrete and exercises the parser end-to-end through the binary.

---

## Task 1: Initialize module and branch

**Files:**
- Create: `go.mod`

- [ ] **Step 1: Create the feature branch**

Run:
```bash
git checkout -b feat/foundations
```
Expected: `Switched to a new branch 'feat/foundations'`

- [ ] **Step 2: Initialize the Go module**

Run:
```bash
go mod init github.com/farazhassan/tailor-swift
```
Then edit `go.mod` so the Go line reads exactly:
```
module github.com/farazhassan/tailor-swift

go 1.26
```

- [ ] **Step 3: Verify it builds (no packages yet)**

Run: `go build ./...`
Expected: exits 0 with no output.

- [ ] **Step 4: Commit**

```bash
git add go.mod
git commit -m "chore: initialize go module"
```

---

## Task 2: CLI skeleton with testable dispatch

**Files:**
- Create: `cmd/tailor/run.go`
- Create: `cmd/tailor/main.go`
- Test: `cmd/tailor/run_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/tailor/run_test.go`:
```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func runCapture(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRun_NoArgs_PrintsUsage(t *testing.T) {
	code, _, errOut := runCapture()
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "usage:") {
		t.Fatalf("stderr = %q, want usage text", errOut)
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	code, _, errOut := runCapture("frobnicate")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("stderr = %q, want unknown command", errOut)
	}
}

func TestRun_KnownStubs(t *testing.T) {
	for _, cmd := range []string{"ingest", "generate", "evaluate"} {
		code, out, _ := runCapture(cmd)
		if code != 0 {
			t.Fatalf("%s exit code = %d, want 0", cmd, code)
		}
		if !strings.Contains(out, "not implemented") {
			t.Fatalf("%s stdout = %q, want not implemented", cmd, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tailor/`
Expected: FAIL — `undefined: run`.

- [ ] **Step 3: Write minimal implementation**

`cmd/tailor/run.go`:
```go
package main

import (
	"fmt"
	"io"
)

const usage = `usage: tailor <command> [flags]

commands:
  ingest      build the content store from base resumes (not implemented)
  generate    generate a tailored resume for a job description (not implemented)
  evaluate    evaluate a resume against a job description (not implemented)
  validate    parse a content store file and print a summary
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "ingest", "generate", "evaluate":
		fmt.Fprintf(stdout, "%s: not implemented yet\n", args[0])
		return 0
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %q\n\n%s", args[0], usage)
		return 2
	}
}
```

`cmd/tailor/main.go`:
```go
package main

import (
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
```

Note: `runValidate` is defined in Task 7. To make this task compile and pass on its own, temporarily add a stub at the bottom of `run.go`:
```go
func runValidate(args []string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stdout, "validate: not implemented yet")
	return 0
}
```
(Task 7 replaces this stub with the real implementation.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tailor/`
Expected: PASS (ok).

- [ ] **Step 5: Commit**

```bash
git add cmd/tailor/
git commit -m "feat: add tailor CLI skeleton with subcommand dispatch"
```

---

## Task 3: Achievement ID derivation

**Files:**
- Create: `internal/store/id.go`
- Test: `internal/store/id_test.go`

- [ ] **Step 1: Write the failing test**

`internal/store/id_test.go`:
```go
package store

import "testing"

func TestDeriveID_StableAndPrefixed(t *testing.T) {
	a := DeriveID("Cut billing settlement latency 40%")
	b := DeriveID("Cut billing settlement latency 40%")
	if a != b {
		t.Fatalf("ids not stable: %q != %q", a, b)
	}
	if a[:4] != "ach_" {
		t.Fatalf("id %q missing ach_ prefix", a)
	}
	if len(a) != 16 { // "ach_" + 12 hex chars
		t.Fatalf("id %q length = %d, want 16", a, len(a))
	}
}

func TestDeriveID_NormalizesWhitespaceAndCase(t *testing.T) {
	if DeriveID("Hello   World") != DeriveID("hello world") {
		t.Fatal("expected normalized text to produce same id")
	}
}

func TestDeriveID_DiffersOnContent(t *testing.T) {
	if DeriveID("alpha") == DeriveID("beta") {
		t.Fatal("different text must produce different ids")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/`
Expected: FAIL — `undefined: DeriveID`.

- [ ] **Step 3: Write minimal implementation**

`internal/store/id.go`:
```go
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DeriveID returns a stable id for an achievement derived from its text.
// Text is normalized (lowercased, whitespace collapsed) so cosmetic edits
// do not change the id, but content changes do.
func DeriveID(text string) string {
	norm := strings.ToLower(strings.Join(strings.Fields(text), " "))
	sum := sha256.Sum256([]byte(norm))
	return "ach_" + hex.EncodeToString(sum[:])[:12]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestDeriveID`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/id.go internal/store/id_test.go
git commit -m "feat: add stable achievement id derivation"
```

---

## Task 4: Heading and tag parsing helpers

**Files:**
- Create: `internal/store/headings.go`
- Test: `internal/store/headings_test.go`

- [ ] **Step 1: Write the failing test**

`internal/store/headings_test.go`:
```go
package store

import (
	"reflect"
	"testing"
)

func TestParseTags(t *testing.T) {
	clean, tags := parseTags("Cut latency 40% `#performance #kafka`")
	if clean != "Cut latency 40%" {
		t.Fatalf("clean = %q", clean)
	}
	if !reflect.DeepEqual(tags, []string{"performance", "kafka"}) {
		t.Fatalf("tags = %v", tags)
	}
}

func TestParseTags_NoTags(t *testing.T) {
	clean, tags := parseTags("Just a plain bullet")
	if clean != "Just a plain bullet" {
		t.Fatalf("clean = %q", clean)
	}
	if len(tags) != 0 {
		t.Fatalf("tags = %v, want empty", tags)
	}
}

func TestParseRoleHeading(t *testing.T) {
	c, ti, s, e := parseRoleHeading("Acme — Senior Engineer (2021-03 – present)")
	if c != "Acme" || ti != "Senior Engineer" || s != "2021-03" || e != "present" {
		t.Fatalf("got %q / %q / %q / %q", c, ti, s, e)
	}
}

func TestParseRoleHeading_HyphenSeparators(t *testing.T) {
	c, ti, s, e := parseRoleHeading("Globex - Staff Engineer (2019-01 - 2021-02)")
	if c != "Globex" || ti != "Staff Engineer" || s != "2019-01" || e != "2021-02" {
		t.Fatalf("got %q / %q / %q / %q", c, ti, s, e)
	}
}

func TestParseRoleHeading_NoDates(t *testing.T) {
	c, ti, s, e := parseRoleHeading("Initech — Engineer")
	if c != "Initech" || ti != "Engineer" || s != "" || e != "" {
		t.Fatalf("got %q / %q / %q / %q", c, ti, s, e)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run "ParseTags|ParseRoleHeading"`
Expected: FAIL — `undefined: parseTags` / `parseRoleHeading`.

- [ ] **Step 3: Write minimal implementation**

`internal/store/headings.go`:
```go
package store

import (
	"regexp"
	"strings"
)

var (
	backtickSpan = regexp.MustCompile("`[^`]*`")
	tagToken     = regexp.MustCompile(`#[A-Za-z0-9_-]+`)
	dateParen    = regexp.MustCompile(`\s*\(([^)]*)\)\s*$`)
	// A range separator is a dash surrounded by whitespace, so internal
	// hyphens in dates like "2021-03" are never split.
	spacedDash = regexp.MustCompile(`\s+[–—-]\s+`)
)

// parseTags extracts #tags from backtick spans on a line and returns the
// line with those spans removed and trimmed.
func parseTags(line string) (string, []string) {
	var tags []string
	for _, span := range backtickSpan.FindAllString(line, -1) {
		for _, tok := range tagToken.FindAllString(span, -1) {
			tags = append(tags, strings.TrimPrefix(tok, "#"))
		}
	}
	clean := strings.TrimSpace(backtickSpan.ReplaceAllString(line, ""))
	return clean, tags
}

// parseRoleHeading parses "Company — Title (start – end)" into its parts.
// Title and dates are optional.
func parseRoleHeading(h string) (company, title, start, end string) {
	h = strings.TrimSpace(h)
	if m := dateParen.FindStringSubmatch(h); m != nil {
		dates := strings.TrimSpace(m[1])
		h = strings.TrimSpace(dateParen.ReplaceAllString(h, ""))
		dp := spacedDash.Split(dates, 2)
		start = strings.TrimSpace(dp[0])
		if len(dp) > 1 {
			end = strings.TrimSpace(dp[1])
		}
	}
	p := spacedDash.Split(h, 2)
	company = strings.TrimSpace(p[0])
	if len(p) > 1 {
		title = strings.TrimSpace(p[1])
	}
	return
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run "ParseTags|ParseRoleHeading"`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/headings.go internal/store/headings_test.go
git commit -m "feat: add markdown heading and tag parsing helpers"
```

---

## Task 5: Content store types

**Files:**
- Create: `internal/store/types.go`

- [ ] **Step 1: Write the type definitions**

There is no separate test for this task; the types are exercised by the parser test in Task 6. Create `internal/store/types.go`:
```go
package store

// Provenance records where a unit came from in the source file.
type Provenance struct {
	File string
	Line int // 1-based line number
}

type Contact struct {
	Email    string
	Phone    string
	Location string
	Links    []string
}

type Profile struct {
	Name    string
	Contact Contact
}

type Achievement struct {
	ID         string
	Text       string
	Tags       []string
	Provenance Provenance
}

type Project struct {
	Name         string
	Tags         []string
	Achievements []Achievement
	Provenance   Provenance
}

type Role struct {
	Company    string
	Title      string
	Start      string
	End        string
	Projects   []Project
	Provenance Provenance
}

type Skill struct {
	Raw string // e.g. "Go (expert)"
}

type Store struct {
	Source  string // path the store was parsed from
	Profile Profile
	Roles   []Role
	Skills  []Skill
}

// Achievements flattens all achievements across roles/projects. Later plans
// embed and retrieve over this list.
func (s *Store) Achievements() []Achievement {
	var out []Achievement
	for _, r := range s.Roles {
		for _, p := range r.Projects {
			out = append(out, p.Achievements...)
		}
	}
	return out
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/store/`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/store/types.go
git commit -m "feat: add content store types"
```

---

## Task 6: The parser

**Files:**
- Create: `internal/store/testdata/sample.md`
- Create: `internal/store/parse.go`
- Test: `internal/store/parse_test.go`

- [ ] **Step 1: Create the fixture**

`internal/store/testdata/sample.md`:
```markdown
# Jane Doe

## Contact
Email: jane@example.com
Phone: 555-0100
Location: Remote
Links: github.com/jane, linkedin.com/in/jane

## Acme — Senior Engineer (2021-03 – present)

### Billing platform revamp  `#go #payments`
- Cut billing settlement latency 40% by sharding the ledger `#performance #kafka`
- Migrated 200 services off the monolith `#go`

### Observability rollout
- Introduced distributed tracing across 30 teams

## Globex - Staff Engineer (2019-01 - 2021-02)

### Search relevance
- Improved search CTR 12% with a learning-to-rank model `#ml`

## Skills
Go (expert), Kafka, Postgres, Kubernetes
```

- [ ] **Step 2: Write the failing test**

`internal/store/parse_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestParse_Sample(t *testing.T) {
	path := filepath.Join("testdata", "sample.md")
	s, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if s.Profile.Name != "Jane Doe" {
		t.Errorf("name = %q", s.Profile.Name)
	}
	if s.Profile.Contact.Email != "jane@example.com" {
		t.Errorf("email = %q", s.Profile.Contact.Email)
	}
	if len(s.Profile.Contact.Links) != 2 {
		t.Errorf("links = %v", s.Profile.Contact.Links)
	}

	if len(s.Roles) != 2 {
		t.Fatalf("roles = %d, want 2", len(s.Roles))
	}
	r0 := s.Roles[0]
	if r0.Company != "Acme" || r0.Title != "Senior Engineer" || r0.End != "present" {
		t.Errorf("role0 = %+v", r0)
	}
	if len(r0.Projects) != 2 {
		t.Fatalf("role0 projects = %d, want 2", len(r0.Projects))
	}
	p0 := r0.Projects[0]
	if p0.Name != "Billing platform revamp" {
		t.Errorf("project0 name = %q", p0.Name)
	}
	if len(p0.Tags) != 2 { // go, payments
		t.Errorf("project0 tags = %v", p0.Tags)
	}
	if len(p0.Achievements) != 2 {
		t.Fatalf("project0 achievements = %d, want 2", len(p0.Achievements))
	}

	a0 := p0.Achievements[0]
	if a0.Text != "Cut billing settlement latency 40% by sharding the ledger" {
		t.Errorf("ach0 text = %q", a0.Text)
	}
	if a0.ID == "" || a0.ID[:4] != "ach_" {
		t.Errorf("ach0 id = %q", a0.ID)
	}
	if a0.Provenance.File != path {
		t.Errorf("ach0 provenance file = %q", a0.Provenance.File)
	}
	if a0.Provenance.Line == 0 {
		t.Error("ach0 provenance line not set")
	}

	if len(s.Skills) != 4 {
		t.Errorf("skills = %v, want 4", s.Skills)
	}

	if got := len(s.Achievements()); got != 4 {
		t.Errorf("flattened achievements = %d, want 4", got)
	}
}

func TestParse_BulletWithoutProjectIsError(t *testing.T) {
	_, err := ParseReader([]byte("## Acme — Engineer\n- orphan bullet\n"), "mem")
	if err == nil {
		t.Fatal("expected error for bullet with no project")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestParse`
Expected: FAIL — `undefined: Parse` / `ParseReader`.

- [ ] **Step 4: Write the implementation**

`internal/store/parse.go`:
```go
package store

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Parse reads and parses a content store markdown file.
func Parse(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseReader(data, path)
}

type section int

const (
	secNone section = iota
	secContact
	secSkills
	secRole
)

// ParseReader parses content store markdown from raw bytes. path is recorded
// as provenance for each unit.
func ParseReader(data []byte, path string) (*Store, error) {
	s := &Store{Source: path}
	sec := secNone

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "# "):
			s.Profile.Name = strings.TrimSpace(line[2:])
			sec = secNone

		case strings.HasPrefix(line, "## "):
			heading := strings.TrimSpace(line[3:])
			switch strings.ToLower(heading) {
			case "contact":
				sec = secContact
			case "skills":
				sec = secSkills
			default:
				c, ti, st, e := parseRoleHeading(heading)
				s.Roles = append(s.Roles, Role{
					Company: c, Title: ti, Start: st, End: e,
					Provenance: Provenance{File: path, Line: lineNo},
				})
				sec = secRole
			}

		case strings.HasPrefix(line, "### "):
			if sec != secRole || len(s.Roles) == 0 {
				return nil, fmt.Errorf("%s:%d: project heading outside of a role", path, lineNo)
			}
			name, tags := parseTags(strings.TrimSpace(line[4:]))
			r := &s.Roles[len(s.Roles)-1]
			r.Projects = append(r.Projects, Project{
				Name: name, Tags: tags,
				Provenance: Provenance{File: path, Line: lineNo},
			})

		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			text, tags := parseTags(strings.TrimSpace(trimmed[2:]))
			if sec != secRole || len(s.Roles) == 0 {
				return nil, fmt.Errorf("%s:%d: achievement bullet outside of a role/project", path, lineNo)
			}
			r := &s.Roles[len(s.Roles)-1]
			if len(r.Projects) == 0 {
				return nil, fmt.Errorf("%s:%d: achievement bullet with no project", path, lineNo)
			}
			p := &r.Projects[len(r.Projects)-1]
			p.Achievements = append(p.Achievements, Achievement{
				ID: DeriveID(text), Text: text, Tags: tags,
				Provenance: Provenance{File: path, Line: lineNo},
			})

		default: // non-heading, non-bullet content
			switch sec {
			case secContact:
				applyContactLine(&s.Profile.Contact, trimmed)
			case secSkills:
				for _, sk := range strings.Split(trimmed, ",") {
					if v := strings.TrimSpace(sk); v != "" {
						s.Skills = append(s.Skills, Skill{Raw: v})
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func applyContactLine(c *Contact, line string) {
	key, val, ok := strings.Cut(line, ":")
	if !ok {
		return
	}
	val = strings.TrimSpace(val)
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "email":
		c.Email = val
	case "phone":
		c.Phone = val
	case "location":
		c.Location = val
	case "links":
		for _, l := range strings.Split(val, ",") {
			if v := strings.TrimSpace(l); v != "" {
				c.Links = append(c.Links, v)
			}
		}
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store/`
Expected: PASS (all store tests).

- [ ] **Step 6: Commit**

```bash
git add internal/store/parse.go internal/store/parse_test.go internal/store/testdata/sample.md
git commit -m "feat: parse content store markdown into typed units"
```

---

## Task 7: Wire the `validate` subcommand

**Files:**
- Modify: `cmd/tailor/run.go` (replace the `runValidate` stub from Task 2)
- Test: `cmd/tailor/run_test.go` (add a case)

- [ ] **Step 1: Write the failing test (append to `run_test.go`)**

```go
func TestRun_Validate_Summarizes(t *testing.T) {
	code, out, errOut := runCapture("validate", "../../internal/store/testdata/sample.md")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, errOut)
	}
	for _, want := range []string{"Jane Doe", "roles: 2", "achievements: 4", "skills: 4"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, missing %q", out, want)
		}
	}
}

func TestRun_Validate_MissingFile(t *testing.T) {
	code, _, _ := runCapture("validate")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 when no path given", code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tailor/ -run TestRun_Validate`
Expected: FAIL — stub prints "not implemented", assertions fail.

- [ ] **Step 3: Replace the `runValidate` stub in `run.go`**

Remove the stub added in Task 2 and add the real implementation. Add `"github.com/farazhassan/tailor-swift/internal/store"` to the imports:
```go
func runValidate(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: tailor validate <content.md>")
		return 2
	}
	s, err := store.Parse(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "validate: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "name: %s\n", s.Profile.Name)
	fmt.Fprintf(stdout, "roles: %d\n", len(s.Roles))
	fmt.Fprintf(stdout, "achievements: %d\n", len(s.Achievements()))
	fmt.Fprintf(stdout, "skills: %d\n", len(s.Skills))
	return 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 5: Manual smoke test**

Run: `go run ./cmd/tailor validate internal/store/testdata/sample.md`
Expected output:
```
name: Jane Doe
roles: 2
achievements: 4
skills: 4
```

- [ ] **Step 6: Commit**

```bash
git add cmd/tailor/run.go cmd/tailor/run_test.go
git commit -m "feat: add validate subcommand to summarize a content store"
```

---

## Task 8: Final verification

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: all packages `ok`.

- [ ] **Step 2: Vet and build**

Run: `go vet ./... && go build ./...`
Expected: no output, exit 0.

- [ ] **Step 3: Confirm branch state**

Run: `git log --oneline feat/foundations`
Expected: the commits from Tasks 1–7, all on `feat/foundations`, `main` untouched.

---

## Self-Review Notes

- **Spec coverage:** This plan covers the spec's *content store* (lightly-structured markdown → atomic achievement units with derived ids + file/line provenance) and the *CLI surface skeleton* (`ingest`/`generate`/`evaluate` stubs). `validate` is an added review aid supporting the spec's human-review-of-`content.md` requirement. Embeddings, retrieval, JD fetch, generate, render, evaluate, orchestrate, and ingest are explicitly deferred to Plans 2–7.
- **No live keys / no gantry:** intentional — this plan is fully testable offline, establishing the foundation later plans build on.
- **Type consistency:** `Store`, `Achievement`, `Provenance`, `DeriveID`, `parseTags`, `parseRoleHeading`, and `Store.Achievements()` are used consistently across tasks; the `runValidate` stub in Task 2 is explicitly replaced in Task 7.
