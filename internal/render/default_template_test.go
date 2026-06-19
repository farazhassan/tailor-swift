package render

import (
	"os"
	"strings"
	"testing"

	"github.com/farazhassan/tailor-swift/internal/generate"
)

// readDefaultTemplate loads templates/default.tex relative to the repo root.
// `go test` runs with the package directory (internal/render) as CWD, so the
// repo root is two levels up.
func readDefaultTemplate(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../templates/default.tex")
	if err != nil {
		t.Fatalf("read default template: %v", err)
	}
	return string(data)
}

func TestDefaultTemplateRendersValidLatexSkeleton(t *testing.T) {
	tmpl := readDefaultTemplate(t)
	s := twoRoleStore()
	res := &generate.Result{Bullets: []generate.Bullet{
		{UnitID: "u1", Text: "Cut cost 50%"},
		{UnitID: "u2", Text: "Led the team"},
	}}

	out, err := Render(tmpl, s, res)
	if err != nil {
		t.Fatalf("Render with default template: %v", err)
	}
	for _, want := range []string{
		`\documentclass`,
		`\begin{document}`,
		`\end{document}`,
		"Ada Lovelace",
		`Acme \& Co`,
		`\item Cut cost 50\%`,
		`\item Led the team`,
		"Skills",
		`C\#`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("default-template output missing %q\n---\n%s", want, out)
		}
	}
	// Balanced document environment.
	if strings.Count(out, `\begin{document}`) != 1 || strings.Count(out, `\end{document}`) != 1 {
		t.Errorf("document environment not balanced:\n%s", out)
	}
}
