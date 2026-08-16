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
// Large enough that it almost never decides the answer.
//
// It was 50,000, and that was the wrong instrument: Library and any real source
// tree pass it immediately, so every folder worth asking about came back "not
// examined" — a refusal to answer dressed up as a result. Watching it on a real
// machine, every large folder in a home directory reported that and nothing else.
//
// The cost of a walk was never the problem. Measured on a tree of 192,635 files
// it takes 456ms, about 2.4 microseconds per entry, because size and
// modification time both arrive with the directory read. What made the machine
// unusable was running every folder's walk at once, and that is fixed where it
// belongs — the caller resolves a few at a time.
//
// So this is now a backstop against something pathological rather than a limit
// on ordinary use: half a million entries is a second or so, and a folder that
// large is one where "not examined" is a fair thing to say.
const examineBudget = 500000

// DiffersWithin reports whether anything under two directories differs.
//
// The second return says the question was actually answered. False means the
// budget ran out first, and the caller should say so rather than claim the tree
// is unchanged — the whole reason this exists is that Level used to make exactly
// that claim without looking.
func DiffersWithin(snapshotDir, liveDir string, opt Options) (differs, answered bool) {
	differs, answered, _ = Explain(snapshotDir, liveDir, opt)
	return differs, answered
}

// Explain is DiffersWithin with the reason it could not answer, when it could
// not.
//
// It exists because "could not check" told nobody anything, including me: three
// separate explanations for one report of it were all wrong, and each was a
// guess because the application had the answer and was throwing it away.
func Explain(snapshotDir, liveDir string, opt Options) (differs, answered bool, why string) {
	remaining := examineBudget
	differs, answered, why = differsWithin(snapshotDir, liveDir, opt, &remaining)
	if !answered && why == "" {
		why = "the folder is too large to walk within the time allowed"
	}
	return differs, answered, why
}

// differsWithinBudget is DiffersWithin with the backstop as a parameter, so a
// test can reach it without creating half a million files to do so.
func differsWithinBudget(snapshotDir, liveDir string, opt Options, budget int) (differs, answered bool) {
	remaining := budget
	differs, answered, _ = differsWithin(snapshotDir, liveDir, opt, &remaining)
	return differs, answered
}

func differsWithin(snapshotDir, liveDir string, opt Options, remaining *int) (differs, answered bool, why string) {
	snapEntries, snapErr := readDirMap(snapshotDir)
	liveEntries, liveErr := readDirMap(liveDir)
	if snapErr != nil || liveErr != nil {
		// One side unreadable — most often macOS privacy protection over a
		// subfolder. Not knowing is not the same as nothing having changed, so
		// this subtree is skipped rather than answered for.
		//
		// It used to abort the entire walk, which meant a single protected folder
		// anywhere beneath a directory made the whole thing unanswerable — and the
		// interface then reported that as "too large to check", which was not what
		// had happened at all.
		// Which side, and what the system said. On macOS this is usually
		// "operation not permitted", which is privacy protection rather than a
		// permission bit — and knowing that is the difference between fixing it
		// and guessing at it.
		if snapErr != nil {
			return false, false, "cannot read " + snapshotDir + ": " + snapErr.Error()
		}
		return false, false, "cannot read " + liveDir + ": " + liveErr.Error()
	}

	// Set when a subtree could not be read. It only matters if nothing else
	// differs: a difference found elsewhere answers the question outright.
	var skipped bool

	for _, name := range mergedNames(snapEntries, liveEntries) {
		if *remaining <= 0 {
			return false, false, ""
		}
		*remaining--

		snapInfo, inSnapshot := snapEntries[name]
		liveInfo, onDisk := liveEntries[name]

		// Something added or removed is a difference, and the cheapest one to
		// find: no stat of the contents is needed at all.
		if inSnapshot != onDisk {
			return true, true, ""
		}

		switch {
		case snapInfo.IsDir() != liveInfo.IsDir():
			return true, true, ""

		case snapInfo.IsDir():
			snapPath := filepath.Join(snapshotDir, name)
			livePath := filepath.Join(liveDir, name)
			childDiffers, childAnswered, childWhy := differsWithin(snapPath, livePath, opt, remaining)
			if childDiffers {
				// A difference anywhere is the answer, whatever else was skipped.
				return true, true, ""
			}
			if !childAnswered {
				if why == "" {
					why = childWhy
				}
				// Carry on rather than giving up: another subtree may hold a
				// difference, and finding one answers the question outright. Only
				// if nothing is found does the skip matter, and that is remembered
				// below.
				skipped = true
			}

		default:
			status, err := (&walker{opt: opt, res: &Result{}}).compareFiles(
				filepath.Join(snapshotDir, name), filepath.Join(liveDir, name), snapInfo, liveInfo)
			if err != nil {
				// A file that cannot be compared is not evidence either way.
				return false, false, "cannot compare " + name + ": " + err.Error()
			}
			if status != Same {
				return true, true, ""
			}
		}
	}

	// Nothing differed in what could be read. If something could not be, that is
	// the honest answer rather than "unchanged".
	return false, !skipped, why
}
