package orchestrate

import (
	"context"

	"github.com/farazhassan/gantry"
	"github.com/farazhassan/gantry/components/limiter"
)

// budgetedClient wraps a gantry.LLMClient and enforces a shared budget across
// every call: it refuses a call once the accumulated usage has crossed a
// configured limit (returning a gantry.ErrLimitExceeded-wrapped error without
// invoking the inner client) and records each response's usage afterward. A
// single BudgetLimiter is shared by the generate, evaluate, and repair clients
// so the cap covers the whole run.
type budgetedClient struct {
	inner gantry.LLMClient
	lim   *limiter.BudgetLimiter
}

// newBudgetedClient wraps inner so its usage counts against lim.
func newBudgetedClient(inner gantry.LLMClient, lim *limiter.BudgetLimiter) *budgetedClient {
	return &budgetedClient{inner: inner, lim: lim}
}

// Generate refuses the call if the accumulated budget is already exceeded;
// otherwise it calls the inner client and records the response usage. The
// limit-crossing call still completes (its usage is recorded); the next call is
// the one refused — matching gantry's own limiter middleware semantics.
func (c *budgetedClient) Generate(ctx context.Context, req gantry.LLMRequest) (gantry.LLMResponse, error) {
	if err := c.lim.Check(ctx, &gantry.State{Usage: c.lim.Total()}); err != nil {
		return gantry.LLMResponse{}, err
	}
	resp, err := c.inner.Generate(ctx, req)
	if err != nil {
		return resp, err
	}
	c.lim.Record(ctx, resp.Usage)
	return resp, nil
}
