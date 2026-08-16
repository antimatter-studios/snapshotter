package services

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snapshotter/internal/config"
)

// The settings service is the only writer of the file that every installation
// shares, so the interesting cases are the ones where it must refuse.

func TestGetReportsTheFileItIsEditing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	view := (&ConfigService{}).Get()
	if view.Error != "" {
		t.Errorf("a machine with no settings file is not an error: %q", view.Error)
	}
	if view.Path == "" {
		t.Error("a settings screen that will not say which file it edits cannot be fixed by hand")
	}
	if !strings.HasSuffix(view.Path, filepath.Join("snapshotter", "config.yaml")) {
		t.Errorf("unexpected path %q", view.Path)
	}
	if !reflect.DeepEqual(view.Config, config.Defaults()) {
		t.Errorf("want the defaults, got %+v", view.Config)
	}
}

func TestSetThemeAcceptsTheThreeAndRefusesTheRest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	svc := &ConfigService{}

	for _, theme := range []string{"system", "light", "dark"} {
		if err := svc.SetTheme(theme); err != nil {
			t.Errorf("%s was refused: %v", theme, err)
		}
		if got := svc.Get().Config.Appearance.Theme; got != theme {
			t.Errorf("stored %q, read back %q", theme, got)
		}
	}

	for _, bad := range []string{"", "Dark", "solarized", "system; rm -rf /"} {
		if err := svc.SetTheme(bad); err == nil {
			t.Errorf("%q was accepted as a theme", bad)
		}
	}
}

// Changing a colour scheme must never cost someone the rest of their settings.
// If the file cannot be parsed, saving on top of it would replace whatever they
// had written with defaults — so it refuses, and says why.
func TestSetThemeRefusesToOverwriteAFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path := filepath.Join(dir, "snapshotter", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := []byte("schedule: [ this is not a mapping\nwindow:\n  width: 1400\n")
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (&ConfigService{}).SetTheme("dark"); err == nil {
		t.Error("wrote on top of a file it could not read")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(broken) {
		t.Errorf("the unreadable file was modified; the user's own text must survive:\n%s", after)
	}
}

// The window still has to work when the file is broken, and it has to say so
// rather than silently behaving as though nothing were configured.
func TestGetSurfacesAnUnreadableFileWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "snapshotter", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\tthis is not yaml at all\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	view := (&ConfigService{}).Get()
	if view.Error == "" {
		t.Error("a broken settings file was not reported")
	}
	if !reflect.DeepEqual(view.Config, config.Defaults()) {
		t.Error("want usable defaults alongside the error")
	}
	if view.Path == "" {
		t.Error("the path is the one thing that makes the error actionable")
	}
}
