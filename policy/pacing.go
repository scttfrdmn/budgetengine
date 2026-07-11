package policy

import (
	"time"

	be "github.com/scttfrdmn/budgetengine"
)

// BankAndReserve holds the go-forward rate at the *nominal* (planned) level rather than re-pacing to
// consume the remainder by the deadline: it reports the rate the funding curve grants over the rest of
// the window, capped by solvency. Under-spending therefore banks — the recomputed rate stays at plan
// instead of floating up — which is what makes a later deliberate burst across a funding gap possible.
// This is the stateful mode (§4 Axis 2 of the architecture doc), and the correct choice for a
// multi-month grant that must last to its end date.
type BankAndReserve struct{}

// SustainableRate = min(nominalForwardRate, remaining_principal / remaining_time), USD/second, where
// nominalForwardRate is the funding curve integrated over (now, window.End] spread across that span.
// Holding the nominal rate is what banks underspend; the solvency term caps it so an over-funded tail
// can never project spending more real money than remains. Zero when the window has closed or nothing
// remains. Falls back to the solvency rate when no capacity curve is available (degenerate plans).
func (BankAndReserve) SustainableRate(as be.AllocationState, window be.Window, now time.Time) float64 {
	rem := window.End.Sub(now).Seconds()
	if rem <= 0 || as.RemainingPrincipal <= 0 {
		return 0
	}
	solvencyRate := as.RemainingPrincipal / rem

	// Nominal forward rate: what the funding curve grants from now to the window end, evenly paced.
	nominalForward := as.Capacity.AvailableBetween(now, window.End) / rem
	if nominalForward <= 0 {
		// No forward capacity described (e.g. single-source parity plans with no curve past now) —
		// fall back to the solvency rate so the projection still reflects the remaining budget.
		return solvencyRate
	}
	if nominalForward < solvencyRate {
		return nominalForward // hold the plan rate; underspend banks instead of inflating the rate
	}
	return solvencyRate // never project spending faster than real money allows
}

var _ be.PacingPolicy = BankAndReserve{}

// RateAdjust is the memoryless alternative: it always re-paces the remaining principal over the
// remaining window, so idling simply lets the recomputed rate float up next time (underspend is NOT
// banked — it is redistributed forward). This is the correct choice for a simple cloud budget that
// answers "at my current pace, when do I run out?". It differs from BankAndReserve whenever the
// remaining principal exceeds the nominal forward capacity (i.e. after under-spending): RateAdjust
// reports the higher re-paced rate, BankAndReserve holds the lower nominal rate. The two coincide when
// spend has tracked the plan exactly.
type RateAdjust struct{}

// SustainableRate = remaining_principal / remaining_time (re-paced; no reservation).
func (RateAdjust) SustainableRate(as be.AllocationState, window be.Window, now time.Time) float64 {
	rem := window.End.Sub(now).Seconds()
	if rem <= 0 || as.RemainingPrincipal <= 0 {
		return 0
	}
	return as.RemainingPrincipal / rem
}

var _ be.PacingPolicy = RateAdjust{}
