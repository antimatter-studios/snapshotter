package services

import (
	"testing"

	"snapshotter/internal/config"
	"snapshotter/internal/watch"
)

// The sensitivity the window offers has to be the sensitivity the watcher uses.
// They are resolved in two places — the dropdown here, the trigger in the agent —
// and the whole value of the setting is that those two agree.

func TestTheSettingsOnOfferCarryTheirCounts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	offered, current := c.TripwireSensitivities()

	if len(offered) != len(watch.Sensitivities) {
		t.Fatalf("offered %d settings, the domain has %d", len(offered), len(watch.Sensitivities))
	}
	for i, s := range offered {
		want := watch.Sensitivities[i]
		if s.ID != string(want) {
			t.Errorf("position %d is %q, want %q — the order is the scale", i, s.ID, want)
		}
		// The count is what the name means, and it comes from the code that decides
		// with it rather than a second copy here.
		if s.Deletions != watch.ThresholdFor(want) {
			t.Errorf("%s says %d deletions, the trigger uses %d", s.ID, s.Deletions, watch.ThresholdFor(want))
		}
		if s.WindowSeconds <= 0 {
			t.Errorf("%s has no window, so its count means nothing", s.ID)
		}
	}
	// Nothing configured is balanced, which is what every build before this setting
	// existed used.
	if current != string(watch.Balanced) {
		t.Errorf("with nothing set, %q is in force", current)
	}
}

func TestTheSettingInForceIsTheOneStored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	if err := c.SetTripwireSensitivity(string(watch.VerySensitive)); err != nil {
		t.Fatalf("setting it: %v", err)
	}

	_, current := c.TripwireSensitivities()
	if current != string(watch.VerySensitive) {
		t.Errorf("stored very-sensitive, %q is in force", current)
	}
	// And it reached the file, which is what the agent reads.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tripwire.Sensitivity != string(watch.VerySensitive) {
		t.Errorf("the settings file says %q", cfg.Tripwire.Sensitivity)
	}
}

func TestASensitivityNobodyOffersIsRefused(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	if err := c.SetTripwireSensitivity("paranoid"); err == nil {
		t.Fatal("a name nobody offers was accepted")
	}
	// And nothing was written, so a rejected setting cannot half-apply.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tripwire.Sensitivity == "paranoid" {
		t.Error("the refused name was saved anyway")
	}
}

// A stored name this build does not know shows as the default rather than as
// nothing selected — the dropdown must always show what is actually in force.
func TestAStoredNameThisBuildDoesNotKnowShowsAsTheDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Tripwire.Sensitivity = "written-by-a-newer-version"
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	_, current := NewConfigService().TripwireSensitivities()

	// The same answer the watcher reaches, which is what stops the screen and the
	// agent disagreeing.
	if current != string(watch.Balanced) {
		t.Errorf("an unknown stored name shows as %q", current)
	}
}

func TestTheDefaultSettingsNameASensitivityTheBuildKnows(t *testing.T) {
	// Otherwise a fresh installation writes a file it cannot read back.
	if got := config.Defaults().Tripwire.Sensitivity; !watch.Known(watch.Sensitivity(got)) {
		t.Errorf("the default sensitivity is %q, which is not one on offer", got)
	}
}
