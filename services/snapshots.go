package services

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/elevate"
)

// SnapshotService lists, creates, deletes and mounts snapshots.
type SnapshotService struct{ Deps }

// NewSnapshotService builds the service.
func NewSnapshotService(d Deps) *SnapshotService { return &SnapshotService{Deps: d} }

// SnapshotView is one snapshot as the frontend sees it.
type SnapshotView struct {
	Name  string    `json:"name"`
	Stamp string    `json:"stamp"`
	Taken time.Time `json:"taken"`
	// Device and UUID identify this COPY, on this volume.
	//
	// The same date exists on every volume mounted when it was taken, because
	// `tmutil localsnapshot` writes to all of them at once. So a row in a list is
	// not a date, it is one volume's copy of a date, and deleting it must not take
	// the others with it. The UUID is the only identifier that can tell them
	// apart; the stamp cannot.
	Device string `json:"device"`
	UUID   string `json:"uuid"`
	// Purgeable reports that macOS may reclaim this copy without being asked.
	Purgeable bool `json:"purgeable"`
	// LimitsContainer marks the copy holding this volume's container at its
	// current size, which is the one whose deletion actually returns space.
	LimitsContainer bool `json:"limitsContainer"`
	// Mounted and MountPoint describe whether the contents can be read right
	// now. Mounting is the only step that needs authorization.
	Mounted    bool   `json:"mounted"`
	MountPoint string `json:"mountPoint"`
}

// VolumeSnapshots is one volume's snapshots, under the name of the disk.
//
// The list is grouped because the snapshots are: `tmutil localsnapshot` takes no
// arguments and writes to every mounted APFS volume at once, so an ungrouped list
// of one volume's copies was most of a machine's snapshots missing. There was no
// way to see the ones on an external disk at all.
type VolumeSnapshots struct {
	// Name is what the disk is called — "sdcard256gb" — which is the heading a
	// person recognises. MountPoint and Device are what commands are addressed
	// with, and are shown beside it because two mount points can name one volume.
	Name       string `json:"name"`
	MountPoint string `json:"mountPoint"`
	Device     string `json:"device"`
	// IsStartupDisk marks the data volume, which is the only one whose snapshots
	// can be browsed, compared and restored from here. Everything else can be
	// listed and deleted, and opening one is on the way.
	IsStartupDisk bool           `json:"isStartupDisk"`
	Snapshots     []SnapshotView `json:"snapshots"`
	FreeBytes     uint64         `json:"freeBytes"`
	TotalBytes    uint64         `json:"totalBytes"`
}

// Overview is everything the main screen needs in one call.
type Overview struct {
	// Snapshots is the data volume's, which is what the browsing screens work
	// against. Kept alongside Volumes rather than derived from it, because those
	// screens are about one volume by nature: they compare a snapshot with the
	// live disk, and the live disk they mean is this one.
	Snapshots []SnapshotView `json:"snapshots"`
	// Volumes is every volume's, grouped, for the list.
	Volumes []VolumeSnapshots `json:"volumes"`
	// TimeMachineWarning is set when Time Machine has a destination, because
	// backupd then thins these same snapshots to roughly a day and any longer
	// retention shown elsewhere would be a lie.
	TimeMachineWarning string `json:"timeMachineWarning"`
	VolumeTotalBytes   uint64 `json:"volumeTotalBytes"`
	VolumeFreeBytes    uint64 `json:"volumeFreeBytes"`
}

// List returns the snapshots of the data volume, newest first.
func (s *SnapshotService) List(ctx context.Context) ([]SnapshotView, error) {
	snaps, err := apfs.List(ctx, s.Runner, s.Volume)
	if err != nil {
		return nil, err
	}
	views := make([]SnapshotView, 0, len(snaps))
	for _, snap := range snaps {
		mp, err := s.Mounts.MountPoint(snap.Name)
		if err != nil {
			return nil, err
		}
		mounted, err := s.Mounts.IsMounted(snap.Name)
		if err != nil {
			return nil, err
		}
		views = append(views, SnapshotView{
			Name: snap.Name, Stamp: snap.Stamp, Taken: snap.Taken,
			Mounted: mounted, MountPoint: mp,
		})
	}
	return views, nil
}

// Overview returns the snapshot list plus the context needed to read it
// honestly: disk headroom, and whether Time Machine will thin the history.
func (s *SnapshotService) Overview(ctx context.Context) (Overview, error) {
	var out Overview

	views, err := s.List(ctx)
	if err != nil {
		return out, err
	}
	out.Volumes = s.grouped(ctx, views)
	// The flat list is the grouped startup disk, not a second copy of it.
	//
	// Every row needs the volume and identifier it is deleted by, and only the
	// grouping knows them. Leaving the flat list bare would give the window rows
	// it cannot act on whenever it fell back to using it — which is exactly when
	// the least is working and the most is being asked of it.
	out.Snapshots = views
	for _, g := range out.Volumes {
		if g.IsStartupDisk {
			out.Snapshots = g.Snapshots
			break
		}
	}

	if tm := apfs.DestinationInfo(ctx, s.Runner); tm.HasDestination {
		out.TimeMachineWarning = apfs.ThinningWarning()
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.Volume, &stat); err == nil {
		out.VolumeTotalBytes = stat.Blocks * uint64(stat.Bsize)
		out.VolumeFreeBytes = stat.Bavail * uint64(stat.Bsize)
	}
	return out, nil
}

// grouped lists every volume's snapshots under the name of its disk.
//
// The data volume's rows come from views rather than being rebuilt, so the group
// shown for the startup disk is the same list the browsing screens work against
// — mount state included. Rebuilding it here would be a second answer to the same
// question, free to disagree with the first.
//
// A failure to enumerate costs the grouping and not the screen: the data volume's
// list is still correct, and refusing to show anything because an external disk
// could not be interrogated is the worse trade.
func (s *SnapshotService) grouped(ctx context.Context, dataViews []SnapshotView) []VolumeSnapshots {
	vols, err := apfs.Volumes(ctx, s.Runner)
	if err != nil {
		return nil
	}

	out := make([]VolumeSnapshots, 0, len(vols))
	for _, v := range vols {
		group := VolumeSnapshots{
			Name: v.Name, MountPoint: v.MountPoint, Device: v.Device,
			IsStartupDisk: v.MountPoint == s.Volume,
		}
		var stat syscall.Statfs_t
		if err := syscall.Statfs(v.MountPoint, &stat); err == nil {
			group.TotalBytes = stat.Blocks * uint64(stat.Bsize)
			group.FreeBytes = stat.Bavail * uint64(stat.Bsize)
		}

		// The identifiers are per copy even where the row is not: the startup
		// disk's snapshots are deleted by the same call as everyone else's.
		byStamp := map[string]apfs.VolumeSnapshot{}
		for _, snap := range v.Snapshots {
			byStamp[snap.Stamp] = snap
		}
		if group.IsStartupDisk {
			for _, view := range dataViews {
				if snap, ok := byStamp[view.Stamp]; ok {
					view.Device, view.UUID = v.Device, snap.UUID
					view.Purgeable, view.LimitsContainer = snap.Purgeable, snap.LimitsContainer
				}
				group.Snapshots = append(group.Snapshots, view)
			}
			out = append(out, group)
			continue
		}
		// Mount state per volume, from that volume's own mountpoints. Asking the
		// data volume's would answer about a different snapshot that happens to
		// share a date, and the row would offer Close on something never opened.
		var mounts Mounter
		if s.MountsOn != nil {
			mounts = s.MountsOn(v.MountPoint, v.Device)
		}
		for _, snap := range v.Snapshots {
			view := SnapshotView{
				Name: snap.Name, Stamp: snap.Stamp, Taken: snap.Taken,
				Device: v.Device, UUID: snap.UUID,
				Purgeable: snap.Purgeable, LimitsContainer: snap.LimitsContainer,
			}
			if mounts != nil {
				// Errors leave the row unmounted rather than dropping it. Being
				// unable to say whether it is open is not a reason to hide that it
				// exists, which is the whole point of listing these at all.
				if mp, err := mounts.MountPoint(snap.Name); err == nil {
					view.MountPoint = mp
				}
				if mounted, err := mounts.IsMounted(snap.Name); err == nil {
					view.Mounted = mounted
				}
			}
			group.Snapshots = append(group.Snapshots, view)
		}
		out = append(out, group)
	}
	return out
}

// TakeNow creates a snapshot immediately. No authorization is needed: tmutil
// asks backupd to do the work.
//
// One call, several snapshots: `tmutil localsnapshot` takes no arguments and
// writes to every mounted APFS volume at once. What comes back describes the
// startup disk's, which is the one the window selects and browses — the others
// appear in their own groups on the next refresh. The mountpoint is the startup
// disk's for the same reason, and is where this snapshot would attach rather
// than where it has.
func (s *SnapshotService) TakeNow(ctx context.Context) (SnapshotView, error) {
	snap, err := apfs.Create(ctx, s.Runner)
	if err != nil {
		return SnapshotView{}, err
	}
	mp, err := s.Mounts.MountPoint(snap.Name)
	if err != nil {
		return SnapshotView{}, err
	}
	return SnapshotView{Name: snap.Name, Stamp: snap.Stamp, Taken: snap.Taken, MountPoint: mp}, nil
}

// Delete removes ONE VOLUME'S COPY of a snapshot, identified by the volume it is
// on and its identifier there. This is the one irreversible action in the
// application: a deleted snapshot cannot be recreated, because it recorded a past
// state of the disk.
//
// One copy, which is a change. It used to delete by date through tmutil, and
// tmutil removes a date from every volume holding it — so deleting what looked
// like one row took the external disk's snapshot of the same moment with it,
// silently, because that snapshot was not on screen to begin with. Now that every
// volume's copies are listed, a button beside one row has to mean that row.
//
// Retention still deletes by date, and should: a policy's verdict on a date is
// the same on every volume, so removing it everywhere is the whole intent there.
func (s *SnapshotService) Delete(ctx context.Context, device, uuid, stamp string) error {
	name, err := apfs.NameForStamp(stamp)
	if err != nil {
		return err
	}
	// Fails closed. Without both, the only call available is the one that deletes
	// the date from every volume holding it — which is precisely what a button
	// beside one row must not do.
	if device == "" || uuid == "" {
		return fmt.Errorf("services: %s could not be identified on a volume, so nothing was deleted", stamp)
	}

	// Only the startup disk's snapshots can be mounted from here, and the mount
	// state is keyed by date — so asking about another volume's copy would answer
	// about the startup disk's, and refuse to delete an external snapshot because
	// an unrelated one of the same date happened to be open. The device decides
	// whether the question applies at all.
	if s.onStartupDisk(ctx, device) {
		mounted, err := s.Mounts.IsMounted(name)
		if err != nil {
			return err
		}
		if mounted {
			return fmt.Errorf("unmount %s before deleting it", stamp)
		}
	}
	return apfs.DeleteOn(ctx, s.Runner, device, uuid)
}

// onStartupDisk reports whether a device is the volume this application browses.
//
// Answering "no" wrongly would skip a mount check, so a failure to enumerate is
// treated as "yes": the check then runs, and the worst case is refusing to delete
// something that could have been deleted, which the user can undo by closing it.
func (s *SnapshotService) onStartupDisk(ctx context.Context, device string) bool {
	vols, err := apfs.Volumes(ctx, s.Runner)
	if err != nil {
		return true
	}
	for _, v := range vols {
		if v.Device == device {
			return v.MountPoint == s.Volume
		}
	}
	return true
}

// Mount attaches snapshots so their contents can be read. One authorization
// prompt covers the whole batch.
//
// device names which volume's copies to attach, because a date is not an
// identity: every volume mounted when a snapshot was taken has one of that date.
// Empty means the data volume, which is what the browsing screens ask for.
func (s *SnapshotService) Mount(ctx context.Context, device string, names []string) error {
	m, err := s.mountsFor(ctx, device)
	if err != nil {
		return err
	}
	return describeAuth(m.Mount(ctx, names))
}

// Unmount detaches snapshots, on the volume named by device.
func (s *SnapshotService) Unmount(ctx context.Context, device string, names []string) error {
	m, err := s.mountsFor(ctx, device)
	if err != nil {
		return err
	}
	return describeAuth(m.Unmount(ctx, names))
}

// UnmountAll detaches everything this application mounted, which is what the
// window's close handler calls.
//
// Every volume, not the data volume. Leaving another volume's snapshots attached
// would leave them undeletable — a mounted snapshot cannot be removed — and the
// mountpoints would outlive the window that made them with nothing left offering
// to close them.
func (s *SnapshotService) UnmountAll(ctx context.Context) error {
	vols, err := apfs.Volumes(ctx, s.Runner)
	if err != nil {
		return err
	}

	// Each volume is attempted whatever happened to the others, and every failure
	// is carried back rather than returned at the first one: they are separate
	// disks, and one refusing says nothing about the rest.
	var failures []error
	for _, v := range vols {
		mounts, err := s.mountsFor(ctx, v.Device)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		names := make([]string, 0, len(v.Snapshots))
		for _, snap := range v.Snapshots {
			names = append(names, snap.Name)
		}
		attached := mounts.MountedNames(names)
		if len(attached) == 0 {
			continue
		}
		if err := describeAuth(mounts.Unmount(ctx, attached)); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// describeAuth turns a dismissed authorization dialog into a message the UI can
// show as information rather than as a failure.
func describeAuth(err error) error {
	if err == elevate.ErrCancelled {
		return fmt.Errorf("authorization was cancelled, so nothing was mounted or unmounted")
	}
	return err
}
