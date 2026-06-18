package store

import (
	"path/filepath"
	"testing"
)

func TestParse_Sample(t *testing.T) {
	path := filepath.Join("testdata", "sample.md")
	s, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if s.Profile.Name != "Jane Doe" {
		t.Errorf("name = %q", s.Profile.Name)
	}
	if s.Profile.Contact.Email != "jane@example.com" {
		t.Errorf("email = %q", s.Profile.Contact.Email)
	}
	if s.Profile.Contact.Phone != "555-0100" {
		t.Errorf("phone = %q", s.Profile.Contact.Phone)
	}
	if s.Profile.Contact.Location != "Remote" {
		t.Errorf("location = %q", s.Profile.Contact.Location)
	}
	if len(s.Profile.Contact.Links) != 2 {
		t.Errorf("links = %v", s.Profile.Contact.Links)
	} else {
		if s.Profile.Contact.Links[0] != "https://github.com/jane" {
			t.Errorf("links[0] = %q", s.Profile.Contact.Links[0])
		}
		if s.Profile.Contact.Links[1] != "https://linkedin.com/in/jane" {
			t.Errorf("links[1] = %q", s.Profile.Contact.Links[1])
		}
	}

	if len(s.Roles) != 2 {
		t.Fatalf("roles = %d, want 2", len(s.Roles))
	}
	r0 := s.Roles[0]
	if r0.Company != "Acme" || r0.Title != "Senior Engineer" || r0.End != "present" {
		t.Errorf("role0 = %+v", r0)
	}
	if len(r0.Projects) != 2 {
		t.Fatalf("role0 projects = %d, want 2", len(r0.Projects))
	}
	p0 := r0.Projects[0]
	if p0.Name != "Billing platform revamp" {
		t.Errorf("project0 name = %q", p0.Name)
	}
	if len(p0.Tags) != 2 { // go, payments
		t.Errorf("project0 tags = %v", p0.Tags)
	}
	if len(p0.Achievements) != 2 {
		t.Fatalf("project0 achievements = %d, want 2", len(p0.Achievements))
	}

	a0 := p0.Achievements[0]
	if a0.Text != "Cut billing settlement latency 40% by sharding the ledger" {
		t.Errorf("ach0 text = %q", a0.Text)
	}
	if a0.ID == "" || a0.ID[:4] != "ach_" {
		t.Errorf("ach0 id = %q", a0.ID)
	}
	if a0.Provenance.File != path {
		t.Errorf("ach0 provenance file = %q", a0.Provenance.File)
	}
	if a0.Provenance.Line == 0 {
		t.Error("ach0 provenance line not set")
	}

	if len(s.Skills) != 4 {
		t.Errorf("skills = %v, want 4", s.Skills)
	} else if s.Skills[0].Raw != "Go (expert)" {
		t.Errorf("skills[0].Raw = %q, want %q", s.Skills[0].Raw, "Go (expert)")
	}

	if got := len(s.Achievements()); got != 4 {
		t.Errorf("flattened achievements = %d, want 4", got)
	}
}

func TestParse_BulletWithoutProjectIsError(t *testing.T) {
	_, err := ParseReader([]byte("## Acme — Engineer\n- orphan bullet\n"), "mem")
	if err == nil {
		t.Fatal("expected error for bullet with no project")
	}
}

func TestParse_AsteriskBullet(t *testing.T) {
	s, err := ParseReader([]byte("## Acme — Engineer\n### P\n* did a thing\n"), "mem")
	if err != nil {
		t.Fatal(err)
	}
	got := s.Achievements()
	if len(got) != 1 || got[0].Text != "did a thing" {
		t.Fatalf("achievements = %+v", got)
	}
}

func TestParse_ProjectHeadingOutsideRoleIsError(t *testing.T) {
	_, err := ParseReader([]byte("### Orphan project\n"), "mem")
	if err == nil {
		t.Fatal("expected error for project heading with no role")
	}
}

func TestParse_BulletWithNoRoleIsError(t *testing.T) {
	_, err := ParseReader([]byte("- orphan bullet with no role at all\n"), "mem")
	if err == nil {
		t.Fatal("expected error for bullet with no role")
	}
}

func TestParse_EmptyInput(t *testing.T) {
	s, err := ParseReader([]byte(""), "mem")
	if err != nil {
		t.Fatalf("empty input should not error: %v", err)
	}
	if len(s.Roles) != 0 || len(s.Skills) != 0 {
		t.Fatalf("empty store expected, got %+v", s)
	}
}
