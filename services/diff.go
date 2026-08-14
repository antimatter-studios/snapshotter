package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/diffs"
	"snapshotter/internal/vfs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ProgressEvent is the name the frontend listens on while a comparison runs.
const ProgressEvent = "diff:progress"

// progressInterval throttles progress events. A deep comparison visits
// thousands of files a second, and an event per file would spend more time in
// the bridge than in the walk.
const progressInterval = 200 * time.Millisecond

// DiffService compares a folder in a snapshot against the same folder now, or
// against the same folder in another snapshot.
type DiffService struct {
	Deps

	mu     sync.Mutex
	cancel context.CancelFunc
	// runID identifies the comparison that owns cancel, so a finishing run
	// cannot clear the cancel function belonging to one that replaced it.
	runID uint64
}

// NewDiffService builds the service.
func NewDiffService(d Deps) *DiffService { return &DiffService{Deps: d} }

// CompareRequest is one comparison.
type CompareRequest struct {
	Snapshot string `json:"snapshot"`
	// LivePath is the folder to compare, in the running system's terms.
	LivePath string `json:"livePath"`
	// Deep compares contents by hash instead of size and timestamp. It is
	// accurate and slow; the shallow default is the right one for a first look.
	Deep bool `json:"deep"`
	// IncludeSame keeps unchanged files in the result.
	IncludeSame bool `json:"includeSame"`
}

// Compare walks the snapshot and the live disk and returns the differences.
func (d *DiffService) Compare(ctx context.Context, req CompareRequest) (diffs.Result, error) {
	_, snapshotDir, err := d.mountedSide(req.Snapshot, req.LivePath)
	if err != nil {
		return diffs.Result{}, err
	}
	// The snapshot is the past, so it is the older side and goes first.
	return d.walk(ctx, snapshotDir, req.LivePath, diffs.Options{
		Deep:        req.Deep,
		IncludeSame: req.IncludeSame,
	})
}

// CompareSnapshotsRequest asks what changed between two snapshots.
//
// Older and Newer state the roles the caller intends. They are not taken on
// trust — see CompareSnapshots — but naming them rather than numbering them
// means a caller cannot pass two snapshots without having thought about which
// way round the answer will read.
type CompareSnapshotsRequest struct {
	Older string `json:"older"`
	Newer string `json:"newer"`
	// LivePath is the folder to compare, in the running system's terms. Both
	// sides are translated from it, so the same place in the tree is compared in
	// each snapshot however their mountpoints differ.
	LivePath string `json:"livePath"`
	// Deep compares contents by hash instead of size and timestamp, as for a
	// comparison against the live disk.
	Deep bool `json:"deep"`
	// IncludeSame keeps unchanged files in the result.
	IncludeSame bool `json:"includeSame"`
}

// SnapshotComparison is the difference between two snapshots, with the two ends
// named so that no row can be read backwards.
//
// diffs.Change names its fields for a snapshot-against-live comparison, and here
// they mean: AbsSnapshot, SnapSize and SnapModTime describe Older, while AbsLive,
// LiveSize and LiveModTime describe Newer. Status follows from that —
// OnlyInSnapshot is an entry that was gone by the newer snapshot, OnlyOnDisk one
// that had appeared by then. Nothing in diffs has to change for this to hold,
// because it compares two directories and never asks what either one is; but the
// mapping is worth stating, since a reader of the interface would otherwise have
// to infer it from field names that no longer describe the question.
type SnapshotComparison struct {
	Older SnapshotView `json:"older"`
	Newer SnapshotView `json:"newer"`
	// Swapped reports that the request had the two the wrong way round and the
	// roles above were corrected. The interface can then show the direction it
	// actually got rather than the one it asked for.
	Swapped bool `json:"swapped"`
	// LivePath is the folder that was compared, in the running system's terms.
	// Neither side's absolute paths are live paths, so this is what a restore
	// from either of them has to be rebuilt against.
	LivePath string       `json:"livePath"`
	Result   diffs.Result `json:"result"`
}

// CompareSnapshots reports what changed between two snapshots under LivePath.
//
// Which snapshot is the older one is decided by the snapshots' own timestamps,
// not by the order the arguments arrived in. A change between two snapshots has
// no inherent direction, and getting it backwards inverts every row silently: a
// file the user recovered would be reported as one they lost. That is a far worse
// failure than any refusal, so argument order is treated as an intention to be
// checked rather than as a fact. The check is free and authoritative, because
// tmutil puts the instant a snapshot was taken in its name.
//
// Two snapshots cannot tie. The stamp is a whole second of local time and it *is*
// the snapshot's identity, so equal timestamps mean one snapshot named twice,
// which is refused below.
func (d *DiffService) CompareSnapshots(ctx context.Context, req CompareSnapshotsRequest) (SnapshotComparison, error) {
	var out SnapshotComparison

	older, ok := apfs.ParseName(req.Older)
	if !ok {
		return out, fmt.Errorf("%q is not a snapshot", req.Older)
	}
	newer, ok := apfs.ParseName(req.Newer)
	if !ok {
		return out, fmt.Errorf("%q is not a snapshot", req.Newer)
	}
	if older.Name == newer.Name {
		// An empty result would be truthful and useless here: it answers "nothing
		// differs" to a question nobody asked, and reads like an answer about two
		// snapshots when it is an answer about one.
		return out, errors.New("those are the same snapshot, so there is nothing to compare")
	}
	swapped := newer.Taken.Before(older.Taken)
	if swapped {
		older, newer = newer, older
	}

	olderSide, olderDir, olderErr := d.mountedSide(older.Name, req.LivePath)
	newerSide, newerDir, newerErr := d.mountedSide(newer.Name, req.LivePath)
	// Both sides are resolved before either failure is reported, so somebody with
	// neither snapshot open is told both dates at once instead of opening one and
	// being sent straight back for the other. Join keeps errors.Is working, so a
	// caller that only cares that something needs mounting still recognises it.
	if err := errors.Join(olderErr, newerErr); err != nil {
		return out, err
	}

	out = SnapshotComparison{
		Older:    olderSide,
		Newer:    newerSide,
		Swapped:  swapped,
		LivePath: req.LivePath,
	}
	res, err := d.walk(ctx, olderDir, newerDir, diffs.Options{
		Deep:        req.Deep,
		IncludeSame: req.IncludeSame,
	})
	// The partial result is worth returning even on a cancelled walk: what was
	// scanned before the user pressed Stop is still an answer about that much.
	out.Result = res
	return out, err
}

// errNotMountedSide names which snapshot has to be opened first.
//
// A comparison of two snapshots has two candidates, and the bare errNotMounted
// would leave the user opening whichever one they thought of. It is a type rather
// than a wrapped errNotMounted because wrapping appends the general sentence to
// the specific one, and the message is what the interface shows verbatim.
type errNotMountedSide struct{ stamp string }

func (e *errNotMountedSide) Error() string {
	return "the snapshot from " + e.stamp + " is not mounted yet, so open it before comparing"
}

// Is keeps errors.Is(err, errNotMounted) true, so anything that only needs to
// know that a mount is missing does not have to know about this type.
func (e *errNotMountedSide) Is(target error) bool { return target == errNotMounted }

// mountedSide resolves one end of a comparison: the snapshot named, and the
// directory inside its mount that holds livePath. Every comparison goes through
// it, so a missing mount is refused identically whichever side it is on.
func (d *DiffService) mountedSide(name, livePath string) (SnapshotView, string, error) {
	snap, ok := apfs.ParseName(name)
	if !ok {
		return SnapshotView{}, "", fmt.Errorf("%q is not a snapshot", name)
	}
	mounted, err := d.Mounts.IsMounted(snap.Name)
	if err != nil {
		return SnapshotView{}, "", err
	}
	if !mounted {
		return SnapshotView{}, "", &errNotMountedSide{stamp: snap.Stamp}
	}
	mountPoint, err := d.Mounts.MountPoint(snap.Name)
	if err != nil {
		return SnapshotView{}, "", err
	}
	// Each side is translated against its own mountpoint: one live path lands at
	// a different absolute path inside every snapshot.
	dir, err := vfs.ToSnapshot(mountPoint, livePath)
	if err != nil {
		return SnapshotView{}, "", err
	}
	view := SnapshotView{
		Name: snap.Name, Stamp: snap.Stamp, Taken: snap.Taken,
		Mounted: true, MountPoint: mountPoint,
	}
	return view, dir, nil
}

// walk runs one comparison of two directories, older side first.
//
// Only one runs at a time: starting another cancels the first, which is what the
// user means by changing folder mid-scan. Both comparison methods share this so
// they cannot drift apart on cancellation or on how progress is throttled.
func (d *DiffService) walk(ctx context.Context, olderDir, newerDir string, opt diffs.Options) (diffs.Result, error) {
	runCtx, cancel := context.WithCancel(ctx)
	id := d.replaceRunning(cancel)
	defer d.finished(id, cancel)

	last := time.Now()
	progress := func(p diffs.Progress) {
		if time.Since(last) < progressInterval {
			return
		}
		last = time.Now()
		emit(ProgressEvent, p)
	}

	// diffs.Compare calls its parameters snapshotDir and liveDir, but it only
	// ever compares two directories. The older side goes first, which is what
	// makes OnlyInSnapshot mean "gone by the second one".
	res, err := diffs.Compare(runCtx, olderDir, newerDir, opt, progress)
	if err != nil {
		return res, err
	}
	emit(ProgressEvent, diffs.Progress{Scanned: res.Scanned, Changes: len(res.Changes)})
	return res, nil
}

// Cancel stops the comparison in progress, if there is one.
func (d *DiffService) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

// replaceRunning cancels any comparison already in flight and takes its place,
// returning the new run's identifier.
func (d *DiffService) replaceRunning(cancel context.CancelFunc) uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
	}
	d.runID++
	d.cancel = cancel
	return d.runID
}

// finished releases the run's context, and clears the stored cancel function
// only if no newer comparison has already replaced it.
func (d *DiffService) finished(id uint64, cancel context.CancelFunc) {
	cancel()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.runID == id {
		d.cancel = nil
	}
}

// emit sends an event when the application is running. Under `go test` there
// is no application, and a progress report is not worth a panic.
func emit(name string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}
