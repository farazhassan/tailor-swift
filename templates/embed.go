// Package templates provides the built-in LaTeX resume template, embedded into
// the binary so the CLI works from any working directory.
package templates

import _ "embed"

// Default is the built-in LaTeX template (templates/default.tex), used unless
// the caller supplies an override.
//
//go:embed default.tex
var Default string
