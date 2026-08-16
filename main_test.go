package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snapshotter/internal/config"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/version"
	"snapshotter/services"
)

// What is testable here is what the menu bar and the entry points decide before
// any window exists. runWindow and installTray are not: they build a Wails
// application and a status item, which need a running event loop, so they are
// exercised by launching the application rather than by this file.

// The menu bar has room for a symbol and a number, and it is the only surface
// most people will look at. Getting the glyph wrong means reporting a healthy
// machine as broken, or worse, the reverse.
func TestTheMenuBarGlyphMatchesTheLevel(t *testing.T) {
	for _, tc := range []struct {
		level services.Level
		want  []byte
		name  string
	}{
		{services.LevelOK, trayIconOK, "ok"},
		{services.LevelWarn, trayIconWarn, "warn"},
		{services.LevelBad, trayIconBad, "bad"},
	} {
		if got := trayIcon(tc.level); &got[0] != &tc.want[0] {
			t.Errorf("%s got the wrong glyph", tc.name)
		}
	}
}

// An unrecognised level must read as something to look at rather than as health.
// A level added to services and forgotten here would otherwise show a healthy
// menu bar for a machine nobody has checked.
func TestAnUnknownLevelIsTreatedAsBadRatherThanHealthy(t *testing.T) {
	got := trayIcon(services.Level("something-new"))
	if &got[0] == &trayIconOK[0] {
		t.Error("an unknown level showed the healthy glyph")
	}
	if &got[0] != &trayIconBad[0] {
		t.Error("an unknown level should show the worst glyph available")
	}
}

// Every glyph must actually be embedded. An empty one is a menu bar item with no
// icon, which looks like the application failed to start.
func TestEveryGlyphIsEmbedded(t *testing.T) {
	for name, data := range map[string][]byte{
		"ok": trayIconOK, "warn": trayIconWarn, "bad": trayIconBad,
	} {
		if len(data) == 0 {
			t.Errorf("%s glyph is empty", name)
		}
		// PNG magic: the runtime hands these straight to macOS, and a truncated or
		// wrong-format file fails silently as a missing icon.
		if len(data) < 8 || string(data[1:4]) != "PNG" {
			t.Errorf("%s glyph is not a PNG", name)
		}
	}
}

// The label sits beside the glyph. It carries the count because "how many restore
// points do I have" is answerable at a glance; it does not carry the level,
// because the glyph beside it already says that.
func TestTheMenuBarLabelIsJustTheCount(t *testing.T) {
	for _, n := range []int{0, 1, 42} {
		got := trayLabel(services.Health{SnapshotCount: n})
		if got != itoa(n) {
			t.Errorf("want %q, got %q", itoa(n), got)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// findingPrefix marks severity in the menu, where there is no icon to lean on and
// no colour available. The three must be distinguishable from each other.
func TestFindingPrefixesAreDistinct(t *testing.T) {
	seen := map[string]services.Level{}
	for _, level := range []services.Level{services.LevelOK, services.LevelWarn, services.LevelBad} {
		p := findingPrefix(level)
		if p == "" {
			t.Errorf("%s has no prefix", level)
		}
		if other, clash := seen[p]; clash {
			t.Errorf("%s and %s share the prefix %q", level, other, p)
		}
		seen[p] = level
	}
}

// The server address is the whole application behind an HTTP port: anything that
// can reach it can take and delete this machine's snapshots without a password.
// So ":8080" quietly meaning every interface is a poor thing to acquire by
// accident.
func TestServerOptionsDefaultsToLocalhostAndNeverToEveryInterface(t *testing.T) {
	t.Setenv(serverAddrEnv, "")
	opts, err := serverOptions()
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if opts.Host != "localhost" {
		t.Errorf("default host is %q, not localhost", opts.Host)
	}
	if opts.Port == 0 {
		t.Error("default port is zero")
	}

	t.Setenv(serverAddrEnv, ":9100")
	opts, err = serverOptions()
	if err != nil {
		t.Fatalf("bare port: %v", err)
	}
	if opts.Host != "localhost" {
		t.Errorf("a bare port bound %q rather than localhost", opts.Host)
	}
	if opts.Port != 9100 {
		t.Errorf("want port 9100, got %d", opts.Port)
	}
}

func TestServerOptionsRefusesWhatItCannotBind(t *testing.T) {
	for _, bad := range []string{"not-a-host-port", "localhost:", "localhost:0", "localhost:70000", "localhost:-1", "localhost:http"} {
		t.Setenv(serverAddrEnv, bad)
		if _, err := serverOptions(); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestServerOptionsAcceptsAnExplicitHost(t *testing.T) {
	t.Setenv(serverAddrEnv, "127.0.0.1:8081")
	opts, err := serverOptions()
	if err != nil {
		t.Fatalf("explicit host: %v", err)
	}
	if opts.Host != "127.0.0.1" || opts.Port != 8081 {
		t.Errorf("got %s:%d", opts.Host, opts.Port)
	}
}

// resolvePaths decides where mounts and logs live. The defaults matter because
// every previous version used them, and a changed default would orphan an
// installed agent's log or leave old mounts behind.
func TestResolvePathsDefaultsToTheStandardLocations(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no settings file: the defaults apply
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}

	p, err := resolvePaths()
	if err != nil {
		t.Fatalf("resolvePaths: %v", err)
	}
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"mounts", p.mountRoot, filepath.Join(home, "Library", "Application Support", "Snapshotter", "mounts")},
		{"agents", p.agentDir, filepath.Join(home, "Library", "LaunchAgents")},
		{"log", p.logPath, filepath.Join(home, "Library", "Logs", "snapshotter.log")},
		{"tripwire log", p.tripwireLogPath, filepath.Join(home, "Library", "Logs", "snapshotter-tripwire.log")},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, tc.got)
		}
	}
	if p.program == "" {
		t.Error("no program path, so no agent could be installed naming this binary")
	}
}

// Configured locations win, because the whole point of moving them into a file
// was that a machine where /Users is not where the data lives can say so.
func TestResolvePathsHonoursTheSettingsFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := config.Defaults()
	cfg.Paths.MountRoot = filepath.Join(dir, "mounts")
	cfg.Paths.Log = filepath.Join(dir, "agent.log")
	cfg.Paths.TripwireLog = filepath.Join(dir, "tripwire.log")
	if err := config.Save(cfg); err != nil {
		t.Fatalf("saving settings: %v", err)
	}

	p, err := resolvePaths()
	if err != nil {
		t.Fatalf("resolvePaths: %v", err)
	}
	if p.mountRoot != cfg.Paths.MountRoot {
		t.Errorf("mount root: want %q, got %q", cfg.Paths.MountRoot, p.mountRoot)
	}
	if p.logPath != cfg.Paths.Log {
		t.Errorf("log: want %q, got %q", cfg.Paths.Log, p.logPath)
	}
	if p.tripwireLogPath != cfg.Paths.TripwireLog {
		t.Errorf("tripwire log: want %q, got %q", cfg.Paths.TripwireLog, p.tripwireLogPath)
	}

	// launchd looks in exactly one place, so this one is deliberately not
	// configurable — a plist written anywhere else is a file nobody reads.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if p.agentDir != filepath.Join(home, "Library", "LaunchAgents") {
		t.Errorf("the agent directory moved: %q", p.agentDir)
	}
}

// A settings file that cannot be parsed must not stop the application starting.
// Someone with a broken config still needs the window in order to fix it.
func TestABrokenSettingsFileStillYieldsUsablePaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "snapshotter", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("paths: [broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := resolvePaths()
	if err != nil {
		t.Fatalf("a broken settings file stopped the application: %v", err)
	}
	if p.mountRoot == "" || p.logPath == "" || p.agentDir == "" {
		t.Errorf("unusable paths from a broken file: %+v", p)
	}
}

// openEveryFakeMount is development scaffolding, but it runs at startup under a
// flag and must not panic on a machine with no snapshots.
func TestOpeningEveryFakeMountSurvivesHavingNothingToOpen(t *testing.T) {
	deps := services.Deps{
		Runner: emptyRunner{},
		Volume: "/System/Volumes/Data",
		Mounts: mountmgr.NewFake(t.TempDir(), t.TempDir()),
	}
	openEveryFakeMount(deps) // must not panic
}

// emptyRunner answers every command with nothing, which is what a machine with
// no snapshots looks like to this code.
type emptyRunner struct{}

func (emptyRunner) Run(context.Context, string, ...string) (string, error) { return "", nil }

func TestTrayLabelUnderAScenarioStillCounts(t *testing.T) {
	got := trayLabel(services.Health{SnapshotCount: 7, Scenario: "healthy"})
	if !strings.Contains(got, "7") {
		t.Errorf("the count went missing under a scenario: %q", got)
	}
}

// A development build must not quietly join a menu bar that already has the
// installed one in it. Two identical icons, and only the copy in /Applications
// holds the Full Disk Access grant — so the working build looks the same and
// cannot mount anything. One left running consumed three cores for nineteen
// hours on this machine before anyone noticed it was there.
func TestADevelopmentBuildRefusesToJoinTheInstalledOne(t *testing.T) {
	if version.IsRelease() {
		t.Skip("this binary is stamped, so the guard does not apply to it")
	}

	// The escape hatch the error message advertises has to work, or the advice is
	// worse than useless.
	t.Setenv("SNAPSHOTTER_ALLOW_SECOND_COPY", "1")
	if err := refuseIfInstalledCopyIsRunning(); err != nil {
		t.Errorf("the documented override did not let it through: %v", err)
	}
}

// A released build never refuses: two released copies cannot happen, because
// Homebrew replaces the one in /Applications, and refusing to start a binary
// someone deliberately invoked would be an odd thing for it to do.
func TestAReleasedBuildNeverRefuses(t *testing.T) {
	if !version.IsRelease() {
		t.Skip("this binary is not stamped")
	}
	if err := refuseIfInstalledCopyIsRunning(); err != nil {
		t.Errorf("a released build refused to start: %v", err)
	}
}
