package mountmgr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"snapshotter/internal/apfs"
	"snapshotter/internal/vfs"
)

// Fake stands in for Manager where mounting is impossible.
//
// Mounting a real snapshot needs root and Full Disk Access, and is refused by
// TCC on a machine that has not granted both. Nothing in vfs, diffs or restore
// knows how the tree under a mountpoint arrived there, so a populated directory
// exercises the whole browse and compare surface without any of that — during
// development, and in tests that cannot mount.
//
// It is deliberately awkward to switch on. Fake data reaching a real restore
// would write invented files over real ones, so the mode announces itself in the
// interface and Replace restores are refused while it is active.
type Fake struct {
	// Root holds one directory per faked snapshot, as Manager's does.
	Root string
	// Seed is the live directory each fake snapshot is cloned from. Its contents
	// appear inside the fake at their real paths, so browsing shows somewhere
	// recognisable rather than an invented tree.
	Seed string
}

// FakeMarker names the file written into every fake mountpoint. Its presence is
// what IsMounted reports on, and what Unmount requires before deleting
// anything, so a stray Root can never turn Unmount into a recursive delete of
// something real.
const FakeMarker = ".snapshotter-fake-mount"

// NewFake builds a Fake. Neither directory needs to exist yet.
func NewFake(root, seed string) *Fake { return &Fake{Root: root, Seed: seed} }

// MountPoint matches Manager's, so paths are identical in either mode.
func (f *Fake) MountPoint(name string) (string, error) {
	s, ok := apfs.ParseName(name)
	if !ok {
		return "", fmt.Errorf("mountmgr: %q is not a snapshot name", name)
	}
	return filepath.Join(f.Root, s.Stamp), nil
}

// IsMounted reports whether a fake mountpoint has been populated. It checks for
// the marker rather than for the directory, because Manager creates the
// directory before mounting into it and an empty one must not read as mounted.
func (f *Fake) IsMounted(name string) (bool, error) {
	mp, err := f.MountPoint(name)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(mp, FakeMarker))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// Mount populates a directory for each snapshot that has none. No authorization
// is raised, which is the entire point.
func (f *Fake) Mount(ctx context.Context, names []string) error {
	for _, name := range names {
		mounted, err := f.IsMounted(name)
		if err != nil {
			return err
		}
		if mounted {
			continue
		}
		snap, ok := apfs.ParseName(name)
		if !ok {
			return fmt.Errorf("mountmgr: %q is not a snapshot name", name)
		}
		mp, err := f.MountPoint(name)
		if err != nil {
			return err
		}
		if err := f.populate(ctx, mp, snap); err != nil {
			return err
		}
	}
	return nil
}

// Unmount empties fake mountpoints. It refuses any directory without the
// marker, so it cannot be aimed at real data by a mistaken Root.
func (f *Fake) Unmount(ctx context.Context, names []string) error {
	for _, name := range names {
		mp, err := f.MountPoint(name)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(mp, FakeMarker)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		// The tree was sealed read-only to imitate a mount, and a read-only
		// directory cannot be removed, so the write bits go back first. Reached
		// only past the marker check above.
		if err := unseal(mp); err != nil {
			return err
		}
		if err := os.RemoveAll(mp); err != nil {
			return fmt.Errorf("mountmgr: emptying the fake mount %s: %w", mp, err)
		}
	}
	return nil
}

// MountedNames filters names down to those currently populated.
func (f *Fake) MountedNames(names []string) []string {
	var out []string
	for _, name := range names {
		if ok, err := f.IsMounted(name); err == nil && ok {
			out = append(out, name)
		}
	}
	return out
}

// populate clones the seed into the mountpoint at its real path, then edits the
// clone so the snapshot differs from the live disk in each of the ways the
// interface has to render. Every write lands inside mp; the live side is never
// touched.
func (f *Fake) populate(ctx context.Context, mp string, snap apfs.Snapshot) error {
	seed := filepath.Clean(f.Seed)
	// The data volume explicitly: the fake stands in for the startup disk's
	// snapshots and seeds from a directory on it.
	canonical, err := vfs.Volume{}.Canonical(seed)
	if err != nil {
		return fmt.Errorf("mountmgr: the fake seed %s is not on the data volume: %w", seed, err)
	}

	dest := filepath.Join(mp, canonical)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("mountmgr: creating the fake mountpoint: %w", err)
	}

	// -c asks for APFS clones, which makes this near-instant and exact however
	// large the seed is; -p keeps modification times, without which every file
	// would compare as changed for a reason that never happens in production.
	cmd := exec.CommandContext(ctx, "/bin/cp", "-Rpc", seed, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mountmgr: cloning %s into the fake mount: %w: %s",
			seed, err, strings.TrimSpace(string(out)))
	}

	if err := f.injectDifferences(dest, snap); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mp, FakeMarker), []byte(snap.Name+"\n"), 0o600); err != nil {
		return err
	}
	// A real snapshot is mounted read-only, and code that writes into one has to
	// fail here too or the fake would pass what a mount rejects. This is the one
	// way a populated directory is not already identical to a mount, so it is
	// worth closing rather than remembering.
	return sealReadOnly(mp)
}

// sealReadOnly clears the write bits over a whole tree, deepest first so a
// directory is not sealed before its contents are.
func sealReadOnly(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		return os.Chmod(path, 0o400)
	})
	if err != nil {
		return fmt.Errorf("mountmgr: sealing the fake mount: %w", err)
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i], 0o500); err != nil {
			return fmt.Errorf("mountmgr: sealing the fake mount: %w", err)
		}
	}
	return nil
}

// unseal restores the write bits, shallowest first, so the tree can be removed.
// Only ever called on a directory carrying the marker.
func unseal(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if d.IsDir() {
			mode = 0o700
		}
		return os.Chmod(path, mode)
	})
}

// injectDifferences makes the fake snapshot differ from the live disk on
// purpose, so Browse and Compare have all four states to show rather than a
// wall of "unchanged".
//
// The snapshot is the past, which decides the direction of each edit: a file
// added here is one the user has since deleted, and a file removed here is one
// they have since created.
func (f *Fake) injectDifferences(dest string, snap apfs.Snapshot) error {
	files, err := shallowFiles(dest)
	if err != nil {
		return err
	}

	// Deleted since the snapshot: present in the past, gone now.
	gone := filepath.Join(dest, "deleted-since-"+snap.Stamp+".txt")
	body := "This file existed when the snapshot was taken and has since been deleted.\n"
	if err := os.WriteFile(gone, []byte(body), 0o600); err != nil {
		return err
	}
	if err := os.Chtimes(gone, snap.Taken, snap.Taken); err != nil {
		return err
	}

	if len(files) > 0 {
		// Changed since the snapshot: same path, different contents. Rewriting
		// it also moves its modification time, which is what a shallow compare
		// reads.
		changed := files[0]
		old := "This is the version of the file recorded in the snapshot.\n"
		if err := os.WriteFile(changed, []byte(old), 0o600); err != nil {
			return err
		}
		if err := os.Chtimes(changed, snap.Taken, snap.Taken); err != nil {
			return err
		}
	}
	if len(files) > 1 {
		// New since the snapshot: absent from the past, present now.
		if err := os.Remove(files[1]); err != nil {
			return err
		}
	}
	return nil
}

// shallowFiles lists regular files directly inside dir, sorted, so the same
// seed always produces the same fake and a rerun is comparable with the last.
func shallowFiles(dir string) ([]string, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, item := range items {
		if item.IsDir() || strings.HasPrefix(item.Name(), ".") {
			continue
		}
		info, err := item.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, filepath.Join(dir, item.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// FakeEnabled reports whether fake mounting was asked for.
const FakeEnabled = "SNAPSHOTTER_FAKE_MOUNTS"

// FakeSeed names the directory a fake snapshot is cloned from.
const FakeSeed = "SNAPSHOTTER_FAKE_SEED"

// FakeFromEnv builds a Fake if the environment asks for one, and reports
// whether it did. The seed falls back to the user's home directory, which is
// both on the data volume and the place a user would look first.
func FakeFromEnv(root string) (*Fake, bool) {
	if os.Getenv(FakeEnabled) != "1" {
		return nil, false
	}
	seed := os.Getenv(FakeSeed)
	if seed == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, false
		}
		seed = home
	}
	return NewFake(root, seed), true
}
