package policy_test

import (
	"testing"
	"time"

	be "github.com/scttfrdmn/budgetengine"
	"github.com/scttfrdmn/budgetengine/policy"
)

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }

// ExpiryFirst drains the soonest-to-expire source first.
func TestExpiryFirst_DrainOrder(t *testing.T) {
	late := be.FundingSource{ID: "late", End: d(2026, 12, 1)}
	soon := be.FundingSource{ID: "soon", End: d(2026, 6, 1)}
	got := policy.ExpiryFirst{}.DrainOrder([]be.FundingSource{late, soon}, d(2026, 1, 1))
	if got[0].ID != "soon" {
		t.Fatalf("drain order[0] = %s, want soon (earliest End first)", got[0].ID)
	}
}

// ExpiryFirst nominal rate sums active sources' even-spread rates; empty (a gap) is 0.
func TestExpiryFirst_NominalRate(t *testing.T) {
	s := be.FundingSource{ID: "A", Amount: 3_650, Start: d(2026, 1, 1), End: d(2027, 1, 1)} // ~$10/day
	r := (policy.ExpiryFirst{}).NominalRate([]be.FundingSource{s}, be.Window{})
	perDay := r * 86400
	if perDay < 9.9 || perDay > 10.1 {
		t.Fatalf("nominal ≈ $%.4f/day, want ~$10/day", perDay)
	}
	if (policy.ExpiryFirst{}).NominalRate(nil, be.Window{}) != 0 {
		t.Fatal("no active sources (gap) must be rate 0")
	}
}

// With no capacity curve (degenerate plan), BankAndReserve falls back to the solvency rate and so
// matches RateAdjust; both are remaining/remaining_time.
func TestPacing_SustainableRate_NoCurveMatches(t *testing.T) {
	as := be.AllocationState{RemainingPrincipal: 3_650}
	w := be.Window{Start: d(2026, 1, 1), End: d(2027, 1, 1)}
	now := d(2026, 1, 1) // full year remaining
	bank := (policy.BankAndReserve{}).SustainableRate(as, w, now) * 86400
	adj := (policy.RateAdjust{}).SustainableRate(as, w, now) * 86400
	if bank < 9.9 || bank > 10.1 {
		t.Fatalf("BankAndReserve ≈ $%.4f/day, want ~$10/day", bank)
	}
	if bank != adj {
		t.Fatalf("with no curve the two policies coincide: bank=%v adj=%v", bank, adj)
	}
	// No time left → 0.
	if (policy.BankAndReserve{}).SustainableRate(as, w, d(2027, 6, 1)) != 0 {
		t.Fatal("past window end → rate 0")
	}
}

// After under-spending, RemainingPrincipal exceeds the nominal forward capacity. BankAndReserve holds
// the lower nominal rate (banking the surplus); RateAdjust re-paces to the higher rate.
func TestPacing_SustainableRate_BankHoldsNominal(t *testing.T) {
	// $10/day source over a full year; a full year still remains in the window ahead.
	w := be.Window{Start: d(2026, 1, 1), End: d(2027, 1, 1)}
	now := d(2026, 1, 1)
	curve := be.CapacityCurve{Segments: []be.CapacitySegment{
		{From: d(2026, 1, 1), To: d(2027, 1, 1), Rate: 3650.0 / (365 * 86400)}, // ~$10/day
	}}
	// Under-spender: more principal remains ($7300) than the forward plan grants ($3650 over the year).
	as := be.AllocationState{RemainingPrincipal: 7_300, Capacity: curve}

	bankPerDay := (policy.BankAndReserve{}).SustainableRate(as, w, now) * 86400
	adjPerDay := (policy.RateAdjust{}).SustainableRate(as, w, now) * 86400

	if bankPerDay < 9.9 || bankPerDay > 10.1 {
		t.Fatalf("BankAndReserve should hold the nominal ~$10/day, got $%.4f/day", bankPerDay)
	}
	if adjPerDay < 19.9 || adjPerDay > 20.1 {
		t.Fatalf("RateAdjust should re-pace to ~$20/day, got $%.4f/day", adjPerDay)
	}
	if !(bankPerDay < adjPerDay) {
		t.Fatalf("underspend: bank (%v) must be below re-paced adj (%v)", bankPerDay, adjPerDay)
	}
}

// When the nominal forward capacity exceeds the remaining principal (an over-funded tail),
// BankAndReserve is capped by solvency so it never projects spending more than remains.
func TestPacing_SustainableRate_SolvencyCap(t *testing.T) {
	w := be.Window{Start: d(2026, 1, 1), End: d(2027, 1, 1)}
	now := d(2026, 1, 1)
	curve := be.CapacityCurve{Segments: []be.CapacitySegment{
		{From: d(2026, 1, 1), To: d(2027, 1, 1), Rate: 3650.0 / (365 * 86400)}, // nominal ~$10/day
	}}
	// Only $1825 of real money left → solvency rate ~$5/day, below the ~$10/day nominal.
	as := be.AllocationState{RemainingPrincipal: 1_825, Capacity: curve}

	bankPerDay := (policy.BankAndReserve{}).SustainableRate(as, w, now) * 86400
	if bankPerDay < 4.9 || bankPerDay > 5.1 {
		t.Fatalf("solvency cap: BankAndReserve should be ~$5/day (remaining-bound), got $%.4f/day", bankPerDay)
	}
}

// RateTargetPolicy is on-track unless borrowed beyond tolerance; DeadlineFloat unless borrowed at all.
func TestProjection_OnTrack(t *testing.T) {
	behind := be.AllocationState{PaceDeviation: -500} // borrowed $500
	ahead := be.AllocationState{PaceDeviation: 500}   // banked $500
	w, now := be.Window{}, time.Time{}

	if (policy.RateTargetPolicy{}).OnTrack(behind, w, now) {
		t.Fatal("RateTarget: borrowed beyond zero tolerance → off track")
	}
	if !(policy.RateTargetPolicy{Tolerance: 1_000}).OnTrack(behind, w, now) {
		t.Fatal("RateTarget: within tolerance → on track")
	}
	if !(policy.DeadlineFloatPolicy{}).OnTrack(ahead, w, now) {
		t.Fatal("DeadlineFloat: banked → on track")
	}
	if (policy.DeadlineFloatPolicy{}).OnTrack(behind, w, now) {
		t.Fatal("DeadlineFloat: borrowed → off track")
	}
}
