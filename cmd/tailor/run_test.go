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
