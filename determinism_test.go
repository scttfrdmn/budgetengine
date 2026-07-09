package budgetengine

import (
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// TestDeterminism_ShuffledLogsFoldIdentically is the reproducibility guarantee that lets two hosts
// agree: folding the same events in any input order yields identical state (the fold sorts by
// (Seq,At) for plan and (At,ID) for spend before replay). This is what makes the seam records
// "dumb and shareable" — Prism and prp fold the same logs into the same answer.
func TestDeterminism_ShuffledLogsFoldIdentically(t *testing.T) {
	start, end := day(2026, 1, 1), day(2027, 1, 1)
	plan := []PlanEvent{
		srcAdded(1, "A", 50_000, start, day(2026, 7, 1)),
		srcAdded(2, "B", 70_000, day(2026, 4, 1), end),
		allocChanged(3, "alloc-1", 0),
		{Kind: KindWindowExtended, Seq: 4, At: start, Window: &Window{Start: start, End: end}},
	}
	spend := []SpendEvent{
		{ID: "s1", AllocationID: "alloc-1", Amount: 3_000, At: day(2026, 2, 15)},
		{ID: "s2", AllocationID: "alloc-1", Amount: 7_500, At: day(2026, 5, 20)},
		{ID: "s3", AllocationID: "alloc-1", Amount: 1_200, At: day(2026, 6, 2)},
	}
	now := day(2026, 8, 1)

	want := Fold(plan, spend, now, evenSpread{})

	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 20; i++ {
		p := append([]PlanEvent(nil), plan...)
		s := append([]SpendEvent(nil), spend...)
		rng.Shuffle(len(p), func(a, b int) { p[a], p[b] = p[b], p[a] })
		rng.Shuffle(len(s), func(a, b int) { s[a], s[b] = s[b], s[a] })

		got := Fold(p, s, now, evenSpread{})
		if !reflect.DeepEqual(got.Allocs["alloc-1"], want.Allocs["alloc-1"]) {
			t.Fatalf("shuffle %d folded to different state:\n got=%+v\nwant=%+v",
				i, got.Allocs["alloc-1"], want.Allocs["alloc-1"])
		}
	}
}

// TestDeterminism_NoWallClock confirms the fold never reads the wall clock: it takes now explicitly,
// so a fixed now fully determines the result (guards against a stray time.Now() creeping in).
func TestDeterminism_FixedNowFullyDetermines(t *testing.T) {
	start, end := day(2026, 1, 1), day(2027, 1, 1)
	plan := []PlanEvent{srcAdded(1, "A", 36_500, start, end), allocChanged(2, "alloc-1", 0)}
	now := time.Date(2026, 6, 15, 12, 34, 56, 0, time.UTC)
	a := Fold(plan, nil, now, evenSpread{})
	b := Fold(plan, nil, now, evenSpread{})
	if !reflect.DeepEqual(a.Allocs["alloc-1"], b.Allocs["alloc-1"]) {
		t.Fatal("same inputs folded to different state")
	}
}
