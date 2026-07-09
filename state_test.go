package budgetengine

import (
	"testing"
	"time"
)

// evenSpread is the sourcing behavior the fold tests assume (matches policy.ExpiryFirst.NominalRate
// without importing the policy package, keeping core tests dependency-free).
type evenSpread struct{}

func (evenSpread) NominalRate(active []FundingSource, _ Window) float64 {
	var r float64
	for _, s := range active {
		r += s.NominalRate()
	}
	return r
}
func (evenSpread) DrainOrder(active []FundingSource, _ time.Time) []FundingSource { return active }

func srcAdded(seq uint64, id string, amt float64, start, end time.Time) PlanEvent {
	return PlanEvent{Kind: KindSourceAdded, Seq: seq, At: start,
		Source: &FundingSource{ID: id, Amount: amt, Start: start, End: end}}
}
func allocChanged(seq uint64, id string, amt float64) PlanEvent {
	return PlanEvent{Kind: KindAllocationChanged, Seq: seq,
		Allocation: &Allocation{ID: id, Amount: amt}}
}

func TestFold_SingleSource_AvailableToDate(t *testing.T) {
	start, end := day(2026, 1, 1), day(2027, 1, 1) // 365 days, $36,500 → $100/day
	plan := []PlanEvent{
		srcAdded(1, "A", 36_500, start, end),
		allocChanged(2, "alloc-1", 36_500),
	}
	// 100 days in → available_to_date ≈ $10,000.
	st := Fold(plan, nil, day(2026, 4, 11), evenSpread{})
	as := st.Allocs["alloc-1"]
	if as == nil {
		t.Fatal("alloc-1 missing")
	}
	if !approx(as.AvailableToDate, 10_000, 1.0) {
		t.Fatalf("AvailableToDate = %.2f, want ~10000", as.AvailableToDate)
	}
	if !approx(as.RemainingPrincipal, 36_500, 0.01) {
		t.Fatalf("RemainingPrincipal = %.2f, want 36500 (all landed, nothing spent)", as.RemainingPrincipal)
	}
}

func TestFold_DisjointSources_GapIsZeroRate(t *testing.T) {
	// Source A: Jan–Jun; Source B: Sep–Dec. July–Aug is a funding gap (rate 0).
	a := srcAdded(1, "A", 6_000, day(2026, 1, 1), day(2026, 7, 1)) // ~$33/day
	b := srcAdded(2, "B", 6_000, day(2026, 9, 1), day(2027, 1, 1))
	plan := []PlanEvent{a, b, allocChanged(3, "alloc-1", 12_000)}
	st := Fold(plan, nil, day(2026, 8, 1), evenSpread{}) // mid-gap

	as := st.Allocs["alloc-1"]
	// Find the segment covering July (the gap) and assert rate 0.
	gapFound := false
	for _, seg := range as.Capacity.Segments {
		if seg.From.Equal(day(2026, 7, 1)) {
			gapFound = true
			if seg.Rate != 0 {
				t.Fatalf("gap segment rate = %v, want 0", seg.Rate)
			}
		}
	}
	if !gapFound {
		t.Fatal("no segment starting Jul 1 (gap not represented)")
	}
	// available_to_date at Aug 1 = only source A fully spread over Jan–Jun = $6,000.
	if !approx(as.AvailableToDate, 6_000, 1.0) {
		t.Fatalf("AvailableToDate mid-gap = %.2f, want ~6000 (A only)", as.AvailableToDate)
	}
}

func TestFold_FloorAtZero(t *testing.T) {
	start, end := day(2026, 1, 1), day(2027, 1, 1)
	plan := []PlanEvent{srcAdded(1, "A", 1_000, start, end), allocChanged(2, "alloc-1", 1_000)}
	// Spend $1,500 against a $1,000 source → remaining floored at 0, not negative.
	spend := []SpendEvent{{ID: "s1", AllocationID: "alloc-1", Amount: 1_500, At: day(2026, 6, 1)}}
	st := Fold(plan, spend, day(2026, 7, 1), evenSpread{})
	as := st.Allocs["alloc-1"]
	if as.RemainingPrincipal != 0 {
		t.Fatalf("RemainingPrincipal = %.2f, want 0 (floored)", as.RemainingPrincipal)
	}
	if as.CumulativeSpend != 1_500 {
		t.Fatalf("CumulativeSpend = %.2f, want 1500", as.CumulativeSpend)
	}
}

func TestFold_CausalPlanEdit_NoRetroactiveChange(t *testing.T) {
	start, end := day(2026, 1, 1), day(2027, 1, 1)
	base := []PlanEvent{srcAdded(1, "A", 36_500, start, end), allocChanged(2, "alloc-1", 36_500)}
	at := day(2026, 4, 11) // 100 days in

	before := Fold(base, nil, at, evenSpread{}).Allocs["alloc-1"].AvailableToDate

	// Add a second source effective LATER (Jul 1). It must not change available_to_date at Apr 11.
	withEdit := append(append([]PlanEvent(nil), base...),
		srcAdded(3, "B", 18_250, day(2026, 7, 1), end))
	after := Fold(withEdit, nil, at, evenSpread{}).Allocs["alloc-1"].AvailableToDate

	if !approx(before, after, 0.01) {
		t.Fatalf("available_to_date changed by a future source add: before=%.2f after=%.2f", before, after)
	}
}

func TestFold_Reversal_DoesNotGoNegative(t *testing.T) {
	start, end := day(2026, 1, 1), day(2027, 1, 1)
	plan := []PlanEvent{srcAdded(1, "A", 10_000, start, end), allocChanged(2, "alloc-1", 10_000)}
	spend := []SpendEvent{
		{ID: "s1", AllocationID: "alloc-1", Amount: 500, At: day(2026, 2, 1)},
		{ID: "s2", AllocationID: "alloc-1", Amount: -900, At: day(2026, 3, 1)}, // over-large reversal
	}
	st := Fold(plan, spend, day(2026, 4, 1), evenSpread{})
	if got := st.Allocs["alloc-1"].CumulativeSpend; got != 0 {
		t.Fatalf("CumulativeSpend after over-large reversal = %.2f, want 0", got)
	}
}
