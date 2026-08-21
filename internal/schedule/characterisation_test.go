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
		// Presets are now built from the chosen period and window, so a preset has
		// no single count: it has one per period. These are taken at a fourteen-day
		// window, the same window the flat row above uses, which is what makes the
		// two rows comparable at all.
		//
		// They were 74/42/34/29/27 and 81/49/41/36/34 when the first band was
		// hardcoded to "everything for two days" and the person's choices selected
		// nothing. The rise is the point: the policy now keeps what was asked for.
		{"tiered-13-weeks", presetPolicy(t, "tiered-13-weeks"), [5]int{349, 125, 69, 41, 27}},
		{"tiered-52-weeks", presetPolicy(t, "tiered-52-weeks"), [5]int{356, 132, 76, 48, 34}},
	} {
		for i, interval := range characterisationIntervals {
			policy := c.policy
			// A preset is a function of the period, so the row is rebuilt per
			// column. A flat policy is not, and passes through unchanged.
			if !policy.IsFlat() {
				policy = presetAt(t, c.name, interval)
			}
			got := Retained(policy, interval, now)
			if got != c.want[i] {
				t.Errorf("%s at %v: retains %d, was %d", c.name, interval, got, c.want[i])
			}
		}
	}
}

// The bug that characterisation existed to record, now asserted as fixed.
//
// A preset's first band is the period and window the person chose. Before, it was
// hardcoded to "everything for two days" and both settings selected nothing at
// all: picking a tiered preset silently discarded them.
//
// The claim that used to sit here — that tiering costs less than a flat fortnight
// — is gone rather than corrected. It was a sentence written by hand next to three
// values derived from the policy, it was false at two of the five intervals, and a
// corrected sentence would have drifted again at the next change to a band.
func TestAPresetOpensWithTheChosenPeriodAndWindow(t *testing.T) {
	for _, window := range []time.Duration{3 * day, 7 * day, 14 * day, 30 * day} {
		for _, period := range characterisationIntervals {
			for _, preset := range Presets(period, window) {
				bands := preset.Policy.Bands()
				if len(bands) == 0 {
					t.Fatalf("%s at %v/%v has no bands", preset.ID, period, window)
				}
				if bands[0].Every != period || bands[0].For != window {
					t.Errorf("%s at every %v for %v opens with every %v for %v",
						preset.ID, period, window, bands[0].Every, bands[0].For)
				}
				// And nothing after it may be finer, or the newest snapshots would
				// be governed by a coarser band than the one chosen.
				for k := 1; k < len(bands); k++ {
					if bands[k].Every < bands[k-1].Every {
						t.Errorf("%s at %v/%v refines with age: %v then %v",
							preset.ID, period, window, bands[k-1], bands[k])
					}
					if bands[k].For <= bands[k-1].For {
						t.Errorf("%s at %v/%v does not reach further: %v then %v",
							preset.ID, period, window, bands[k-1], bands[k])
					}
				}
			}
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

// presetPolicy builds a preset the way the interface does: from a period and a
// window. The period here varies per column, so it is passed in per lookup.
func presetPolicy(t *testing.T, id string) Policy {
	t.Helper()
	return presetAt(t, id, 6*time.Hour)
}

func presetAt(t *testing.T, id string, period time.Duration) Policy {
	t.Helper()
	for _, p := range Presets(period, 14*day) {
		if p.ID == id {
			return p.Policy
		}
	}
	t.Fatalf("no preset %q", id)
	return Policy{}
}
