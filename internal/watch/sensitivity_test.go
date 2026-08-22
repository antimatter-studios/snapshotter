package watch

import (
	"fmt"
	"testing"
	"time"
)

// How readily the wire trips, as a name rather than a count.
//
// The count alone is unanswerable — nobody knows whether two hundred files in five
// seconds is a lot without knowing what their machine does all day — so the setting
// is a name and the count is shown beside it.

func TestTheDefaultIsWhatEveryBuildBeforeThisUsed(t *testing.T) {
	// Balanced has to be the old behaviour exactly, or adding this setting silently
	// changed what every existing installation does.
	if got := ThresholdFor(Balanced); got != DefaultThreshold {
		t.Errorf("balanced is %d, want the default of %d", got, DefaultThreshold)
	}
}

func TestSensitivityRisesAsTheCountFalls(t *testing.T) {
	// The scale has to be monotonic or the names lie: something called "sensitive"
	// that needs more deletions than "balanced" is worse than no setting at all.
	previous := 1 << 30
	for _, s := range Sensitivities {
		got := ThresholdFor(s)
		if got >= previous {
			t.Errorf("%s needs %d deletions, which is not fewer than the setting before it (%d)", s, got, previous)
		}
		previous = got
	}
}

func TestEverySettingOnOfferIsRecognised(t *testing.T) {
	for _, s := range Sensitivities {
		if !Known(s) {
			t.Errorf("%s is offered and not recognised", s)
		}
		if ThresholdFor(s) <= 0 {
			t.Errorf("%s has a threshold of %d", s, ThresholdFor(s))
		}
	}
}

func TestAnUnknownNameFallsBackToTheDefaultRatherThanNothing(t *testing.T) {
	// This arrives from a settings file someone may have typed into. A watcher that
	// refused to start over a misspelling would trade the protection for the typo.
	for _, name := range []Sensitivity{"", "Balanced", "medium", "very sensitive"} {
		if Known(name) {
			t.Errorf("%q was accepted as a setting", name)
		}
		if got := ThresholdFor(name); got != DefaultThreshold {
			t.Errorf("%q gave %d rather than the default", name, got)
		}
	}
}

// The setting has to reach the decision, which is the only thing that makes it a
// setting at all.
func TestASensitivityActuallyChangesWhenTheWireTrips(t *testing.T) {
	now := time.Now()

	for _, c := range []struct {
		sensitivity Sensitivity
		deletions   int
		wantTrip    bool
	}{
		// Thirty files: past very sensitive, nowhere near the rest.
		{VerySensitive, 30, true},
		{Sensitive, 30, false},
		{Balanced, 30, false},
		{Cautious, 30, false},
		// A hundred: past sensitive too.
		{Sensitive, 100, true},
		{Balanced, 100, false},
		// Three hundred: past balanced, still short of cautious.
		{Balanced, 300, true},
		{Cautious, 300, false},
		{Cautious, 600, true},
	} {
		trigger := NewTrigger(ThresholdFor(c.sensitivity), 0, 0)
		tripped := false
		for i := range c.deletions {
			// Inside one window, all in the same folder, which is the shape of a
			// folder being emptied.
			// One folder, one file each, all inside a single window — the shape of a
			// folder being emptied.
			fired, _ := trigger.Deletion(now.Add(time.Duration(i)*time.Millisecond),
				fmt.Sprintf("/Users/someone/Documents/file-%d.txt", i))
			if fired {
				tripped = true
			}
		}
		if tripped != c.wantTrip {
			t.Errorf("%s with %d deletions tripped=%v, want %v", c.sensitivity, c.deletions, tripped, c.wantTrip)
		}
	}
}
