package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"snapshotter/internal/i18n"
	"snapshotter/internal/trace"
	"strings"
	"sync/atomic"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/diffs"
	"snapshotter/internal/fsevents"
	"snapshotter/internal/verdict"
	"snapshotter/internal/vfs"
)

// BrowseService reads directories, on the live disk and inside snapshots.
type BrowseService struct {
	Deps
	// checks rises whenever the window abandons the folder checks it asked for.
	//
	// A folder's verdict cannot be interrupted from outside: proving one unchanged
	// means reading everything under it, and the read is already under way. So each
	// walk remembers the number it started under and gives up as soon as it no
	// longer matches — which is how clicking into a folder stops the work for the
	// one being left rather than queueing behind it.
	checks atomic.Uint64
}

// NewBrowseService builds the service.
func NewBrowseService(d Deps) *BrowseService { return &BrowseService{Deps: d} }

// AbandonFolderChecks gives up on every folder verdict still being computed.
//
// Called by the window when it starts a new listing. Without it, navigating out
// of a large folder left three walks running to the end for rows nobody would see
// again, and the new folder's answers queued behind them — measured at five to
// ten seconds on an SD card, which reads as the application having locked up.
//
// It does not wait. The walks notice within about a millisecond of reading and
// unwind on their own, and there is nothing for the caller to do with the news
// that they have.
func (b *BrowseService) AbandonFolderChecks() {
	b.checks.Add(1)
}

// Listing is one directory's contents, on one side.
type Listing struct {
	// LivePath is the location in the running system's terms, which is what
	// the user recognises even when reading a snapshot.
	LivePath string `json:"livePath"`
	Parent   string `json:"parent"`
	// Root is the highest folder this volume's snapshots say anything about: "/"
	// for the startup disk, and the mount point for any other.
	//
	// Sent so the window can stop offering folders above it. The trail across the
	// top used to read "/ › Volumes › sdcard256gb › projects" for a snapshot of an
	// SD card, with the first two clickable and leading straight to an error
	// saying that volume's snapshots do not cover them.
	Root    string      `json:"root"`
	Entries []vfs.Entry `json:"entries"`
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
//
// A volume that cannot be identified is an error rather than a third place to
// start. The window can say so; it cannot recover from being quietly put
// somewhere that was never right.
func (b *BrowseService) Home(device string) (string, error) {
	return b.rootFor(context.Background(), device)
}

// ListLive reads a directory on the running system.
//
// device says which volume's coverage to judge against. Asked about the data
// volume, a path on an external disk is correctly "not covered" — and that answer
// is nonsense when the disk in question is the one on screen.
func (b *BrowseService) ListLive(device, livePath string) (Listing, error) {
	volume := b.volumeFor(context.Background(), device)
	out := Listing{
		LivePath: filepath.Clean(livePath),
		Parent:   volume.Parent(livePath),
		Root:     volume.Top(),
		Covered:  volume.Covered(livePath),
	}
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
	volume := b.volumeFor(context.Background(), device)
	out := Listing{
		LivePath: filepath.Clean(livePath),
		Parent:   volume.Parent(livePath),
		Root:     volume.Top(),
		Covered:  true,
	}

	mountPoint, err := b.mountPointOf(device, snapshotName)
	if err != nil {
		return out, err
	}
	snapPath, err := volume.ToSnapshot(mountPoint, livePath)
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
		if live, err := volume.ToLive(mountPoint, entries[i].Path); err == nil {
			entries[i].Path = live
		}
	}
	out.Entries = entries
	return out, nil
}

// MergedListing is one directory shown from both sides at once, with every
// entry already marked as unchanged, changed, deleted or new.
type MergedListing struct {
	LivePath string `json:"livePath"`
	Parent   string `json:"parent"`
	// Root is the highest folder this volume's snapshots say anything about: "/"
	// for the startup disk, and the mount point for any other.
	//
	// Sent so the window can stop offering folders above it. The trail across the
	// top used to read "/ › Volumes › sdcard256gb › projects" for a snapshot of an
	// SD card, with the first two clickable and leading straight to an error
	// saying that volume's snapshots do not cover them.
	Root string         `json:"root"`
	Rows []diffs.Change `json:"rows"`
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
	volume := b.volumeFor(context.Background(), device)
	out := MergedListing{
		LivePath: filepath.Clean(livePath),
		Parent:   volume.Parent(livePath),
		Root:     volume.Top(),
	}

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

// KnownDirectoryStatus answers a folder from what is already recorded, and never
// reads a tree.
//
// The window asks this of every folder before it asks anything else, because the
// three sources of an answer cost wildly different amounts and this is the only
// free one:
//
//  1. What is already recorded. A verdict reached a moment ago, or a difference
//     recorded under this folder that a single stat can confirm. Nothing is
//     scanned — the answer is looked up.
//  2. The event log, which has to be replayed and every path it names verified.
//     Cheap against a walk, not against a lookup.
//  3. Reading the tree, which is the thing all of this exists to avoid.
//
// Running them in that order matters. The event-log pass used to go first, so a
// listing whose folders were all already known still waited on a replay before it
// could say so.
//
// A folder nothing is known about comes back NotExamined, which here means "not
// yet" rather than "could not" — the window asks again properly afterwards.
func (b *BrowseService) KnownDirectoryStatus(device, snapshotName, livePath string) (FolderVerdict, error) {
	return b.directoryStatus(device, snapshotName, livePath, true)
}

func (b *BrowseService) DirectoryStatus(device, snapshotName, livePath string) (FolderVerdict, error) {
	return b.directoryStatus(device, snapshotName, livePath, false)
}

// directoryStatus answers for one folder. knownOnly stops it before the walk.
func (b *BrowseService) directoryStatus(device, snapshotName, livePath string, knownOnly bool) (FolderVerdict, error) {
	ignore := b.changeIgnore()

	// The whole point of the list, and the only part of it that is free: a folder
	// that is itself ignored is answered without reading anything at all. On the
	// machine this was written for, 17,239 of a project's 19,788 entries were node
	// modules — about nine seconds of an SD card's reading, per project, to prove
	// something nobody would restore.
	//
	// Before the cache rather than after, because this costs a string comparison
	// and looking it up would not be cheaper.
	if ignore.Match(livePath) {
		return FolderVerdict{
			Status: string(diffs.Ignored),
			Why:    "not looked inside: " + livePath + " matches change_detection.ignore",
		}, nil
	}

	// Remembered from last time unless the disk has been touched since.
	//
	// Browsing asks this constantly: open a folder, look inside, come back, and
	// every sibling would be walked again to reach the conclusion it reached a
	// moment ago. Nothing changed in between, so nothing needed recomputing — and
	// the filesystem is what says when that stops being true.
	if b.Verdicts != nil {
		// Editing the ignore list changes what "unchanged" means, so verdicts
		// reached under the old list are not answers to the new question.
		b.Verdicts.UnderRules(ignoreFingerprint(ignore))
		if a, ok := b.Verdicts.Get(snapshotName, livePath); ok {
			return FolderVerdict{Status: string(a.Verdict), Why: a.Note}, nil
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

	// A difference already known about somewhere under this folder, re-checked on
	// its own before anything is walked.
	//
	// This is the cheapest answer this function has. A walk stops at the first
	// difference it finds, so a "changed" verdict always rests on one file — and if
	// that file still differs, the verdict still holds, for this folder and for
	// every folder between it and the file. One stat in place of a tree.
	//
	// It only ever answers "changed". A path that matches again says nothing about
	// the rest of the tree, so it is dropped and the walk happens as before —
	// once, finding the next difference to remember.
	if b.Verdicts != nil {
		if changed, ok := b.Verdicts.ChangedPathUnder(snapshotName, livePath); ok {
			if differs, known := b.confirms(device, mountPoint, changed, ignore); known {
				if differs {
					trace.Printf("verdict for %s: changed, on the strength of %s alone", livePath, changed)
					v := verdict.Answer{Verdict: verdict.Modified, ChangedPath: changed}
					b.Verdicts.Put(snapshotName, livePath, v)
					return FolderVerdict{Status: string(diffs.Modified)}, nil
				}
				// Put back, or written again to match. Nothing follows about the
				// rest of the tree, so this stops being worth a stat.
				b.Verdicts.ForgetChangedPath(snapshotName, changed)
			}
		}
	}

	// Everything above this point was a lookup or a stat. Everything below reads a
	// tree, which is what a caller asking only for what is known wants to avoid.
	if knownOnly {
		return FolderVerdict{Status: string(diffs.NotExamined)}, nil
	}

	// The number this walk belongs to. Anything the window abandons after this
	// point moves it on, and the walk below stops at its next check.
	generation := b.checks.Load()

	started := time.Now()
	out := diffs.Explain(snapshotDir, filepath.Clean(livePath), diffs.Options{
		Ignore:    ignore,
		Abandoned: func() bool { return b.checks.Load() != generation },
	})
	why := out.Why
	trace.Printf("verdict for %s: differs=%v answered=%v ignored=%d in %s%s",
		livePath, out.Differs, out.Answered, out.Ignored, time.Since(started).Round(time.Millisecond),
		func() string {
			if why == "" {
				return ""
			}
			return " — " + why
		}())

	// An abandoned walk is not a finding about the folder, so it is neither
	// reported as one nor remembered as one. The window discards it: it asked for
	// this before it moved on, and the row it belonged to is gone.
	if out.Abandoned {
		return FolderVerdict{Status: string(diffs.NotExamined), Why: out.Why}, nil
	}

	var status diffs.Status
	switch {
	case !out.Answered:
		status = diffs.NotExamined
	case out.Differs:
		status = diffs.Modified
	default:
		status = diffs.Same
	}

	// "Nothing changed" and "nothing changed in the parts you asked me to read"
	// are different sentences, and the second one is the truth here. The word on
	// the badge stays "unchanged" because that is what the person asked for when
	// they wrote the list — they redefined the question rather than failing to
	// answer it — but the row says what it skipped, so nobody has to remember
	// their own settings to know what they are looking at.
	if status == diffs.Same && out.Ignored > 0 && why == "" {
		why = fmt.Sprintf("unchanged, apart from %s not looked inside (change_detection.ignore)",
			plural(out.Ignored, "path", "paths"))
	}

	// Every folder this walk read in full and found nothing wrong in.
	//
	// The walk has already paid for them, and throwing them away meant each one
	// was walked again the moment somebody opened the folder above it. One
	// difference proves every folder above it changed; a complete walk that found
	// none proves every folder below it identical, and that is the direction that
	// costs something to establish.
	//
	// Not written to the table: an unchanged verdict is a claim about a period
	// nobody watched, and it is only safe while this process is the one watching.
	if b.Verdicts != nil && status == diffs.Same {
		for _, dir := range out.Clean {
			b.Verdicts.Put(snapshotName, dir, verdict.Answer{Verdict: verdict.Same, Note: why})
		}
	}

	if b.Verdicts != nil {
		// The changed path travels with the verdict. It is what makes the next question
		// about this folder — or about any folder between it and the difference —
		// a single stat rather than another walk.
		b.Verdicts.Put(snapshotName, livePath, verdict.Answer{
			Verdict:     verdict.Verdict(status),
			Note:        why,
			ChangedPath: out.ChangedPath,
		})
	}
	return FolderVerdict{Status: string(status), Why: why}, nil
}

// ignoreFingerprint identifies one ignore list, so the verdict cache can notice
// it being edited.
//
// The patterns joined with a character that cannot appear in one, rather than a
// hash: the list is a handful of short strings, this runs once per folder, and a
// value somebody can read in a log is worth more here than eight saved bytes.
func ignoreFingerprint(ig diffs.Ignore) string {
	return "change_detection.ignore=" + strings.Join(ig.Patterns(), "\x00")
}

// plural writes a count with the right noun, because "1 paths" in an explanation
// undermines the explanation.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Lanes is how many folders the window should check at once on one volume.
//
// Asked of the service rather than decided in the window, because the answer is a
// property of the disk — how it is attached — and the window has no way to know
// that. Three was hardcoded, which was too many for an SD card and far too few
// for internal storage.
//
// An unknown device answers with the cautious number rather than an error. This
// decides a queue width, and a listing that refused to appear because a disk
// would not say how it was attached would be a much worse failure than a queue
// that is narrower than it could be.
func (b *BrowseService) Lanes(device string) int {
	vols, err := b.volumes(context.Background())
	if err != nil {
		return apfs.Volume{}.Lanes()
	}
	for _, v := range vols {
		if v.Device == device || (device == "" && v.MountPoint == b.Volume) {
			return v.Lanes()
		}
	}
	return apfs.Volume{}.Lanes()
}

// confirms re-checks one recorded change: is that difference still there?
//
// known=false means the question could not be answered — the path is not
// reachable on one side, or the snapshot is no longer mounted where it was — and
// is not evidence either way, so the caller walks as if nothing had been
// recorded at all.
func (b *BrowseService) confirms(device, mountPoint, changed string, ignore diffs.Ignore) (differs, known bool) {
	// A change recorded inside a folder somebody has since told this application
	// not to read is not usable, whatever it would have said. Checked here as well
	// as in the walk, because this route skips the walk entirely.
	if ignore.Match(changed) {
		return false, false
	}
	snapshotPath, err := b.volumeFor(context.Background(), device).ToSnapshot(mountPoint, changed)
	if err != nil {
		return false, false
	}
	return diffs.StillDiffers(snapshotPath, filepath.Clean(changed), diffs.Options{Ignore: ignore})
}

// EventLogScan is what one pass over a volume's event log found.
type EventLogScan struct {
	// Offered is how many candidate paths the log named.
	Offered int `json:"offered"`
	// Found is how many of them turned out to differ from the snapshot and were
	// recorded. Always the smaller number, usually by a lot: the log records that
	// something was written, not that the result differs from what a snapshot
	// holds.
	Found int `json:"found"`
	// Usable is false when the log said nothing this pass can rely on — no
	// history yet, a log that was wiped, or one that admitted to dropping events.
	// It is never a reason to conclude anything is unchanged.
	Usable bool `json:"usable"`
	// Why says what happened, for the window and for the log.
	Why string `json:"why,omitempty"`
}

// ScanEventLog harvests what macOS already remembers about a volume, so that
// browsing has somewhere cheap to look before it starts reading trees.
//
// The order of cost, worst to best: walking a folder to prove it unchanged reads
// everything under it; re-checking one recorded difference is a stat; and this is
// how recorded differences get there without anybody having walked anything.
// Measured on the volume this was written for, one pass took 145ms and named 43
// paths — against 178,570 and a timeout for the same call anchored at the start
// of history, which is why it is anchored at where we last looked instead.
//
// Nothing here can conclude that anything is unchanged. Every path the log offers
// is compared against the snapshot before it is believed, and a log that says
// nothing leaves every folder exactly as unknown as it was.
func (b *BrowseService) ScanEventLog(device, snapshotName string) (EventLogScan, error) {
	if b.Verdicts == nil {
		return EventLogScan{Why: "nothing remembers verdicts in this process"}, nil
	}
	mounts, err := b.volumeRoot(device)
	if err != nil {
		return EventLogScan{Why: err.Error()}, nil
	}

	uuid, err := fsevents.UUID(mounts)
	if err != nil || uuid == "" {
		// A volume with no readable history is a volume this cannot help with. Not
		// an error: the data volume's log needs Full Disk Access, and browsing has
		// never needed it.
		return EventLogScan{Why: "this volume keeps no event log this process can read"}, nil
	}

	// Taken before the replay, never after. Anything that happens while it runs
	// then falls into the next pass rather than into the gap between them.
	next := fsevents.Latest()

	// Where the log was last read up to, from the previous run if there was one.
	// This is the half of the table that makes a restart cheap: without it every
	// start would be a first start, and a first start learns nothing.
	wasUUID, wasID, seen := b.Changes.Cursor(mounts)
	fresh := !seen || wasUUID != uuid

	if fresh {
		// Nothing to replay from. Replaying from the beginning of history is not
		// the fallback it looks like: on this machine it named 178,570 paths and
		// still reported dropped events, which is a great deal of reading to learn
		// nothing dependable. So the first pass only starts the clock.
		if seen && wasUUID != uuid {
			// The log was wiped and started again — something a person can do to
			// their own removable disk. Every id recorded against the old one is
			// meaningless, and so is anything that was concluded from it.
			trace.Printf("event log for %s: the log was replaced, starting again", mounts)
		}
		_ = b.Changes.SetCursor(mounts, uuid, next)
		return EventLogScan{Why: "the event log has nothing recorded since this volume was last looked at"}, nil
	}

	replay, err := fsevents.Since(mounts, wasID, eventLogDeadline)
	if err != nil {
		return EventLogScan{Why: err.Error()}, nil
	}
	_ = b.Changes.SetCursor(mounts, uuid, next)

	mountPoint, err := b.mountPointOf(device, snapshotName)
	if err != nil {
		return EventLogScan{Offered: len(replay.Paths), Why: err.Error()}, nil
	}
	ignore := b.changeIgnore()

	out := EventLogScan{Offered: len(replay.Paths), Usable: !replay.Dropped}
	if replay.Dropped {
		// Incomplete, and said so. What it did name is still worth checking —
		// every one is verified — but the absence of a path proves nothing, which
		// was already true and is now certain.
		out.Why = "the event log dropped events, so it is not a complete account"
	}

	seenPath := map[string]bool{}
	for _, rel := range replay.Paths {
		// Relative to the volume's own root, not to the startup disk. Joining
		// against the wrong thing would check a path on another disk entirely and
		// record whatever it found there.
		live := filepath.Join(mounts, rel)
		if seenPath[live] {
			continue
		}
		seenPath[live] = true
		if ignore.Match(live) {
			continue
		}
		if differs, known := b.confirms(device, mountPoint, live, ignore); known && differs {
			b.Verdicts.Put(snapshotName, filepath.Dir(live), verdict.Answer{
				Verdict:     verdict.Modified,
				ChangedPath: live,
			})
			out.Found++
		}
	}
	trace.Printf("event log for %s: offered %d, recorded %d, usable=%v",
		mounts, out.Offered, out.Found, out.Usable)
	return out, nil
}

// eventLogDeadline caps one replay.
//
// A pass anchored where the last one finished is normally milliseconds. This is
// the backstop for a log that will not say it has finished — reaching it is
// reported as dropped, because a replay that was cut short has not accounted for
// the range it was asked about.
const eventLogDeadline = 5 * time.Second

// volumeRoot is the mount point a device is attached at.
func (b *BrowseService) volumeRoot(device string) (string, error) {
	if device == "" {
		return b.Volume, nil
	}
	vols, err := b.volumes(context.Background())
	if err != nil {
		return "", err
	}
	for _, v := range vols {
		if v.Device == device {
			return v.MountPoint, nil
		}
	}
	return "", fmt.Errorf("services: no volume with device %s is mounted", device)
}
