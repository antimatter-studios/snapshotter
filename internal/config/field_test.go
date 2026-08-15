package config

import (
	"strings"
	"testing"
)

// Addressing settings by name is what makes the application scriptable, so the
// property that matters is completeness: every field in the file must be
// reachable, or automation hits one that silently is not.

func TestEverySettingIsAddressable(t *testing.T) {
	keys := Keys()
	if len(keys) == 0 {
		t.Fatal("no settings are addressable at all")
	}

	// Derived from the struct rather than listed here, so a field added to Config
	// is covered by this test without anyone remembering to add it.
	cfg := Defaults()
	for _, key := range keys {
		if _, err := Get(cfg, key); err != nil {
			t.Errorf("%s is listed but cannot be read: %v", key, err)
		}
	}

	// The sections a person would look for, as a guard against the walk silently
	// stopping at the top level.
	for _, want := range []string{
		"schedule.interval_hours", "schedule.retention_days", "schedule.policy",
		"tripwire.enabled", "appearance.theme",
		"window.width", "window.height",
		"refresh.menu_bar_seconds", "refresh.window_seconds",
		"paths.mount_root", "paths.log", "paths.tripwire_log",
	} {
		found := false
		for _, k := range keys {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not addressable; keys are %v", want, keys)
		}
	}
}

// What Get prints must be what Set accepts, or a script cannot read a value,
// decide something and write it back.
func TestGetAndSetAgreeOnHowAValueIsSpelled(t *testing.T) {
	cfg := Defaults()
	for _, key := range Keys() {
		before, err := Get(cfg, key)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if err := Set(&cfg, key, before); err != nil {
			t.Errorf("%s: Get produced %q, which Set refuses: %v", key, before, err)
			continue
		}
		after, err := Get(cfg, key)
		if err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Errorf("%s changed by being written back: %q -> %q", key, before, after)
		}
	}
}

// Hours and days are floats underneath and are read by people. "6.000000 hours"
// reads as a bug in the application rather than as six hours.
func TestNumbersAreSpelledTheWayPeopleWriteThem(t *testing.T) {
	cfg := Defaults()
	if err := Set(&cfg, "schedule.interval_hours", "6"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(cfg, "schedule.interval_hours")
	if err != nil {
		t.Fatal(err)
	}
	if got != "6" {
		t.Errorf("want %q, got %q", "6", got)
	}

	// A genuine fraction still survives: half-hourly is a real choice.
	if err := Set(&cfg, "schedule.interval_hours", "0.5"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(cfg, "schedule.interval_hours"); got != "0.5" {
		t.Errorf("want %q, got %q", "0.5", got)
	}
}

func TestSetRefusesAValueTheFieldCannotHold(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"window.width", "wide"},
		{"schedule.interval_hours", "often"},
		{"tripwire.enabled", "maybe"},
		{"refresh.window_seconds", "1.5"}, // a whole number of seconds
	} {
		cfg := Defaults()
		before, _ := Get(cfg, tc.key)
		if err := Set(&cfg, tc.key, tc.value); err == nil {
			t.Errorf("%s accepted %q", tc.key, tc.value)
		}
		// A refused write must change nothing, or a failed script leaves the
		// settings half-applied.
		if after, _ := Get(cfg, tc.key); after != before {
			t.Errorf("%s changed despite the refusal: %q -> %q", tc.key, before, after)
		}
	}
}

// The error has to name the thing that does not exist and say how to find out
// what does — this is the most likely mistake anyone scripting will make.
func TestAnUnknownSettingSaysHowToListTheRealOnes(t *testing.T) {
	cfg := Defaults()
	_, err := Get(cfg, "schedule.every_fortnight")
	if err == nil {
		t.Fatal("an invented setting was read without complaint")
	}
	if !strings.Contains(err.Error(), "every_fortnight") {
		t.Errorf("did not name the setting: %v", err)
	}
	if !strings.Contains(err.Error(), "keys") {
		t.Errorf("did not say how to list the real ones: %v", err)
	}

	if err := Set(&cfg, "schedule.every_fortnight", "1"); err == nil {
		t.Error("an invented setting was written without complaint")
	}
}

// Set takes a pointer for a reason: on a copy it would appear to work and
// change nothing.
func TestSetChangesTheConfigItWasGiven(t *testing.T) {
	cfg := Defaults()
	if err := Set(&cfg, "appearance.theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if cfg.Appearance.Theme != "dark" {
		t.Errorf("the field was not changed: %+v", cfg.Appearance)
	}
}
