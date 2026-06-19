package templates

import (
	"strings"
	"testing"
)

func TestDefaultTemplateEmbedded(t *testing.T) {
	if !strings.Contains(Default, `\documentclass`) {
		t.Errorf("Default missing \\documentclass; got %d bytes", len(Default))
	}
	if !strings.Contains(Default, `\end{document}`) {
		t.Errorf("Default missing \\end{document}")
	}
}
