package store

// Provenance records where a unit came from in the source file.
type Provenance struct {
	File string
	Line int // 1-based line number
}

type Contact struct {
	Email    string
	Phone    string
	Location string
	Links    []string
}

type Profile struct {
	Name    string
	Contact Contact
}

type Achievement struct {
	ID         string
	Text       string
	Tags       []string
	Provenance Provenance
}

type Project struct {
	Name         string
	Tags         []string
	Achievements []Achievement
	Provenance   Provenance
}

type Role struct {
	Company    string
	Title      string
	Start      string
	End        string
	Projects   []Project
	Provenance Provenance
}

type Skill struct {
	Raw string // e.g. "Go (expert)"
}

type Store struct {
	Source  string // path the store was parsed from
	Profile Profile
	Roles   []Role
	Skills  []Skill
}

// Achievements flattens all achievements across roles/projects. Later plans
// embed and retrieve over this list.
func (s *Store) Achievements() []Achievement {
	var out []Achievement
	for _, r := range s.Roles {
		for _, p := range r.Projects {
			out = append(out, p.Achievements...)
		}
	}
	return out
}
