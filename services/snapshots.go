package services

import (
	"context"
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
	// Mounted and MountPoint describe whether the contents can be read right
	// now. Mounting is the only step that needs authorization.
	Mounted    bool   `json:"mounted"`
	MountPoint string `json:"mountPoint"`
}

// Overview is everything the main screen needs in one call.
type Overview struct {
	Snapshots []SnapshotView `json:"snapshots"`
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
	out.Snapshots = views

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

// TakeNow creates a snapshot immediately. No authorization is needed: tmutil
// asks backupd to do the work.
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

// Delete removes a snapshot, identified by its date stamp. This is the one
// irreversible action in the application: a deleted snapshot cannot be
// recreated, because it recorded a past state of the disk.
func (s *SnapshotService) Delete(ctx context.Context, stamp string) error {
	name, err := apfs.NameForStamp(stamp)
	if err != nil {
		return err
	}
	mounted, err := s.Mounts.IsMounted(name)
	if err != nil {
		return err
	}
	if mounted {
		return fmt.Errorf("unmount %s before deleting it", stamp)
	}
	return apfs.Delete(ctx, s.Runner, stamp)
}

// Mount attaches snapshots so their contents can be read. One authorization
// prompt covers the whole batch.
func (s *SnapshotService) Mount(ctx context.Context, names []string) error {
	return describeAuth(s.Mounts.Mount(ctx, names))
}

// Unmount detaches snapshots.
func (s *SnapshotService) Unmount(ctx context.Context, names []string) error {
	return describeAuth(s.Mounts.Unmount(ctx, names))
}

// UnmountAll detaches everything this application mounted, which is what the
// window's close handler calls.
func (s *SnapshotService) UnmountAll(ctx context.Context) error {
	snaps, err := apfs.List(ctx, s.Runner, s.Volume)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		names = append(names, snap.Name)
	}
	return describeAuth(s.Mounts.Unmount(ctx, s.Mounts.MountedNames(names)))
}

// describeAuth turns a dismissed authorization dialog into a message the UI can
// show as information rather than as a failure.
func describeAuth(err error) error {
	if err == elevate.ErrCancelled {
		return fmt.Errorf("authorization was cancelled, so nothing was mounted or unmounted")
	}
	return err
}
