package render

import (
	"strings"
	"testing"

	"github.com/farazhassan/tailor-swift/internal/generate"
)

const testTemplate = `NAME: {{.Name}}
{{range .Roles}}ROLE: {{.Company}} | {{.Title}} ({{.Start}}-{{.End}})
{{range .Bullets}}* {{.Text}}
{{end}}{{end}}{{if .Skills}}SKILLS: {{range $i, $s := .Skills}}{{if $i}}, {{end}}{{$s}}{{end}}
{{end}}`

func TestRenderFillsTemplateWithEscapedContent(t *testing.T) {
	s := twoRoleStore() // defined in model_test.go (same package)
	res := &generate.Result{Bullets: []generate.Bullet{
		{UnitID: "u1", Text: "Cut cost 50%"},
		{UnitID: "u2", Text: "Led & shipped"},
	}}

	out, err := Render(testTemplate, s, res)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"NAME: Ada Lovelace",
		`ROLE: Acme \& Co | Engineer (2021-2024)`,
		`* Cut cost 50\%`,
		"ROLE: Globex | Lead (2018-2021)",
		`* Led \& shipped`,
		`SKILLS: Go (expert), C\#`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderErrorsOnUnknownUnitID(t *testing.T) {
	s := twoRoleStore()
	res := &generate.Result{Bullets: []generate.Bullet{{UnitID: "ghost", Text: "x"}}}
	if _, err := Render(testTemplate, s, res); err == nil {
		t.Error("Render: want error for unknown unit id, got nil")
	}
}

func TestRenderErrorsOnBadTemplate(t *testing.T) {
	s := twoRoleStore()
	res := &generate.Result{Bullets: []generate.Bullet{{UnitID: "u1", Text: "x"}}}
	if _, err := Render("{{.Name", s, res); err == nil {
		t.Error("Render: want parse error for malformed template, got nil")
	}
}
