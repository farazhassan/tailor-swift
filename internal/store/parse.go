package store

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Parse reads and parses a content store markdown file.
func Parse(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseReader(data, path)
}

type section int

const (
	secNone section = iota
	secContact
	secSkills
	secRole
)

// ParseReader parses content store markdown from raw bytes. path is recorded
// as provenance for each unit.
func ParseReader(data []byte, path string) (*Store, error) {
	s := &Store{Source: path}
	sec := secNone

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "# "):
			s.Profile.Name = strings.TrimSpace(line[2:])
			sec = secNone

		case strings.HasPrefix(line, "## "):
			heading := strings.TrimSpace(line[3:])
			switch strings.ToLower(heading) {
			case "contact":
				sec = secContact
			case "skills":
				sec = secSkills
			default:
				c, ti, st, e := parseRoleHeading(heading)
				s.Roles = append(s.Roles, Role{
					Company: c, Title: ti, Start: st, End: e,
					Provenance: Provenance{File: path, Line: lineNo},
				})
				sec = secRole
			}

		case strings.HasPrefix(line, "### "):
			if sec != secRole || len(s.Roles) == 0 {
				return nil, fmt.Errorf("%s:%d: project heading outside of a role", path, lineNo)
			}
			name, tags := parseTags(strings.TrimSpace(line[4:]))
			r := &s.Roles[len(s.Roles)-1]
			r.Projects = append(r.Projects, Project{
				Name: name, Tags: tags,
				Provenance: Provenance{File: path, Line: lineNo},
			})

		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			text, tags := parseTags(strings.TrimSpace(trimmed[2:]))
			if sec != secRole || len(s.Roles) == 0 {
				return nil, fmt.Errorf("%s:%d: achievement bullet outside of a role/project", path, lineNo)
			}
			r := &s.Roles[len(s.Roles)-1]
			if len(r.Projects) == 0 {
				return nil, fmt.Errorf("%s:%d: achievement bullet with no project", path, lineNo)
			}
			p := &r.Projects[len(r.Projects)-1]
			p.Achievements = append(p.Achievements, Achievement{
				ID: DeriveID(text), Text: text, Tags: tags,
				Provenance: Provenance{File: path, Line: lineNo},
			})

		default: // non-heading, non-bullet content
			switch sec {
			case secContact:
				applyContactLine(&s.Profile.Contact, trimmed)
			case secSkills:
				for _, sk := range strings.Split(trimmed, ",") {
					if v := strings.TrimSpace(sk); v != "" {
						s.Skills = append(s.Skills, Skill{Raw: v})
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func applyContactLine(c *Contact, line string) {
	key, val, ok := strings.Cut(line, ":")
	if !ok {
		return
	}
	val = strings.TrimSpace(val)
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "email":
		c.Email = val
	case "phone":
		c.Phone = val
	case "location":
		c.Location = val
	case "links":
		for _, l := range strings.Split(val, ",") {
			if v := strings.TrimSpace(l); v != "" {
				c.Links = append(c.Links, v)
			}
		}
	}
}
