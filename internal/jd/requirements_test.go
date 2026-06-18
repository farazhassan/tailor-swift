package jd

import (
	"context"
	"strings"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/eval"
)

func TestExtractRequirementsParsesJSON(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    `[{"text":"Go","must_have":true},{"text":"Kafka","must_have":false}]`,
		StopReason: gantry.StopReasonEnd,
	})
	got, err := ExtractRequirements(context.Background(), mock, "we need a Go engineer")
	if err != nil {
		t.Fatalf("ExtractRequirements: %v", err)
	}
	if len(got) != 2 || got[0].Text != "Go" || !got[0].MustHave || got[1].MustHave {
		t.Errorf("requirements = %+v", got)
	}
	// The job text must be sent to the model.
	reqs := mock.Requests()
	if len(reqs) != 1 || !strings.Contains(reqs[0].Messages[0].Content, "Go engineer") {
		t.Errorf("LLM request = %+v", reqs)
	}
}

func TestExtractRequirementsStripsCodeFence(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    "```json\n[{\"text\":\"Go\",\"must_have\":true}]\n```",
		StopReason: gantry.StopReasonEnd,
	})
	got, err := ExtractRequirements(context.Background(), mock, "jd text")
	if err != nil {
		t.Fatalf("ExtractRequirements: %v", err)
	}
	if len(got) != 1 || got[0].Text != "Go" {
		t.Errorf("requirements = %+v", got)
	}
}

func TestExtractRequirementsErrorsOnBadJSON(t *testing.T) {
	mock := eval.NewMockLLMClient(gantry.LLMResponse{
		Content:    "not json at all",
		StopReason: gantry.StopReasonEnd,
	})
	if _, err := ExtractRequirements(context.Background(), mock, "jd text"); err == nil {
		t.Error("ExtractRequirements: want error on non-JSON content, got nil")
	}
}

func TestExtractRequirementsErrorsOnEmptyText(t *testing.T) {
	mock := eval.NewMockLLMClient()
	if _, err := ExtractRequirements(context.Background(), mock, "   "); err == nil {
		t.Error("ExtractRequirements: want error on empty job text, got nil")
	}
}
