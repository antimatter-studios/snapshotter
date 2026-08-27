package services

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"snapshotter/internal/i18n"
	"snapshotter/internal/trace"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/diffs"
	"snapshotter/internal/verdict"
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
// Home is where browsing a volume starts: the home directory on the startup disk,
// and the volume's own root anywhere else.
//
// Another volume has no home directory, and the whole of it is what someone
// plugged in to look at. Starting at a home path that does not exist there would
// open an empty listing and look like an empty snapshot.
func (b *BrowseService) Home(device string) string {
	return b.rootFor(context.Background(), device)
}

// ListLive reads a directory on the running system.
//
// device says which volume's coverage to judge against. Asked about the data
// volume, a path on an external disk is correctly "not covered" — and that answer
// is nonsense when the disk in question is the one on screen.
func (b *BrowseService) ListLive(device, livePath string) (Listing, error) {
	volume := b.volumeFor(context.Background(), device)
	out := Listing{LivePath: filepath.Clean(livePath), Parent: parentOf(livePath), Covered: volume.Covered(livePath)}
	if !out.Covered {
		out.Note = i18n.T("browse.notCovered")
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
func (b *BrowseService) ListSnapshot(device, snapshotName, livePath string) (Listing, error) {
	out := Listing{LivePath: filepath.Clean(livePath), Parent: parentOf(livePath), Covered: true}

	mountPoint, err := b.mountPointOf(device, snapshotName)
	if err != nil {
		return out, err
	}
	snapPath, err := b.volumeFor(context.Background(), device).ToSnapshot(mountPoint, livePath)
	if err != nil {
		out.Covered = false
		out.Note = err.Error()
		return out, nil
	}
	entries, err := vfs.ListDir(snapPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			out.Note = i18n.T("browse.folderDidNotExist")
			return out, nil
		}
		return out, err
	}

	// Report paths in live terms so the two panes line up.
	for i := range entries {
		if live, err := b.volumeFor(context.Background(), device).ToLive(mountPoint, entries[i].Path); err == nil {
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
func (b *BrowseService) Merged(device, snapshotName, livePath string, includeSame bool) (MergedListing, error) {
	out := MergedListing{LivePath: filepath.Clean(livePath), Parent: parentOf(livePath)}

	mountPoint, err := b.mountPointOf(device, snapshotName)
	if err != nil {
		return out, err
	}
	snapshotDir, err := b.volumeFor(context.Background(), device).ToSnapshot(mountPoint, livePath)
	if err != nil {
		out.Note = err.Error()
		return out, nil
	}

	out.SnapshotExists = diffs.DirExists(snapshotDir)
	out.LiveExists = diffs.DirExists(out.LivePath)
	// Directories come back unexamined and are resolved one at a time by
	// DirectoryStatus below, so the listing appears at once rather than waiting
	// on however many full walks it would take to prove some folder is untouched.
	rows, err := diffs.Level(snapshotDir, out.LivePath, diffs.Options{
		IncludeSame: includeSame, DeferDirectories: true,
	})
	if err != nil {
		return out, err
	}
	out.Rows = rows

	switch {
	case !out.SnapshotExists && out.LiveExists:
		out.Note = i18n.T("browse.folderDidNotExist")
	case out.SnapshotExists && !out.LiveExists:
		out.Note = i18n.T("browse.folderGoneFromDisk")
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
	// One volume's snapshots, and the startup disk's. Locate has no route in from
	// the window — see reachable_test — so it is left describing the volume it
	// always described rather than being half-widened.
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
			// The data volume throughout: this lists its snapshots and resolves
			// them through its mounts, so translating against any other volume
			// would be reading one disk's paths out of another's snapshot.
			if snapPath, err := (vfs.Volume{}).ToSnapshot(mountPoint, livePath); err == nil {
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
func (b *BrowseService) RevealInFinder(device, snapshotName, livePath string) error {
	target := livePath
	if snapshotName != "" {
		mountPoint, err := b.mountPointOf(device, snapshotName)
		if err != nil {
			return err
		}
		target, err = b.volumeFor(context.Background(), device).ToSnapshot(mountPoint, livePath)
		if err != nil {
			return err
		}
	}
	return exec.Command("open", "-R", target).Run()
}

// mountPointOf returns where a snapshot is mounted, refusing when it is not.
// device names the volume the snapshot is on, empty meaning the startup disk.
//
// It has to be asked for rather than worked out. The same date exists on every
// volume mounted when it was taken, and both copies can be attached at once — so
// a snapshot name alone does not say which mountpoint to read, and guessing would
// show someone another disk's files under the row they opened.
func (b *BrowseService) mountPointOf(device, snapshotName string) (string, error) {
	mounts, err := b.mountsFor(context.Background(), device)
	if err != nil {
		return "", err
	}
	mounted, err := mounts.IsMounted(snapshotName)
	if err != nil {
		return "", err
	}
	if !mounted {
		return "", errors.New("this snapshot is not mounted yet")
	}
	return mounts.MountPoint(snapshotName)
}

func parentOf(path string) string {
	clean := filepath.Clean(path)
	if clean == "/" {
		return "/"
	}
	return filepath.Dir(clean)
}

// DirectoryStatus answers whether anything under one directory has changed.
//
// Separate from Merged because the two have opposite costs. A listing is two
// directory reads and is instant; a directory's verdict may be a walk of
// everything beneath it, and only in the case where nothing has changed — a
// difference is found and returned the moment it appears. Asking for them
// together would make every listing as slow as its slowest folder.
//
// The window calls this once per folder row and fills each in as it answers, so
// a large untouched tree delays nothing but its own row.
// FolderVerdict is what was concluded about one folder, and why not, when it
// could not be concluded.
type FolderVerdict struct {
	Status string `json:"status"`
	// Why is empty unless the walk gave up. It is carried back rather than only
	// logged, because somebody looking at "could not check" wants to know why
	// without going to find a log file — and because the application knowing and
	// discarding it is what made three wrong guesses possible.
	Why string `json:"why,omitempty"`
}

func (b *BrowseService) DirectoryStatus(device, snapshotName, livePath string) (FolderVerdict, error) {
	// Remembered from last time unless the disk has been touched since.
	//
	// Browsing asks this constantly: open a folder, look inside, come back, and
	// every sibling would be walked again to reach the conclusion it reached a
	// moment ago. Nothing changed in between, so nothing needed recomputing — and
	// the filesystem is what says when that stops being true.
	if b.Verdicts != nil {
		if v, ok := b.Verdicts.Get(snapshotName, livePath); ok {
			return FolderVerdict{Status: string(v)}, nil
		}
	}

	mountPoint, err := b.mountPointOf(device, snapshotName)
	if err != nil {
		return FolderVerdict{Status: string(diffs.NotExamined), Why: err.Error()}, err
	}
	// vfs.ToSnapshot, not string surgery: it is the same mapping Merged uses, and
	// a hand-rolled one here would compare a different directory than the one the
	// row came from.
	snapshotDir, err := b.volumeFor(context.Background(), device).ToSnapshot(mountPoint, livePath)
	if err != nil {
		return FolderVerdict{Status: string(diffs.NotExamined), Why: err.Error()}, err
	}

	started := time.Now()
	differs, answered, why := diffs.Explain(snapshotDir, filepath.Clean(livePath), diffs.Options{})
	trace.Printf("verdict for %s: differs=%v answered=%v in %s%s",
		livePath, differs, answered, time.Since(started).Round(time.Millisecond),
		func() string {
			if why == "" {
				return ""
			}
			return " — " + why
		}())

	var status diffs.Status
	switch {
	case !answered:
		status = diffs.NotExamined
	case differs:
		status = diffs.Modified
	default:
		status = diffs.Same
	}

	if b.Verdicts != nil {
		b.Verdicts.Put(snapshotName, livePath, verdict.Verdict(status))
	}
	return FolderVerdict{Status: string(status), Why: why}, nil
}
