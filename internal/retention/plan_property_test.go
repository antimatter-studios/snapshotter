package retention

import (
	"math/rand"
	"testing"
	"time"
)

// Properties, not examples.
//
// The examples that existed tested this logic through the package that owns
// snapshots, so the rule deciding what gets destroyed had no direct coverage at
// all. Worse, an example proves one point: the claim that tiering costs less than
// a flat fortnight was tested at exactly one of the five intervals a person can
// choose, and was false at two of the others.
//
// These assert things that must hold for every policy and every history, checked
// against a few thousand generated cases. A property that fails prints the case
// that broke it, and the seed is fixed so it breaks the same way tomorrow.

const day = 24 * time.Hour

// generate builds a policy and a history that are plausible rather than uniform:
// real machines take snapshots on a rhythm, with gaps.
func generate(r *rand.Rand) (Policy, []time.Time, time.Time) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	tiers := make([]Tier, 0, 4)
	reach := time.Duration(0)
	every := time.Duration(0)
	for i := 0; i < 1+r.Intn(4); i++ {
		// Non-decreasing density, increasing reach: the shape a sane policy has,
		// and the shape the interface will soon be able to construct.
		every += time.Duration(r.Intn(48)) * time.Hour
		reach += time.Duration(1+r.Intn(30)) * day
		tiers = append(tiers, Tier{Every: every, For: reach})
	}

	interval := []time.Duration{time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, day}[r.Intn(5)]
	var taken []time.Time
	at := now
	for i := 0; i < r.Intn(400); i++ {
		at = at.Add(-interval)
		if r.Intn(10) == 0 {
			at = at.Add(-time.Duration(r.Intn(72)) * time.Hour) // a gap
		}
		taken = append(taken, at)
	}
	return Policy{Tiers: tiers}, taken, now
}

func eachCase(t *testing.T, f func(t *testing.T, p Policy, taken []time.Time, now time.Time)) {
	t.Helper()
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		p, taken, now := generate(r)
		f(t, p, taken, now)
		if t.Failed() {
			t.Logf("failing case %d: policy=%v snapshots=%d", i, p.Tiers, len(taken))
			return
		}
	}
}

// Every snapshot is either kept or pruned, exactly once. Anything else means the
// planner has lost or duplicated one, and a lost snapshot is one nobody deletes
// and nobody can find.
func TestPlanPartitionsItsInput(t *testing.T) {
	eachCase(t, func(t *testing.T, p Policy, taken []time.Time, now time.Time) {
		keep, prune := Plan(taken, p, now)

		seen := make([]int, len(taken))
		for _, i := range keep {
			seen[i]++
		}
		for _, i := range prune {
			seen[i]++
		}
		for i, n := range seen {
			if n != 1 {
				t.Errorf("index %d appears %d times across keep and prune", i, n)
				return
			}
		}
	})
}

// Planning what survived must change nothing. If it does not hold, running the
// pruner twice deletes more than running it once — so the history depends on how
// often the schedule happened to fire rather than on the policy.
func TestPlanIsIdempotent(t *testing.T) {
	eachCase(t, func(t *testing.T, p Policy, taken []time.Time, now time.Time) {
		keep, _ := Plan(taken, p, now)

		survivors := make([]time.Time, len(keep))
		for i, at := range keep {
			survivors[i] = taken[at]
		}
		again, prunedAgain := Plan(survivors, p, now)
		if len(prunedAgain) != 0 {
			t.Errorf("a second plan pruned %d of %d survivors", len(prunedAgain), len(survivors))
			return
		}
		if len(again) != len(survivors) {
			t.Errorf("a second plan kept %d of %d", len(again), len(survivors))
		}
	})
}

// The history must never be emptied. A policy that plans away the last snapshot
// leaves the machine with no restore point, which is the state this exists to
// prevent.
func TestPlanNeverEmptiesTheHistory(t *testing.T) {
	eachCase(t, func(t *testing.T, p Policy, taken []time.Time, now time.Time) {
		if len(taken) == 0 {
			return
		}
		keep, _ := Plan(taken, p, now)
		if len(keep) == 0 {
			t.Error("every snapshot was pruned")
		}
	})
}

// The order the caller supplies must not change the decision. It arrives sorted
// today; a caller that sorted differently would otherwise get a different history.
func TestPlanIgnoresInputOrder(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	eachCase(t, func(t *testing.T, p Policy, taken []time.Time, now time.Time) {
		if len(taken) < 2 {
			return
		}
		shuffled := append([]time.Time(nil), taken...)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		want := map[time.Time]bool{}
		keep, _ := Plan(taken, p, now)
		for _, i := range keep {
			want[taken[i]] = true
		}

		got := map[time.Time]bool{}
		keep2, _ := Plan(shuffled, p, now)
		for _, i := range keep2 {
			got[shuffled[i]] = true
		}

		if len(want) != len(got) {
			t.Errorf("order changed the kept set: %d against %d", len(want), len(got))
			return
		}
		for at := range want {
			if !got[at] {
				t.Errorf("%v kept in one order and not the other", at)
				return
			}
		}
	})
}

// The property the whole design rests on, stated precisely.
//
// A snapshot kept today is still kept tomorrow *while it remains in the same
// tier*. Buckets are absolute and the oldest in each is chosen, so advancing the
// clock cannot re-choose within a band.
//
// Not across bands, and that distinction is the whole of tiering. A snapshot kept
// under "one a day" is expected to be discarded when it ages into "one a week" —
// if it were not, a tiered policy would keep every daily snapshot for ever and
// tiering would do nothing at all.
//
// The first version of this test asserted stability across tiers too, and failed
// on the policy {29h for 96h}, {64h for 120h}: a snapshot kept as the oldest in a
// 29-hour bucket was pruned six hours later, when it aged into 64-hour buckets
// where it was not. The code was right and the property was wrong, which is worth
// recording — the difference between "kept for ever" and "kept until it ages" is
// exactly what someone reading this needs to know.
func TestASnapshotIsStableWithinItsTier(t *testing.T) {
	eachCase(t, func(t *testing.T, p Policy, taken []time.Time, now time.Time) {
		tiers := p.Bands()
		if len(tiers) == 0 {
			return
		}

		const later = 6 * time.Hour
		bandAt := func(at time.Time, when time.Time) (Tier, bool) {
			age := when.Sub(at)
			if age < 0 {
				age = 0
			}
			return tierFor(tiers, age)
		}

		keep, _ := Plan(taken, p, now)
		kept := map[time.Time]bool{}
		for _, i := range keep {
			kept[taken[i]] = true
		}

		after, _ := Plan(taken, p, now.Add(later))
		still := map[time.Time]bool{}
		for _, i := range after {
			still[taken[i]] = true
		}

		for at := range kept {
			was, okBefore := bandAt(at, now)
			is, okAfter := bandAt(at, now.Add(later))
			if !okBefore || !okAfter {
				continue // aged out of the policy entirely, which it asked for
			}
			if was != is {
				continue // moved to a coarser band, where thinning is the point
			}
			if !still[at] {
				t.Errorf("%v was kept and then dropped six hours later, without changing band", at)
				return
			}
		}
	})
}
