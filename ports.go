package budgetengine

import (
	"context"
	"time"
)

// Persistence ports are split read/write because one host writes spend and the other does not.
// Prism's daemon observes state transitions and appends spend (SpendWriter); prp's spend is written
// by an external collector/CUR pipeline (not prp's code), so prp wires only the read ports. The
// engine's evaluate/check paths need ONLY the read ports + Clock.

// SpendSource lists the actuals ledger for a scope, in append order. Both hosts implement it.
type SpendSource interface {
	Spend(ctx context.Context, scope Scope) ([]SpendEvent, error)
}

// PlanSource lists the plan-mutation log for a scope, in Seq order. Both hosts implement it.
type PlanSource interface {
	Plan(ctx context.Context, scope Scope) ([]PlanEvent, error)
}

// SpendWriter appends a spend event. The appending host (Prism) implements it; a read-only host
// (prp) omits it — its writes come from the external collector.
type SpendWriter interface {
	AppendSpend(ctx context.Context, scope Scope, e SpendEvent) error
}

// PlanWriter appends a plan-mutation event. Implemented by the host that edits the plan.
type PlanWriter interface {
	AppendPlan(ctx context.Context, scope Scope, e PlanEvent) error
}

// ActionSink is the reactive (push) enforcement port: the engine hands the host a fresh BurnState
// after spend moves, and the host performs any effect (alert, hibernate, throttle). The engine
// decides *whether*; the host decides *how*. A read-mostly host may leave it a no-op.
type ActionSink interface {
	OnState(ctx context.Context, scope Scope, s BurnState) error
}

// Clock supplies "now", injectable for determinism and tests.
type Clock interface {
	Now() time.Time
}

// SystemClock is the real wall clock.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// FixedClock is a deterministic Clock for tests.
type FixedClock struct{ T time.Time }

// Now returns the fixed time.
func (c FixedClock) Now() time.Time { return c.T }
