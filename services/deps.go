// Package services exposes the application's behaviour to the frontend.
//
// Each service is a thin translation layer: it turns frontend requests into
// calls on the internal packages and shapes the results for display. The rules
// about what is safe to do live in those packages, not here.
package services

import (
	"context"

	"snapshotter/internal/apfs"
	"snapshotter/internal/schedule"
	"snapshotter/internal/verdict"
)

// Mounter attaches snapshots so their contents can be read.
//
// It is an interface so the services can be driven by mountmgr.Fake, which
// populates a directory rather than calling mount_apfs. Mounting needs root and
// Full Disk Access; browsing and comparing need neither, and this is the seam
// that keeps the second from waiting on the first.
type Mounter interface {
	MountPoint(name string) (string, error)
	IsMounted(name string) (bool, error)
	Mount(ctx context.Context, names []string) error
	Unmount(ctx context.Context, names []string) error
	MountedNames(names []string) []string
}

// Deps is everything the services share.
type Deps struct {
	Runner apfs.Runner
	Mounts Mounter
	Agent  *schedule.Agent
	// Tripwire is the continuously running bulk-deletion watcher, which is a
	// separate launchd job from Agent because they fail differently and are
	// worth turning on and off independently.
	Tripwire *schedule.Tripwire
	Volume   string
	// Faking reports that Mounts is a stand-in and that everything under a
	// mountpoint was invented. The interface says so, and restores that would
	// overwrite a real file are refused while it is true.
	Faking bool
	// FakeSeed is the directory a faked snapshot was cloned from, and so the
	// only place its contents differ from the live disk. Empty unless Faking.
	FakeSeed string
	// Scenario names the simulated machine every reading came from, empty when
	// the readings are this Mac's. It has to reach the interface and not just the
	// log: a scenario that looks like real state is indistinguishable from a
	// scenario that lies, and the whole value of the mode is that what is on
	// screen is known.
	Scenario string
	// Space reports a volume's total and available bytes. Nil means ask the
	// kernel.
	//
	// It exists because free space is the one input that does not arrive through
	// Runner: statfs(2) is a syscall, not a command, so a scenario could describe
	// any machine except a full one — and "free space is low, so retention is not
	// guaranteed" is exactly the finding worth being able to look at without
	// filling a real disk to see it.
	Space func(volume string) (total, free uint64, err error)
	// Verdicts remembers whether a folder differs from a snapshot, so browsing
	// does not walk the same tree every time somebody navigates back to it. Nil
	// simply means every answer is computed afresh, which is what the command
	// line does.
	Verdicts *verdict.Cache
}
