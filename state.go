package budgetengine

import (
	"sort"
	"time"
)

// This file is the load-bearing core: the deterministic fold from two event logs to state.
//
// NOTE — ceiling semantics (a v0 refinement of the design doc). The design doc §5.4 wrote
// EffectiveBalance = remaining_principal + min(banked, cap). Implementing it against the doc's own
// §9 numbers ($95k spent, $100k ceiling, $180k grant) showed that formula double-counts: banked is
// (available_to_date − spent) and remaining_principal is (landed − spent), so adding them counts the
// −spent term twice. In a *continuous, never-resetting* budget there is no separate "carried bank"
// pot (that was a discrete-period artifact of prp's per-month model): under-spending simply makes
// (available_to_date − spent) larger, and that IS your banked headroom. So v0 uses:
//
//	EffectiveBalance = available_to_date            (the paced ceiling; matches §9's $100k)
//	headroom         = available_to_date − spent    (naturally grows when you under-spend = banking)
//	remaining_principal = landed − spent  ≥ 0       (the hard solvency floor, real money)
//	pace_deviation   = available_to_date − spent    (signed: + under pace/banked, − ahead/borrowed)
//
// Borrowing beyond the paced ceiling (spending down to remaining_principal even when ahead of pace)
// is a Projection-policy option deferred to v0.1; v0's built-in RateTargetPolicy enforces the paced
// ceiling. This reconciliation is flagged for the design doc.

// CapacitySegment is one piece of the piecewise-constant nominal capacity curve: over [From, To)
// the nominal rate is Rate USD/second, from the sources active in that segment. A funding gap is a
// segment with Rate == 0.
type CapacitySegment struct {
	From, To time.Time
	Rate     float64  // USD per second
	Sources  []string // contributing source IDs (for breakdown / drain order)
}

// CapacityCurve is the ordered, possibly-disjoint nominal curve for a plan.
type CapacityCurve struct {
	Segments []CapacitySegment
}

// AvailableToDate integrates the curve from the window start up to now: the paced ceiling, i.e. how
// much budget has become spendable by now under even pacing. Exact finite sum (piecewise-constant),
// so it is fully deterministic — no numeric integration, no drift.
func (c CapacityCurve) AvailableToDate(now time.Time) float64 {
	var total float64
	for _, s := range c.Segments {
		to := s.To
		if to.After(now) {
			to = now
		}
		if to.After(s.From) {
			total += s.Rate * to.Sub(s.From).Seconds()
		}
	}
	return total
}

// AvailableBetween integrates the curve over [from, to): the nominal budget that becomes spendable in
// that span under even pacing. Generalizes AvailableToDate (which is AvailableBetween(curveStart, now)).
// Used by BankAndReserve to read the go-forward nominal rate. Exact piecewise-constant sum.
func (c CapacityCurve) AvailableBetween(from, to time.Time) float64 {
	var total float64
	for _, s := range c.Segments {
		lo := s.From
		if lo.Before(from) {
			lo = from
		}
		hi := s.To
		if hi.After(to) {
			hi = to
		}
		if hi.After(lo) {
			total += s.Rate * hi.Sub(lo).Seconds()
		}
	}
	return total
}

// AllocationState is the folded state for one allocation at a point in time.
type AllocationState struct {
	Alloc              Allocation
	CumulativeSpend    float64 // Σ spend for this alloc, floored ≥ 0 during ordered replay
	RemainingPrincipal float64 // landed − spend, ≥ 0 (hard solvency)
	AvailableToDate    float64 // paced ceiling (nominal integral to now)
	PaceDeviation      float64 // available_to_date − spend (+ banked, − borrowed)
	Capacity           CapacityCurve
	LastSpendAt        time.Time
	Frozen             bool
}

// EngineState is the folded result of (planLog, spendLog, now) for one scope.
type EngineState struct {
	Now     time.Time
	Window  Window
	Sources []FundingSource
	Allocs  map[string]*AllocationState
	Frozen  map[string]bool // allocationID → frozen; "" = whole-plan freeze
}

// Fold replays the two logs into state at time now. It is pure and deterministic: the same logs
// (in any input order) fold to identical state, because plan events are ordered by (Seq, At) and
// spend by (At, ID) before replay.
//
// Causal / go-forward-only plan edits: a source added at T contributes to the nominal curve only for
// τ ≥ T, so already-accrued pace_deviation rides through a plan edit untouched — this falls out of
// building the curve from the sources known at each segment's start, no retroactive rewrite.
func Fold(planLog []PlanEvent, spendLog []SpendEvent, now time.Time, sourcing SourcingPolicy) EngineState {
	plan := append([]PlanEvent(nil), planLog...)
	sort.SliceStable(plan, func(i, j int) bool {
		if plan[i].Seq != plan[j].Seq {
			return plan[i].Seq < plan[j].Seq
		}
		return plan[i].At.Before(plan[j].At)
	})

	st := EngineState{Now: now, Allocs: map[string]*AllocationState{}, Frozen: map[string]bool{}}
	sources := map[string]FundingSource{}
	allocs := map[string]Allocation{}

	for _, e := range plan {
		switch e.Kind {
		case KindSourceAdded:
			if e.Source != nil {
				sources[e.Source.ID] = *e.Source
			}
		case KindSourceExpired:
			if e.Source != nil {
				if s, ok := sources[e.Source.ID]; ok {
					// Effective end moves earlier to the event's source End (go-forward).
					s.End = e.Source.End
					sources[e.Source.ID] = s
				}
			}
		case KindWindowExtended:
			if e.Window != nil {
				st.Window = *e.Window
			}
		case KindAllocationChanged:
			if e.Allocation != nil {
				allocs[e.Allocation.ID] = *e.Allocation
			}
		case KindFreeze:
			frozen := e.Frozen != nil && *e.Frozen
			st.Frozen[e.AllocationID] = frozen
		}
	}

	for _, s := range sources {
		st.Sources = append(st.Sources, s)
	}
	sort.Slice(st.Sources, func(i, j int) bool { return st.Sources[i].ID < st.Sources[j].ID })

	// If no explicit window was set by a WindowExtended event, derive it from the span of sources
	// (earliest Start → latest End). An explicit window always wins (it may extend past the sources).
	if st.Window.Start.IsZero() && st.Window.End.IsZero() {
		for i, s := range st.Sources {
			if i == 0 || s.Start.Before(st.Window.Start) {
				st.Window.Start = s.Start
			}
			if s.End.After(st.Window.End) {
				st.Window.End = s.End
			}
		}
	}

	// Landed money (sources whose Start ≤ now), for the solvency floor.
	landed := 0.0
	for _, s := range st.Sources {
		if !s.Start.After(now) {
			landed += s.Amount
		}
	}

	// Per-allocation spend (ordered replay, floored at 0).
	spend := append([]SpendEvent(nil), spendLog...)
	sort.SliceStable(spend, func(i, j int) bool {
		if !spend[i].At.Equal(spend[j].At) {
			return spend[i].At.Before(spend[j].At)
		}
		return spend[i].ID < spend[j].ID
	})
	seenSpend := map[string]bool{}
	spendByAlloc := map[string]float64{}
	lastByAlloc := map[string]time.Time{}
	for _, ev := range spend {
		if ev.At.After(now) || seenSpend[ev.ID] {
			continue
		}
		seenSpend[ev.ID] = true
		v := spendByAlloc[ev.AllocationID] + ev.Amount
		if v < 0 {
			v = 0 // reversal cannot drive cumulative spend below zero
		}
		spendByAlloc[ev.AllocationID] = v
		lastByAlloc[ev.AllocationID] = ev.At
	}

	// Pool-level nominal curve (unshared) — built once, causally from the sources' own dates.
	poolCurve := buildCurve(st.Sources, st.Window, sourcing)

	for id, a := range allocs {
		as := &AllocationState{Alloc: a, Frozen: st.Frozen[id] || st.Frozen[""]}
		as.Capacity = poolCurve
		as.CumulativeSpend = spendByAlloc[id]
		as.LastSpendAt = lastByAlloc[id]
		// v0 allocation model: an allocation is an absolute CAP over the pool. available_to_date and
		// the solvency floor are the pool figures capped at the allocation's Amount (Amount ≤ 0 means
		// "uncapped" — the allocation may draw the whole pool, which is the single-allocation case).
		poolAvail := poolCurve.AvailableToDate(now)
		as.AvailableToDate = capAt(poolAvail, a.Amount)
		as.RemainingPrincipal = clampZero(capAt(landed, a.Amount) - as.CumulativeSpend)
		as.PaceDeviation = as.AvailableToDate - as.CumulativeSpend
		st.Allocs[id] = as
	}
	return st
}

// capAt caps v at the allocation amount. A non-positive cap means uncapped (draw the whole pool),
// which is the single-allocation case that all worked scenarios use. Multi-allocation carving of a
// shared pool is a v0.1 concern (see BUDGET_ENGINE_ARCHITECTURE §6 / open items).
func capAt(v, cap float64) float64 {
	if cap <= 0 || v <= cap {
		return v
	}
	return cap
}

// buildCurve constructs the piecewise-constant pool-level nominal capacity curve. Breakpoints are
// every source Start/End and the window bounds; between adjacent breakpoints the active-source set
// is constant, so the rate is constant. It is causal by construction: a source added with a future
// Start only contributes to segments at/after that Start, so a plan edit never rewrites past
// segments (and thus never disturbs already-accrued pace_deviation).
func buildCurve(sources []FundingSource, window Window, sourcing SourcingPolicy) CapacityCurve {
	bps := map[int64]time.Time{}
	add := func(t time.Time) { bps[t.UnixNano()] = t }
	if !window.Start.IsZero() {
		add(window.Start)
	}
	if !window.End.IsZero() {
		add(window.End)
	}
	for _, s := range sources {
		add(s.Start)
		add(s.End)
	}
	var points []time.Time
	for _, t := range bps {
		points = append(points, t)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })

	var curve CapacityCurve
	for i := 0; i+1 < len(points); i++ {
		from, to := points[i], points[i+1]
		var active []FundingSource
		for _, s := range sources {
			// A source is active in [from,to) if it covers the segment's start.
			if !from.Before(s.Start) && from.Before(s.End) {
				active = append(active, s)
			}
		}
		rate := sourcing.NominalRate(active, Window{Start: from, End: to})
		ids := make([]string, 0, len(active))
		for _, s := range active {
			ids = append(ids, s.ID)
		}
		curve.Segments = append(curve.Segments, CapacitySegment{From: from, To: to, Rate: rate, Sources: ids})
	}
	return curve
}

func clampZero(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}
