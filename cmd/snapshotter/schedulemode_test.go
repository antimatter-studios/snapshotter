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
//
// What this no longer tests is the wording. It used to build its own sentence
// from the interval and the retention window, ignoring the policy — announcing
// "every 3 hours, kept 364 days" for a tiered schedule that keeps one snapshot
// every four weeks past the twenty-sixth. The words come from
// internal/schedule.Headline now, which is where they are asserted; what is left
// here is the choosing between them.

func TestScheduleModeShowsWhatTheServiceWorded(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	// Verbatim. Rewording it here would be a second version of the sentence, and
	// two versions is the thing that went wrong.
	const line = "Tiered — daily, then weekly: every 3 hours, thinning out to 26 weeks"
	got := scheduleMode(services.Health{
		ScheduleInstalled: true,
		IntervalHours:     3,
		RetentionDays:     14,
		ScheduleHeadline:  line,
	})
	if got != line {
		t.Errorf("the menu reworded it:\n  got  %s\n  want %s", got, line)
	}
}

// A status check that failed part way leaves the headline empty while still
// reporting a schedule as installed. Better to say there is no schedule than to
// draw a blank row, or to fall back to the numbers and start wording it here
// again.
func TestScheduleModeSaysNothingRatherThanShowingABlankRow(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	got := scheduleMode(services.Health{
		ScheduleInstalled: true,
		IntervalHours:     3,
		RetentionDays:     14,
	})
	if got == "" {
		t.Error("an unworded schedule produced an empty menu row")
	}
	if strings.Contains(got, "3") || strings.Contains(got, "14") {
		t.Errorf("it fell back to wording the numbers itself: %q", got)
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

// The one line this file still words itself follows the language. Worth asserting
// because the menu is redrawn from a different goroutine than the one that
// changes the language, and a line that cached its text would not follow.
//
// The scheduled line's own translation is asserted in internal/schedule, which is
// where it is now built.
func TestTheNoScheduleLineFollowsTheLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })

	h := services.Health{ScheduleInstalled: false}

	i18n.SetLanguage("en")
	english := scheduleMode(h)
	i18n.SetLanguage("de")
	german := scheduleMode(h)

	if english == german {
		t.Errorf("the language did not change the line: %q", german)
	}
	if german == "" || strings.HasPrefix(german, "tray.") {
		t.Errorf("German line reads %q", german)
	}
}
