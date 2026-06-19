//go:build pdflatex

package render

import (
	"context"
	"strings"
	"testing"
)

func TestPDFLaTeXCompilesValidDocument(t *testing.T) {
	tex := `\documentclass{article}
\begin{document}
Hello \textbf{world}.
\end{document}
`
	res, err := PDFLaTeX(context.Background(), tex)
	if err != nil {
		t.Fatalf("PDFLaTeX: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK; log:\n%s", res.Log)
	}
	if !strings.HasPrefix(string(res.PDF), "%PDF") {
		t.Errorf("PDF does not start with %%PDF magic; got %d bytes", len(res.PDF))
	}
}

func TestPDFLaTeXReportsErrorOnBrokenDocument(t *testing.T) {
	tex := `\documentclass{article}
\begin{document}
\thiscommanddoesnotexist
\end{document}
`
	res, err := PDFLaTeX(context.Background(), tex)
	if err != nil {
		t.Fatalf("PDFLaTeX returned an environment error: %v", err)
	}
	if res.OK {
		t.Error("expected OK=false for a broken document")
	}
	if res.Log == "" {
		t.Error("expected a non-empty log on failure")
	}
}
