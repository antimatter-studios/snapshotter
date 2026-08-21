package main

import (
	"strings"
	"testing"

	"snapshotter/internal/i18n"
	"snapshotter/services"
)

// The line under the coverage strip, saying what the schedule is supposed to be
// doing against a picture of what it actually did.
//
// It went in without a test, which is how the strip itself spent months measuring
// the machine against a schedule nobody had chosen: a menu is the one surface
// nothing else exercises, so anything wrong in it is only found by looking.

func TestScheduleModeNamesTheIntervalAndRetention(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	got := scheduleMode(services.Health{
		ScheduleInstalled: true,
		IntervalHours:     3,
		RetentionDays:     14,
	})
	for _, want := range []string{"3 hours", "14 days"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q does not mention %q", got, want)
		}
	}
}

// With nothing installed it must say so rather than describing a schedule that
// is not running. Reporting "every 6 hours, kept 14 days" on a machine taking no
// snapshots is the exact failure this application exists to prevent.
func TestScheduleModeSaysWhenNothingIsScheduled(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	got := scheduleMode(services.Health{ScheduleInstalled: false})
	if strings.Contains(got, "kept") || strings.Contains(got, "Every") {
		t.Errorf("%q describes a schedule that is not installed", got)
	}
	if got == "" {
		t.Error("said nothing at all")
	}
}

// A schedule with no interval is not a schedule, and the zero value of Health
// reaches here whenever the status check failed part way.
func TestScheduleModeSurvivesAZeroHealth(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	if got := scheduleMode(services.Health{}); got == "" {
		t.Error("an empty Health produced no line at all")
	}
}

// It follows the language, like everything else the menu draws. Worth asserting
// because the menu is redrawn from a different goroutine than the one that
// changes the language, and a line that cached its text would not follow.
func TestScheduleModeFollowsTheLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })

	h := services.Health{
		ScheduleInstalled: true,
		IntervalHours:     3,
		RetentionDays:     14,
	}

	i18n.SetLanguage("en")
	english := scheduleMode(h)
	i18n.SetLanguage("de")
	german := scheduleMode(h)

	if english == german {
		t.Errorf("the language did not change the line: %q", german)
	}
	if !strings.Contains(german, "Stunden") {
		t.Errorf("German line reads %q", german)
	}
}
