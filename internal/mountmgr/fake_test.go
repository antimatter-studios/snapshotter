package mountmgr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeSnapshot = "com.apple.TimeMachine.2026-08-14-003200.local"

// newFakeInTemp builds a Fake whose seed is a small tree, and returns both
// roots. The seed has to look like a data-volume path to vfs, so it is created
// under the real home directory rather than under t.TempDir, which lives on
// /private/var and translates differently.
func newFakeInTemp(t *testing.T) (*Fake, string) {
	t.Helper()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	seed, err := os.MkdirTemp(home, ".snapshotter-fake-seed-")
	if err != nil {
		t.Skipf("cannot create a seed under home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(seed) })

	for name, body := range map[string]string{
		"alpha.txt": "alpha as it is now\n",
		"beta.txt":  "beta as it is now\n",
	} {
		if err := os.WriteFile(filepath.Join(seed, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Fake mounts are sealed read-only to imitate a real one, and t.TempDir's own
	// cleanup cannot remove a read-only tree. Registered after TempDir so it runs
	// before it (cleanups are LIFO).
	root := t.TempDir()
	t.Cleanup(func() { _ = unseal(root) })

	return NewFake(root, seed), seed
}

func TestFakeMountPopulatesAndReportsMounted(t *testing.T) {
	f, seed := newFakeInTemp(t)

	if mounted, err := f.IsMounted(fakeSnapshot); err != nil || mounted {
		t.Fatalf("IsMounted before mounting = %v, %v; want false, nil", mounted, err)
	}
	if err := f.Mount(context.Background(), []string{fakeSnapshot}); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	mounted, err := f.IsMounted(fakeSnapshot)
	if err != nil || !mounted {
		t.Fatalf("IsMounted after mounting = %v, %v; want true, nil", mounted, err)
	}

	// The seed's contents must appear at their live path inside the mountpoint,
	// because that is the mapping vfs.ToSnapshot performs.
	mp, err := f.MountPoint(fakeSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(mp, seed)
	if _, err := os.Stat(inside); err != nil {
		t.Fatalf("the seed is not at its live path inside the mount: %v", err)
	}
}

// Browse and Compare are only worth looking at if the snapshot differs from the
// live disk, so the fake makes each of the three states happen on purpose.
func TestFakeMountInjectsEachKindOfDifference(t *testing.T) {
	f, seed := newFakeInTemp(t)
	if err := f.Mount(context.Background(), []string{fakeSnapshot}); err != nil {
		t.Fatal(err)
	}
	mp, _ := f.MountPoint(fakeSnapshot)
	inside := filepath.Join(mp, seed)

	items, err := os.ReadDir(inside)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, i := range items {
		names = append(names, i.Name())
	}
	joined := strings.Join(names, " ")

	if !strings.Contains(joined, "deleted-since-") {
		t.Errorf("no deleted-since file was injected; got %v", names)
	}
	// alpha.txt is rewritten and beta.txt removed, so exactly one of the two
	// seeded files should survive.
	survivors := 0
	for _, n := range []string{"alpha.txt", "beta.txt"} {
		if strings.Contains(joined, n) {
			survivors++
		}
	}
	if survivors != 1 {
		t.Errorf("want exactly one seeded file left in the fake, got %d of 2: %v", survivors, names)
	}

	// The surviving file must differ from the live one, or Compare shows nothing.
	if body, err := os.ReadFile(filepath.Join(inside, "alpha.txt")); err == nil {
		if strings.Contains(string(body), "as it is now") {
			t.Error("alpha.txt was not rewritten, so it will compare as unchanged")
		}
	}
}

func TestFakeUnmountRemovesOnlyMarkedDirectories(t *testing.T) {
	f, _ := newFakeInTemp(t)
	if err := f.Mount(context.Background(), []string{fakeSnapshot}); err != nil {
		t.Fatal(err)
	}
	if err := f.Unmount(context.Background(), []string{fakeSnapshot}); err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	if mounted, _ := f.IsMounted(fakeSnapshot); mounted {
		t.Error("still reports mounted after unmounting")
	}
}

// The guard that matters: Unmount is a recursive delete, and must refuse any
// directory it did not create. A mistaken Root would otherwise erase real data.
func TestFakeUnmountRefusesADirectoryWithoutTheMarker(t *testing.T) {
	f, _ := newFakeInTemp(t)

	mp, err := f.MountPoint(fakeSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mp, 0o700); err != nil {
		t.Fatal(err)
	}
	precious := filepath.Join(mp, "not-ours.txt")
	if err := os.WriteFile(precious, []byte("real data\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := f.Unmount(context.Background(), []string{fakeSnapshot}); err != nil {
		t.Fatalf("Unmount should skip an unmarked directory, not fail: %v", err)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Fatal("Unmount deleted a directory that has no fake-mount marker")
	}
}

func TestFakeRefusesSomethingThatIsNotASnapshotName(t *testing.T) {
	f, _ := newFakeInTemp(t)
	if _, err := f.MountPoint("/"); err == nil {
		t.Error("MountPoint accepted \"/\" as a snapshot name")
	}
	if err := f.Mount(context.Background(), []string{"not-a-snapshot"}); err == nil {
		t.Error("Mount accepted a name that is not a snapshot")
	}
}

func TestFakeFromEnvIsOffUnlessAskedFor(t *testing.T) {
	t.Setenv(FakeEnabled, "")
	if _, ok := FakeFromEnv(t.TempDir()); ok {
		t.Error("faking is on with the environment variable unset")
	}
	t.Setenv(FakeEnabled, "yes")
	if _, ok := FakeFromEnv(t.TempDir()); ok {
		t.Error("faking is on for a value other than 1")
	}
	t.Setenv(FakeEnabled, "1")
	t.Setenv(FakeSeed, "/Users/somebody")
	fake, ok := FakeFromEnv(t.TempDir())
	if !ok {
		t.Fatal("faking is off with the environment variable set to 1")
	}
	if fake.Seed != "/Users/somebody" {
		t.Errorf("seed = %q, want the one from the environment", fake.Seed)
	}
}

// The whole point of the classifier: a TCC refusal has to be distinguishable
// from an ordinary failure, because the two need completely different advice.
func TestClassifyMountRecognisesATCCRefusal(t *testing.T) {
	const denied = "0:239: execution error: mount_apfs: volume could not be mounted: Operation not permitted (77)"

	if err := classifyMount(denied, errors.New("exit status 1")); !errors.Is(err, ErrNeedsFullDiskAccess) {
		t.Errorf("a refusal was not recognised: %v", err)
	}
	// mount_apfs can be refused while the shell still exits zero.
	if err := classifyMount(denied, nil); !errors.Is(err, ErrNeedsFullDiskAccess) {
		t.Errorf("a refusal with a zero exit status was not recognised: %v", err)
	}
}

func TestClassifyMountLeavesOtherFailuresAlone(t *testing.T) {
	other := errors.New("exit status 1")
	got := classifyMount("mount_apfs: volume could not be mounted: No such file or directory", other)
	if errors.Is(got, ErrNeedsFullDiskAccess) {
		t.Error("a missing-file failure was reported as a permission problem")
	}
	if !errors.Is(got, other) {
		t.Errorf("the original error was lost: %v", got)
	}
	if classifyMount("", nil) != nil {
		t.Error("success was turned into a failure")
	}
}

// A real snapshot is mounted read-only. If the fake were writable, code that
// wrote into a snapshot would pass here and fail against a mount — which is
// exactly the class of bug a stand-in is supposed not to hide.
func TestFakeMountIsReadOnlyLikeARealOne(t *testing.T) {
	f, seed := newFakeInTemp(t)
	if err := f.Mount(context.Background(), []string{fakeSnapshot}); err != nil {
		t.Fatal(err)
	}
	mp, _ := f.MountPoint(fakeSnapshot)
	inside := filepath.Join(mp, seed)

	if err := os.WriteFile(filepath.Join(inside, "intruder.txt"), []byte("x"), 0o600); err == nil {
		t.Error("created a new file inside the fake mount; a real mount would refuse")
	}
	if err := os.WriteFile(filepath.Join(inside, "alpha.txt"), []byte("overwritten"), 0o600); err == nil {
		t.Error("overwrote a file inside the fake mount; a real mount would refuse")
	}

	// Reading must still work, or the stand-in is useless.
	if _, err := os.ReadFile(filepath.Join(inside, "alpha.txt")); err != nil {
		t.Errorf("cannot read inside the sealed fake mount: %v", err)
	}
	if _, err := os.ReadDir(inside); err != nil {
		t.Errorf("cannot list inside the sealed fake mount: %v", err)
	}

	// And it must still come apart, which a sealed tree would otherwise prevent.
	if err := f.Unmount(context.Background(), []string{fakeSnapshot}); err != nil {
		t.Fatalf("Unmount could not remove a sealed tree: %v", err)
	}
	if mounted, _ := f.IsMounted(fakeSnapshot); mounted {
		t.Error("still mounted after unmounting a sealed tree")
	}
}
