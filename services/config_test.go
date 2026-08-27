package services

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// The reason for silencing a folder arrives while looking at a warning that
// should not have happened, so this is the button under that warning.

func TestIgnoringAFolderStopsTheTripwireCountingIt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	view, err := c.IgnoreFolder("/Users/someone/Library/Caches/Microsoft Edge/Default/Cache")
	if err != nil {
		t.Fatalf("ignore: %v", err)
	}

	var found bool
	for _, f := range view.Config.Tripwire.Ignore {
		if f == "/Users/someone/Library/Caches/Microsoft Edge/Default/Cache/" {
			found = true
		}
	}
	if !found {
		t.Errorf("the folder was not added: %v", view.Config.Tripwire.Ignore)
	}
}

// Stored with separators around it, so it matches that folder and anything under
// it without matching a sibling whose name merely starts the same way.
func TestAFolderIsStoredAsAFragmentNotAPrefix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	view, err := c.IgnoreFolder("/a/build")
	if err != nil {
		t.Fatal(err)
	}
	last := view.Config.Tripwire.Ignore[len(view.Config.Tripwire.Ignore)-1]
	if last != "/a/build/" {
		t.Errorf("stored as %q, want /a/build/ — without the trailing separator it "+
			"would also silence /a/build-output", last)
	}
}

// A button must not be able to silence the whole tripwire by accident.
func TestTheRootCannotBeIgnored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	before := len(NewConfigService().Get().Config.Tripwire.Ignore)
	for _, bad := range []string{"/", "", "   "} {
		if _, err := c.IgnoreFolder(bad); err == nil {
			t.Errorf("%q was accepted, which would silence every path", bad)
		}
	}
	if after := NewConfigService().Get().Config.Tripwire.Ignore; len(after) != before {
		t.Errorf("a refused entry was still written: %v", after)
	}
}

// Removing matters as much as adding: a list nobody can shorten grows until the
// tripwire watches nothing, and that failure is silent by construction.
func TestAFolderCanBeWatchedAgain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	if _, err := c.IgnoreFolder("/a/build"); err != nil {
		t.Fatal(err)
	}
	view, err := c.StopIgnoringFolder("/a/build/")
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	for _, f := range view.Config.Tripwire.Ignore {
		if f == "/a/build/" {
			t.Errorf("it is still ignored: %v", view.Config.Tripwire.Ignore)
		}
	}
}

// Adding the same folder twice is what a second click on the same row is, and it
// should be a no-op rather than a duplicate or an error.
func TestIgnoringTwiceIsHarmless(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	if _, err := c.IgnoreFolder("/a/build"); err != nil {
		t.Fatal(err)
	}
	view, err := c.IgnoreFolder("/a/build")
	if err != nil {
		t.Errorf("a second click was an error: %v", err)
	}
	var n int
	for _, f := range view.Config.Tripwire.Ignore {
		if f == "/a/build/" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("stored %d times", n)
	}
}

// The list of directories the tripwire watches.
//
// It is the whole of what the tripwire watches, so everything about how it is
// added to matters more than an ignore list ever did: what is not on it is not
// protected, and nothing on screen says so afterwards.

func TestAWatchedDirectoryIsRecorded(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	c := NewConfigService()

	view, err := c.WatchDirectory(dir)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if !slices.Contains(view.Config.Tripwire.Watch, dir) {
		t.Errorf("the directory was not added: %v", view.Config.Tripwire.Watch)
	}
}

// Stored as it was typed, resolved when it is read. "~/projects" keeps meaning
// what it says on a machine where the home directory is somewhere else, and the
// settings file says back what the person wrote.
func TestATildeDirectoryIsStoredAsWrittenAndResolvedWhenRead(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	c := NewConfigService()

	view, err := c.WatchDirectory("~")
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if !slices.Contains(view.Config.Tripwire.Watch, "~") {
		t.Errorf("stored as %v, want the tilde back", view.Config.Tripwire.Watch)
	}
	if roots := view.Config.Tripwire.WatchRoots(); len(roots) != 1 || roots[0] != filepath.Clean(home) {
		t.Errorf("resolved to %v, want %s", roots, home)
	}
}

// A directory that is not there is not watched, and the tripwire cannot say so
// afterwards without someone reading its log — which nobody does until something
// has already been lost.
func TestADirectoryThatIsNotThereIsRefused(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	if _, err := c.WatchDirectory(filepath.Join(t.TempDir(), "no-such-directory")); err == nil {
		t.Error("a directory that does not exist was accepted")
	}
	if got := NewConfigService().Get().Config.Tripwire.Watch; len(got) != 0 {
		t.Errorf("a refused directory was still written: %v", got)
	}
}

func TestAFileIsNotADirectoryToWatch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewConfigService().WatchDirectory(file); err == nil {
		t.Error("a file was accepted as a directory to watch")
	}
}

// Watching the whole disk is what naming directories exists to stop, and a
// button must not be able to reinstate it in one click.
func TestTheWholeDiskCannotBeWatchedFromTheWindow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewConfigService()

	for _, bad := range []string{"/", "", "   ", "projects"} {
		if _, err := c.WatchDirectory(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// A second click on the same row is a no-op, not a duplicate: two counters for
// one directory would each need the full threshold, so a burst would count half
// as fast as it should.
func TestWatchingTheSameDirectoryTwiceIsHarmless(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	c := NewConfigService()

	if _, err := c.WatchDirectory(dir); err != nil {
		t.Fatal(err)
	}
	view, err := c.WatchDirectory(dir)
	if err != nil {
		t.Errorf("a second click was an error: %v", err)
	}
	if n := len(view.Config.Tripwire.Watch); n != 1 {
		t.Errorf("stored %d times: %v", n, view.Config.Tripwire.Watch)
	}
}

func TestADirectoryCanBeTakenOffTheWatchList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	c := NewConfigService()

	if _, err := c.WatchDirectory(dir); err != nil {
		t.Fatal(err)
	}
	view, err := c.UnwatchDirectory(dir)
	if err != nil {
		t.Fatalf("unwatch: %v", err)
	}
	if len(view.Config.Tripwire.Watch) != 0 {
		t.Errorf("it is still watched: %v", view.Config.Tripwire.Watch)
	}
}

// The row is shown in one spelling and stored in another, and the button beside
// it has to remove the row it is beside.
func TestUnwatchingMatchesTheResolvedDirectoryToo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	c := NewConfigService()

	if _, err := c.WatchDirectory("~"); err != nil {
		t.Fatal(err)
	}
	view, err := c.UnwatchDirectory(home)
	if err != nil {
		t.Fatalf("unwatch: %v", err)
	}
	if len(view.Config.Tripwire.Watch) != 0 {
		t.Errorf("the resolved form did not match the stored one: %v", view.Config.Tripwire.Watch)
	}
}

// A screen showing only what was typed cannot say that the directory is not
// there, and a directory that is not there is not being watched.
func TestTheListSaysWhatIsWatchedAndWhetherItIsThere(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	c := NewConfigService()

	if _, err := c.WatchDirectory(dir); err != nil {
		t.Fatal(err)
	}
	rows := c.WatchedDirectories()
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Configured != dir || rows[0].Resolved == "" {
		t.Errorf("row = %+v", rows[0])
	}
	if rows[0].Missing {
		t.Error("a directory that exists was reported missing")
	}

	// Deleted behind the application's back, which is the case worth reporting.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if rows = c.WatchedDirectories(); len(rows) != 1 || !rows[0].Missing {
		t.Errorf("a directory that has gone was not reported missing: %+v", rows)
	}
}
