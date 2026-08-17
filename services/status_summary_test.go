package services

import (
	"snapshotter/internal/i18n"
	"strings"
	"testing"
	"time"
)

// summarise reduces everything to one level and one sentence, and that sentence
// is what the menu bar shows. It is the only text most people will ever read from
// this application, so it has to be right when nobody is looking at the window.

func TestVerdictIsTheWorstThingFound(t *testing.T) {
	base := protected(time.Now())
	base.SnapshotCount = 5
	base.CoverageHours = 30

	for _, tc := range []struct {
		name     string
		findings []Finding
		want     Level
	}{
		{"nothing wrong", nil, LevelOK},
		{"one warning", []Finding{{Level: LevelWarn}}, LevelWarn},
		{"one failure", []Finding{{Level: LevelBad}}, LevelBad},
		{"a failure outranks warnings", []Finding{{Level: LevelWarn}, {Level: LevelBad}}, LevelBad},
		{"order does not matter", []Finding{{Level: LevelBad}, {Level: LevelWarn}}, LevelBad},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := base
			h.Findings = tc.findings
			got, _ := summarise(h)
			if got != tc.want {
				t.Errorf("want %s, got %s", tc.want, got)
			}
		})
	}
}

// An informational finding is something worth saying, not something wrong.
// Letting one set the verdict would mean a simulated machine could never show a
// clean one — and the scenario banner is informational by design.
func TestInformationalFindingsDoNotSpoilTheVerdict(t *testing.T) {
	h := protected(time.Now())
	h.SnapshotCount = 5
	h.Findings = []Finding{{Level: LevelInfo, Title: "These readings are simulated"}}

	level, headline := summarise(h)
	if level != LevelOK {
		t.Errorf("an info finding set the verdict to %s", level)
	}
	if strings.Contains(headline, "to look at") {
		t.Errorf("an info finding was counted as actionable: %q", headline)
	}
}

// The count in the headline is what someone acts on, so it must count only the
// things that can be acted on.
func TestHeadlineCountsOnlyActionableFindings(t *testing.T) {
	h := protected(time.Now())
	h.SnapshotCount = 5
	h.Findings = []Finding{
		{Level: LevelWarn},
		{Level: LevelInfo},
		{Level: LevelWarn},
	}
	_, headline := summarise(h)
	if !strings.Contains(headline, "2 things to look at") {
		t.Errorf("want two actionable things counted, got %q", headline)
	}
}

func TestHeadlinePluralisesOneThing(t *testing.T) {
	h := protected(time.Now())
	h.SnapshotCount = 5
	h.Findings = []Finding{{Level: LevelWarn}}
	_, headline := summarise(h)
	if !strings.Contains(headline, "1 thing to look at") || strings.Contains(headline, "1 things") {
		t.Errorf("want singular, got %q", headline)
	}
}

// The two states that override everything, because in both of them the rest of
// the sentence would be beside the point.
func TestTheTwoOverridingVerdicts(t *testing.T) {
	t.Run("no snapshots is always bad, whatever else is true", func(t *testing.T) {
		h := protected(time.Now())
		h.SnapshotCount = 0
		level, headline := summarise(h)
		if level != LevelBad {
			t.Errorf("want bad, got %s", level)
		}
		if !strings.Contains(headline, "nothing to roll back to") {
			t.Errorf("headline does not say what is wrong: %q", headline)
		}
	})

	t.Run("snapshots but nothing taking more is bad", func(t *testing.T) {
		h := protected(time.Now())
		h.SnapshotCount = 3
		h.ScheduleInstalled = false
		level, headline := summarise(h)
		if level != LevelBad {
			t.Errorf("want bad, got %s", level)
		}
		if !strings.Contains(headline, "nothing is taking more") {
			t.Errorf("headline does not say what is wrong: %q", headline)
		}
	})
}

// The headline is read at a glance in a menu bar, so it has to carry the two
// numbers that matter without being read carefully.
func TestAHealthyHeadlineStatesCountAndCover(t *testing.T) {
	h := protected(time.Now())
	h.SnapshotCount = 8
	h.CoverageHours = 50
	level, headline := summarise(h)
	if level != LevelOK {
		t.Fatalf("want ok, got %s (%q)", level, headline)
	}
	for _, want := range []string{"8 snapshots", "2 days"} {
		if !strings.Contains(headline, want) {
			t.Errorf("want %q in the headline, got %q", want, headline)
		}
	}
}

// Cover is worded in the largest unit that stays honest, because "47 hours" is
// arithmetic and "2 days" is an answer.
func TestCoverIsWordedInTheLargestHonestUnit(t *testing.T) {
	for _, tc := range []struct {
		hours float64
		want  string
	}{
		{0, "under an hour"},
		{0.5, "under an hour"},
		{1, "1 hour"},
		{1.4, "1 hour"}, // rounds to 1, and must not read "1 hours"
		{5, "5 hours"},
		{47, "47 hours"},
		{48, "2 days"},
		{72, "3 days"},
	} {
		if got := i18n.Span(tc.hours); got != tc.want {
			t.Errorf("%.1fh: want %q, got %q", tc.hours, tc.want, got)
		}
	}
}

func TestSnapshotCountIsWordedNaturally(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		// 0 is never displayed: summarise overrides the headline entirely when
		// there are no snapshots, so this only records what the helper does.
		{0, "0 snapshots"},
		{1, "1 snapshot"},
		{2, "2 snapshots"},
	} {
		if got := snapshotCount(tc.n); got != tc.want {
			t.Errorf("%d: want %q, got %q", tc.n, tc.want, got)
		}
	}
}
