package policy

import (
	"time"

	be "github.com/scttfrdmn/budgetengine"
)

// BankAndReserve holds the go-forward rate at the *paced* level: remaining_principal spread over the
// remaining window. Under-spending accumulates as positive pace_deviation (banked headroom in the
// paced ceiling) rather than inflating the forward rate — the stateful mode, and the one that makes
// bursting across a funding gap possible. This is the default pacing policy.
type BankAndReserve struct{}

// SustainableRate = remaining_principal / remaining_time (window end − now), USD/second. Zero when
// the window has closed or nothing remains.
func (BankAndReserve) SustainableRate(as be.AllocationState, window be.Window, now time.Time) float64 {
	rem := window.End.Sub(now).Seconds()
	if rem <= 0 || as.RemainingPrincipal <= 0 {
		return 0
	}
	return as.RemainingPrincipal / rem
}

var _ be.PacingPolicy = BankAndReserve{}

// RateAdjust is the memoryless alternative: it reports the same remaining/remaining_time rate, but
// carries no banked reserve — idling simply lets the recomputed rate float up next time. For a
// single continuous source with no gaps the two policies coincide on the forward rate; they differ
// only in how the paced ceiling treats accrued underspend (RateAdjust does not reserve it). v0 ships
// both; the distinction is exercised by the disjoint-funding scenario test.
type RateAdjust struct{}

// SustainableRate = remaining_principal / remaining_time (same formula; no reservation).
func (RateAdjust) SustainableRate(as be.AllocationState, window be.Window, now time.Time) float64 {
	rem := window.End.Sub(now).Seconds()
	if rem <= 0 || as.RemainingPrincipal <= 0 {
		return 0
	}
	return as.RemainingPrincipal / rem
}

var _ be.PacingPolicy = RateAdjust{}
