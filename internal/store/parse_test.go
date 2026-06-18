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
	if len(s.Profile.Contact.Links) != 2 {
		t.Errorf("links = %v", s.Profile.Contact.Links)
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
