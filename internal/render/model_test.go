package render

import (
	"testing"

	"github.com/farazhassan/tailor-swift/internal/generate"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// twoRoleStore returns a store with two roles; role A owns u1, role B owns u2.
func twoRoleStore() *store.Store {
	return &store.Store{
		Profile: store.Profile{
			Name: "Ada Lovelace",
			Contact: store.Contact{
				Email:    "ada@x.com",
				Phone:    "555-0100",
				Location: "London",
				Links:    []string{"github.com/ada"},
			},
		},
		Roles: []store.Role{
			{
				Company: "Acme & Co", Title: "Engineer", Start: "2021", End: "2024",
				Projects: []store.Project{{Achievements: []store.Achievement{{ID: "u1", Text: "orig one"}}}},
			},
			{
				Company: "Globex", Title: "Lead", Start: "2018", End: "2021",
				Projects: []store.Project{{Achievements: []store.Achievement{{ID: "u2", Text: "orig two"}}}},
			},
		},
		Skills: []store.Skill{{Raw: "Go (expert)"}, {Raw: "C#"}},
	}
}

func TestBuildViewGroupsBulletsUnderTheirRoleInStoreOrder(t *testing.T) {
	s := twoRoleStore()
	// Generator emits u2's bullet first, then u1's — but roles render in store order.
	res := &generate.Result{Bullets: []generate.Bullet{
		{UnitID: "u2", Text: "Led 10 engineers"},
		{UnitID: "u1", Text: "Cut latency 40%"},
	}}

	v, err := buildView(s, res)
	if err != nil {
		t.Fatalf("buildView: %v", err)
	}
	if len(v.Roles) != 2 {
		t.Fatalf("roles = %d, want 2", len(v.Roles))
	}
	// Store order: Acme first, Globex second.
	if v.Roles[0].Company != `Acme \& Co` {
		t.Errorf("role[0].Company = %q, want escaped Acme", v.Roles[0].Company)
	}
	if v.Roles[1].Company != "Globex" {
		t.Errorf("role[1].Company = %q", v.Roles[1].Company)
	}
	// Bullets land under the right role, and Text is the generator's text, escaped.
	if len(v.Roles[0].Bullets) != 1 || v.Roles[0].Bullets[0].Text != `Cut latency 40\%` {
		t.Errorf("role[0].Bullets = %+v", v.Roles[0].Bullets)
	}
	if len(v.Roles[1].Bullets) != 1 || v.Roles[1].Bullets[0].Text != "Led 10 engineers" {
		t.Errorf("role[1].Bullets = %+v", v.Roles[1].Bullets)
	}
}

func TestBuildViewOmitsRolesWithoutBullets(t *testing.T) {
	s := twoRoleStore()
	res := &generate.Result{Bullets: []generate.Bullet{{UnitID: "u1", Text: "only acme"}}}
	v, err := buildView(s, res)
	if err != nil {
		t.Fatalf("buildView: %v", err)
	}
	if len(v.Roles) != 1 || v.Roles[0].Company != `Acme \& Co` {
		t.Errorf("expected only the Acme role, got %+v", v.Roles)
	}
}

func TestBuildViewErrorsOnUnknownUnitID(t *testing.T) {
	s := twoRoleStore()
	res := &generate.Result{Bullets: []generate.Bullet{{UnitID: "ghost", Text: "x"}}}
	if _, err := buildView(s, res); err == nil {
		t.Error("buildView: want error when a bullet references an unknown unit id, got nil")
	}
}

func TestBuildViewEscapesProfileAndSkills(t *testing.T) {
	s := twoRoleStore()
	res := &generate.Result{Bullets: []generate.Bullet{{UnitID: "u1", Text: "x"}}}
	v, err := buildView(s, res)
	if err != nil {
		t.Fatalf("buildView: %v", err)
	}
	if v.Name != "Ada Lovelace" || v.Email != "ada@x.com" {
		t.Errorf("profile = %q / %q", v.Name, v.Email)
	}
	if len(v.Skills) != 2 || v.Skills[1] != `C\#` {
		t.Errorf("skills = %+v, want C# escaped", v.Skills)
	}
}
