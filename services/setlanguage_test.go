package services

import (
	"os"
	"path/filepath"
	"testing"

	"snapshotter/internal/config"
)

// The setting the language picker writes, and the one the menu bar reads back.
//
// It went in with no test. Both surfaces depend on it: the window changes
// language immediately from its own state, and the menu bar only follows because
// this reaches the settings file and the watcher notices. A failure here is
// invisible in the window and total in the menu bar.

func TestSetLanguageWritesTheChoice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := &ConfigService{}

	if err := c.SetLanguage("de"); err != nil {
		t.Fatalf("setting German: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if cfg.Language() != "de" {
		t.Errorf("the file says %q", cfg.Language())
	}
}

// A language this build does not carry is refused rather than written. Saving it
// would leave every surface falling back to English with nothing explaining why.
func TestSetLanguageRefusesOneItDoesNotCarry(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := &ConfigService{}

	if err := c.SetLanguage("kl"); err == nil {
		t.Fatal("an unknown language was accepted")
	}
	if _, err := config.Load(); err != nil {
		t.Fatalf("the settings became unreadable: %v", err)
	}
}

// Every language the build offers must be settable, or the picker shows a choice
// that cannot be made.
func TestEveryOfferedLanguageCanBeSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := &ConfigService{}

	for _, code := range config.Languages {
		if err := c.SetLanguage(code); err != nil {
			t.Errorf("%s is offered and refused: %v", code, err)
		}
	}
}

// Changing the language must not disturb anything else in the file. It is one
// field in a document somebody may have hand-edited.
func TestSetLanguageLeavesTheRestOfTheSettingsAlone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	before := config.Defaults()
	before.Schedule.IntervalHours = 12
	before.Schedule.RetentionDays = 30
	before.Appearance.Theme = "dark"
	before.Tripwire.Ignore = []string{"~/Library/Caches"}
	if err := config.Save(before); err != nil {
		t.Fatal(err)
	}

	if err := (&ConfigService{}).SetLanguage("fr"); err != nil {
		t.Fatal(err)
	}

	after, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after.Schedule.IntervalHours != 12 || after.Schedule.RetentionDays != 30 {
		t.Errorf("the schedule changed: %+v", after.Schedule)
	}
	if after.Appearance.Theme != "dark" {
		t.Errorf("the theme became %q", after.Appearance.Theme)
	}
	if len(after.Tripwire.Ignore) != 1 {
		t.Errorf("the ignore list became %v", after.Tripwire.Ignore)
	}
}

// A settings file that cannot be parsed is not overwritten. Saving on top of it
// would destroy whatever somebody had written there, to change which words are
// shown.
func TestSetLanguageRefusesToSaveOverAFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path := filepath.Join(dir, "snapshotter", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := "schedule:\n  interval_hours: [this is not a number\n"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (&ConfigService{}).SetLanguage("de"); err == nil {
		t.Fatal("saved over a settings file it could not read")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != broken {
		t.Error("the unreadable file was modified anyway")
	}
}
