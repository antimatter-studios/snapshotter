package schedule

import (
	"testing"
	"time"
)

// What the retention policies do today, pinned exactly.
//
// This is a characterisation test rather than a statement of intent: the numbers
// below are what the code produces now, correct or not, so that moving this logic
// into its own package can be proved to have changed nothing. Where a number is
// wrong the comment says so — the refactor must reproduce it, and the change of
// behaviour comes after, on its own, where a failure can only mean one thing.
//
// It exists because the test it sits beside,
// TestTieringReachesFurtherThanTheFlatFortnightForNoMoreSnapshots, fixes the
// interval at six hours — one of the five a person can choose — and the claim it
// checks is stated in the interface as though it held at all of them. It does
// not, and a single-point test could not see that.

var characterisationIntervals = []time.Duration{
	time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

func TestWhatEachPolicyRetainsAtEveryInterval(t *testing.T) {
	now := planNow

	for _, c := range []struct {
		name   string
		policy Policy
		// One count per interval, in the order above.
		want [5]int
	}{
		{"flat 3 days", FlatPolicy(3 * day), [5]int{73, 25, 13, 7, 4}},
		{"flat 1 week", FlatPolicy(7 * day), [5]int{169, 57, 29, 15, 8}},
		{"flat 2 weeks", FlatPolicy(14 * day), [5]int{337, 113, 57, 29, 15}},
		{"flat 1 month", FlatPolicy(30 * day), [5]int{721, 241, 121, 61, 31}},
		// Sensitive to where now falls against a band boundary: these are taken
		// with planNow, and a different now shifts the twelve-hour column by one.
		// A tiered policy has a floor the flat ones do not: its daily and weekly
		// bands keep the same number however often snapshots are taken. That is
		// why it holds fewer than a flat fortnight at one hour and more at
		// twenty-four, and why any claim comparing the two is interval-dependent.
		{"tiered-13-weeks", presetPolicy(t, "tiered-13-weeks"), [5]int{74, 42, 34, 29, 27}},
		{"tiered-52-weeks", presetPolicy(t, "tiered-52-weeks"), [5]int{81, 49, 41, 36, 34}},
	} {
		for i, interval := range characterisationIntervals {
			got := Retained(c.policy, interval, now)
			if got != c.want[i] {
				t.Errorf("%s at %v: retains %d, was %d", c.name, interval, got, c.want[i])
			}
		}
	}
}

// The claim the interface makes about the tiered presets, checked at every
// interval rather than at one.
//
// It fails at twelve and twenty-four hours: the tiered policies keep more than
// the flat fortnight they are described as costing less than. Recorded here as
// the current truth rather than asserted as a requirement, because the fix is a
// design decision — whether the presets should scale with the chosen interval —
// and not a number to adjust.
func TestTheTieringClaimHoldsOnlyAtSomeIntervals(t *testing.T) {
	flat := FlatPolicy(14 * day)

	holds := map[time.Duration]bool{}
	for _, interval := range characterisationIntervals {
		flatCount := Retained(flat, interval, planNow)
		fewer := true
		for _, preset := range Presets() {
			if Retained(preset.Policy, interval, planNow) > flatCount {
				fewer = false
			}
		}
		holds[interval] = fewer
	}

	for _, c := range []struct {
		interval time.Duration
		want     bool
	}{
		{time.Hour, true},
		{3 * time.Hour, true},
		{6 * time.Hour, true}, // the only one the neighbouring test checks
		{12 * time.Hour, false},
		{24 * time.Hour, false},
	} {
		if holds[c.interval] != c.want {
			t.Errorf("at %v: tiered-keeps-fewer is %v, was %v", c.interval, holds[c.interval], c.want)
		}
	}
}

// Density is not validated, only reach. A policy whose bands grow finer with age
// is accepted and applied, which nothing constructs today but a per-tier editor
// would put two clicks away.
//
// Pinned so the invariant, when it arrives, is a deliberate change with a test
// that flips rather than a silent tightening.
func TestAPolicyMayCurrentlyRefineWithAge(t *testing.T) {
	weeklyThenHourly := Policy{Tiers: []Tier{
		{Every: 7 * day, For: 2 * day},
		{Every: time.Hour, For: 90 * day},
	}}

	// Accepted without complaint, and it keeps almost everything for ninety days
	// while thinning the newest two to one a week.
	if got := Retained(weeklyThenHourly, time.Hour, planNow); got == 0 {
		t.Fatal("a policy that refines with age was rejected; it used to be accepted")
	} else {
		t.Logf("a refining policy retains %d — nonsense, but currently legal", got)
	}
}

func presetPolicy(t *testing.T, id string) Policy {
	t.Helper()
	for _, p := range Presets() {
		if p.ID == id {
			return p.Policy
		}
	}
	t.Fatalf("no preset %q", id)
	return Policy{}
}
