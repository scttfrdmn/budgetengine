package budgetengine

import "time"

// SpendEvent is one actual, already-attributed cost line item — the atom of the actuals ledger.
// The host attributes each raw cost line to a Scope + Allocation (via CUR tags, a collector, or
// observed instance state); the engine does no attribution and treats Source as opaque provenance.
//
// Amount is USD. Positive amounts are spend; a correction/reversal is a negative Amount (the fold
// floors cumulative spend at zero, so a reversal can only give back money that was spent). At is the
// accrual time (when the cost was incurred), not ingest time — the fold orders by it.
//
// The optional line-item fields (ResourceID, Compute/Storage/Network) are host-supplied *metadata*
// for per-resource breakdown and reporting. They are additive and backward-compatible: the fold and
// CheckLaunch use ONLY Amount (the authoritative delta), so zero-value line items on older events
// fold exactly as before. A host that doesn't populate them loses only breakdown detail, never
// correctness. Amount SHOULD equal Compute+Storage+Network when the components are provided, but the
// engine does not enforce it — Amount remains authoritative.
type SpendEvent struct {
	ID           string    // idempotency key; replaying the same ID twice is a no-op
	AllocationID string    // which allocation this spend draws from
	Amount       float64   // USD; >0 spend, <0 correction/reversal — the authoritative delta
	At           time.Time // accrual time
	Source       string    // opaque provenance (CUR line id, meter name, instance id, …)

	// Optional line-item metadata (v0.2.0+); zero values are valid and ignored by the fold.
	ResourceID string  `json:",omitempty"` // e.g. instance/volume id this cost is attributed to
	Compute    float64 `json:",omitempty"` // USD portion attributable to compute
	Storage    float64 `json:",omitempty"` // USD portion attributable to storage
	Network    float64 `json:",omitempty"` // USD portion attributable to network/egress
}

// PlanEventKind is the discriminant of the plan-mutation log. PlanEvent is a tagged struct (not a Go
// interface): event-sourced records must round-trip through JSON/DynamoDB via a single concrete
// seam.Store[PlanEvent], and a tagged struct serializes cleanly where an interface would need a
// custom type-switch unmarshaller.
type PlanEventKind string

const (
	KindSourceAdded       PlanEventKind = "source_added"
	KindSourceExpired     PlanEventKind = "source_expired"
	KindWindowExtended    PlanEventKind = "window_extended"
	KindAllocationChanged PlanEventKind = "allocation_changed"
	KindFreeze            PlanEventKind = "freeze" // freeze or unfreeze via Frozen
)

// PlanEvent is one mutation to the plan (window + funding sources + allocations + freeze). Kind
// selects which payload pointer is meaningful; the rest are nil.
//
// Ordering: Seq is the authoritative total order the fold replays in (a host supplies a monotonic
// sequence; Prism's daemon trivially, prp via a DynamoDB sort key). At is the *effect time* on the
// plan timeline — a SourceAdded{At: T} reshapes the nominal curve only for τ ≥ T (causal,
// go-forward-only: §3.4 of the design). When two events share At, Seq breaks the tie.
type PlanEvent struct {
	Kind PlanEventKind
	At   time.Time
	Seq  uint64

	Source     *FundingSource // SourceAdded (full source) / SourceExpired (ID + effective end)
	Window     *Window        // WindowExtended (new/extended window)
	Allocation *Allocation    // AllocationChanged

	Frozen       *bool  // Freeze: true = freeze (block launches), false = unfreeze
	AllocationID string // Freeze scoped to one allocation; "" = whole plan
}
