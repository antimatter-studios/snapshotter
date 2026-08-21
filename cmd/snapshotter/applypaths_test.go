package main

import (
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/config"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/schedule"
	"snapshotter/services"
)

// Where snapshots are mounted and where the agents write, applied without a
// relaunch.
//
// This was unreachable until it was separated out: it sat inside applySettings,
// which takes an application.Window, and that interface has ninety-five methods.
// What cannot be tested does not get tested, and this decides paths that
// everything else depends on.

func TestAConfiguredMountRootIsApplied(t *testing.T) {
	m := &mountmgr.Manager{Volume: apfs.DataVolume, Root: "/old/root"}
	deps := services.Deps{Mounts: m}

	cfg := config.Defaults()
	cfg.Paths.MountRoot = "/new/root"
	applyPaths(cfg, paths{mountRoot: "/fallback"}, deps)

	if m.Root != "/new/root" {
		t.Errorf("mount root is %q, want the configured one", m.Root)
	}
}

// An unset path falls back to the resolved default rather than to empty, which
// would mount snapshots at the filesystem root.
func TestAnUnsetMountRootFallsBackRatherThanEmptying(t *testing.T) {
	m := &mountmgr.Manager{Volume: apfs.DataVolume, Root: "/old/root"}
	deps := services.Deps{Mounts: m}

	cfg := config.Defaults()
	cfg.Paths.MountRoot = ""
	applyPaths(cfg, paths{mountRoot: "/fallback"}, deps)

	if m.Root != "/fallback" {
		t.Errorf("mount root is %q, want the fallback", m.Root)
	}
}

func TestTheAgentLogPathsAreApplied(t *testing.T) {
	agent := &schedule.Agent{}
	tripwire := &schedule.Tripwire{}
	deps := services.Deps{Agent: agent, Tripwire: tripwire}

	cfg := config.Defaults()
	cfg.Paths.Log = "/logs/schedule.log"
	cfg.Paths.TripwireLog = "/logs/tripwire.log"
	applyPaths(cfg, paths{logPath: "/fallback/a.log", tripwireLogPath: "/fallback/b.log"}, deps)

	if agent.LogPath != "/logs/schedule.log" {
		t.Errorf("agent log is %q", agent.LogPath)
	}
	if tripwire.LogPath != "/logs/tripwire.log" {
		t.Errorf("tripwire log is %q", tripwire.LogPath)
	}
}

// The agents are absent under a scenario and on a machine where nothing is
// installed. Applying settings must not depend on them being there.
func TestApplyingPathsWithNothingInstalledDoesNotPanic(t *testing.T) {
	applyPaths(config.Defaults(), paths{}, services.Deps{})
}

// A mount manager this application did not build — the fake used for scenarios —
// has no root to move, and must be left alone rather than asserted into.
func TestAFakeMountManagerIsLeftAlone(t *testing.T) {
	fake := mountmgr.NewFake(t.TempDir(), t.TempDir())
	deps := services.Deps{Mounts: fake}

	cfg := config.Defaults()
	cfg.Paths.MountRoot = "/new/root"
	applyPaths(cfg, paths{mountRoot: "/fallback"}, deps)
}
