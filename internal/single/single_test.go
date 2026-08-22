package single

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One window at a time.
//
// The guard this replaces searched the process table for the installed
// application's path, which meant it caught exactly one of the four ways two
// copies happen: it refused a development build beside the installed one, and did
// nothing about two development builds, two installed copies, or a copy launched
// from anywhere else. The path was what it matched, and the path is what differs.

func lockPath(t *testing.T) string {
	t.Helper()
	return Path(filepath.Join(t.TempDir(), "snapshotter"))
}

func TestTheFirstCopyGetsTheLock(t *testing.T) {
	release, err := Hold(lockPath(t))
	if err != nil {
		t.Fatalf("the first copy was refused: %v", err)
	}
	release()
}

func TestASecondCopyIsRefused(t *testing.T) {
	path := lockPath(t)

	release, err := Hold(path)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer release()

	_, err = Hold(path)
	if err == nil {
		t.Fatal("a second copy took the lock as well")
	}
	var already *ErrAlreadyRunning
	if !errors.As(err, &already) {
		t.Fatalf("the refusal is not recognisable as one: %v", err)
	}
	// It names the copy that holds it, which is the question the menu bar cannot
	// answer: two identical icons, and no way to tell which is which.
	if already.PID != os.Getpid() {
		t.Errorf("the refusal names process %d, want %d", already.PID, os.Getpid())
	}
	if already.Held == "" {
		t.Error("the refusal does not say which executable is running")
	}
	if !strings.Contains(already.Error(), "Full Disk Access") {
		t.Errorf("the refusal does not say why it matters: %v", already)
	}
}

func TestTheLockIsFreeAgainOnceReleased(t *testing.T) {
	path := lockPath(t)

	release, err := Hold(path)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	release()

	// Quitting and reopening is the ordinary case, and a lock that outlived the
	// process would make the application impossible to restart.
	release2, err := Hold(path)
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	release2()
}

func TestTheEscapeHatchAllowsASecondCopy(t *testing.T) {
	path := lockPath(t)

	release, err := Hold(path)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer release()

	// Comparing two builds side by side is a real thing to want, and the guard
	// exists to stop it happening by accident rather than to forbid it.
	t.Setenv(AllowSecondCopy, "1")
	release2, err := Hold(path)
	if err != nil {
		t.Fatalf("the escape hatch did not work: %v", err)
	}
	release2()
}

func TestAnyValueOtherThanOneIsNotTheEscapeHatch(t *testing.T) {
	path := lockPath(t)

	release, err := Hold(path)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer release()

	// "0", "false", "no" — someone turning it off should not turn it on.
	for _, value := range []string{"0", "false", "no", ""} {
		t.Setenv(AllowSecondCopy, value)
		if _, err := Hold(path); err == nil {
			t.Errorf("%q was read as permission for a second copy", value)
		}
	}
}

func TestTheDirectoryIsMadeIfItIsNotThere(t *testing.T) {
	// A fresh installation has no settings directory yet, and refusing to start
	// over that would be absurd.
	path := Path(filepath.Join(t.TempDir(), "never", "existed"))

	release, err := Hold(path)
	if err != nil {
		t.Fatalf("a missing directory stopped the window opening: %v", err)
	}
	defer release()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the lock file was not created: %v", err)
	}
}

// A lock file left behind by a copy that crashed must not block the next launch.
// This is the failure a pid file has and a lock does not.
func TestAFileLeftBehindDoesNotBlockAnything(t *testing.T) {
	path := lockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// A plausible pid and path, as a crashed copy would have left.
	if err := os.WriteFile(path, []byte("99999\n/Applications/Snapshotter.app/Contents/MacOS/snapshotter\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	release, err := Hold(path)
	if err != nil {
		t.Fatalf("a leftover lock file blocked the window: %v", err)
	}
	release()
}

func TestTheLockSitsBesideTheSettings(t *testing.T) {
	// The same file for every copy belonging to one person, which is what makes it
	// a guard at all. A per-launch temporary path would lock nothing.
	dir := "/somewhere/config/snapshotter"
	if got := Path(dir); filepath.Dir(got) != dir {
		t.Errorf("the lock is at %s, not in %s", got, dir)
	}
	if Path(dir) != Path(dir) {
		t.Error("the path is not stable between calls")
	}
}
