package budgetengine_test

import (
	"context"
	"testing"
	"time"

	be "github.com/scttfrdmn/budgetengine"
	"github.com/scttfrdmn/budgetengine/policy"
)

// End-to-end worked scenarios from BUDGET_ENGINE_ARCHITECTURE §4. These live in an external test
// package so they compose the real policy implementations, proving the axes work together.

type store struct {
	spend []be.SpendEvent
	plan  []be.PlanEvent
}

func (s *store) Spend(context.Context, be.Scope) ([]be.SpendEvent, error) { return s.spend, nil }
func (s *store) Plan(context.Context, be.Scope) ([]be.PlanEvent, error)   { return s.plan, nil }

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
func src(seq uint64, id string, amt float64, a, b time.Time) be.PlanEvent {
	return be.PlanEvent{Kind: be.KindSourceAdded, Seq: seq, At: a,
		Source: &be.FundingSource{ID: id, Amount: amt, Start: a, End: b}}
}
func alloc(seq uint64, id string) be.PlanEvent {
	return be.PlanEvent{Kind: be.KindAllocationChanged, Seq: seq, Allocation: &be.Allocation{ID: id}}
}
func spend(id string, amt float64, at time.Time) be.SpendEvent {
	return be.SpendEvent{ID: id, AllocationID: "a", Amount: amt, At: at}
}

// Scenario 1: multi-month grant, expiry-first × bank-and-reserve × fixed-date. Under-spend early →
// stays on track and solvent, sustainable rate positive.
func TestScenario_MultiMonthGrant(t *testing.T) {
	start, end := d(2026, 1, 1), d(2026, 12, 31)
	st := &store{
		plan:  []be.PlanEvent{src(1, "grant", 120_000, start, end), alloc(2, "a")},
		spend: []be.SpendEvent{spend("s1", 20_000, d(2026, 2, 1))}, // light early usage
	}
	eng := be.New(st, st, be.FixedClock{T: d(2026, 4, 1)},
		be.WithSourcing(policy.ExpiryFirst{}),
		be.WithPacing(policy.BankAndReserve{}),
		be.WithProjection(policy.RateTargetPolicy{}))

	bs, err := eng.Evaluate(context.Background(), be.Scope{Tenant: "h"}, "a")
	if err != nil {
		t.Fatal(err)
	}
	if !bs.Solvent {
		t.Fatal("should be solvent")
	}
	if !bs.OnTrack {
		t.Fatalf("under-spender should be on track; pace_deviation=%.2f", bs.PaceDeviation)
	}
	if bs.PaceDeviation <= 0 {
		t.Fatalf("under-spend should bank (positive deviation); got %.2f", bs.PaceDeviation)
	}
	if bs.SustainableRate <= 0 {
		t.Fatal("sustainable rate should be positive with money + time left")
	}
}

// Scenario 2: monthly cloud budget, single-source × rate-adjust × floating-date. Over-pace →
// projected zero-date before window end → off track under the deadline-float projection.
func TestScenario_MonthlyCloud(t *testing.T) {
	start, end := d(2026, 6, 1), d(2026, 7, 1) // one month, $3,000 → $100/day
	st := &store{
		plan: []be.PlanEvent{src(1, "month", 3_000, start, end), alloc(2, "a")},
		// Spent $1,500 in the first 5 days = $300/day, 3× the sustainable pace.
		spend: []be.SpendEvent{spend("s1", 1_500, d(2026, 6, 3))},
	}
	eng := be.New(st, st, be.FixedClock{T: d(2026, 6, 6)},
		be.WithSourcing(policy.ExpiryFirst{}),
		be.WithPacing(policy.RateAdjust{}),
		be.WithProjection(policy.DeadlineFloatPolicy{}))

	bs, _ := eng.Evaluate(context.Background(), be.Scope{Tenant: "h"}, "a")
	// available_to_date at day 5 ≈ $500; spent $1,500 → borrowed (negative deviation) → off track.
	if bs.PaceDeviation >= 0 {
		t.Fatalf("over-pacer should be borrowed (negative deviation); got %.2f", bs.PaceDeviation)
	}
	if bs.OnTrack {
		t.Fatal("over-pacer should be off track under DeadlineFloatPolicy")
	}
	if !bs.Solvent {
		t.Fatal("still has money, should be solvent")
	}
}

// Scenario 3: disjoint two-source grant with a summer gap. Banking across the gap is what keeps a
// fall burst solvent — and the contrast that proves banking is load-bearing: available_to_date only
// grows during funded segments, and money banked before the gap remains available through it.
func TestScenario_DisjointGapBanking(t *testing.T) {
	// A: Jan–Jun $6k; gap Jul–Aug; B: Sep–Dec $6k. Total $12k.
	a := src(1, "A", 6_000, d(2026, 1, 1), d(2026, 7, 1))
	b := src(2, "B", 6_000, d(2026, 9, 1), d(2027, 1, 1))
	st := &store{
		plan: []be.PlanEvent{a, b, alloc(3, "a")},
		// Under-spend spring ($1k of ~$6k available by Jun) → large banked surplus going into the gap.
		spend: []be.SpendEvent{spend("s1", 1_000, d(2026, 3, 1))},
	}
	// Check mid-gap (Aug 1): no source active, but banked surplus persists.
	eng := be.New(st, st, be.FixedClock{T: d(2026, 8, 1)},
		be.WithSourcing(policy.ExpiryFirst{}),
		be.WithPacing(policy.BankAndReserve{}),
		be.WithProjection(policy.RateTargetPolicy{}))

	bs, _ := eng.Evaluate(context.Background(), be.Scope{Tenant: "h"}, "a")
	// available_to_date mid-gap = source A fully spread ($6k); spent $1k → $5k banked headroom.
	if !approx2(bs.AvailableToDate, 6_000, 1.0) {
		t.Fatalf("available_to_date mid-gap = %.2f, want ~6000 (A only, B not yet active)", bs.AvailableToDate)
	}
	if bs.PaceDeviation <= 4_000 {
		t.Fatalf("should carry banked surplus through the gap; deviation=%.2f", bs.PaceDeviation)
	}
	// A $5k burst mid-gap is covered by banked headroom → CheckLaunch allows (within ceiling).
	dec, _ := eng.CheckLaunch(context.Background(), be.Scope{Tenant: "h"}, "a", 4_000)
	if dec.Verdict == be.Block {
		t.Fatalf("banked surplus should cover a mid-gap burst; got block: %s", dec.Reason)
	}
	if !bs.Solvent {
		t.Fatal("solvent — $11k still remains of $12k grant")
	}
}

func approx2(a, b, tol float64) bool {
	x := a - b
	if x < 0 {
		x = -x
	}
	return x <= tol
}
