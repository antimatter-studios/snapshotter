// Package services exposes the application's behaviour to the frontend.
//
// Each service is a thin translation layer: it turns frontend requests into
// calls on the internal packages and shapes the results for display. The rules
// about what is safe to do live in those packages, not here.
package services

import (
	"context"
	"fmt"
	"os"

	"snapshotter/internal/apfs"
	"snapshotter/internal/schedule"
	"snapshotter/internal/verdict"
	"snapshotter/internal/vfs"
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
	// Mounts is the data volume's mounts, and is what the browsing screens use.
	//
	// They are about one volume by nature: they compare a snapshot against the
	// live disk, and the live disk they mean is this one. So the data volume keeps
	// a distinguished place here rather than every caller having to say which
	// volume it means when there is only ever one answer.
	Mounts Mounter
	// MountsOn is the mounts for any other volume, built on demand.
	//
	// Snapshots exist on every mounted APFS volume — `tmutil localsnapshot` takes
	// no arguments — so opening one means naming which volume's copy. Each volume
	// gets its own mountpoint directory, because two volumes' snapshots of the same
	// moment share a date and would otherwise share a mountpoint, the second mount
	// landing on top of the first.
	//
	// Nil is allowed and means only the data volume can be opened, which is what
	// every caller that does not set it gets.
	MountsOn func(volume, device string) Mounter
	Agent    *schedule.Agent
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

// mountsFor resolves a volume identifier to the mounts that belong to it.
//
// On Deps rather than on one service, because every screen that reads inside a
// snapshot has to ask it: browsing, comparing, searching and restoring all resolve
// a snapshot to a mountpoint, and a second answer to that question would be free
// to disagree with the first.
//
// The data volume answers from Mounts rather than being rebuilt. Rebuilding it
// would move where it mounts, which would orphan anything already attached.
func (d Deps) mountsFor(ctx context.Context, device string) (Mounter, error) {
	if device == "" {
		return d.Mounts, nil
	}
	vols, err := apfs.Volumes(ctx, d.Runner)
	if err != nil {
		return nil, err
	}
	for _, v := range vols {
		if v.Device != device {
			continue
		}
		if v.MountPoint == d.Volume {
			return d.Mounts, nil
		}
		if d.MountsOn == nil {
			return nil, fmt.Errorf("services: this build can only open snapshots of %s", d.Volume)
		}
		return d.MountsOn(v.MountPoint, v.Device), nil
	}
	return nil, fmt.Errorf("services: %q is not a volume holding snapshots", device)
}

// rootFor is where browsing a volume's snapshot starts.
//
// The home directory for the startup disk, because that is where a person's work
// is and starting at "/" would put four system directories in front of it. Any
// other volume starts at its own root: it has no home directory, and the whole of
// it is what someone plugged in to look at.
func (d Deps) rootFor(ctx context.Context, device string) string {
	if device == "" {
		return d.homeDir()
	}
	vols, err := apfs.Volumes(ctx, d.Runner)
	if err != nil {
		return d.homeDir()
	}
	for _, v := range vols {
		if v.Device == device && v.MountPoint != d.Volume {
			return v.MountPoint
		}
	}
	return d.homeDir()
}

func (d Deps) homeDir() string {
	if d.Faking && d.FakeSeed != "" {
		return d.FakeSeed
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "/Users"
}

// volumeFor is the live root a device's snapshots were taken of, which is what
// turns a path inside one back into a path on the running system.
//
// The data volume answers as the zero Volume rather than as its mount point: it
// is the one root that is not a plain prefix, being presented at "/" with a fixed
// set of top-level directories and the system volume's symlinks pointing into it.
//
// A device that cannot be resolved answers as the data volume. That is the
// conservative end: translating against it refuses anything under /Volumes, so a
// failure here shows "not covered" rather than reading a path out of the wrong
// disk.
func (d Deps) volumeFor(ctx context.Context, device string) vfs.Volume {
	if device == "" {
		return vfs.Volume{}
	}
	vols, err := apfs.Volumes(ctx, d.Runner)
	if err != nil {
		return vfs.Volume{}
	}
	for _, v := range vols {
		if v.Device == device {
			return vfs.At(v.MountPoint)
		}
	}
	return vfs.Volume{}
}
