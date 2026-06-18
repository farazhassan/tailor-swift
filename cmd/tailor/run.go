package main

import (
	"fmt"
	"io"

	"github.com/farazhassan/tailor-swift/internal/store"
)

const usage = `usage: tailor <command> [flags]

commands:
  ingest      build the content store from base resumes (not implemented)
  generate    generate a tailored resume for a job description (not implemented)
  evaluate    evaluate a resume against a job description (not implemented)
  validate    parse a content store file and print a summary
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "ingest", "generate", "evaluate":
		fmt.Fprintf(stdout, "%s: not implemented yet\n", args[0])
		return 0
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %q\n\n%s", args[0], usage)
		return 2
	}
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: tailor validate <content.md>")
		return 2
	}
	s, err := store.Parse(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "validate: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "name: %s\n", s.Profile.Name)
	fmt.Fprintf(stdout, "roles: %d\n", len(s.Roles))
	fmt.Fprintf(stdout, "achievements: %d\n", len(s.Achievements()))
	fmt.Fprintf(stdout, "skills: %d\n", len(s.Skills))
	return 0
}
