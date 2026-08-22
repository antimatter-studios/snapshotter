package schedule

import (
	"strings"
	"testing"
	"time"

	"snapshotter/internal/i18n"
)

// One expression for what the schedule does, because there used to be two.
//
// The menu bar built its own line from the interval and the retention window,
// which ignores the policy entirely. On a tiered schedule it announced "every 3
// hours, kept 364 days" when only one snapshot every four weeks survives past the
// twenty-sixth — it read the horizon as the retention, which is true of a flat
// window and of nothing else. The settings screen, reading the same policy through
// Describe, said something different and correct.
//
// So these tests are about agreement as much as wording: anywhere that says what
// the schedule does has to say it from here.

const (
	period = 3 * time.Hour
	window = 14 * day
)

func TestAFlatWindowSaysWhatItKeeps(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	got := Headline(period, FlatPolicy(window))

	// Everything, for the whole window. "Kept" is the honest word here, and only
	// here.
	for _, want := range []string{"Flat window", "3 hours", "14 days", "kept"} {
		if !strings.Contains(got, want) {
			t.Errorf("the line does not mention %q: %s", want, got)
		}
	}
}

// The bug, stated as a test: a tiered policy must not claim to keep everything for
// its horizon. That is the claim the menu bar was making.
func TestATieredPolicyDoesNotClaimToKeepEverything(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	for _, preset := range Presets(period, window) {
		got := Headline(period, preset.Policy)

		if strings.Contains(got, "kept") {
			t.Errorf("%s says things are kept when they are thinned: %s", preset.ID, got)
		}
		if !strings.Contains(got, "thinning") {
			t.Errorf("%s does not say it thins: %s", preset.ID, got)
		}
		// Its own name, so the reader knows which of the shapes is in force rather
		// than having to infer it from two numbers.
		if !strings.Contains(got, preset.Name) {
			t.Errorf("%s does not name itself: %s", preset.ID, got)
		}
	}
}

// The reach shown must be the policy's own horizon, not the flat window it was
// built from. Showing the window would understate a tiered policy by a year.
func TestTheReachIsThePolicysOwnHorizon(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	for _, preset := range Presets(period, window) {
		got := Headline(period, preset.Policy)
		want := words(preset.Policy.Horizon())
		if !strings.Contains(got, want) {
			t.Errorf("%s should reach %s: %s", preset.ID, want, got)
		}
		// And the horizon really is further than the window, or this test proves
		// nothing.
		if preset.Policy.Horizon() <= window {
			t.Errorf("%s reaches no further than its window, so the two cannot be told apart", preset.ID)
		}
	}
}

func TestEveryPolicyIsNamedRatherThanLeftBlank(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })

	policies := map[string]Policy{
		"flat":   FlatPolicy(window),
		"custom": {Tiers: []Tier{{Every: 5 * time.Hour, For: 3 * day}}},
		"empty":  {},
	}
	for _, preset := range Presets(period, window) {
		policies[preset.ID] = preset.Policy
	}

	for _, code := range []string{"en", "de", "es", "fr"} {
		i18n.SetLanguage(code)
		for id, p := range policies {
			name := ModeName(p)
			if name == "" {
				t.Errorf("%s/%s has no name", code, id)
			}
			// A key coming back instead of a sentence means the catalogue is
			// missing an entry, and it renders as "retention.mode.flat" in a menu.
			if strings.HasPrefix(name, "retention.") {
				t.Errorf("%s/%s came back as a key: %s", code, id, name)
			}
		}
	}
}

// A policy with nothing in it prunes nothing, and the line says so rather than
// naming a rate and a reach that do not exist.
func TestAPolicyThatPrunesNothingSaysOnlyThat(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	got := Headline(period, Policy{})

	if strings.Contains(got, "3 hours") {
		t.Errorf("a policy with no bands described a schedule: %s", got)
	}
	if got == "" {
		t.Error("it said nothing at all")
	}
}

func TestTheLineFollowsTheLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })

	i18n.SetLanguage("en")
	english := Headline(period, FlatPolicy(window))
	i18n.SetLanguage("de")
	german := Headline(period, FlatPolicy(window))

	if english == german {
		t.Errorf("the line is the same in German: %s", german)
	}
	if strings.Contains(german, "kept") {
		t.Errorf("an English word survived into the German line: %s", german)
	}
}

// Describe and Headline are two views of one policy, and they have to agree about
// which policy they are describing — the divergence this replaces was exactly a
// disagreement of that kind.
func TestTheShortAndLongFormsAgreeOnTheRate(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })
	i18n.SetLanguage("en")

	for _, preset := range Presets(period, window) {
		short := Headline(period, preset.Policy)
		full := Describe(preset.Policy)

		// The first band's rate is the one fact both forms state.
		rate := words(period)
		if !strings.Contains(short, rate) {
			t.Errorf("%s: the line omits the rate %q: %s", preset.ID, rate, short)
		}
		if !strings.Contains(full, rate) {
			t.Errorf("%s: the sentence omits the rate %q: %s", preset.ID, rate, full)
		}
	}
}
