package render

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CompileResult is the outcome of one LaTeX compilation.
type CompileResult struct {
	OK  bool   // a PDF was produced
	PDF []byte // compiled PDF bytes; nil when OK is false
	Log string // compiler output and .log contents, for diagnostics and repair
}

// CompileFunc compiles LaTeX source into a PDF. Implementations return OK=false
// with a populated Log on a LaTeX error (so the repair loop can react), and a
// non-nil error only for environment failures (e.g. the compiler is missing).
type CompileFunc func(ctx context.Context, tex string) (CompileResult, error)

// PDFLaTeX compiles tex by invoking pdflatex in a fresh temporary directory. A
// single pass suffices for single-page resumes (no bibliography or
// cross-references). The PDF bytes are read into the result before the temp
// directory is removed. A LaTeX error yields OK=false with the log and a nil
// error; a missing pdflatex binary yields a non-nil error.
func PDFLaTeX(ctx context.Context, tex string) (CompileResult, error) {
	dir, err := os.MkdirTemp("", "tailor-render-*")
	if err != nil {
		return CompileResult{}, fmt.Errorf("render: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	texPath := filepath.Join(dir, "resume.tex")
	if err := os.WriteFile(texPath, []byte(tex), 0o644); err != nil {
		return CompileResult{}, fmt.Errorf("render: write tex: %w", err)
	}

	cmd := exec.CommandContext(ctx, "pdflatex",
		"-interaction=nonstopmode", "-halt-on-error",
		"-output-directory", dir, texPath)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()

	var log strings.Builder
	log.Write(out)
	if logBytes, rerr := os.ReadFile(filepath.Join(dir, "resume.log")); rerr == nil {
		log.WriteString("\n")
		log.Write(logBytes)
	}

	// A missing binary is an environment error, distinct from a LaTeX error.
	if runErr != nil && errors.Is(runErr, exec.ErrNotFound) {
		return CompileResult{}, fmt.Errorf("render: pdflatex not found: %w", runErr)
	}
	// A non-zero exit (LaTeX error) is reported via OK below, not as a Go error.

	pdf, perr := os.ReadFile(filepath.Join(dir, "resume.pdf"))
	if perr != nil {
		return CompileResult{OK: false, Log: log.String()}, nil
	}
	return CompileResult{OK: true, PDF: pdf, Log: log.String()}, nil
}
