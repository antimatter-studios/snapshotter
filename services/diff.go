package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"snapshotter/internal/apfs"
	"snapshotter/internal/diffs"
	"snapshotter/internal/i18n"
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

// FileVersions is one file as two chosen versions hold it, ready to be shown
// side by side.
//
// Left and Right rather than Snapshot and Live, because the right side is no
// longer always the disk: it is whichever version was picked to compare against,
// and another mounted snapshot is as valid a choice as the live filesystem. The
// left side is always the snapshot being browsed.
type FileVersions struct {
	// Left and Right are the two texts. Empty with Readable false means the file
	// was not returned as text at all, and the reason is in Note.
	Left  string `json:"left"`
	Right string `json:"right"`
	// Readable is false for a file that is binary or too large. Both are ordinary
	// outcomes rather than errors: a JPEG has no line-by-line difference to show
	// and a 500MB log would take the window down with it.
	Readable bool   `json:"readable"`
	Note     string `json:"note,omitempty"`
	// The figures are given whatever happens, because "2.1 MB became 2.4 MB" is
	// still an answer about a file that cannot be diffed.
	LeftSize  int64 `json:"leftSize"`
	RightSize int64 `json:"rightSize"`
	// Missing sides are how a file created or deleted between the two versions
	// appears.
	LeftExists  bool `json:"leftExists"`
	RightExists bool `json:"rightExists"`
	// RightLabel names what the right side turned out to be, so the window can say
	// so without repeating the rule for resolving it.
	//
	// A snapshot's stamp, or empty for the live disk. Empty rather than a phrase
	// because the window interpolates this into a sentence — "no longer in
	// {{version}}" — and it used to be the English words "the live disk", which
	// arrived intact in the middle of a German sentence. A stamp is the same in
	// every language; prose is not, and the window has its own word for this one.
	RightLabel string `json:"rightLabel"`
	// Kind is how the window should show this, and is one of:
	//
	//	"text"    lines to compare, in Left and Right
	//	"image"   two pictures, in LeftImage and RightImage
	//	"binary"  no lines to compare; Note says so
	//	"absent"  nothing on either side
	//	"large"   text, but past the size worth rendering; Note says so
	//
	// Readable stays what it was — true only for text — so nothing that already
	// checks it starts rendering an image into a line-by-line view. The two agree
	// by construction, and diffKindsTest pins that they do.
	Kind string `json:"kind"`
	// The two pictures, as data URIs, when Kind is "image". Inlined rather than
	// served from a URL because a snapshot's mountpoint is not reachable from the
	// web view, and an image the window cannot fetch is no better than none.
	LeftImage  string `json:"leftImage,omitempty"`
	RightImage string `json:"rightImage,omitempty"`
	// Pixel dimensions, when they could be read. Empty for a format the Go side
	// has no decoder for — the web view can still draw several of those, and a
	// picture without its dimensions beats no picture.
	LeftDims  string `json:"leftDims,omitempty"`
	RightDims string `json:"rightDims,omitempty"`
	// Identical says the two sides are byte-for-byte the same. Worth stating for
	// an image, where two versions can look alike on screen and a diff of lines
	// is not available to settle it.
	Identical bool `json:"identical"`
}

// maxDiffableBytes is the largest file each side may be for a text comparison.
//
// Both sides are loaded into memory and then into a web view, so this is a limit
// on what the window can survive rather than on what the disk can supply. A
// Sixteen megabytes takes in large logs and generated files as well as source.
// It was one megabyte, which turned out to be the wrong instrument for the same
// reason the folder-walk budget was: it declined things people actually wanted
// to look at.
const maxDiffableBytes = 16 << 20

// maxImageBytes is the largest image each side may be to be shown.
//
// Lower than the text cap because a data URI is a third larger than the bytes it
// carries and both sides are held at once: eight megabytes each becomes about
// twenty-one megabytes of string in the web view, which it manages comfortably.
// Screenshots and photographs sit far below this.
const maxImageBytes = 8 << 20

// FileVersions reads one file from both sides so the window can show what
// changed inside it.
//
// This is the question the tree comparison never answered: it produced a list of
// paths that had changed, which tells someone where to look and nothing about
// what they would find there.
func (d *DiffService) FileVersions(snapshotName, livePath, targetSnapshot string) (FileVersions, error) {
	var out FileVersions

	_, leftPath, err := d.mountedSide(snapshotName, livePath)
	if err != nil {
		return out, err
	}

	// An empty target means the live disk, which is both the default and the
	// common case. Any other value names a snapshot, which must be mounted for
	// the same reason the left side must be — an unmounted snapshot has no paths
	// to read.
	rightPath := filepath.Clean(livePath)
	// Left empty: the window words the live disk itself. See RightLabel.
	if targetSnapshot != "" {
		if targetSnapshot == snapshotName {
			return out, fmt.Errorf("services: %s cannot be compared with itself", snapshotName)
		}
		view, path, err := d.mountedSide(targetSnapshot, livePath)
		if err != nil {
			return out, err
		}
		rightPath, out.RightLabel = path, view.Stamp
	}

	leftInfo, leftErr := os.Stat(leftPath)
	rightInfo, rightErr := os.Stat(rightPath)
	out.LeftExists, out.RightExists = leftErr == nil, rightErr == nil
	if !out.LeftExists && !out.RightExists {
		// Not an error. It is reachable by ordinary use — open a file that exists
		// only on the live disk, then point the right side at a snapshot taken
		// before it was made — and there is nothing wrong with asking. An error
		// would put a red banner over a question that simply has no answer.
		out.Kind = "absent"
		out.Note = i18n.T("diff.inNeitherVersion")
		return out, nil
	}
	if out.LeftExists {
		out.LeftSize = leftInfo.Size()
	}
	if out.RightExists {
		out.RightSize = rightInfo.Size()
	}

	// A picture is shown rather than described. This comes before the binary
	// check because every image format is binary, and "no lines to compare" is a
	// poor answer to "what changed in this screenshot" when both versions can
	// simply be put side by side.
	// Either side settles it: a picture deleted since the snapshot exists only on
	// the left, and one created since only on the right. Asking the right first
	// means a file whose type changed is shown as whatever it is now.
	mediaType := imageMediaType(rightPath, out.RightExists)
	if mediaType == "" {
		mediaType = imageMediaType(leftPath, out.LeftExists)
	}
	if mediaType != "" {
		out.Kind = "image"
		out.LeftImage = dataURI(leftPath, mediaType, out.LeftExists, out.LeftSize)
		out.RightImage = dataURI(rightPath, mediaType, out.RightExists, out.RightSize)
		out.LeftDims = pixelDimensions(leftPath, out.LeftExists)
		out.RightDims = pixelDimensions(rightPath, out.RightExists)
		out.Identical = sameBytes(leftPath, rightPath, out.LeftExists, out.RightExists, out.LeftSize, out.RightSize)
		if out.LeftImage == "" && out.RightImage == "" {
			out.Note = i18n.T("diff.tooLargeToShow")
		}
		return out, nil
	}

	// Binary is asked first, and deliberately. The size gate used to come first,
	// so a 1.5MB PNG was told it was "too large to compare line by line" — which
	// implies a smaller PNG would diff, and it would not. The message named the
	// first gate it hit rather than the reason, which is the same fault as a
	// folder reporting "too large to check" when it was really unreadable.
	//
	// This costs one small read of each side rather than a full one.
	if looksBinary(leftPath, out.LeftExists) || looksBinary(rightPath, out.RightExists) {
		out.Kind = "binary"
		out.Note = i18n.T("diff.looksBinary")
		return out, nil
	}

	if out.LeftSize > maxDiffableBytes || out.RightSize > maxDiffableBytes {
		out.Kind = "large"
		out.Note = i18n.T("diff.tooLargeForLines")
		return out, nil
	}

	leftText, okLeft := readableFile(leftPath, out.LeftExists)
	rightText, okRight := readableFile(rightPath, out.RightExists)
	if !okLeft || !okRight {
		// The prefix said text and the whole file disagreed: a NUL a long way in,
		// or invalid UTF-8 past the sample. Named the same as a file caught at the
		// sample, because it is the same answer — it was left unnamed here, so the
		// field said "binary" for one of the three ways of reaching this and
		// nothing for the other two.
		out.Kind = "binary"
		out.Note = i18n.T("diff.looksBinary")
		return out, nil
	}

	out.Left, out.Right, out.Readable, out.Kind = leftText, rightText, true, "text"
	return out, nil
}

// imageTypes maps an extension to the media type a data URI needs.
//
// By extension rather than by sniffing content: the media type has to be right
// for the web view to draw the picture at all, and an extension is what every
// other program on the machine trusts for that. A file misnamed .png that is
// really a JPEG still draws, because the web view sniffs too.
//
// The list is what WebKit on macOS will render. HEIC is included because Apple's
// own screenshots and photographs use it, even though the Go side has no decoder
// for its dimensions.
var imageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".svg":  "image/svg+xml",
	".tif":  "image/tiff",
	".tiff": "image/tiff",
	".heic": "image/heic",
	".heif": "image/heif",
	".avif": "image/avif",
}

// sniffedTypeBytes is how much of a file http.DetectContentType needs. Its own
// documented figure; more is read from no file than this.
const sniffedTypeBytes = 512

// imageMediaType returns the media type for a file, or empty if it is not a
// picture this can show.
//
// The name and the contents have to agree, because each alone gets it wrong in a
// different direction.
//
// Trusting the extension alone means a ZIP renamed photo.png is base64-encoded,
// sent to the window and handed to an <img> tag, which fails to draw it. Trusting
// the sniff alone rejects HEIC, AVIF and SVG, which the standard library does not
// recognise but the web view draws perfectly well — and HEIC is what this Mac's
// own screenshots and photographs are.
//
// So: the sniff decides when it recognises a picture, and the extension is
// allowed to decide only when the sniff has no opinion at all. Anything the sniff
// positively identifies as something else — a zip, a PDF, an executable — is not
// a picture whatever it is called.
func imageMediaType(path string, exists bool) string {
	byName := imageTypes[strings.ToLower(filepath.Ext(path))]
	if byName == "" || !exists {
		return ""
	}

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, sniffedTypeBytes)
	n, err := f.Read(buf)
	if n == 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return ""
	}
	sniffed := http.DetectContentType(buf[:n])

	// The contents say picture: believe them over the name, since the web view
	// sniffs too and will draw what it actually is.
	if strings.HasPrefix(sniffed, "image/") {
		return sniffed
	}
	// No opinion. This is what HEIC, AVIF and SVG look like to the standard
	// library, so the extension is allowed to stand.
	if sniffed == "application/octet-stream" || strings.HasPrefix(sniffed, "text/") {
		return byName
	}
	// A confident answer that is not a picture: application/zip, application/pdf,
	// and everything else it knows by signature.
	return ""
}

// dataURI reads a file and encodes it for an <img> tag.
//
// Returns empty for a side that is absent or past the cap, which the window
// shows as a missing panel rather than an error: one side missing is how a
// picture added or deleted since the snapshot appears.
func dataURI(path, mediaType string, exists bool, size int64) string {
	if !exists || size > maxImageBytes {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// pixelDimensions reads just the header of an image to find its size.
//
// Only the formats the standard library decodes are registered, which is most of
// what anyone compares. An unknown format returns empty rather than an error: the
// picture is still worth showing without its dimensions beside it.
func pixelDimensions(path string, exists bool) string {
	if !exists {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return ""
	}
	return strconv.Itoa(cfg.Width) + "\u00d7" + strconv.Itoa(cfg.Height)
}

// sameBytes reports whether two files hold exactly the same contents.
//
// Sizes are compared first, which settles most pairs without reading anything.
// Worth answering for a picture: two versions can look identical on a screen and
// there is no line-by-line comparison to settle it.
func sameBytes(leftPath, rightPath string, left, right bool, leftSize, rightSize int64) bool {
	if !left || !right || leftSize != rightSize {
		return false
	}
	a, err := os.ReadFile(leftPath)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(rightPath)
	if err != nil {
		return false
	}
	return bytes.Equal(a, b)
}

// binarySniffBytes is how much of a file is read to decide whether it is text.
//
// Enough to catch any real binary format — every one of them carries a NUL or an
// invalid sequence in its header — and small enough that asking costs nothing
// next to reading a file that may be sixteen megabytes.
const binarySniffBytes = 8 << 10

// looksBinary reports whether a file's opening bytes say it is not text.
//
// A side that does not exist is not binary: a created or deleted file has one
// empty side, and that must still diff as a whole side added or removed.
func looksBinary(path string, exists bool) bool {
	if !exists {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		// Unreadable is not the same as binary. The full read below will produce
		// the honest error rather than this guessing at one.
		return false
	}
	defer f.Close()

	buf := make([]byte, binarySniffBytes)
	n, err := f.Read(buf)
	if n == 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return false
	}
	sample := buf[:n]

	if bytes.IndexByte(sample, 0) >= 0 {
		return true
	}
	// A sample cut mid-character would fail a UTF-8 check for a reason that says
	// nothing about the file, so the trailing partial rune is dropped first.
	for len(sample) > 0 && !utf8.Valid(sample) {
		if utf8.RuneStart(sample[len(sample)-1]) || len(sample) < 2 {
			sample = sample[:len(sample)-1]
			break
		}
		sample = sample[:len(sample)-1]
	}
	return !utf8.Valid(sample)
}

// readableFile returns a file's text, and whether it is text at all.
//
// A side that does not exist reads as empty and readable, which is what makes a
// created or deleted file show as one whole side added or removed rather than as
// an error.
func readableFile(path string, exists bool) (string, bool) {
	if !exists {
		return "", true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	// A NUL byte is the oldest and still the most reliable sign that something is
	// not text. Rendering a JPEG as lines produces noise, not a comparison.
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", false
	}
	return string(data), true
}
