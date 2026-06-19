package orchestrate

import (
	"context"
	"errors"
	"testing"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/limiter"
	"github.com/farazhassan/gantry/eval"
)

func TestBudgetedClientRecordsThenBlocks(t *testing.T) {
	mock := eval.NewMockLLMClient(
		gantry.LLMResponse{Content: "a", StopReason: gantry.StopReasonEnd, Usage: gantry.Usage{InputTokens: 80}},
		gantry.LLMResponse{Content: "b", StopReason: gantry.StopReasonEnd, Usage: gantry.Usage{InputTokens: 80}},
	)
	lim := limiter.NewBudget(limiter.Limits{MaxTokens: 100})
	c := newBudgetedClient(mock, lim)

	// First call: accumulated total is 0, allowed; records 80.
	if _, err := c.Generate(context.Background(), gantry.LLMRequest{}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call: total 80 <= 100, still allowed; records → 160.
	if _, err := c.Generate(context.Background(), gantry.LLMRequest{}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	// Third call: total 160 > 100, refused before invoking the inner client.
	if _, err := c.Generate(context.Background(), gantry.LLMRequest{}); !errors.Is(err, gantry.ErrLimitExceeded) {
		t.Fatalf("third call err = %v, want ErrLimitExceeded", err)
	}
	// The inner mock was only invoked twice (third was refused).
	if len(mock.Requests()) != 2 {
		t.Errorf("inner calls = %d, want 2", len(mock.Requests()))
	}
}

func TestBudgetedClientUnlimitedPassesThrough(t *testing.T) {
	mock := eval.NewMockLLMClient(
		gantry.LLMResponse{Content: "a", StopReason: gantry.StopReasonEnd, Usage: gantry.Usage{InputTokens: 1000}},
		gantry.LLMResponse{Content: "b", StopReason: gantry.StopReasonEnd, Usage: gantry.Usage{InputTokens: 1000}},
	)
	c := newBudgetedClient(mock, limiter.NewBudget(limiter.Limits{})) // zero limits = unlimited
	for i := 0; i < 2; i++ {
		if _, err := c.Generate(context.Background(), gantry.LLMRequest{}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}
