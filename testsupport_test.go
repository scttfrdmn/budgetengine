package budgetengine

import (
	"context"
	"time"
)

// In-memory read ports — the engine's equivalent of a fake store. Keeps every test host-free.

type memStore struct {
	spend []SpendEvent
	plan  []PlanEvent
}

func (m *memStore) Spend(_ context.Context, _ Scope) ([]SpendEvent, error) { return m.spend, nil }
func (m *memStore) Plan(_ context.Context, _ Scope) ([]PlanEvent, error)   { return m.plan, nil }

func day(y int, mo time.Month, d int) time.Time {
	return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
}

// usdPerDay converts a whole-window amount into the even-spread rate the fold produces, so tests can
// assert in dollars/day without recomputing seconds.
func usdPerSecond(amount float64, from, to time.Time) float64 {
	return amount / to.Sub(from).Seconds()
}

func approx(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}
