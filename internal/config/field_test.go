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

// A string field will hold "purple" perfectly well, and the application will
// then have no palette. Type is not the only thing worth checking.
func TestSetRefusesAValueThatIsNotAThemeAtAll(t *testing.T) {
	cfg := Defaults()
	before := cfg.Appearance.Theme

	for _, bad := range []string{"purple", "Dark", "", "light dark"} {
		if err := Set(&cfg, "appearance.theme", bad); err == nil {
			t.Errorf("appearance.theme accepted %q", bad)
		}
	}
	if cfg.Appearance.Theme != before {
		t.Errorf("a refused theme still changed the value: %q", cfg.Appearance.Theme)
	}

	// The three the window offers must all still be accepted, or this has traded
	// one bug for a worse one.
	for _, good := range []string{"system", "light", "dark"} {
		if err := Set(&cfg, "appearance.theme", good); err != nil {
			t.Errorf("%s was refused: %v", good, err)
		}
	}
}

// The error has to say what IS allowed. "invalid value" sends someone to the
// source, which is what the whole config command exists to avoid.
func TestTheThemeErrorNamesTheValidValues(t *testing.T) {
	cfg := Defaults()
	err := Set(&cfg, "appearance.theme", "purple")
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"system", "light", "dark", "purple"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q missing from the error: %v", want, err)
		}
	}
}

func TestSetRefusesAnEmptyPolicy(t *testing.T) {
	cfg := Defaults()
	if err := Set(&cfg, "schedule.policy", "   "); err == nil {
		t.Error("an empty policy was accepted")
	}
	if err := Set(&cfg, "schedule.policy", "flat"); err != nil {
		t.Errorf("flat was refused: %v", err)
	}
}

// The ignore list is a list, and lists are exactly the sort of thing someone
// scripting this wants to change — "stop warning me about node_modules" is a
// one-liner or it does not happen.
func TestAListCanBeReadEditedAndWrittenBack(t *testing.T) {
	cfg := Defaults()

	before, err := Get(cfg, "tripwire.ignore")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, "/Library/Caches/") {
		t.Errorf("the defaults are not being reported: %q", before)
	}

	if err := Set(&cfg, "tripwire.ignore", before+",/node_modules/"); err != nil {
		t.Fatalf("adding one entry was refused: %v", err)
	}
	if len(cfg.Tripwire.Ignore) != len(Defaults().Tripwire.Ignore)+1 {
		t.Errorf("wrong length after adding one: %v", cfg.Tripwire.Ignore)
	}
	if cfg.Tripwire.Ignore[len(cfg.Tripwire.Ignore)-1] != "/node_modules/" {
		t.Errorf("the added entry is wrong: %v", cfg.Tripwire.Ignore)
	}
}

// An empty value means an empty list, not a list containing "". The difference
// matters: a single empty fragment matches every path, which would silence the
// tripwire completely while looking like a configured setting.
func TestClearingAListDoesNotLeaveAnEmptyEntry(t *testing.T) {
	cfg := Defaults()

	if err := Set(&cfg, "tripwire.ignore", ""); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tripwire.Ignore) != 0 {
		t.Errorf("clearing left %d entries: %v", len(cfg.Tripwire.Ignore), cfg.Tripwire.Ignore)
	}

	// And spaces around entries are trimmed, because a person typing a list will
	// put them there.
	if err := Set(&cfg, "tripwire.ignore", " /a/ , /b/ "); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tripwire.Ignore) != 2 || cfg.Tripwire.Ignore[0] != "/a/" {
		t.Errorf("spacing was not handled: %v", cfg.Tripwire.Ignore)
	}
}

// A closed set of answers is worth refusing rather than falling back on. The
// watcher would use the default and say so in a log nobody opens, leaving the
// person believing they had changed how readily it trips.
func TestSetRefusesASensitivityNobodyOffers(t *testing.T) {
	cfg := Defaults()

	err := Set(&cfg, "tripwire.sensitivity", "paranoid")
	if err == nil {
		t.Fatal("a name nobody offers was accepted")
	}
	// The message lists what is on offer, because "not paranoid" does not tell
	// anyone what to type instead.
	for _, want := range []string{"cautious", "balanced", "sensitive", "very-sensitive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
	if cfg.Tripwire.Sensitivity == "paranoid" {
		t.Error("the refused value was written anyway")
	}
}

func TestSetAcceptsEverySensitivityOnOffer(t *testing.T) {
	for _, name := range []string{"cautious", "balanced", "sensitive", "very-sensitive"} {
		cfg := Defaults()
		if err := Set(&cfg, "tripwire.sensitivity", name); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
		if cfg.Tripwire.Sensitivity != name {
			t.Errorf("%s was accepted and not stored", name)
		}
	}
}

// The window refused an unknown language and this did not, so a value typed at
// the command line was accepted and then silently ignored — which reads as the
// setting not working rather than as the value being wrong.
func TestSetRefusesALanguageThisBuildDoesNotCarry(t *testing.T) {
	cfg := Defaults()

	if err := Set(&cfg, "appearance.language", "kl"); err == nil {
		t.Fatal("a language this build does not carry was accepted")
	}
	if cfg.Appearance.Language == "kl" {
		t.Error("the refused language was written anyway")
	}

	// And every language it does carry still works.
	for _, code := range Languages {
		cfg := Defaults()
		if err := Set(&cfg, "appearance.language", code); err != nil {
			t.Errorf("%s was refused: %v", code, err)
		}
	}
}
