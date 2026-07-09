package policy

import (
	"time"

	be "github.com/scttfrdmn/budgetengine"
)

// RateTargetPolicy is the fixed-date / floating-rate projection: the window end is sacred (a grant
// that must last to its end date). Bursting lowers the go-forward rate; the setpoint is "land at
// zero on the date." On-track means not meaningfully behind pace (pace_deviation ≥ −tolerance).
// This is the default projection policy.
type RateTargetPolicy struct {
	// Tolerance is the allowed dollars ahead-of-pace (borrowed) before OnTrack flips false. Zero
	// means any borrowing is off-track; a positive value permits transient bursting.
	Tolerance float64
}

// OnTrack reports whether pacing is healthy: not borrowed beyond Tolerance.
func (p RateTargetPolicy) OnTrack(as be.AllocationState, _ be.Window, _ time.Time) bool {
	return as.PaceDeviation >= -p.Tolerance
}

var _ be.ProjectionPolicy = RateTargetPolicy{}

// DeadlineFloatPolicy is the fixed-rate / floating-date projection: the chosen rate is sacred (a
// cloud budget — "spend how I want, tell me when I'm broke"). Zero is an outcome to forecast, not a
// target. On-track means the projected zero-date lands on or after the window end (you won't run out
// before you mean to).
type DeadlineFloatPolicy struct{}

// OnTrack reports whether remaining money outlasts the window: available_to_date ≥ spent implies
// you are pacing at-or-under budget, so you will not be broke before the window ends.
func (DeadlineFloatPolicy) OnTrack(as be.AllocationState, _ be.Window, _ time.Time) bool {
	return as.PaceDeviation >= 0
}

var _ be.ProjectionPolicy = DeadlineFloatPolicy{}
