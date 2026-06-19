package render

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/farazhassan/tailor-swift/internal/generate"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// Render fills templateText (a text/template) with the resume content derived
// from the store and the generator's selection, returning the resulting LaTeX
// source. All content is LaTeX-escaped before it reaches the template, so the
// template itself emits values verbatim. Rendering is deterministic: no LLM call
// and no PDF compilation (that is Plan 7). An unknown bullet unit id or a
// malformed template is an error.
func Render(templateText string, s *store.Store, res *generate.Result) (string, error) {
	view, err := buildView(s, res)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New("resume").Parse(templateText)
	if err != nil {
		return "", fmt.Errorf("render: parse template: %w", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, view); err != nil {
		return "", fmt.Errorf("render: execute template: %w", err)
	}
	return b.String(), nil
}
