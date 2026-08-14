package scenario

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"snapshotter/internal/schedule"
)

// sandboxed prepares a scenario inside the test's own directory, so nothing a
// test does can reach ~/Library/LaunchAgents.
func sandboxed(t *testing.T, spec Spec) (*Scenario, Sandbox) {
	t.Helper()
	sc, err := New(spec, Options{Now: fixedClock(), Program: "/usr/local/bin/snapshotter", Root: t.TempDir()})
	if err != nil {
		t.Fatalf("New(%s): %v", spec.Name, err)
	}
	box, err := sc.Sandbox(context.Background())
	if err != nil {
		t.Fatalf("Sandbox(%s): %v", spec.Name, err)
	}
	return sc, box
}

func agentFor(sc *Scenario, box Sandbox) *schedule.Agent {
	return &schedule.Agent{
		Runner:   sc.Runner,
		AgentDir: box.AgentDir,
		Program:  "/usr/local/bin/snapshotter",
		LogPath:  box.LogPath,
		UID:      os.Getuid(),
	}
}

func tripwireFor(sc *Scenario, box Sandbox) *schedule.Tripwire {
	return &schedule.Tripwire{
		Runner:   sc.Runner,
		AgentDir: box.AgentDir,
		Program:  "/usr/local/bin/snapshotter",
		LogPath:  box.TripwireLogPath,
		UID:      os.Getuid(),
	}
}

// A scenario claiming an installed schedule is only worth anything if the code
// that reads a real installed schedule agrees. The plist here is written by the
// real installer and parsed by the real parser, so this asserts the whole
// round-trip rather than the scenario's own bookkeeping.
func TestBuiltInsReadBackThroughTheRealAgentStatus(t *testing.T) {
	ctx := context.Background()
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			spec := mustLoad(t, name)
			sc, box := sandboxed(t, spec)

			st, err := agentFor(sc, box).Status(ctx)
			if err != nil {
				t.Fatalf("schedule status: %v", err)
			}
			if st.Installed != spec.Schedule.Installed {
				t.Errorf("schedule installed = %v, want %v", st.Installed, spec.Schedule.Installed)
			}
			if st.Loaded != spec.Schedule.Loaded {
				t.Errorf("schedule loaded = %v, want %v", st.Loaded, spec.Schedule.Loaded)
			}
			if spec.Schedule.Installed {
				want := spec.scheduleConfig()
				if st.Config.Interval != want.Interval || st.Config.Retention != want.Retention {
					t.Errorf("schedule config = %s/%s, want %s/%s",
						st.Config.Interval, st.Config.Retention, want.Interval, want.Retention)
				}
			}

			tw, err := tripwireFor(sc, box).Status(ctx)
			if err != nil {
				t.Fatalf("tripwire status: %v", err)
			}
			if tw.Installed != spec.Tripwire.Installed {
				t.Errorf("tripwire installed = %v, want %v", tw.Installed, spec.Tripwire.Installed)
			}
			if tw.Loaded != spec.Tripwire.Loaded {
				t.Errorf("tripwire loaded = %v, want %v", tw.Loaded, spec.Tripwire.Loaded)
			}
		})
	}
}

// A plist on disk that launchd has not loaded takes no snapshots while looking
// configured, and it has its own finding in the interface. Installing writes the
// plist and loads it in one go, so this state is only reachable if the scenario
// unloads afterwards.
func TestAnInstalledButUnloadedScheduleIsExpressible(t *testing.T) {
	sc, box := sandboxed(t, Spec{
		Name:     "stalled",
		Summary:  "the plist is there and launchd has not loaded it",
		Schedule: ScheduleSpec{AgentSpec: AgentSpec{Installed: true}},
	})

	st, err := agentFor(sc, box).Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Installed {
		t.Error("the plist was not written")
	}
	if st.Loaded {
		t.Error("launchctl print reports the agent loaded, so the installed-but-not-running state cannot be shown")
	}
}

// Install boots out before it bootstraps, because launchd refuses a label it
// already holds. The fake refuses it too, so installing twice only works if that
// ordering is still there.
func TestInstallingTwiceStillWorks(t *testing.T) {
	ctx := context.Background()
	sc, box := sandboxed(t, mustLoad(t, "healthy"))

	if err := agentFor(sc, box).Install(ctx, schedule.DefaultConfig); err != nil {
		t.Fatalf("reinstalling over a loaded agent: %v", err)
	}
	st, err := agentFor(sc, box).Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Loaded {
		t.Error("the agent is not loaded after being reinstalled")
	}
}

// The competing agent's plist names only a shell script, never tmutil, because
// that is the case the conflict scan originally missed. A scenario faking the
// easy case would leave the fix untested.
func TestTheCompetingAgentIsFoundByTheRealConflictScan(t *testing.T) {
	spec := mustLoad(t, "conflict")
	sc, box := sandboxed(t, spec)

	st, err := agentFor(sc, box).Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	found := false
	for _, c := range st.Conflicts {
		if c == spec.CompetingAgent {
			found = true
		}
		if c == schedule.Label || c == schedule.TripwireLabel {
			t.Errorf("%s is one of ours and was reported as a conflict", c)
		}
	}
	if !found {
		t.Errorf("the conflict scan found %v, not %s", st.Conflicts, spec.CompetingAgent)
	}

	plist, err := os.ReadFile(filepath.Join(box.AgentDir, spec.CompetingAgent+".plist"))
	if err != nil {
		t.Fatalf("reading the competing plist: %v", err)
	}
	if strings.Contains(string(plist), "tmutil") || strings.Contains(string(plist), "localsnapshot") {
		t.Error("the competing plist names tmutil, which makes it the easy case rather than the one that caught the scan out")
	}
}

// The healthy scenario installs both of our own agents, and the scan has to keep
// ignoring them: the tripwire really does take snapshots, so reporting it as a
// rival would send the user to switch off the thing protecting them.
func TestOurOwnAgentsAreNeverReportedAsConflicts(t *testing.T) {
	sc, box := sandboxed(t, mustLoad(t, "healthy"))

	st, err := agentFor(sc, box).Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("the healthy scenario reports conflicts: %v", st.Conflicts)
	}
}

func TestSpecsThatCouldNotDescribeAMachineAreRefused(t *testing.T) {
	for _, tc := range []struct {
		why  string
		spec Spec
	}{
		{
			why:  "a name that is not a usable path component, because the sandbox is deleted by that path",
			spec: Spec{Name: "../../etc"},
		},
		{
			why:  "no name at all",
			spec: Spec{},
		},
		{
			// diskutil names one snapshot as limiting the container, and the
			// interface reports it as the one worth deleting first. Two would make
			// that answer depend on map iteration order.
			why: "two snapshots limiting the container",
			spec: Spec{Name: "two-pinned", Snapshots: []SnapshotSpec{
				{Age: Span(time.Hour), LimitsContainer: true},
				{Age: Span(2 * time.Hour), LimitsContainer: true},
			}},
		},
		{
			why:  "a snapshot with no age",
			spec: Spec{Name: "ageless", Snapshots: []SnapshotSpec{{}}},
		},
		{
			// The conflict scan skips our own plists, so this would claim a
			// conflict nothing can ever see.
			why:  "a competing agent that is one of ours",
			spec: Spec{Name: "self-conflict", CompetingAgent: schedule.TripwireLabel},
		},
		{
			why:  "a competing agent whose label is not a usable filename",
			spec: Spec{Name: "bad-label", CompetingAgent: "../../../etc/rogue"},
		},
	} {
		if _, err := New(tc.spec, Options{Now: fixedClock()}); err == nil {
			t.Errorf("accepted %s", tc.why)
		}
	}
}

// Two snapshots of the same age collapse into one, because APFS names a snapshot
// by its date to the second. Duplicating an entry is an easy mistake to make in a
// file, and showing one snapshot where two were asked for is the kind of quiet
// divergence a scenario cannot afford.
func TestSnapshotsThatWouldShareAStampAreRefused(t *testing.T) {
	_, err := New(Spec{Name: "clashing", Snapshots: []SnapshotSpec{
		{Age: Span(time.Hour)},
		{Age: Span(time.Hour)},
	}}, Options{Now: fixedClock()})
	if err == nil {
		t.Fatal("two snapshots landing on the same second were accepted")
	}
}

// A scenario that inherited the last run's sandbox would stop being the state that
// was written down — an agent uninstalled in one run would still be gone in the
// next.
func TestTheSandboxIsEmptiedOnEveryStart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	spec := mustLoad(t, "healthy")

	sc, err := New(spec, Options{Now: fixedClock(), Program: "/usr/local/bin/snapshotter", Root: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	box, err := sc.Sandbox(ctx)
	if err != nil {
		t.Fatalf("Sandbox: %v", err)
	}
	leftover := filepath.Join(box.AgentDir, "com.example.leftover.plist")
	if err := os.WriteFile(leftover, []byte("from the last run"), 0o644); err != nil {
		t.Fatal(err)
	}

	again, err := New(spec, Options{Now: fixedClock(), Program: "/usr/local/bin/snapshotter", Root: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := again.Sandbox(ctx); err != nil {
		t.Fatalf("Sandbox: %v", err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("%s survived a restart (stat error %v)", leftover, err)
	}
}

// Being able to mistake a scenario for the machine is the one failure this mode
// must never have: the application exists because somebody believed they were
// protected and was not. The banner is the guard, so it is worth asserting rather
// than assuming.
func TestTheBannerRefusesToBeMistakenForRealState(t *testing.T) {
	sc := mustNew(t, mustLoad(t, "overdue"))
	banner := strings.Join(sc.Banner(), "\n")

	for _, want := range []string{
		"SCENARIO",
		"nothing reported below describes this Mac",
		"overdue",
		sc.Spec.Summary,
		"are not being run",
		EnvName,
	} {
		if !strings.Contains(banner, want) {
			t.Errorf("the banner does not say %q:\n%s", want, banner)
		}
	}
}

// Everything the scenario asserts belongs in the banner, because the way a
// surprising screen gets diagnosed is by reading what was claimed.
func TestTheBannerStatesWhatTheScenarioAsserts(t *testing.T) {
	sc := mustNew(t, mustLoad(t, "conflict"))
	banner := strings.Join(sc.Banner(), "\n")

	for _, want := range []string{
		"10 invented",
		"newest 1h ago",
		"installed and loaded",
		"every 6h",
		"kept 2w",
		"com.christhomas.apfs-snapshot",
	} {
		if !strings.Contains(banner, want) {
			t.Errorf("the banner does not say %q:\n%s", want, banner)
		}
	}
}

func TestFromEnvSelectsNothingByDefault(t *testing.T) {
	t.Setenv(EnvName, "")
	t.Setenv(EnvFile, "")
	sc, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if sc != nil {
		t.Fatalf("a scenario was built without being asked for: %s", sc.Spec.Name)
	}
}

func TestFromEnvSelectsABuiltInAndRejectsAnUnknownOne(t *testing.T) {
	t.Setenv(EnvFile, "")

	t.Setenv(EnvName, "overdue")
	sc, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if sc == nil || sc.Spec.Name != "overdue" {
		t.Fatalf("FromEnv gave %v", sc)
	}

	t.Setenv(EnvName, "healty")
	if _, err := FromEnv(); err == nil {
		t.Error("a misspelt scenario name was accepted, so the run would have shown real state instead")
	}
}

// Preferring one silently would make a stale variable in a shell look like a
// broken scenario file.
func TestFromEnvRefusesBothVariablesAtOnce(t *testing.T) {
	t.Setenv(EnvName, "healthy")
	t.Setenv(EnvFile, "/tmp/whatever.json")
	if _, err := FromEnv(); err == nil {
		t.Error("both selectors at once were accepted")
	}
}

// FromEnv must not touch the filesystem: the command line runs through the same
// path, and `snapshotter list` under a scenario has no business writing plists.
func TestFromEnvWritesNothing(t *testing.T) {
	t.Setenv(EnvFile, "")
	t.Setenv(EnvName, "healthy")

	sc, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	dir := filepath.Join(os.TempDir(), sandboxDir, "healthy-"+strconv.Itoa(os.Getpid()))
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("%s exists after FromEnv alone (stat error %v)", dir, err)
	}
	if sc.Runner == nil {
		t.Error("no Runner, which is the only thing FromEnv was supposed to build")
	}
}

// The file format is the escape hatch for every state no built-in covers, so it
// has to accept what a built-in would produce.
func TestAScenarioFileDescribesTheSameThingsAsABuiltIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stalled.json")
	const written = `{
  "name": "from-a-file",
  "summary": "installed, not loaded, and Time Machine is attached",
  "snapshots": [
    {"age": "2h"},
    {"age": "3d", "purgeable": false, "limitsContainer": true}
  ],
  "timeMachine": true,
  "schedule": {"installed": true, "loaded": false, "interval": "90m", "retention": "3d"},
  "tripwire": {"installed": true, "loaded": true},
  "competingAgent": "com.example.rival"
}`
	if err := os.WriteFile(path, []byte(written), 0o644); err != nil {
		t.Fatal(err)
	}

	spec, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(spec.Snapshots) != 2 || spec.Snapshots[1].Age.Duration() != 72*time.Hour {
		t.Fatalf("snapshots read as %+v", spec.Snapshots)
	}
	if spec.Snapshots[1].purgeable() {
		t.Error("purgeable: false read as purgeable")
	}
	if !spec.TimeMachine || spec.CompetingAgent != "com.example.rival" {
		t.Errorf("the rest of the file read as %+v", spec)
	}

	cfg := spec.scheduleConfig()
	if cfg.Interval != 90*time.Minute || cfg.Retention != 72*time.Hour {
		t.Errorf("the schedule read as %s/%s", cfg.Interval, cfg.Retention)
	}

	sc, box := sandboxed(t, spec)
	st, err := agentFor(sc, box).Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Installed || st.Loaded {
		t.Errorf("installed = %v, loaded = %v, want installed and not loaded", st.Installed, st.Loaded)
	}
	if st.Config.Interval != 90*time.Minute {
		t.Errorf("the installed plist reads back as every %s", st.Config.Interval)
	}
}

// A misspelt key that silently did nothing would be the worst failure this file
// format could have: the scenario would run, look plausible, and not be the one
// that was written.
func TestAScenarioFileRefusesAKeyItDoesNotKnow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "typo.json")
	if err := os.WriteFile(path, []byte(`{"name":"typo","snapshot":[{"age":"2h"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Error("a misspelt key was accepted")
	}
}
