package budgetengine

import (
	"context"
	"fmt"
)

// Verdict is the outcome of a pre-flight budget check.
type Verdict int

const (
	// Allow — within budget, below the warn threshold.
	Allow Verdict = iota
	// Warn — allowed, but approaching the ceiling; the host surfaces headroom.
	Warn
	// Block — refused (frozen scope, or would exceed the ceiling).
	Block
)

// String renders the verdict.
func (v Verdict) String() string {
	switch v {
	case Allow:
		return "allow"
	case Warn:
		return "warn"
	case Block:
		return "block"
	default:
		return "unknown"
	}
}

// Decision is the result of CheckLaunch: a verdict plus the numbers that explain it. Shape adopted
// from prp's budget.Decision, which this engine replaces. Spent is always server-authoritative
// (from SpendSource) — never client-supplied.
type Decision struct {
	Verdict          Verdict
	EstimatedCost    float64 // the action's estimated cost, as checked
	EffectiveBalance float64 // the paced ceiling for this allocation right now
	Spent            float64 // authoritative cumulative spend
	Projected        float64 // Spent + EstimatedCost
	Remaining        float64 // headroom before the action (≥ 0)
	Reason           string  // human-facing explanation
}

// Allowed reports whether the launch may proceed (Warn is allowed).
func (d Decision) Allowed() bool { return d.Verdict != Block }

// CheckLaunch is the synchronous, budget-only pre-flight query. It reuses the exact same fold as
// Evaluate, then compares projected spend against the paced ceiling. It does NOT evaluate non-budget
// preconditions (e.g. an auto-stop policy) — the host composes those around this call.
//
// Ordered verdict rules (mirroring prp so both hosts decide identically):
//  1. Block   — allocation (or whole plan) is frozen. Unconditional.
//  2. Block   — projected > effective ceiling.
//  3. Warn    — projected ≥ ceiling × warnThreshold (default 0.80).
//  4. Allow   — within budget.
func (e *Engine) CheckLaunch(ctx context.Context, scope Scope, allocID string, estimatedCost float64) (Decision, error) {
	now := e.clock.Now()
	planLog, err := e.plan.Plan(ctx, scope)
	if err != nil {
		return Decision{}, err
	}
	spendLog, err := e.spend.Spend(ctx, scope)
	if err != nil {
		return Decision{}, err
	}
	st := Fold(planLog, spendLog, now, e.sourcing)
	as := st.Allocs[allocID]
	if as == nil {
		as = &AllocationState{}
	}

	eff := as.AvailableToDate
	spent := as.CumulativeSpend
	projected := spent + estimatedCost
	remaining := clampZero(eff - spent)

	d := Decision{
		EstimatedCost:    estimatedCost,
		EffectiveBalance: eff,
		Spent:            spent,
		Projected:        projected,
		Remaining:        remaining,
	}

	switch {
	case as.Frozen || st.Frozen[""]:
		d.Verdict = Block
		d.Reason = "scope is frozen (grant period ended / launch prevented); launches are blocked"
	case projected > eff:
		d.Verdict = Block
		d.Reason = fmt.Sprintf(
			"launch would exceed budget ceiling: projected $%.2f > ceiling $%.2f (spent $%.2f + est $%.2f)",
			projected, eff, spent, estimatedCost)
	case eff > 0 && projected >= eff*e.warn():
		d.Verdict = Warn
		d.Reason = fmt.Sprintf(
			"approaching budget ceiling: projected $%.2f of $%.2f (%.0f%%); $%.2f remaining after this launch",
			projected, eff, projected/eff*100, clampZero(eff-projected))
	default:
		d.Verdict = Allow
		d.Reason = fmt.Sprintf("within budget: projected $%.2f of $%.2f", projected, eff)
	}
	return d, nil
}
