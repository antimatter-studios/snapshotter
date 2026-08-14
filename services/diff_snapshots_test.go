package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"snapshotter/internal/diffs"
	"snapshotter/internal/mountmgr"
)

// The two snapshots the direction tests use. Their stamps are what decide which
// one is older, so they sit a clear day apart and are recognisable in a failure.
const (
	tuesday   = "com.apple.TimeMachine.2026-08-11-090000.local"
	wednesday = "com.apple.TimeMachine.2026-08-12-090000.local"
)

// fixtureModTime is stamped on every file the fixtures create.
//
// A real snapshot shares blocks with the live volume, so the same file has an
// identical timestamp in every snapshot that holds it. Two seeds built seconds
// apart would instead make every unchanged file look modified, for a reason that
// never happens in production — and would hide the one case deep comparison
// exists for.
var fixtureModTime = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

// scopeDir is the folder the fixture trees are built inside, and the folder the
// comparisons are scoped to.
//
// Nothing sits at the top level of a seed on purpose. mountmgr.Fake manufactures
// its own differences there — it appends a file, rewrites the first top-level
// regular file and removes the second — which is what makes the fake useful
// against the live disk and what would quietly rearrange a fixture built for a
// comparison between two snapshots. One level down, the trees below are exactly
// what the mounts hold.
const scopeDir = "Work"

// twoSnapshots stands up one fake mount per snapshot, with genuinely different
// contents, and returns the service plus the folder to compare.
//
// Each side gets its own seed, because the differences under test are between the
// two snapshots: a single shared seed could only ever produce the differences Fake
// injects for itself.
//
// Both seeds live under the real home directory. t.TempDir is under /private/var,
// which vfs translates by another route, so a seed there is not found inside the
// mount at the path the service looks for.
func twoSnapshots(t *testing.T, older, newer map[string]string) (*DiffService, string) {
	t.Helper()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	seedRoot, err := os.MkdirTemp(home, ".snapshotter-difftwo-")
	if err != nil {
		t.Skipf("cannot create a seed under home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(seedRoot) })

	mounts := &pairFake{byName: map[string]*mountmgr.Fake{}}
	for _, side := range []struct {
		name string
		tree map[string]string
	}{{tuesday, older}, {wednesday, newer}} {
		// Each tree is staged and mounted before the next is staged. Fake clones
		// its seed when it mounts, not when it is built, so staging both first
		// would clone whichever tree was written last into both snapshots — and a
		// pair of identical mounts reports no differences at all, which reads as a
		// broken comparison rather than a broken fixture.
		stage(t, seedRoot, side.tree)

		fake := mountmgr.NewFake(t.TempDir(), seedRoot)
		if err := fake.Mount(context.Background(), []string{side.name}); err != nil {
			t.Fatalf("faking a mount for %s: %v", side.name, err)
		}
		mounts.byName[side.name] = fake
		// A fake mount is sealed read-only to imitate a real one, and t.TempDir
		// cannot remove a read-only tree. Unmount is what takes it apart.
		snapshot, f := side.name, fake
		t.Cleanup(func() { _ = f.Unmount(context.Background(), []string{snapshot}) })
	}

	svc := NewDiffService(Deps{Mounts: mounts, Volume: "/System/Volumes/Data", Faking: true})
	return svc, filepath.Join(seedRoot, scopeDir)
}

// stage empties the shared seed root and writes one side's tree into it.
//
// The root is shared rather than one per side because Fake clones a seed to the
// seed's own live path: both snapshots have to hold the same live path or the
// service has nothing to line up. Emptying it between sides also leaves the live
// disk innocent of the fixture, so a comparison that reached the live disk by
// mistake fails loudly instead of passing on leftovers.
func stage(t *testing.T, seedRoot string, tree map[string]string) {
	t.Helper()

	entries, err := os.ReadDir(seedRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(seedRoot, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	for rel, body := range tree {
		p := filepath.Join(seedRoot, scopeDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		// Fake clones with cp -p, so this levelled timestamp is what the mount
		// ends up holding.
		if err := os.Chtimes(p, fixtureModTime, fixtureModTime); err != nil {
			t.Fatal(err)
		}
	}
}

// pairFake serves a different Fake per snapshot name, which is what lets two
// mounts with unrelated contents exist at once. Every method is a lookup and a
// delegation; the behaviour being exercised is entirely mountmgr.Fake's.
type pairFake struct{ byName map[string]*mountmgr.Fake }

func (p *pairFake) forName(name string) (*mountmgr.Fake, error) {
	f, ok := p.byName[name]
	if !ok {
		return nil, errors.New("no fake mount was set up for " + name)
	}
	return f, nil
}

func (p *pairFake) MountPoint(name string) (string, error) {
	f, err := p.forName(name)
	if err != nil {
		return "", err
	}
	return f.MountPoint(name)
}

// IsMounted treats a snapshot nobody set up as simply not mounted, which is the
// state the refusal tests need rather than an error from the mounter.
func (p *pairFake) IsMounted(name string) (bool, error) {
	f, err := p.forName(name)
	if err != nil {
		return false, nil
	}
	return f.IsMounted(name)
}

func (p *pairFake) Mount(ctx context.Context, names []string) error {
	for _, name := range names {
		f, err := p.forName(name)
		if err != nil {
			return err
		}
		if err := f.Mount(ctx, []string{name}); err != nil {
			return err
		}
	}
	return nil
}

func (p *pairFake) Unmount(ctx context.Context, names []string) error {
	for _, name := range names {
		f, err := p.forName(name)
		if err != nil {
			return err
		}
		if err := f.Unmount(ctx, []string{name}); err != nil {
			return err
		}
	}
	return nil
}

func (p *pairFake) MountedNames(names []string) []string {
	var out []string
	for _, name := range names {
		if ok, err := p.IsMounted(name); err == nil && ok {
			out = append(out, name)
		}
	}
	return out
}

// statusOf reduces a comparison to relative path against status, so a test can
// state exactly what the answer should be instead of hunting through rows.
func statusOf(res SnapshotComparison) map[string]diffs.Status {
	out := map[string]diffs.Status{}
	for _, c := range res.Result.Changes {
		out[c.RelPath] = c.Status
	}
	return out
}

// The three differences worth naming, and the trees that produce them: on Tuesday
// there was keep.txt, gone.txt and changed.txt; by Wednesday gone.txt had been
// deleted, changed.txt rewritten, and arrived.txt created.
var (
	tuesdayTree = map[string]string{
		"keep.txt":    "unchanged all week\n",
		"gone.txt":    "deleted on the Tuesday night\n",
		"changed.txt": "the Tuesday version\n",
	}
	wednesdayTree = map[string]string{
		"keep.txt":    "unchanged all week\n",
		"changed.txt": "the Wednesday version, which is longer\n",
		"arrived.txt": "created on the Wednesday morning\n",
	}
)

// TestCompareSnapshotsReportsEachDifferenceInTheRightDirection is the property the
// feature stands on.
//
// A change between two snapshots has no inherent before, so a direction mistake
// does not fail — it inverts every row, and a file the user recovered is reported
// as one they lost. Asserting that some rows came back would pass just as happily
// with the sides reversed, so every status is pinned to the path it belongs to.
func TestCompareSnapshotsReportsEachDifferenceInTheRightDirection(t *testing.T) {
	svc, scope := twoSnapshots(t, tuesdayTree, wednesdayTree)

	res, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: tuesday, Newer: wednesday, LivePath: scope,
	})
	if err != nil {
		t.Fatalf("CompareSnapshots: %v", err)
	}

	got := statusOf(res)
	want := map[string]diffs.Status{
		// Present on Tuesday, gone by Wednesday. Inverted, this row would claim
		// the file had just been created.
		"gone.txt": diffs.OnlyInSnapshot,
		// Absent on Tuesday, present by Wednesday. Inverted, this row would claim
		// the file was deleted, which is the worst lie this application can tell.
		"arrived.txt": diffs.OnlyOnDisk,
		"changed.txt": diffs.Modified,
	}
	for rel, status := range want {
		if got[rel] != status {
			t.Errorf("%s: status = %q, want %q (all rows: %v)", rel, got[rel], status, got)
		}
	}
	if _, reported := got["keep.txt"]; reported {
		t.Errorf("keep.txt is identical in both snapshots and is not a difference: %v", got)
	}

	// Neither side's absolute paths are live paths, so the folder that was
	// compared has to come back for a restore to be rebuilt against it.
	if res.LivePath != scope {
		t.Errorf("livePath = %q, want %q", res.LivePath, scope)
	}
}

// The sizes and paths have to follow the same direction as the status, or the
// interface shows the right verdict beside the wrong numbers.
func TestCompareSnapshotsPutsEachSidesFiguresOnItsOwnSide(t *testing.T) {
	svc, scope := twoSnapshots(t, tuesdayTree, wednesdayTree)

	res, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: tuesday, Newer: wednesday, LivePath: scope,
	})
	if err != nil {
		t.Fatalf("CompareSnapshots: %v", err)
	}

	var changed *diffs.Change
	for i := range res.Result.Changes {
		if res.Result.Changes[i].RelPath == "changed.txt" {
			changed = &res.Result.Changes[i]
		}
	}
	if changed == nil {
		t.Fatalf("changed.txt is not in the result: %+v", res.Result.Changes)
	}

	// SnapSize describes the older snapshot and LiveSize the newer one. The
	// Wednesday version is deliberately the longer of the two, so an inversion
	// shows up here as a file that shrank.
	if int(changed.SnapSize) != len(tuesdayTree["changed.txt"]) {
		t.Errorf("snapSize = %d, want the Tuesday length %d", changed.SnapSize, len(tuesdayTree["changed.txt"]))
	}
	if int(changed.LiveSize) != len(wednesdayTree["changed.txt"]) {
		t.Errorf("liveSize = %d, want the Wednesday length %d", changed.LiveSize, len(wednesdayTree["changed.txt"]))
	}
	// Each side's absolute path must point inside its own mount, or a restore
	// taken from the row reads the wrong snapshot.
	if !under(changed.AbsSnapshot, res.Older.MountPoint) {
		t.Errorf("absSnapshot %q is not inside the older mount %q", changed.AbsSnapshot, res.Older.MountPoint)
	}
	if !under(changed.AbsLive, res.Newer.MountPoint) {
		t.Errorf("absLive %q is not inside the newer mount %q", changed.AbsLive, res.Newer.MountPoint)
	}
}

// Argument order is an intention, not a fact. The snapshots' own timestamps decide
// the roles, so passing them backwards gives the same answer rather than the
// inverse of it — which is the mistake the ordering exists to make impossible.
func TestCompareSnapshotsIgnoresArgumentOrder(t *testing.T) {
	svc, scope := twoSnapshots(t, tuesdayTree, wednesdayTree)

	forwards, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: tuesday, Newer: wednesday, LivePath: scope,
	})
	if err != nil {
		t.Fatalf("CompareSnapshots: %v", err)
	}
	backwards, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: wednesday, Newer: tuesday, LivePath: scope,
	})
	if err != nil {
		t.Fatalf("CompareSnapshots reversed: %v", err)
	}

	if forwards.Swapped {
		t.Error("swapped is set for a request that already had the two in order")
	}
	if !backwards.Swapped {
		t.Error("swapped is not set for a request that arrived newest-first, so the interface cannot report the direction it got")
	}
	// The roles are established, not echoed: both calls have to name Tuesday as
	// the older end.
	for _, res := range []SnapshotComparison{forwards, backwards} {
		if res.Older.Name != tuesday || res.Newer.Name != wednesday {
			t.Errorf("older = %s, newer = %s; want %s then %s", res.Older.Stamp, res.Newer.Stamp, tuesday, wednesday)
		}
	}
	if a, b := statusOf(forwards), statusOf(backwards); !sameStatuses(a, b) {
		t.Errorf("the answer depends on argument order:\n forwards: %v\nbackwards: %v", a, b)
	}
}

// A snapshot that is not mounted cannot be read, and with two in play the refusal
// has to say which one — otherwise the user opens the one they thought of first
// and is refused again.
func TestCompareSnapshotsNamesTheUnmountedSide(t *testing.T) {
	svc, scope := twoSnapshots(t, tuesdayTree, wednesdayTree)
	const friday = "com.apple.TimeMachine.2026-08-14-090000.local"

	_, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: tuesday, Newer: friday, LivePath: scope,
	})
	if err == nil {
		t.Fatal("comparing against an unmounted snapshot was allowed")
	}
	// errors.Is has to keep holding, so callers that only need to know a mount is
	// missing are unaffected by the naming.
	if !errors.Is(err, errNotMounted) {
		t.Errorf("error is not a not-mounted error: %v", err)
	}
	if !strings.Contains(err.Error(), "2026-08-14-090000") {
		t.Errorf("the refusal does not name the snapshot to open: %v", err)
	}
	if strings.Contains(err.Error(), "2026-08-11-090000") {
		t.Errorf("the refusal names the snapshot that is already open, sending the user to the wrong one: %v", err)
	}
}

// Comparing a snapshot with itself reports nothing differing, which is true and
// answers a question nobody asked. Read as an answer about two snapshots it is
// actively misleading, so it is refused.
func TestCompareSnapshotsRefusesOneSnapshotTwice(t *testing.T) {
	svc, scope := twoSnapshots(t, tuesdayTree, wednesdayTree)

	_, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: tuesday, Newer: tuesday, LivePath: scope,
	})
	if err == nil {
		t.Fatal("comparing a snapshot with itself was allowed")
	}
}

// A directory present in only one of the two snapshots is one row naming it, not
// one row per file inside it — the same rule as against the live disk, and the
// reason losing a large tree stays legible.
func TestCompareSnapshotsReportsALostDirectoryAsOneRow(t *testing.T) {
	older := map[string]string{
		"keep.txt":               "unchanged\n",
		"Projects/app/main.go":   "package main\n",
		"Projects/app/go.mod":    "module app\n",
		"Projects/app/README.md": "app\n",
	}
	newer := map[string]string{"keep.txt": "unchanged\n"}

	svc, scope := twoSnapshots(t, older, newer)
	res, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: tuesday, Newer: wednesday, LivePath: scope,
	})
	if err != nil {
		t.Fatalf("CompareSnapshots: %v", err)
	}

	got := statusOf(res)
	if got["Projects"] != diffs.OnlyInSnapshot {
		t.Errorf("Projects: status = %q, want %q: %v", got["Projects"], diffs.OnlyInSnapshot, got)
	}
	for rel := range got {
		if strings.HasPrefix(rel, "Projects/") {
			t.Errorf("the lost directory was expanded into %q, which buries it in its own contents", rel)
		}
	}
}

// The comparison is scoped to a folder, and it has to be the same folder in both
// snapshots even though each mountpoint gives it a different absolute path.
func TestCompareSnapshotsScopesToTheLivePath(t *testing.T) {
	older := map[string]string{
		"Documents/notes.md": "tuesday notes\n",
		"Downloads/big.dmg":  "tuesday image\n",
	}
	newer := map[string]string{
		"Documents/notes.md": "wednesday notes, longer\n",
		"Downloads/big.dmg":  "wednesday image, longer\n",
	}

	svc, scope := twoSnapshots(t, older, newer)
	res, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: tuesday, Newer: wednesday, LivePath: filepath.Join(scope, "Documents"),
	})
	if err != nil {
		t.Fatalf("CompareSnapshots: %v", err)
	}

	got := statusOf(res)
	if got["notes.md"] != diffs.Modified {
		t.Errorf("notes.md: status = %q, want %q: %v", got["notes.md"], diffs.Modified, got)
	}
	if len(got) != 1 {
		t.Errorf("the comparison escaped the folder it was scoped to: %v", got)
	}
}

// Both comparison depths have to reach the snapshot-to-snapshot path, and the case
// that distinguishes them is a file rewritten to the same length with its
// timestamp unchanged — which is what every file in a pair of real snapshots looks
// like, since both share blocks with the live volume.
func TestCompareSnapshotsDeepSeesASameLengthRewrite(t *testing.T) {
	older := map[string]string{"vault.kdbx": "aaaaaaaaaa\n"}
	newer := map[string]string{"vault.kdbx": "bbbbbbbbbb\n"}

	svc, scope := twoSnapshots(t, older, newer)

	shallow, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: tuesday, Newer: wednesday, LivePath: scope,
	})
	if err != nil {
		t.Fatalf("shallow: %v", err)
	}
	if status, reported := statusOf(shallow)["vault.kdbx"]; reported {
		t.Errorf("a shallow comparison reported %q for a same-length rewrite with a matching timestamp, which it cannot see", status)
	}

	deep, err := svc.CompareSnapshots(context.Background(), CompareSnapshotsRequest{
		Older: tuesday, Newer: wednesday, LivePath: scope, Deep: true,
	})
	if err != nil {
		t.Fatalf("deep: %v", err)
	}
	if got := statusOf(deep)["vault.kdbx"]; got != diffs.Modified {
		t.Errorf("deep comparison missed a rewritten file: status = %q", got)
	}
}

func under(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func sameStatuses(a, b map[string]diffs.Status) bool {
	if len(a) != len(b) {
		return false
	}
	for rel, status := range a {
		if b[rel] != status {
			return false
		}
	}
	return true
}
