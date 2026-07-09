package budgetengine

import (
	"context"
	"testing"
	"time"
)

// engineForCheck builds an engine over in-memory logs with the even-spread sourcing and a trivial
// pacing/projection (CheckLaunch doesn't use them, but New requires non-nil for Evaluate paths).
func engineForCheck(plan []PlanEvent, spend []SpendEvent, now time.Time) *Engine {
	m := &memStore{spend: spend, plan: plan}
	return New(m, m, FixedClock{T: now},
		WithSourcing(evenSpread{}),
		WithPacing(nilPacing{}),
		WithProjection(nilProjection{}),
	)
}

type nilPacing struct{}

func (nilPacing) SustainableRate(AllocationState, Window, time.Time) float64 { return 0 }

type nilProjection struct{}

func (nilProjection) OnTrack(AllocationState, Window, time.Time) bool { return true }

// TestCheckLaunch_Section9 reproduces the design §9 worked scenario exactly:
// a grant paced so that available_to_date = $100k at the check time, with $95k already spent.
func TestCheckLaunch_Section9(t *testing.T) {
	// One source spread so that by the check date exactly $100k has become available.
	// $180k over Jan 1 2026 – Jan 1 2027 (365d) → ~$493.15/day. To reach $100k we check at
	// 100000/493.15 ≈ 202.78 days in. Simpler: size the source so a round date lands on $100k.
	// Use $200k over 400 days = $500/day; $100k available at exactly day 200 (Jul 20 2026).
	start := day(2026, 1, 1)
	end := start.AddDate(0, 0, 400)
	checkAt := start.AddDate(0, 0, 200) // $500/day × 200 = $100,000 available_to_date
	plan := []PlanEvent{
		srcAdded(1, "grant", 200_000, start, end),
		allocChanged(2, "alloc-1", 0), // uncapped: draw the whole pool
	}
	spend := []SpendEvent{{ID: "s1", AllocationID: "alloc-1", Amount: 95_000, At: start.AddDate(0, 0, 190)}}

	eng := engineForCheck(plan, spend, checkAt)

	// $9k launch → Projected $104k > $100k ceiling → Block.
	d, err := eng.CheckLaunch(context.Background(), Scope{Tenant: "harvard"}, "alloc-1", 9_000)
	if err != nil {
		t.Fatal(err)
	}
	if d.Verdict != Block {
		t.Fatalf("verdict = %s, want block", d.Verdict)
	}
	if !approx(d.EffectiveBalance, 100_000, 1.0) {
		t.Fatalf("EffectiveBalance = %.2f, want ~100000", d.EffectiveBalance)
	}
	if !approx(d.Spent, 95_000, 0.01) {
		t.Fatalf("Spent = %.2f, want 95000", d.Spent)
	}
	if !approx(d.Projected, 104_000, 1.0) {
		t.Fatalf("Projected = %.2f, want ~104000", d.Projected)
	}
	if !approx(d.Remaining, 5_000, 1.0) {
		t.Fatalf("Remaining = %.2f, want ~5000", d.Remaining)
	}

	// $4k launch → Projected $99k ≥ 80% of $100k → Warn (allowed).
	d2, _ := eng.CheckLaunch(context.Background(), Scope{Tenant: "harvard"}, "alloc-1", 4_000)
	if d2.Verdict != Warn {
		t.Fatalf("verdict = %s, want warn", d2.Verdict)
	}
	if !d2.Allowed() {
		t.Fatal("warn should be allowed")
	}

	// $1k launch → Projected $96k < 80% ceiling? 96k is 96% → still Warn. Use a fresh low-spend
	// scope to hit Allow: $10k spent, $1k launch → $11k of $100k = 11% → Allow.
	lowSpend := []SpendEvent{{ID: "s1", AllocationID: "alloc-1", Amount: 10_000, At: start.AddDate(0, 0, 50)}}
	engLow := engineForCheck(plan, lowSpend, checkAt)
	d3, _ := engLow.CheckLaunch(context.Background(), Scope{Tenant: "harvard"}, "alloc-1", 1_000)
	if d3.Verdict != Allow {
		t.Fatalf("verdict = %s, want allow", d3.Verdict)
	}
}

func TestCheckLaunch_FrozenBlocksUnconditionally(t *testing.T) {
	start := day(2026, 1, 1)
	plan := []PlanEvent{
		srcAdded(1, "grant", 200_000, start, start.AddDate(0, 0, 400)),
		allocChanged(2, "alloc-1", 0),
		{Kind: KindFreeze, Seq: 3, AllocationID: "alloc-1", Frozen: boolp(true)},
	}
	eng := engineForCheck(plan, nil, start.AddDate(0, 0, 10))
	// Tiny cost, tons of headroom — but frozen → Block.
	d, _ := eng.CheckLaunch(context.Background(), Scope{Tenant: "harvard"}, "alloc-1", 1)
	if d.Verdict != Block {
		t.Fatalf("verdict = %s, want block (frozen)", d.Verdict)
	}
}

func boolp(b bool) *bool { return &b }
