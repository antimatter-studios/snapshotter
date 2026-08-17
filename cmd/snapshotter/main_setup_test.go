package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/scenario"
)

// What runWindow decides before it builds a window: where things are written,
// and what the services are handed. Both were inside runWindow and so could only
// be checked by launching the application — which meant the one rule that really
// matters here was never checked at all: a scenario must not write a launchd
// plist into the real ~/Library/LaunchAgents, because a plist left there outlives
// the run and starts taking real snapshots on a real timer.

func realPaths(t *testing.T) paths {
	t.Helper()
	dir := t.TempDir()
	return paths{
		mountRoot:       filepath.Join(dir, "mounts"),
		agentDir:        filepath.Join(dir, "LaunchAgents"),
		logPath:         filepath.Join(dir, "snapshotter.log"),
		tripwireLogPath: filepath.Join(dir, "tripwire.log"),
		program:         "/usr/bin/true",
	}
}

// No scenario: the paths are left exactly as resolvePaths produced them.
func TestWithoutAScenarioNothingIsRedirected(t *testing.T) {
	p := realPaths(t)

	s, err := setupFor(context.Background(), p, nil)
	if err != nil {
		t.Fatalf("setupFor: %v", err)
	}
	if s.paths != p {
		t.Errorf("the paths were changed with no scenario asking:\n got %+v\nwant %+v", s.paths, p)
	}
	if s.scenario != "" {
		t.Errorf("a scenario name appeared from nowhere: %q", s.scenario)
	}
	if s.space != nil {
		t.Error("disk space was answered by something other than the kernel")
	}
}

// The rule this whole indirection exists for.
func TestAScenarioWritesNothingWhereTheRealAgentsLive(t *testing.T) {
	p := realPaths(t)

	spec, err := scenario.Load("healthy")
	if err != nil {
		t.Fatalf("loading the scenario: %v", err)
	}
	sim, err := scenario.New(spec, scenario.Options{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("scenario: %v", err)
	}

	s, err := setupFor(context.Background(), p, sim)
	if err != nil {
		t.Fatalf("setupFor: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(s.paths.agentDir) })

	for _, tc := range []struct {
		name string
		got  string
		was  string
	}{
		{"agent directory", s.paths.agentDir, p.agentDir},
		{"log", s.paths.logPath, p.logPath},
		{"tripwire log", s.paths.tripwireLogPath, p.tripwireLogPath},
		{"mount root", s.paths.mountRoot, p.mountRoot},
	} {
		if tc.got == tc.was {
			t.Errorf("%s was not redirected: still %s", tc.name, tc.got)
		}
	}

	home, err := os.UserHomeDir()
	if err == nil {
		real := filepath.Join(home, "Library", "LaunchAgents")
		if strings.HasPrefix(s.paths.agentDir, real) {
			t.Fatalf("a scenario would write a launchd plist into %s", real)
		}
	}

	// The program is deliberately NOT redirected: a plist naming something other
	// than the real binary is a plist that proves nothing when it is read back.
	if s.paths.program != p.program {
		t.Errorf("the program was redirected to %q", s.paths.program)
	}
	if s.scenario != spec.Name {
		t.Errorf("scenario name is %q, want %q", s.scenario, spec.Name)
	}
	// nil where the spec has no opinion, non-nil where it does: either way it is
	// the spec that decides, not this function.
	if (s.space == nil) != (spec.Space() == nil) {
		t.Error("the scenario's answer about disk space was not the one passed through")
	}
}

// Every service reads from one Deps. A field left unset here is a service that
// silently does nothing — an agent with no directory writes its plist to ".".
func TestBuildDepsFillsInEverythingAServiceNeeds(t *testing.T) {
	p := realPaths(t)
	deps := buildDeps(setup{paths: p, scenario: "healthy"}, emptyRunner{})

	if deps.Runner == nil {
		t.Error("no runner")
	}
	if deps.Mounts == nil {
		t.Error("no mounter")
	}
	if deps.Volume != apfs.DataVolume {
		t.Errorf("volume is %q, want the data volume", deps.Volume)
	}
	if deps.Scenario != "healthy" {
		t.Errorf("the scenario name was lost: %q", deps.Scenario)
	}
	if deps.Agent == nil || deps.Agent.AgentDir != p.agentDir || deps.Agent.LogPath != p.logPath {
		t.Errorf("the agent was not pointed at the resolved paths: %+v", deps.Agent)
	}
	if deps.Tripwire == nil || deps.Tripwire.AgentDir != p.agentDir || deps.Tripwire.LogPath != p.tripwireLogPath {
		t.Errorf("the tripwire was not pointed at the resolved paths: %+v", deps.Tripwire)
	}
	// The two agents write continuously and occasionally; sharing a log would
	// make both harder to read at the moment either one matters.
	if deps.Agent.LogPath == deps.Tripwire.LogPath {
		t.Error("both agents write to the same log")
	}
	if deps.Agent.Program != p.program || deps.Tripwire.Program != p.program {
		t.Error("an agent was pointed at something other than the bundle's executable")
	}
}

// Real mounting is the default, and it has to stay the default: a build that
// quietly simulated mounts would show snapshot contents that are not there.
func TestMountsAreRealUnlessTheEnvironmentAsksOtherwise(t *testing.T) {
	t.Setenv(mountmgr.FakeEnabled, "")
	t.Setenv(mountmgr.FakeSeed, "")
	deps := buildDeps(setup{paths: realPaths(t)}, emptyRunner{})

	if deps.Faking {
		t.Error("mounts were simulated without being asked")
	}
	if _, isFake := deps.Mounts.(*mountmgr.Fake); isFake {
		t.Error("the fake mounter was used by default")
	}
}

func TestTheEnvironmentCanAskForSimulatedMounts(t *testing.T) {
	seed := t.TempDir()
	t.Setenv(mountmgr.FakeEnabled, "1")
	t.Setenv(mountmgr.FakeSeed, seed)

	deps := buildDeps(setup{paths: realPaths(t)}, emptyRunner{})
	if !deps.Faking {
		t.Fatal("asking for simulated mounts did not simulate them")
	}
	if deps.FakeSeed != seed {
		t.Errorf("seeded from %q, want %q", deps.FakeSeed, seed)
	}
	// Faking must be visible to the services, because it is what puts the
	// "invented state" wording on screen.
	if _, isFake := deps.Mounts.(*mountmgr.Fake); !isFake {
		t.Errorf("Faking is set but the mounter is %T", deps.Mounts)
	}
}
