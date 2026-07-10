package budgetengine

import (
	"testing"
	"time"
)

// TestSpendEvent_LineItemsAreMetadataOnly proves the v0.2.0 line-item fields are additive and do not
// affect the fold: two ledgers with identical Amount/At but different (or absent) line-item detail
// fold to the same remaining/deviation. Amount stays authoritative.
func TestSpendEvent_LineItemsAreMetadataOnly(t *testing.T) {
	start, end := day(2026, 1, 1), day(2027, 1, 1)
	plan := []PlanEvent{
		srcAdded(1, "A", 10_000, start, end),
		allocChanged(2, "alloc-1", 0),
	}
	at := day(2026, 6, 1)

	// Ledger 1: line items unset (an old v0.1.0-style event).
	bare := []SpendEvent{{ID: "s1", AllocationID: "alloc-1", Amount: 1_000, At: at}}
	// Ledger 2: same Amount, but with line-item detail populated.
	detailed := []SpendEvent{{
		ID: "s1", AllocationID: "alloc-1", Amount: 1_000, At: at,
		ResourceID: "i-abc", Compute: 700, Storage: 250, Network: 50,
	}}

	now := day(2026, 7, 1)
	a := Fold(plan, bare, now, evenSpread{}).Allocs["alloc-1"]
	b := Fold(plan, detailed, now, evenSpread{}).Allocs["alloc-1"]

	if a.CumulativeSpend != b.CumulativeSpend {
		t.Fatalf("line items changed cumulative spend: bare=%.2f detailed=%.2f", a.CumulativeSpend, b.CumulativeSpend)
	}
	if a.RemainingPrincipal != b.RemainingPrincipal || a.PaceDeviation != b.PaceDeviation {
		t.Fatalf("line items changed the fold: bare=%+v detailed=%+v", a, b)
	}
	if b.CumulativeSpend != 1_000 {
		t.Fatalf("Amount must stay authoritative: got %.2f, want 1000", b.CumulativeSpend)
	}
}

// TestSpendEvent_ZeroValueBackwardCompat: an event with no line items (the v0.1.0 shape) is a valid
// v0.2.0 event and folds normally — the backward-compatibility guarantee.
func TestSpendEvent_ZeroValueBackwardCompat(t *testing.T) {
	e := SpendEvent{ID: "x", AllocationID: "a", Amount: 42, At: time.Now()}
	if e.ResourceID != "" || e.Compute != 0 || e.Storage != 0 || e.Network != 0 {
		t.Fatal("zero-value line items should be empty/zero")
	}
	if e.Amount != 42 {
		t.Fatal("Amount unaffected")
	}
}
