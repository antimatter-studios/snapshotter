package diffs

import (
	"os"
	"path/filepath"
)

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
	out := Explain(snapshotDir, liveDir, opt)
	return out.Differs, out.Answered
}

// Explain is DiffersWithin with the reason it could not answer, when it could
// not.
//
// It exists because "could not check" told nobody anything, including me: three
// separate explanations for one report of it were all wrong, and each was a
// guess because the application had the answer and was throwing it away.
func Explain(snapshotDir, liveDir string, opt Options) Outcome {
	st := &examination{remaining: examineBudget}
	out := differsWithin(snapshotDir, liveDir, opt, st)
	if !out.Answered && out.Why == "" {
		out.Why = "the folder is too large to walk within the time allowed"
	}
	out.Ignored = st.ignored
	out.Abandoned = st.abandoned
	// Only a walk that ran to the end and found nothing may hand these over. A
	// walk that stopped — at a difference, at the budget, or because nobody wanted
	// the answer any more — read part of a tree, and part of a tree says nothing
	// about the rest of it.
	if out.Answered && !out.Differs && !out.Abandoned {
		out.Clean = st.clean
	}
	return out
}

// Outcome is everything one folder's verdict has to say.
//
// A struct rather than more return values because the third thing it has to say
// arrived late and would have been the fourth bool nobody reads: what was
// deliberately not looked at. That is not the same fact as "could not answer" —
// one is a refusal by the filesystem and the other is an instruction from the
// person using the machine — and collapsing them would make the settings look
// like a fault.
type Outcome struct {
	// Differs is the answer, and Answered says whether it is one.
	Differs, Answered bool
	// Why is the reason there is no answer, empty when there is.
	Why string
	// ChangedPath is the live path of the first difference found, empty unless Differs.
	//
	// The walk stops at the first difference, so this is the single fact the
	// verdict rests on — and re-checking that one path is enough to reach the same
	// verdict again without walking anything. One difference anywhere means the
	// tree differs, so it answers not just the folder it sits in but every folder
	// between that one and wherever the question was asked.
	//
	// It can only ever prove "changed". If that path turns out to match again,
	// nothing follows about the rest of the tree and the walk has to happen.
	ChangedPath string
	// Abandoned says the walk stopped because the caller said it no longer wanted
	// the answer. Differs and Answered mean nothing when it is set, and the answer
	// must not be cached: it is not a fact about the folder.
	Abandoned bool
	// Clean are the directories this walk read in full and found nothing wrong in.
	//
	// The mirror of ChangedPath, and the other half of what makes a second look
	// cheap. One difference proves every folder ABOVE it differs; a complete walk
	// that found none proves every folder BELOW it is identical — and the walk has
	// already read them all, so the second fact costs nothing to collect and is
	// thrown away otherwise.
	//
	// Only ever filled when the walk completed: Answered is true, Differs is
	// false, nothing was abandoned and no budget ran out. A walk that stopped
	// early read part of a tree, and part of a tree proves nothing about the rest.
	// Directories holding anything the ignore list skipped are left out too, since
	// what was not read cannot be vouched for.
	Clean []string
	// Ignored counts the paths the ignore list kept this walk out of.
	//
	// Carried even when Answered is true, because "nothing changed" and "nothing
	// changed in the parts you asked me to read" are different sentences, and only
	// the caller holding this number can tell which one it is entitled to say.
	Ignored int
}

// examination is the state one verdict's recursion shares.
type examination struct {
	// remaining is the entry budget, counted down across the whole walk rather
	// than per directory.
	remaining int
	// ignored counts what the ignore list skipped.
	ignored int
	// clean collects directories read in full with nothing wrong in them. Held on
	// the walk rather than returned upward, because a subtree that turns out to
	// sit under a difference is not clean whatever it found in itself — so this is
	// only ever read by the caller that sees the whole walk finish cleanly.
	clean []string
	// abandoned is set once the caller has said it no longer wants the answer, so
	// the recursion unwinds without asking again.
	abandoned bool
	// sinceAsked counts entries since Options.Abandoned was last consulted.
	sinceAsked int
}

// askEveryEntries is how often a walk checks whether its answer is still wanted.
//
// Not every entry: the question crosses into the caller and an entry costs about
// 2.4 microseconds, so asking on each one would be a large fraction of the work.
// Not rarely either — the point is to stop within a moment of somebody clicking
// away, and 512 entries is about a millisecond.
const askEveryEntries = 512

// wanted reports whether the answer is still worth computing.
func (st *examination) wanted(opt Options) bool {
	if st.abandoned {
		return false
	}
	if opt.Abandoned == nil {
		return true
	}
	if st.sinceAsked++; st.sinceAsked < askEveryEntries {
		return true
	}
	st.sinceAsked = 0
	if opt.Abandoned() {
		st.abandoned = true
		return false
	}
	return true
}

// differsWithinBudget is DiffersWithin with the backstop as a parameter, so a
// test can reach it without creating half a million files to do so.
func differsWithinBudget(snapshotDir, liveDir string, opt Options, budget int) (differs, answered bool) {
	out := differsWithin(snapshotDir, liveDir, opt, &examination{remaining: budget})
	return out.Differs, out.Answered
}

func differsWithin(snapshotDir, liveDir string, opt Options, st *examination) Outcome {
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
			return Outcome{Why: "cannot read " + snapshotDir + ": " + snapErr.Error()}
		}
		return Outcome{Why: "cannot read " + liveDir + ": " + liveErr.Error()}
	}

	// Set when a subtree could not be read. It only matters if nothing else
	// differs: a difference found elsewhere answers the question outright.
	var skipped bool
	var why string

	// Three passes, cheapest first, and recursion last.
	//
	// The order used to be whatever mergedNames returned, which is alphabetical —
	// so a folder called "archive" was recursed into, and everything beneath it
	// read, before a changed file called "notes.md" sitting in the same directory
	// was ever looked at. The answer was the same and the cost was not.
	//
	// The passes are separated by what they cost rather than by what they examine:
	//
	//  1. Names present on one side only, and names that changed type. These cost
	//     NOTHING — both directory reads already happened, so this is a comparison
	//     of two maps. A folder called "zebra" that is gone is found here, not
	//     after every tree that sorts before it has been read.
	//  2. Files. Size and modification time arrive with the directory read, so
	//     this is usually free too — but with Deep it hashes contents, and then it
	//     is the most expensive thing in this function apart from recursing.
	//  3. Directories, last, because each one is another tree.
	var files, dirs []string
	for _, name := range mergedNames(snapEntries, liveEntries) {
		if st.remaining <= 0 {
			return Outcome{}
		}
		// Asked here as well as in the loops below. A single directory can hold
		// half a million entries, and a walk that only checked between subtrees
		// would keep reading one of those long after nobody wanted the answer.
		if !st.wanted(opt) {
			return Outcome{Why: "abandoned: the answer is no longer wanted"}
		}
		// Asked before anything is read, which is the entire point: an ignored
		// subtree costs one string comparison instead of a walk of everything
		// beneath it. Matched on the live path so a pattern with a separator means
		// the place on the disk, not the place under a mountpoint whose name is a
		// snapshot date.
		if !opt.Ignore.Empty() && opt.Ignore.Match(filepath.Join(liveDir, name)) {
			st.ignored++
			continue
		}
		st.remaining--

		snapInfo, inSnapshot := snapEntries[name]
		liveInfo, onDisk := liveEntries[name]

		// Something added or removed is a difference, and the cheapest one there
		// is: nothing needs opening, and this is as true of a directory as of a
		// file.
		if inSnapshot != onDisk {
			return Outcome{Differs: true, Answered: true, ChangedPath: filepath.Join(liveDir, name)}
		}
		if snapInfo.IsDir() != liveInfo.IsDir() {
			return Outcome{Differs: true, Answered: true, ChangedPath: filepath.Join(liveDir, name)}
		}
		if snapInfo.IsDir() {
			dirs = append(dirs, name)
			continue
		}
		files = append(files, name)
	}

	for _, name := range files {
		if !st.wanted(opt) {
			return Outcome{Why: "abandoned: the answer is no longer wanted"}
		}
		status, err := (&walker{opt: opt, res: &Result{}}).compareFiles(
			filepath.Join(snapshotDir, name), filepath.Join(liveDir, name),
			snapEntries[name], liveEntries[name])
		if err != nil {
			// A file that cannot be compared is not evidence either way.
			return Outcome{Why: "cannot compare " + name + ": " + err.Error()}
		}
		if status != Same {
			return Outcome{Differs: true, Answered: true, ChangedPath: filepath.Join(liveDir, name)}
		}
	}

	// Only now, with everything this directory could settle on its own settled.
	for _, name := range dirs {
		if !st.wanted(opt) {
			return Outcome{Why: "abandoned: the answer is no longer wanted"}
		}
		childLive := filepath.Join(liveDir, name)
		before := st.ignored
		child := differsWithin(filepath.Join(snapshotDir, name), childLive, opt, st)
		if child.Answered && !child.Differs && !child.Abandoned && st.ignored == before {
			// Read in full, nothing wrong in it, and nothing under it skipped. The
			// walk paid for this already; recording it is what stops the folder
			// being walked again the moment somebody opens it.
			st.clean = append(st.clean, childLive)
		}
		if child.Differs {
			// A difference anywhere is the answer, whatever else was skipped. The
			// child's changed path is carried up unchanged: it is a path on the live
			// disk, and it proves this folder differs exactly as well as it proved
			// the one below.
			return Outcome{Differs: true, Answered: true, ChangedPath: child.ChangedPath}
		}
		if !child.Answered {
			if why == "" {
				why = child.Why
			}
			// Carry on rather than giving up: another subtree may hold a difference,
			// and finding one answers the question outright. Only if nothing is found
			// does the skip matter, and that is remembered here.
			skipped = true
		}
	}

	// Nothing differed in what could be read. If something could not be, that is
	// the honest answer rather than "unchanged".
	return Outcome{Answered: !skipped, Why: why}
}

// StillDiffers re-checks one changed path: the single path a previous walk
// stopped at.
//
// This is the whole value of recording it. Proving a folder unchanged means
// reading everything under it, but proving it CHANGED needs exactly one
// difference — so if the difference a previous walk found is still there, the
// verdict still holds, and it holds for every folder between that path and
// wherever the question is being asked. One stat in place of a tree.
//
// It answers in one direction only. differs=true is proof. differs=false proves
// nothing at all about the rest of the tree: the file may have been put back
// while a thousand others changed, so the caller has to fall back to the walk and
// record whatever differs now.
//
// ok=false means the question could not be answered — usually a path neither side
// can be read at — and is not evidence either way.
func StillDiffers(snapshotPath, livePath string, opt Options) (differs, ok bool) {
	snapInfo, snapErr := os.Lstat(snapshotPath)
	liveInfo, liveErr := os.Lstat(livePath)

	inSnapshot := snapErr == nil
	onDisk := liveErr == nil
	// Neither side has it. That is not a difference, and it is not a failure
	// either: a file created and deleted since the snapshot leaves both sides
	// without it.
	if !inSnapshot && !onDisk {
		return false, true
	}
	// Anything other than "not there" is a reason to distrust the answer rather
	// than to read a difference into it. A path under a folder macOS will not let
	// this process open would otherwise report "changed" for ever.
	if snapErr != nil && !os.IsNotExist(snapErr) {
		return false, false
	}
	if liveErr != nil && !os.IsNotExist(liveErr) {
		return false, false
	}
	// Present on one side only: deleted since the snapshot, or created after it.
	// This is the cheapest kind of difference there is and needs nothing read.
	if inSnapshot != onDisk {
		return true, true
	}
	if snapInfo.IsDir() != liveInfo.IsDir() {
		return true, true
	}
	// Both are directories, which happens when one recorded as deleted has since
	// been made again under the same name. A directory carries no data of its own
	// beyond its name, so the question becomes what is inside it — and one read of
	// each side answers that far more cheaply than the walk this is avoiding.
	if snapInfo.IsDir() {
		return shallowDiffers(snapshotPath, livePath, opt)
	}

	status, err := (&walker{opt: opt, res: &Result{}}).compareFiles(snapshotPath, livePath, snapInfo, liveInfo)
	if err != nil {
		return false, false
	}
	return status != Same, true
}

// shallowDiffers compares one directory's own entries, without recursing.
//
// It exists for the case where a recorded difference is a directory that has been
// deleted and made again: both sides now hold something of that name and type, so
// nothing above can settle it, and the alternative was to give up and walk the
// whole tree.
//
// Two directory reads instead. Everything they can settle, they settle: a name on
// one side only, a name that changed type, a file whose size or stamp moved. Only
// if every one of those matches is there nothing left to say — and then it says
// so, because the subdirectories have not been looked at and an unexamined
// subtree is not an unchanged one.
func shallowDiffers(snapshotDir, liveDir string, opt Options) (differs, ok bool) {
	snapEntries, snapErr := readDirMap(snapshotDir)
	liveEntries, liveErr := readDirMap(liveDir)
	if snapErr != nil || liveErr != nil {
		return false, false
	}

	for _, name := range mergedNames(snapEntries, liveEntries) {
		if !opt.Ignore.Empty() && opt.Ignore.Match(filepath.Join(liveDir, name)) {
			continue
		}
		snapInfo, inSnapshot := snapEntries[name]
		liveInfo, onDisk := liveEntries[name]
		if inSnapshot != onDisk {
			return true, true
		}
		if snapInfo.IsDir() != liveInfo.IsDir() {
			return true, true
		}
		if snapInfo.IsDir() {
			// Not descended into. This is the whole point of being shallow.
			continue
		}
		status, err := (&walker{opt: opt, res: &Result{}}).compareFiles(
			filepath.Join(snapshotDir, name), filepath.Join(liveDir, name), snapInfo, liveInfo)
		if err != nil {
			return false, false
		}
		if status != Same {
			return true, true
		}
	}
	// Everything at this level matches, which is not the same as nothing having
	// changed: the subdirectories were deliberately not read.
	return false, false
}
