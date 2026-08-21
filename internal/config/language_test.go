package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The language accessor, and the two places a settings file comes from.
//
// Language had no direct tests at all, which matters because its fallback is the
// one that actually happens: i18n.SetLanguage also guards against a code it does
// not know, but this sanitises first, so this policy is the effective one and
// that one is unreachable through the settings. Both are deliberate; only one is
// reached.

func TestLanguageReturnsWhatIsSetWhenItIsCarried(t *testing.T) {
	for _, code := range Languages {
		cfg := Config{Appearance: Appearance{Language: code}}
		if got := cfg.Language(); got != code {
			t.Errorf("%s came back as %q", code, got)
		}
	}
}

// Anything unrecognised reads as English, because the settings file can be
// hand-edited and a code nobody knows should produce a readable interface rather
// than an empty one.
func TestAnUnknownOrMissingLanguageReadsAsEnglish(t *testing.T) {
	for _, set := range []string{"", "kl", "EN", "en-GB", "english", " en"} {
		cfg := Config{Appearance: Appearance{Language: set}}
		if got := cfg.Language(); got != "en" {
			t.Errorf("%q came back as %q, want en", set, got)
		}
	}
}

// The zero value has to answer, because it is what a failed load hands back and
// what every process starts from.
func TestTheZeroConfigSpeaksEnglish(t *testing.T) {
	if got := (Config{}).Language(); got != "en" {
		t.Errorf("the zero value speaks %q", got)
	}
}

// Every language offered has to be carried, or the picker shows a choice that
// silently becomes English.
func TestEveryOfferedLanguageIsAccepted(t *testing.T) {
	if len(Languages) == 0 {
		t.Fatal("no languages are offered at all")
	}
	for _, code := range Languages {
		if (Config{Appearance: Appearance{Language: code}}).Language() != code {
			t.Errorf("%s is offered and does not survive being read back", code)
		}
	}
	if Defaults().Language() != "en" {
		t.Errorf("the defaults speak %q", Defaults().Language())
	}
}

// XDG_CONFIG_HOME wins where it is set, which is what lets a test — and a person
// with an unusual setup — put the settings somewhere else.
func TestTheSettingsDirectoryFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/somewhere/else")

	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join("/somewhere/else", "snapshotter") {
		t.Errorf("settings would go to %q", dir)
	}
}

func TestWithoutXDGTheSettingsLiveUnderTheHomeDirectory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory here")
	}
	if dir != filepath.Join(home, ".config", "snapshotter") {
		t.Errorf("settings would go to %q", dir)
	}
}

// Saving creates the directory if it is not there. A first run has no ~/.config
// and refusing then would mean nothing could ever be saved.
func TestSavingCreatesTheDirectoryItNeeds(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "never", "made"))

	if err := Save(Defaults()); err != nil {
		t.Fatalf("saving into a directory that does not exist: %v", err)
	}
	back, err := Load()
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if back.Language() != "en" {
		t.Errorf("read back a different language: %q", back.Language())
	}
}

// What is written is readable prose, not an opaque blob. The file is the
// supported way to change what the window does not offer, so somebody has to be
// able to open it.
func TestWhatIsSavedIsReadable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg := Defaults()
	cfg.Appearance.Language = "fr"
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"appearance:", "language: fr", "schedule:"} {
		if !strings.Contains(text, want) {
			t.Errorf("the saved file does not contain %q:\n%s", want, text)
		}
	}
}
