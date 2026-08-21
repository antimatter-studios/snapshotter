package services

import (
	"strings"
	"testing"
	"time"

	"snapshotter/internal/schedule"
)

// The policy chooser is an argument made with two numbers: how many snapshots a
// policy holds, and how far back it reaches. Someone picks a retention policy
// once and lives with it, so those numbers have to be right — and they are
// computed by planning a history rather than by dividing, because with a tiered
// policy those are different answers and only the first is true.

func TestEveryPolicyIsOfferedWithNumbersToCompare(t *testing.T) {
	s := &ScheduleService{}
	got := s.Policies(6, 14)

	if len(got) != len(schedule.Presets(6*time.Hour, 14*24*time.Hour))+1 {
		t.Fatalf("want flat plus every preset (%d), got %d", len(schedule.Presets(6*time.Hour, 14*24*time.Hour))+1, len(got))
	}
	if got[0].ID != schedule.FlatID {
		t.Errorf("flat should be offered first, got %q", got[0].ID)
	}

	seen := map[string]bool{}
	for _, o := range got {
		if seen[o.ID] {
			t.Errorf("%s offered twice", o.ID)
		}
		seen[o.ID] = true

		// Each of these is shown to someone deciding. A blank or zero is not a
		// neutral default here, it is a policy that looks broken.
		if o.Name == "" || o.Summary == "" {
			t.Errorf("%s: incomplete option %+v", o.ID, o)
		}
		if o.Retained <= 0 {
			t.Errorf("%s: holds %d snapshots, which cannot be right", o.ID, o.Retained)
		}
		if o.ReachDays <= 0 {
			t.Errorf("%s: reaches back %.1f days, which cannot be right", o.ID, o.ReachDays)
		}
	}
}

// The whole reason tiering is offered: it reaches much further back for a similar
// number of snapshots. If that ever stops being true the screen is arguing for
// something that is not so, and this is the test that notices.
// What tiering offers, once a preset is built from the choices made rather than
// from hardcoded bands: the same window, and coarser history past it.
//
// The comparison that used to be here — fewer snapshots than the flat window —
// described a preset whose first band was "everything for two days" and which
// therefore threw the chosen window away.
func TestTieringKeepsTheChosenWindowAndReachesPastIt(t *testing.T) {
	s := &ScheduleService{}
	options := s.Policies(6, 14)

	var flat PolicyOption
	tiered := make([]PolicyOption, 0, len(options))
	for _, o := range options {
		if o.ID == schedule.FlatID {
			flat = o
			continue
		}
		tiered = append(tiered, o)
	}
	if flat.ID == "" {
		t.Fatal("no flat policy to compare against")
	}
	if len(tiered) == 0 {
		t.Skip("no tiered presets configured")
	}

	for _, o := range tiered {
		if o.ReachDays <= flat.ReachDays {
			t.Errorf("%s reaches %.0f days, no further than flat's %.0f — tiering has no argument",
				o.ID, o.ReachDays, flat.ReachDays)
		}
		// It must hold at least what the flat window holds, because it opens with
		// that window: a preset is the person's own choice plus coarser history
		// beyond it, so costing less would mean it had thrown some of the choice
		// away — which is exactly the bug this replaced.
		if o.Retained < flat.Retained {
			t.Errorf("%s holds %d against the flat window's %d: it has discarded part of the window "+
				"it is built on", o.ID, o.Retained, flat.Retained)
		}
	}
}

// A shorter interval means more snapshots inside the same window. Obvious, and
// worth pinning: these numbers are the entire basis for choosing.
func TestAShorterIntervalHoldsMore(t *testing.T) {
	s := &ScheduleService{}
	hourly := s.Policies(1, 14)
	sixHourly := s.Policies(6, 14)

	for i := range hourly {
		if hourly[i].ID != sixHourly[i].ID {
			t.Fatalf("policies came back in a different order")
		}
		if hourly[i].Retained <= sixHourly[i].Retained {
			t.Errorf("%s: hourly holds %d, six-hourly holds %d — a shorter interval should hold more",
				hourly[i].ID, hourly[i].Retained, sixHourly[i].Retained)
		}
	}
}

// The flat policy is the one whose reach is stated directly by the retention the
// person typed, so it is the one that can be checked against arithmetic.
func TestFlatReachesExactlyItsRetention(t *testing.T) {
	s := &ScheduleService{}
	for _, retention := range []float64{7, 14, 30} {
		for _, o := range s.Policies(6, retention) {
			if o.ID != schedule.FlatID {
				continue
			}
			if o.ReachDays != retention {
				t.Errorf("flat with %.0f days retention reaches %.1f days", retention, o.ReachDays)
			}
		}
	}
}

func TestDaysConvertsTheUnitTheInterfaceOffers(t *testing.T) {
	if got := days(1).Hours(); got != 24 {
		t.Errorf("one day is %v hours", got)
	}
	if got := days(0.5).Hours(); got != 12 {
		t.Errorf("half a day is %v hours", got)
	}
}

// A summary nobody can read is not a summary. Each policy has to describe itself
// in words, because the tier list underneath is only legible to someone who
// already understands tiering.
func TestEveryPolicyDescribesItselfInWords(t *testing.T) {
	s := &ScheduleService{}
	for _, o := range s.Policies(6, 14) {
		if len(o.Summary) < 10 || !strings.Contains(o.Summary, " ") {
			t.Errorf("%s: %q is not a sentence", o.ID, o.Summary)
		}
	}
}
