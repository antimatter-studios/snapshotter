package services

import (
	"context"
	"errors"

	"snapshotter/internal/apfs"
	"snapshotter/internal/restore"
)

// errNotMounted is returned when a snapshot has to be attached first. Mounting
// is the only step that needs authorization, so this is the prompt-shaped
// boundary in the application.
var errNotMounted = errors.New("this snapshot is not mounted yet")

// RestoreService copies files out of a snapshot.
type RestoreService struct{ Deps }

// NewRestoreService builds the service.
func NewRestoreService(d Deps) *RestoreService { return &RestoreService{Deps: d} }

// RestoreRequest asks for one path to be brought back.
type RestoreRequest struct {
	Snapshot string `json:"snapshot"`
	// Device names the volume the snapshot is on, empty meaning the startup disk.
	//
	// The snapshot name does not say: the same date exists on every volume mounted
	// when it was taken, and both copies can be attached at once. Restoring from
	// the wrong one would write another disk's file over this one's.
	Device string `json:"device"`
	// LivePath is where the file belongs. In the default mode the restored
	// copy lands beside it rather than on it.
	LivePath string `json:"livePath"`
	// Replace puts the restored copy at the original path, moving whatever is
	// there to a .bak- copy first. Nothing is ever deleted.
	Replace bool `json:"replace"`
}

// Restore copies a file or folder from a snapshot back to the live filesystem.
func (r *RestoreService) Restore(ctx context.Context, req RestoreRequest) (restore.Result, error) {
	var out restore.Result

	snap, ok := apfs.ParseName(req.Snapshot)
	if !ok {
		return out, errors.New("that is not a snapshot")
	}
	mounts, err := r.mountsFor(ctx, req.Device)
	if err != nil {
		return out, err
	}
	mounted, err := mounts.IsMounted(req.Snapshot)
	if err != nil {
		return out, err
	}
	if !mounted {
		return out, errNotMounted
	}
	mountPoint, err := mounts.MountPoint(req.Snapshot)
	if err != nil {
		return out, err
	}
	source, err := r.volumeFor(ctx, req.Device).ToSnapshot(mountPoint, req.LivePath)
	if err != nil {
		return out, err
	}

	mode := restore.SideBySide
	if req.Replace {
		// A faked snapshot contains invented files. Restoring one beside the
		// original is harmless and still demonstrates the flow; putting one over
		// the original would destroy real work to prove a point.
		if r.Faking {
			return out, errors.New("mounts are simulated, so Replace is refused: " +
				"the snapshot's contents are invented and would overwrite the real file")
		}
		mode = restore.Replace
	}
	// The snapshot's date tags the restored copy, so a file recovered from
	// last Tuesday says so in its name.
	return restore.Restore(ctx, source, req.LivePath, restore.Options{Mode: mode, Tag: snap.Stamp})
}
