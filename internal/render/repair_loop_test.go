package render

import (
	"context"
	"errors"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
)

// scriptedCompiler returns a CompileFunc that records each source it is given.
// If envErr is non-nil it always returns that error. Otherwise it returns the
// next result from results, repeating the last entry once exhausted.
func scriptedCompiler(results []CompileResult, envErr error) (CompileFunc, *[]string) {
	var seen []string
	i := 0
	fn := func(ctx context.Context, tex string) (CompileResult, error) {
		seen = append(seen, tex)
		if envErr != nil {
			return CompileResult{}, envErr
		}
		r := results[i]
		if i < len(results)-1 {
			i++
		}
		return r, nil
	}
	return fn, &seen
}

func TestCompileWithRepairSucceedsFirstTry(t *testing.T) {
	compile, seen := scriptedCompiler([]CompileResult{{OK: true, PDF: []byte("%PDF-1")}}, nil)
	mock := eval.NewMockLLMClient() // must not be called
	res, err := CompileWithRepair(context.Background(), compile, mock, "src", 2)
	if err != nil {
		t.Fatalf("CompileWithRepair: %v", err)
	}
	if !res.OK || res.Attempts != 1 || res.TeX != "src" {
		t.Errorf("res = %+v", res)
	}
	if len(*seen) != 1 {
		t.Errorf("compiles = %d, want 1", len(*seen))
	}
	if len(mock.Requests()) != 0 {
		t.Errorf("llm called %d times, want 0", len(mock.Requests()))
	}
}

func TestCompileWithRepairFixesThenSucceeds(t *testing.T) {
	compile, seen := scriptedCompiler([]CompileResult{
		{OK: false, Log: "! error one"},
		{OK: true, PDF: []byte("%PDF-1")},
	}, nil)
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: "fixed source", StopReason: gantry.StopReasonEnd})
	res, err := CompileWithRepair(context.Background(), compile, mock, "broken", 2)
	if err != nil {
		t.Fatalf("CompileWithRepair: %v", err)
	}
	if !res.OK || res.Attempts != 2 || res.TeX != "fixed source" {
		t.Errorf("res = %+v", res)
	}
	if len(*seen) != 2 || (*seen)[1] != "fixed source" {
		t.Errorf("seen = %v", *seen)
	}
	if len(mock.Requests()) != 1 {
		t.Errorf("llm calls = %d, want 1", len(mock.Requests()))
	}
}

func TestCompileWithRepairExhaustsAndReturnsErrCompileFailed(t *testing.T) {
	compile, seen := scriptedCompiler([]CompileResult{{OK: false, Log: "! always broken"}}, nil)
	mock := eval.NewMockLLMClient(
		gantry.LLMResponse{Content: "try1", StopReason: gantry.StopReasonEnd},
		gantry.LLMResponse{Content: "try2", StopReason: gantry.StopReasonEnd},
	)
	res, err := CompileWithRepair(context.Background(), compile, mock, "broken", 2)
	if !errors.Is(err, ErrCompileFailed) {
		t.Fatalf("err = %v, want ErrCompileFailed", err)
	}
	if res.OK || res.Attempts != 3 {
		t.Errorf("res = %+v", res)
	}
	if res.TeX != "try2" {
		t.Errorf("final tex = %q, want last repair", res.TeX)
	}
	if len(*seen) != 3 {
		t.Errorf("compiles = %d, want 3", len(*seen))
	}
	if len(mock.Requests()) != 2 {
		t.Errorf("llm calls = %d, want 2", len(mock.Requests()))
	}
}

func TestCompileWithRepairAbortsOnCompilerEnvError(t *testing.T) {
	compile, _ := scriptedCompiler(nil, errors.New("pdflatex not found"))
	mock := eval.NewMockLLMClient() // must not be called
	_, err := CompileWithRepair(context.Background(), compile, mock, "src", 2)
	if err == nil || errors.Is(err, ErrCompileFailed) {
		t.Fatalf("err = %v, want a wrapped environment error (not ErrCompileFailed)", err)
	}
	if len(mock.Requests()) != 0 {
		t.Errorf("llm should not be called on an environment error")
	}
}
