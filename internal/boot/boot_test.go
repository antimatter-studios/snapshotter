package boot

import (
	"os"
	"path/filepath"
	"testing"

	"snapshotter/internal/config"
	"snapshotter/internal/i18n"
	"snapshotter/internal/trace"
)

// This package exists because four entry paths each applied their own subset of
// the settings, and two of them were missing a piece: the agents never set the
// language, so a snapshot failing overnight notified in English somebody who had
// chosen German, and the command line returned before the only call to
// trace.SetEnabled, so logging.verbose was ignored for every command.
//
// A function nobody tests is how that happens again. These assert that Apply
// really does apply each thing, and — the part that actually decays — that it
// applies *everything* a process needs, so a fifth setting added elsewhere and
// forgotten here fails rather than going quiet.

func TestApplySetsTheLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en"); trace.SetEnabled(false) })

	Apply(config.Config{Appearance: config.Appearance{Language: "de"}})
	if got := i18n.Language(); got != "de" {
		t.Errorf("language is %q, want de", got)
	}
}

func TestApplySetsVerboseLogging(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en"); trace.SetEnabled(false) })

	Apply(config.Config{Logging: config.Logging{Verbose: true}})
	if !trace.Enabled() {
		t.Error("verbose logging is off after being switched on")
	}
	Apply(config.Config{Logging: config.Logging{Verbose: false}})
	if trace.Enabled() {
		t.Error("verbose logging is on after being switched off")
	}
}

// An unreadable or absent settings file leaves the defaults in force rather than
// stopping the process. Every caller is on its way to doing something useful and
// none is improved by refusing to start over a settings file.
func TestApplyFromFileFallsBackToTheDefaults(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en"); trace.SetEnabled(false) })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got := ApplyFromFile()
	if got.Language() != config.Defaults().Language() {
		t.Errorf("language %q, want the default", got.Language())
	}
	if i18n.Language() != config.Defaults().Language() {
		t.Errorf("i18n was left at %q", i18n.Language())
	}
}

func TestApplyFromFileReadsWhatIsThere(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en"); trace.SetEnabled(false) })

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "snapshotter"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Appearance.Language = "fr"
	cfg.Logging.Verbose = true
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	got := ApplyFromFile()
	if got.Language() != "fr" {
		t.Errorf("returned language %q, want fr", got.Language())
	}
	if i18n.Language() != "fr" {
		t.Errorf("i18n language %q, want fr", i18n.Language())
	}
	if !trace.Enabled() {
		t.Error("verbose was in the file and is not in force")
	}
}

// A language the build does not carry falls back to English, because the settings
// file can be hand-edited and a code nobody recognises should produce a readable
// interface rather than whatever happened to be set before.
//
// Two layers guard this and they do not agree: config.Config.Language returns
// English for anything unknown, while i18n.SetLanguage ignores it and keeps the
// current language. Config sanitises first, so through here config's answer is
// the one that happens and i18n's is unreachable — worth knowing before anyone
// "fixes" i18n to match, since it is the guard for callers that have no Config.
func TestAnUnknownLanguageFallsBackToEnglish(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en"); trace.SetEnabled(false) })

	i18n.SetLanguage("de")
	Apply(config.Config{Appearance: config.Appearance{Language: "kl"}})
	if got := i18n.Language(); got != "en" {
		t.Errorf("language is %q; an unknown code should read as English", got)
	}
}

// The check that decays. Apply is the answer to "what must every process do
// before it prints anything", and the failure mode is a setting added to the
// configuration and never wired here — which is silent, because the process still
// runs and simply ignores it.
//
// Listed by hand because the compiler cannot see the connection: this is the list
// of process-wide settings, and adding one to config.Config without deciding
// whether it belongs here should be a decision, not an omission.
func TestEveryProcessWideSettingIsApplied(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en"); trace.SetEnabled(false) })

	// Each of these is set to something other than the default, then checked.
	// If a new one is added to Config and not applied here, this test does not
	// fail on its own — the comment above is the guard. What it does catch is one
	// of these quietly ceasing to be applied.
	cfg := config.Defaults()
	cfg.Appearance.Language = "es"
	cfg.Logging.Verbose = true

	Apply(cfg)

	if i18n.Language() != "es" {
		t.Errorf("language not applied: %q", i18n.Language())
	}
	if !trace.Enabled() {
		t.Error("verbose logging not applied")
	}
}
