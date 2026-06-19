package render

import (
	"fmt"

	"github.com/farazhassan/tailor-swift/internal/generate"
	"github.com/farazhassan/tailor-swift/internal/store"
)

// bulletView is one rendered bullet. Text is the generator's rephrasing, escaped.
type bulletView struct {
	Text string
}

// roleView is one resume section: a role with the bullets selected under it.
// All string fields are LaTeX-escaped.
type roleView struct {
	Company string
	Title   string
	Start   string
	End     string
	Bullets []bulletView
}

// resumeView is the flat, LaTeX-escaped data a template renders. Roles appear in
// store order and include only roles that have at least one selected bullet.
type resumeView struct {
	Name     string
	Email    string
	Phone    string
	Location string
	Links    []string
	Roles    []roleView
	Skills   []string
}

// buildView maps each selected bullet back to the role its source achievement
// belongs to (via the store), groups bullets under roles in store order, and
// escapes every user-derived field. A bullet whose UnitID is not in the store is
// an error (defense-in-depth for the truthfulness contract; the generator should
// already have rejected it). A role with no selected bullets is omitted.
func buildView(s *store.Store, res *generate.Result) (resumeView, error) {
	roleOf := make(map[string]int)
	for ri, r := range s.Roles {
		for _, p := range r.Projects {
			for _, a := range p.Achievements {
				roleOf[a.ID] = ri
			}
		}
	}

	rv := make([]roleView, len(s.Roles))
	for i, r := range s.Roles {
		rv[i] = roleView{
			Company: escapeTeX(r.Company),
			Title:   escapeTeX(r.Title),
			Start:   escapeTeX(r.Start),
			End:     escapeTeX(r.End),
		}
	}

	used := make([]bool, len(s.Roles))
	for _, b := range res.Bullets {
		ri, ok := roleOf[b.UnitID]
		if !ok {
			return resumeView{}, fmt.Errorf("render: bullet references unit id %q not in store", b.UnitID)
		}
		rv[ri].Bullets = append(rv[ri].Bullets, bulletView{Text: escapeTeX(b.Text)})
		used[ri] = true
	}

	var roles []roleView
	for i := range rv {
		if used[i] {
			roles = append(roles, rv[i])
		}
	}

	skills := make([]string, len(s.Skills))
	for i, sk := range s.Skills {
		skills[i] = escapeTeX(sk.Raw)
	}
	links := make([]string, len(s.Profile.Contact.Links))
	for i, l := range s.Profile.Contact.Links {
		links[i] = escapeTeX(l)
	}

	return resumeView{
		Name:     escapeTeX(s.Profile.Name),
		Email:    escapeTeX(s.Profile.Contact.Email),
		Phone:    escapeTeX(s.Profile.Contact.Phone),
		Location: escapeTeX(s.Profile.Contact.Location),
		Links:    links,
		Roles:    roles,
		Skills:   skills,
	}, nil
}
