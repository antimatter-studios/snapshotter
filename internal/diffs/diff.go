// Package diffs compares a directory inside a mounted snapshot against the
// same directory on the live filesystem.
//
// The comparison is deliberately scoped to a directory the user chooses. A
// whole-volume diff of a 600 GiB data volume produces noise rather than an
// answer; the question worth asking is "what happened to this folder".
package diffs

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Status describes what happened to one path between the snapshot and now.
type Status string

const (
	// Same means the file is unchanged by whichever comparison was requested.
	Same Status = "same"
	// Modified means the file exists on both sides with different contents.
	Modified Status = "modified"
	// OnlyInSnapshot means the file has been deleted since the snapshot. This
	// is the status that matters after an accidental wipe.
	OnlyInSnapshot Status = "onlyInSnapshot"
	// OnlyOnDisk means the file was created after the snapshot was taken.
	OnlyOnDisk Status = "onlyOnDisk"
	// TypeChanged means a name now refers to a different kind of object, for
	// instance a directory replaced by a symlink.
	TypeChanged Status = "typeChanged"

	// NotExamined is a directory whose contents were not walked far enough to
	// say. It exists because the honest answer to "did anything under here
	// change?" is sometimes "I did not look", and reporting that as unchanged is
	// how this application would tell somebody their work was safe when it had
	// not checked.
	NotExamined Status = "notExamined"
)

// Change is one difference between the snapshot and the live filesystem.
type Change struct {
	// RelPath is relative to the compared directory, which is what the UI
	// shows. AbsLive and AbsSnapshot are the full paths on each side.
	RelPath     string    `json:"relPath"`
	AbsLive     string    `json:"absLive"`
	AbsSnapshot string    `json:"absSnapshot"`
	Status      Status    `json:"status"`
	IsDir       bool      `json:"isDir"`
	SnapSize    int64     `json:"snapSize"`
	LiveSize    int64     `json:"liveSize"`
	SnapModTime time.Time `json:"snapModTime"`
	LiveModTime time.Time `json:"liveModTime"`
}

// Options tunes a comparison.
type Options struct {
	// Deep compares file contents by hash. Without it, files matching on both
	// size and modification time are assumed identical, which is fast and
	// wrong only for a file rewritten with identical length and a restored
	// timestamp.
	Deep bool
	// IncludeSame keeps unchanged files in the result. Off by default: the
	// interesting output is what differs.
	IncludeSame bool
	// MaxDepth limits recursion; zero means no limit.
	MaxDepth int
	// DeferDirectories leaves every directory reported as NotExamined instead of
	// walking it for a verdict.
	//
	// The browser sets it so a listing appears at once: a folder whose contents
	// are unchanged costs a full walk to prove it, and a window that waits for
	// several of those before drawing anything is a window that feels broken. Each
	// row is then resolved on its own, and fills in as its answer arrives.
	DeferDirectories bool
}

// Result is the outcome of a comparison.
type Result struct {
	Root        string   `json:"root"`
	SnapshotDir string   `json:"snapshotDir"`
	Changes     []Change `json:"changes"`
	// Errors records paths that could not be read, usually because macOS
	// privacy protection covers them. A comparison continues past them rather
	// than failing, so a protected subfolder cannot hide the rest of the answer.
	Errors  []string `json:"errors"`
	Scanned int      `json:"scanned"`
}

// Counts summarises a result for the header line of the UI.
func (r Result) Counts() map[Status]int {
	out := map[Status]int{}
	for _, c := range r.Changes {
		out[c.Status]++
	}
	return out
}

// Progress reports how far a comparison has got.
type Progress struct {
	Scanned int
	Current string
	Changes int
}

// Compare walks snapshotDir and liveDir together and reports the differences.
//
// Directories present on only one side are reported as a single change rather
// than expanded, so deleting a large tree produces one row that names it
// instead of thousands that bury it.
func Compare(ctx context.Context, snapshotDir, liveDir string, opt Options, progress func(Progress)) (Result, error) {
	res := Result{Root: liveDir, SnapshotDir: snapshotDir}

	// Both sides must be directories for a walk to mean anything.
	if err := mustBeDir(snapshotDir, "snapshot"); err != nil {
		return res, err
	}
	if err := mustBeDir(liveDir, "live"); err != nil {
		return res, err
	}

	w := &walker{opt: opt, res: &res, progress: progress}
	if err := w.walk(ctx, snapshotDir, liveDir, "", 1); err != nil {
		return res, err
	}
	sort.Slice(res.Changes, func(i, j int) bool { return res.Changes[i].RelPath < res.Changes[j].RelPath })
	return res, nil
}

type walker struct {
	opt      Options
	res      *Result
	progress func(Progress)
}

func (w *walker) walk(ctx context.Context, snapDir, liveDir, rel string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.opt.MaxDepth > 0 && depth > w.opt.MaxDepth {
		return nil
	}

	snapEntries, snapErr := readDirMap(snapDir)
	if snapErr != nil {
		w.res.Errors = append(w.res.Errors, snapErr.Error())
	}
	liveEntries, liveErr := readDirMap(liveDir)
	if liveErr != nil {
		w.res.Errors = append(w.res.Errors, liveErr.Error())
	}

	for _, name := range mergedNames(snapEntries, liveEntries) {
		if err := ctx.Err(); err != nil {
			return err
		}
		childRel := filepath.Join(rel, name)
		snapPath := filepath.Join(snapDir, name)
		livePath := filepath.Join(liveDir, name)

		w.res.Scanned++
		w.report(childRel)

		snapInfo, inSnap := snapEntries[name]
		liveInfo, onDisk := liveEntries[name]

		switch {
		case inSnap && !onDisk:
			w.add(Change{
				RelPath: childRel, AbsLive: livePath, AbsSnapshot: snapPath,
				Status: OnlyInSnapshot, IsDir: snapInfo.IsDir(),
				SnapSize: snapInfo.Size(), SnapModTime: snapInfo.ModTime(),
			})

		case !inSnap && onDisk:
			w.add(Change{
				RelPath: childRel, AbsLive: livePath, AbsSnapshot: snapPath,
				Status: OnlyOnDisk, IsDir: liveInfo.IsDir(),
				LiveSize: liveInfo.Size(), LiveModTime: liveInfo.ModTime(),
			})

		case kindOf(snapInfo) != kindOf(liveInfo):
			w.add(Change{
				RelPath: childRel, AbsLive: livePath, AbsSnapshot: snapPath,
				Status: TypeChanged, IsDir: snapInfo.IsDir(),
				SnapSize: snapInfo.Size(), LiveSize: liveInfo.Size(),
				SnapModTime: snapInfo.ModTime(), LiveModTime: liveInfo.ModTime(),
			})

		case snapInfo.IsDir():
			if err := w.walk(ctx, snapPath, livePath, childRel, depth+1); err != nil {
				return err
			}

		default:
			status, err := w.compareFiles(snapPath, livePath, snapInfo, liveInfo)
			if err != nil {
				w.res.Errors = append(w.res.Errors, err.Error())
				continue
			}
			if status == Same && !w.opt.IncludeSame {
				continue
			}
			w.add(Change{
				RelPath: childRel, AbsLive: livePath, AbsSnapshot: snapPath,
				Status:   status,
				SnapSize: snapInfo.Size(), LiveSize: liveInfo.Size(),
				SnapModTime: snapInfo.ModTime(), LiveModTime: liveInfo.ModTime(),
			})
		}
	}
	return nil
}

func (w *walker) add(c Change) {
	w.res.Changes = append(w.res.Changes, c)
}

func (w *walker) report(current string) {
	if w.progress == nil {
		return
	}
	w.progress(Progress{Scanned: w.res.Scanned, Current: current, Changes: len(w.res.Changes)})
}

// compareFiles decides whether two non-directory entries differ. Symbolic links
// compare by target, never by following them.
func (w *walker) compareFiles(snapPath, livePath string, snapInfo, liveInfo os.FileInfo) (Status, error) {
	if snapInfo.Mode()&os.ModeSymlink != 0 {
		a, err := os.Readlink(snapPath)
		if err != nil {
			return Same, err
		}
		b, err := os.Readlink(livePath)
		if err != nil {
			return Same, err
		}
		if a != b {
			return Modified, nil
		}
		return Same, nil
	}

	if snapInfo.Size() != liveInfo.Size() {
		return Modified, nil
	}
	if !w.opt.Deep {
		if snapInfo.ModTime().Equal(liveInfo.ModTime()) {
			return Same, nil
		}
		return Modified, nil
	}

	same, err := sameContents(snapPath, livePath)
	if err != nil {
		return Same, err
	}
	if same {
		return Same, nil
	}
	return Modified, nil
}

func sameContents(a, b string) (bool, error) {
	ha, err := hashFile(a)
	if err != nil {
		return false, err
	}
	hb, err := hashFile(b)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("diffs: hashing %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("diffs: hashing %s: %w", path, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// kindOf collapses a mode into the three categories worth distinguishing:
// directory, symlink, and regular file.
func kindOf(info os.FileInfo) string {
	switch {
	case info.IsDir():
		return "dir"
	case info.Mode()&os.ModeSymlink != 0:
		return "symlink"
	default:
		return "file"
	}
}

func readDirMap(dir string) (map[string]os.FileInfo, error) {
	out := map[string]os.FileInfo{}
	items, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return out, fmt.Errorf("diffs: reading %s: %w", dir, err)
	}
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			continue
		}
		out[item.Name()] = info
	}
	return out, nil
}

func mergedNames(a, b map[string]os.FileInfo) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for name := range a {
		seen[name] = struct{}{}
	}
	for name := range b {
		seen[name] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

func mustBeDir(path, side string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("diffs: %s side: %w", side, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("diffs: %s side %s is not a directory", side, path)
	}
	return nil
}
