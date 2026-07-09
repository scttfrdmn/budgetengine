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

// BankAndReserve reports remaining/remaining_time; RateAdjust matches it on the forward rate.
func TestPacing_SustainableRate(t *testing.T) {
	as := be.AllocationState{RemainingPrincipal: 3_650}
	w := be.Window{Start: d(2026, 1, 1), End: d(2027, 1, 1)}
	now := d(2026, 1, 1) // full year remaining
	bank := (policy.BankAndReserve{}).SustainableRate(as, w, now) * 86400
	adj := (policy.RateAdjust{}).SustainableRate(as, w, now) * 86400
	if bank < 9.9 || bank > 10.1 {
		t.Fatalf("BankAndReserve ≈ $%.4f/day, want ~$10/day", bank)
	}
	if bank != adj {
		t.Fatalf("forward rate should match: bank=%v adj=%v", bank, adj)
	}
	// No time left → 0.
	if (policy.BankAndReserve{}).SustainableRate(as, w, d(2027, 6, 1)) != 0 {
		t.Fatal("past window end → rate 0")
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
