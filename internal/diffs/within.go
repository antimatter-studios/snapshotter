package diffs

import "path/filepath"

// Answering "does anything under here differ?" for one directory.
//
// The browser asks this per row, and the only useful answer is yes or no. How
// many things changed, and which, is what opening the folder is for — so the
// walk stops at the first difference it finds. That asymmetry is what makes this
// affordable: a changed directory is usually answered in milliseconds, and only
// an unchanged one has to be walked to the end, because proving a negative means
// looking everywhere.
//
// Measured on a real tree of 192,635 files: the first difference was found in
// 11ms, and the full walk that proves "nothing changed" took 456ms.

// examineBudget caps how many entries one directory's verdict may look at.
//
// Without it a listing containing a very large untouched tree would block the
// window while it proved a negative. Past the budget the answer is "not
// examined", which is a worse answer than "unchanged" and a much better one than
// a confident wrong answer.
// Lowered from 50,000 after watching it in practice: a listing of a home
// directory asks about several folders, and Library or a source tree hits the
// budget every time. Ten thousand entries is roughly 25ms of directory reading,
// which is enough to answer for anything of ordinary size and cheap enough that
// the ones it cannot answer for cost little to give up on.
const examineBudget = 10000

// DiffersWithin reports whether anything under two directories differs.
//
// The second return says the question was actually answered. False means the
// budget ran out first, and the caller should say so rather than claim the tree
// is unchanged — the whole reason this exists is that Level used to make exactly
// that claim without looking.
func DiffersWithin(snapshotDir, liveDir string, opt Options) (differs, answered bool) {
	remaining := examineBudget
	differs, answered = differsWithin(snapshotDir, liveDir, opt, &remaining)
	return differs, answered
}

func differsWithin(snapshotDir, liveDir string, opt Options, remaining *int) (differs, answered bool) {
	snapEntries, snapErr := readDirMap(snapshotDir)
	liveEntries, liveErr := readDirMap(liveDir)
	if snapErr != nil || liveErr != nil {
		// One side unreadable — most often macOS privacy protection over a
		// subfolder. Not knowing is not the same as nothing having changed.
		return false, false
	}

	for _, name := range mergedNames(snapEntries, liveEntries) {
		if *remaining <= 0 {
			return false, false
		}
		*remaining--

		snapInfo, inSnapshot := snapEntries[name]
		liveInfo, onDisk := liveEntries[name]

		// Something added or removed is a difference, and the cheapest one to
		// find: no stat of the contents is needed at all.
		if inSnapshot != onDisk {
			return true, true
		}

		switch {
		case snapInfo.IsDir() != liveInfo.IsDir():
			return true, true

		case snapInfo.IsDir():
			snapPath := filepath.Join(snapshotDir, name)
			livePath := filepath.Join(liveDir, name)
			if differs, answered := differsWithin(snapPath, livePath, opt, remaining); differs || !answered {
				return differs, answered
			}

		default:
			status, err := (&walker{opt: opt, res: &Result{}}).compareFiles(
				filepath.Join(snapshotDir, name), filepath.Join(liveDir, name), snapInfo, liveInfo)
			if err != nil {
				// A file that cannot be compared is not evidence either way.
				return false, false
			}
			if status != Same {
				return true, true
			}
		}
	}

	return false, true
}
