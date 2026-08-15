package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// inTempHome points the package at a throwaway directory. XDG_CONFIG_HOME is
// honoured by Dir precisely so this is possible without touching a real home.
func inTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "snapshotter", "config.yaml")
}

// A machine that has never run this must not be an error case.
func TestLoadWithNoFileReturnsDefaults(t *testing.T) {
	inTempHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("a missing file should not be an error: %v", err)
	}
	if cfg != Defaults() {
		t.Errorf("want the defaults, got %+v", cfg)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := inTempHome(t)

	want := Defaults()
	want.Schedule.IntervalHours = 3
	want.Schedule.RetentionDays = 90
	want.Schedule.Policy = "tiered-year"
	want.Tripwire.Enabled = true
	want.Appearance.Theme = "dark"

	if err := Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Errorf("want %+v, got %+v", want, got)
	}

	// The file should be readable by a person, and say what it is.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if !strings.HasPrefix(string(data), "# Snapshotter settings") {
		t.Errorf("no explanatory header:\n%s", data)
	}
	for _, want := range []string{"interval_hours: 3", "retention_days: 90", "theme: dark"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in:\n%s", want, data)
		}
	}
}

// Settings are one person's, in their own home directory.
func TestSaveIsNotWorldReadable(t *testing.T) {
	path := inTempHome(t)
	if err := Save(Defaults()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("want 0600, got %o", perm)
	}
}

// A file written by a version that did not know about a setting must leave that
// setting at its default rather than at Go's zero value — otherwise upgrading
// would silently set an interval of zero hours.
func TestAbsentKeysKeepTheirDefaults(t *testing.T) {
	path := inTempHome(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("appearance:\n  theme: light\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Appearance.Theme != "light" {
		t.Errorf("did not read the one key present: %+v", cfg.Appearance)
	}
	if cfg.Schedule != Defaults().Schedule {
		t.Errorf("absent keys should keep their defaults, got %+v", cfg.Schedule)
	}
}

// A corrupt file is an error worth reporting, but the application still has to
// run — and it must not be overwritten behind the user's back.
func TestCorruptFileReportsAndStillYieldsDefaults(t *testing.T) {
	path := inTempHome(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("schedule: [this is not a mapping\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err == nil {
		t.Error("a file that cannot be parsed should be reported")
	}
	if cfg != Defaults() {
		t.Errorf("want usable defaults alongside the error, got %+v", cfg)
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(original) {
		t.Error("loading rewrote the broken file; it must be left for the user to fix")
	}
}

// A crash part-way through a write must not cost the previous settings, which is
// what the write-and-rename is for. Proven by the leftovers: a partial write
// leaves a temporary file, never a truncated config.yaml.
func TestSaveLeavesNoPartialConfig(t *testing.T) {
	path := inTempHome(t)
	first := Defaults()
	first.Appearance.Theme = "dark"
	if err := Save(first); err != nil {
		t.Fatal(err)
	}

	second := Defaults()
	second.Appearance.Theme = "light"
	if err := Save(second); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".config-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("want exactly config.yaml, got %d entries", len(entries))
	}
}

// The helpers below turn what someone typed into what the application uses, and
// each has a case where the honest answer is to ignore the file.

func TestResolvePathFallsBackAndExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	for _, tc := range []struct {
		name       string
		configured string
		fallback   string
		want       string
	}{
		{"nothing configured", "", "/default/place", "/default/place"},
		{"an absolute path", "/somewhere/else", "/default", "/somewhere/else"},
		{"a tilde is expanded, because nothing else will", "~/snaps", "/default", filepath.Join(home, "snaps")},
		{"a bare tilde is the home directory", "~", "/default", home},
		{"a tilde mid-path is not a home reference", "/opt/~weird", "/default", "/opt/~weird"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolvePath(tc.configured, tc.fallback); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// A zero interval is a typo, not a request for a timer that never fires.
func TestRefreshIntervalsRefuseNonsense(t *testing.T) {
	d := Defaults()
	for _, tc := range []struct {
		seconds int
		want    time.Duration
	}{
		{0, time.Duration(d.Refresh.MenuBarSeconds) * time.Second},
		{-5, time.Duration(d.Refresh.MenuBarSeconds) * time.Second},
		{90, 90 * time.Second},
	} {
		cfg := Defaults()
		cfg.Refresh.MenuBarSeconds = tc.seconds
		if got := cfg.MenuBarRefresh(); got != tc.want {
			t.Errorf("menu bar %d: want %v, got %v", tc.seconds, tc.want, got)
		}
	}

	cfg := Defaults()
	cfg.Refresh.WindowSeconds = 0
	if got, want := cfg.WindowRefresh(), time.Duration(d.Refresh.WindowSeconds)*time.Second; got != want {
		t.Errorf("window: want %v, got %v", want, got)
	}
	cfg.Refresh.WindowSeconds = 15
	if got := cfg.WindowRefresh(); got != 15*time.Second {
		t.Errorf("window: want 15s, got %v", got)
	}
}

// A window twenty pixels wide is a typo, and it would be very hard to correct
// from inside the application it made unusable.
func TestWindowSizeRefusesTheUnusable(t *testing.T) {
	d := Defaults()
	for _, tc := range []struct {
		name  string
		w, h  int
		wantW int
		wantH int
	}{
		{"configured", 900, 620, 900, 620},
		{"too narrow", 20, 620, d.Window.Width, 620},
		{"too short", 900, 1, 900, d.Window.Height},
		{"zero, as an absent file would give", 0, 0, d.Window.Width, d.Window.Height},
		{"negative", -100, -100, d.Window.Width, d.Window.Height},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.Window.Width, cfg.Window.Height = tc.w, tc.h
			gotW, gotH := cfg.WindowSize()
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Errorf("want %dx%d, got %dx%d", tc.wantW, tc.wantH, gotW, gotH)
			}
		})
	}
}
