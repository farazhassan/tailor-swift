package main

import (
	"fmt"
	"io"
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
	fmt.Fprintln(stdout, "validate: not implemented yet")
	return 0
}
