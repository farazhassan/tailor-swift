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
	for _, cmd := range []string{"ingest", "evaluate"} {
		code, out, _ := runCapture(cmd)
		if code != 0 {
			t.Fatalf("%s exit code = %d, want 0", cmd, code)
		}
		if !strings.Contains(out, "not implemented") {
			t.Fatalf("%s stdout = %q, want not implemented", cmd, out)
		}
	}
}

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
