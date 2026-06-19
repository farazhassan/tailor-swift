package render

import (
	"context"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
)

func TestRepairTeXReturnsFixedSourceStrippingFence(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    "```latex\n\\documentclass{article}\\begin{document}ok\\end{document}\n```",
		StopReason: gantry.StopReasonEnd,
	})
	got, err := repairTeX(context.Background(), mock, "broken source", "! Undefined control sequence")
	if err != nil {
		t.Fatalf("repairTeX: %v", err)
	}
	if strings.Contains(got, "```") {
		t.Errorf("code fence not stripped: %q", got)
	}
	if !strings.Contains(got, `\documentclass`) {
		t.Errorf("repaired source missing document: %q", got)
	}
	// The error log and the broken source must both reach the model.
	reqs := mock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("llm requests = %d, want 1", len(reqs))
	}
	msg := reqs[0].Messages[0].Content
	if !strings.Contains(msg, "Undefined control sequence") || !strings.Contains(msg, "broken source") {
		t.Errorf("repair message missing context:\n%s", msg)
	}
}

func TestRepairTeXErrorsOnEmptyReply(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{Content: "   ", StopReason: gantry.StopReasonEnd})
	if _, err := repairTeX(context.Background(), mock, "x", "err"); err == nil {
		t.Error("repairTeX: want error on empty repair, got nil")
	}
}

func TestTailLines(t *testing.T) {
	if got := tailLines("a\nb\nc\nd", 2); got != "c\nd" {
		t.Errorf("tailLines = %q, want %q", got, "c\nd")
	}
	if got := tailLines("only", 5); got != "only" {
		t.Errorf("tailLines = %q, want %q", got, "only")
	}
}
