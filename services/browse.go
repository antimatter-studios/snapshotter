package services

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"snapshotter/internal/apfs"
	"snapshotter/internal/diffs"
	"snapshotter/internal/vfs"
)

// BrowseService reads directories, on the live disk and inside snapshots.
type BrowseService struct{ Deps }

// NewBrowseService builds the service.
func NewBrowseService(d Deps) *BrowseService { return &BrowseService{Deps: d} }

// Listing is one directory's contents, on one side.
type Listing struct {
	// LivePath is the location in the running system's terms, which is what
	// the user recognises even when reading a snapshot.
	LivePath string      `json:"livePath"`
	Parent   string      `json:"parent"`
	Entries  []vfs.Entry `json:"entries"`
	// Covered is false for paths no snapshot of the data volume contains, such
	// as external disks or the sealed system volume.
	Covered bool   `json:"covered"`
	Note    string `json:"note,omitempty"`
}

// Home is the starting point for browsing.
//
// While mounts are simulated it is the seed directory, because that is the only
// place a faked snapshot differs from the live disk. Opening on the home folder
// instead would show a wall of identical rows and look like the comparison was
// broken.
func (b *BrowseService) Home() string {
	if b.Faking && b.FakeSeed != "" {
		return b.FakeSeed
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "/Users"
}

// ListLive reads a directory on the running system.
func (b *BrowseService) ListLive(livePath string) (Listing, error) {
	out := Listing{LivePath: filepath.Clean(livePath), Parent: parentOf(livePath), Covered: vfs.Covered(livePath)}
	if !out.Covered {
		out.Note = "Snapshots of the data volume do not cover this location."
	}
	entries, err := vfs.ListDir(out.LivePath)
	if err != nil {
		return out, err
	}
	out.Entries = entries
	return out, nil
}

// ListSnapshot reads a directory as it was when the snapshot was taken. Paths
// are given and returned in live terms; the mapping to the mountpoint is
// internal.
func (b *BrowseService) ListSnapshot(snapshotName, livePath string) (Listing, error) {
	out := Listing{LivePath: filepath.Clean(livePath), Parent: parentOf(livePath), Covered: true}

	mountPoint, err := b.mountPointOf(snapshotName)
	if err != nil {
		return out, err
	}
	snapPath, err := vfs.ToSnapshot(mountPoint, livePath)
	if err != nil {
		out.Covered = false
		out.Note = err.Error()
		return out, nil
	}
	entries, err := vfs.ListDir(snapPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			out.Note = "This folder did not exist when the snapshot was taken."
			return out, nil
		}
		return out, err
	}

	// Report paths in live terms so the two panes line up.
	for i := range entries {
		if live, err := vfs.ToLive(mountPoint, entries[i].Path); err == nil {
			entries[i].Path = live
		}
	}
	out.Entries = entries
	return out, nil
}

// MergedListing is one directory shown from both sides at once, with every
// entry already marked as unchanged, changed, deleted or new.
type MergedListing struct {
	LivePath string         `json:"livePath"`
	Parent   string         `json:"parent"`
	Rows     []diffs.Change `json:"rows"`
	// SnapshotExists and LiveExists distinguish an empty folder from one that
	// was not there at all, which is the difference between "nothing was lost"
	// and "the whole folder is gone".
	SnapshotExists bool   `json:"snapshotExists"`
	LiveExists     bool   `json:"liveExists"`
	Note           string `json:"note,omitempty"`
}

// Merged reads a folder from the snapshot and from the live disk and returns
// the two combined. It reads two directories and never descends, so it stays
// immediate however large the tree below is; Compare is the recursive answer.
func (b *BrowseService) Merged(snapshotName, livePath string, includeSame bool) (MergedListing, error) {
	out := MergedListing{LivePath: filepath.Clean(livePath), Parent: parentOf(livePath)}

	mountPoint, err := b.mountPointOf(snapshotName)
	if err != nil {
		return out, err
	}
	snapshotDir, err := vfs.ToSnapshot(mountPoint, livePath)
	if err != nil {
		out.Note = err.Error()
		return out, nil
	}

	out.SnapshotExists = diffs.DirExists(snapshotDir)
	out.LiveExists = diffs.DirExists(out.LivePath)
	rows, err := diffs.Level(snapshotDir, out.LivePath, diffs.Options{IncludeSame: includeSame})
	if err != nil {
		return out, err
	}
	out.Rows = rows

	switch {
	case !out.SnapshotExists && out.LiveExists:
		out.Note = "This folder did not exist when the snapshot was taken."
	case out.SnapshotExists && !out.LiveExists:
		out.Note = "This folder is in the snapshot but no longer on disk."
	}
	return out, nil
}

// Presence records whether one path exists in one snapshot.
type Presence struct {
	Snapshot string `json:"snapshot"`
	Stamp    string `json:"stamp"`
	Mounted  bool   `json:"mounted"`
	// Present is meaningful only when Mounted is true; an unmounted snapshot
	// cannot be inspected without authorization.
	Present bool  `json:"present"`
	Size    int64 `json:"size"`
}

// Locate answers the question that matters after a deletion: which snapshots
// still hold this path. Only mounted snapshots can be inspected, so the result
// says which ones were checked rather than implying an answer for the rest.
func (b *BrowseService) Locate(ctx context.Context, livePath string) ([]Presence, error) {
	snaps, err := apfs.List(ctx, b.Runner, b.Volume)
	if err != nil {
		return nil, err
	}

	out := make([]Presence, 0, len(snaps))
	for _, snap := range snaps {
		p := Presence{Snapshot: snap.Name, Stamp: snap.Stamp}

		mounted, err := b.Mounts.IsMounted(snap.Name)
		if err != nil {
			return nil, err
		}
		p.Mounted = mounted
		if mounted {
			mountPoint, err := b.Mounts.MountPoint(snap.Name)
			if err != nil {
				return nil, err
			}
			if snapPath, err := vfs.ToSnapshot(mountPoint, livePath); err == nil {
				if entry, err := vfs.Stat(snapPath); err == nil {
					p.Present = true
					p.Size = entry.Size
				}
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// RevealInFinder opens a path in the Finder. Snapshots are mounted nobrowse, so
// this is how a mounted snapshot is opened in a normal file window.
func (b *BrowseService) RevealInFinder(snapshotName, livePath string) error {
	target := livePath
	if snapshotName != "" {
		mountPoint, err := b.mountPointOf(snapshotName)
		if err != nil {
			return err
		}
		target, err = vfs.ToSnapshot(mountPoint, livePath)
		if err != nil {
			return err
		}
	}
	return exec.Command("open", "-R", target).Run()
}

// mountPointOf returns where a snapshot is mounted, refusing when it is not.
func (b *BrowseService) mountPointOf(snapshotName string) (string, error) {
	mounted, err := b.Mounts.IsMounted(snapshotName)
	if err != nil {
		return "", err
	}
	if !mounted {
		return "", errors.New("this snapshot is not mounted yet")
	}
	return b.Mounts.MountPoint(snapshotName)
}

func parentOf(path string) string {
	clean := filepath.Clean(path)
	if clean == "/" {
		return "/"
	}
	return filepath.Dir(clean)
}
