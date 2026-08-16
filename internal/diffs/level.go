package diffs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Level merges one directory from each side without descending, reporting a row
// for every name that appears on either.
//
// This backs the file browser, where the useful thing is to see a folder's
// contents with each entry already marked as unchanged, changed, deleted or
// new. It is two directory reads, so it stays instant on a folder of any size,
// unlike Compare which walks the whole tree.
//
// A directory present on both sides is walked only until the first difference
// is found, because the browser shows one word per row and "changed" is that
// word whether one file differs or ten thousand. Stopping at the first is what
// makes this affordable; proving the opposite is the expensive direction, and it
// is bounded, with the row reported as NotExamined if the bound is reached.
func Level(snapshotDir, liveDir string, opt Options) ([]Change, error) {
	// A missing directory on one side is meaningful rather than fatal: it is
	// how a folder created or deleted since the snapshot presents itself. With
	// neither side present there is nothing to report on.
	if !DirExists(snapshotDir) && !DirExists(liveDir) {
		return nil, fmt.Errorf("diffs: neither %s nor %s is a readable directory", snapshotDir, liveDir)
	}

	snapEntries, _ := readDirMap(snapshotDir)
	liveEntries, _ := readDirMap(liveDir)

	var out []Change
	for _, name := range mergedNames(snapEntries, liveEntries) {
		snapPath := filepath.Join(snapshotDir, name)
		livePath := filepath.Join(liveDir, name)
		snapInfo, inSnap := snapEntries[name]
		liveInfo, onDisk := liveEntries[name]

		c := Change{RelPath: name, AbsSnapshot: snapPath, AbsLive: livePath}
		switch {
		case inSnap && !onDisk:
			c.Status, c.IsDir = OnlyInSnapshot, snapInfo.IsDir()
			c.SnapSize, c.SnapModTime = snapInfo.Size(), snapInfo.ModTime()

		case !inSnap && onDisk:
			c.Status, c.IsDir = OnlyOnDisk, liveInfo.IsDir()
			c.LiveSize, c.LiveModTime = liveInfo.Size(), liveInfo.ModTime()

		case kindOf(snapInfo) != kindOf(liveInfo):
			c.Status, c.IsDir = TypeChanged, snapInfo.IsDir()
			c.SnapSize, c.LiveSize = snapInfo.Size(), liveInfo.Size()
			c.SnapModTime, c.LiveModTime = snapInfo.ModTime(), liveInfo.ModTime()

		case snapInfo.IsDir():
			c.IsDir = true
			c.SnapModTime, c.LiveModTime = snapInfo.ModTime(), liveInfo.ModTime()
			// This used to be an unconditional Same, which told anyone browsing
			// that a folder was untouched without looking inside it once.
			if opt.DeferDirectories {
				c.Status = NotExamined
				out = append(out, c)
				continue
			}
			differs, answered := DiffersWithin(snapPath, livePath, opt)
			switch {
			case !answered:
				c.Status = NotExamined
			case differs:
				c.Status = Modified
			default:
				c.Status = Same
			}

		default:
			status, err := (&walker{opt: opt, res: &Result{}}).compareFiles(snapPath, livePath, snapInfo, liveInfo)
			if err != nil {
				status = Modified
			}
			c.Status = status
			c.SnapSize, c.LiveSize = snapInfo.Size(), liveInfo.Size()
			c.SnapModTime, c.LiveModTime = snapInfo.ModTime(), liveInfo.ModTime()
		}

		if c.Status == Same && !opt.IncludeSame {
			continue
		}
		out = append(out, c)
	}
	sortLevel(out)
	return out, nil
}

// sortLevel orders a listing the way a file window does: folders first, then
// by name.
func sortLevel(rows []Change) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.RelPath) < strings.ToLower(b.RelPath)
	})
}

// DirExists reports whether a directory is readable, which the browser uses to
// tell "the folder was not there" from "the folder was empty".
func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
