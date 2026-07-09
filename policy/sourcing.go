// Package policy holds the built-in implementations of the three budgetengine policy axes:
// Sourcing (how dated sources combine and drain), Pacing (how idle allowance is treated), and
// Projection (which variable is pinned on deviation). Each axis is an interface in the core package;
// these are the v0 built-ins. New funding models are new implementations here — nothing else changes.
package policy

import (
	"sort"
	"time"

	be "github.com/scttfrdmn/budgetengine"
)

// ExpiryFirst spreads each source evenly over its own life for the nominal rate, and drains the
// soonest-to-expire source first ("use it before you lose it"). This is the default sourcing policy
// and covers all three worked scenarios in the design.
type ExpiryFirst struct{}

// NominalRate sums the even-spread rate of every active source in the segment. A segment with no
// active source yields 0 (a funding gap).
func (ExpiryFirst) NominalRate(active []be.FundingSource, _ be.Window) float64 {
	var r float64
	for _, s := range active {
		r += s.NominalRate()
	}
	return r
}

// DrainOrder ranks active sources by soonest End first; ties break by higher Priority, then ID.
func (ExpiryFirst) DrainOrder(active []be.FundingSource, _ time.Time) []be.FundingSource {
	out := append([]be.FundingSource(nil), active...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].End.Equal(out[j].End) {
			return out[i].End.Before(out[j].End)
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

var _ be.SourcingPolicy = ExpiryFirst{}
