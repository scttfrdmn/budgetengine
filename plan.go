package budgetengine

import "time"

// Window is a closed time interval [Start, End]. End may be lifted by a WindowExtended plan event
// (go-forward only — it never stretches past segments of the nominal curve).
type Window struct {
	Start time.Time
	End   time.Time
}

// Contains reports whether t falls within [Start, End].
func (w Window) Contains(t time.Time) bool {
	return !t.Before(w.Start) && !t.After(w.End)
}

// Duration is End−Start (non-negative; zero if End ≤ Start).
func (w Window) Duration() time.Duration {
	if w.End.Before(w.Start) {
		return 0
	}
	return w.End.Sub(w.Start)
}

// FundingSource is dumb, dated capacity. All expiry/priority logic lives in a SourcingPolicy, never
// in the source itself — keeping the persisted record boring and shareable. A source contributes a
// nominal rate of Amount/(End−Start) across its own active interval; the sum of active sources'
// rates in a time segment is that segment's nominal capacity rate (a funding gap → rate 0).
type FundingSource struct {
	ID       string
	Amount   float64 // USD total capacity of this source
	Start    time.Time
	End      time.Time
	Priority int // consulted only by priority-order sourcing; ignored by ExpiryFirst
}

// ActiveAt reports whether the source is providing capacity at t.
func (f FundingSource) ActiveAt(t time.Time) bool {
	return !t.Before(f.Start) && !t.After(f.End)
}

// NominalRate is the even-spread rate this source contributes while active: Amount over its life.
// (ExpiryFirst governs drain *order*, not the nominal shape — see policy/sourcing.go.)
func (f FundingSource) NominalRate() float64 {
	d := f.End.Sub(f.Start)
	if d <= 0 {
		return 0
	}
	return f.Amount / d.Seconds()
}

// Allocation binds a share of the pool's capacity to a project. Bank/borrow (pace_deviation) is
// scoped per-allocation — the general primitive; pool-level views compose by summing allocations.
type Allocation struct {
	ID        string
	ProjectID string  // reference only; project identity stays in the host
	Amount    float64 // this allocation's capacity share (USD)
	// BankCap bounds how much banked pace_deviation counts as ceiling headroom (absolute USD).
	// Zero means "use BankFraction × Amount" (see Engine option); a negative value disables banking.
	BankCap float64
}
