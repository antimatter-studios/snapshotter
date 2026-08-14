// Package vfs translates between live paths and their counterparts inside a
// mounted snapshot, and reads directories on either side.
package vfs

import (
	"fmt"
	"path/filepath"
	"strings"
)

// dataVolumePrefix is where the data volume is exposed on a running system.
// Snapshots are taken of that volume, so its root is the snapshot's root.
const dataVolumePrefix = "/System/Volumes/Data"

// dataRoots are the top-level directories that live on the data volume. The
// system volume is sealed and separately snapshotted by macOS itself, so
// nothing under /System, /bin, /sbin or /usr (except /usr/local) appears in a
// Time Machine local snapshot.
var dataRoots = []string{
	"/Users",
	"/Applications",
	"/Library",
	"/private",
	"/opt",
	"/cores",
	"/usr/local",
}

// symlinkedRoots are paths the system volume presents as symlinks into the
// data volume. Inside a snapshot only the target exists, so these are rewritten
// before translation.
var symlinkedRoots = map[string]string{
	"/var": "/private/var",
	"/tmp": "/private/tmp",
	"/etc": "/private/etc",
}

// ErrNotCovered reports a path that no snapshot of the data volume can contain.
type ErrNotCovered struct{ Path string }

func (e *ErrNotCovered) Error() string {
	return fmt.Sprintf("%s is not on the data volume, so snapshots do not cover it", e.Path)
}

// Canonical rewrites a live path into its data-volume form: absolute, cleaned,
// with the /System/Volumes/Data prefix and the system volume's symlinks
// resolved. It reports ErrNotCovered for anything on another volume, which
// includes external disks and SD cards mounted under /Volumes.
func Canonical(livePath string) (string, error) {
	if !filepath.IsAbs(livePath) {
		return "", fmt.Errorf("vfs: %q is not an absolute path", livePath)
	}
	p := filepath.Clean(livePath)

	if p == dataVolumePrefix {
		return "/", nil
	}
	if rest, ok := strings.CutPrefix(p, dataVolumePrefix+"/"); ok {
		p = "/" + rest
	}

	for from, to := range symlinkedRoots {
		if p == from {
			p = to
			break
		}
		if rest, ok := strings.CutPrefix(p, from+"/"); ok {
			p = to + "/" + rest
			break
		}
	}

	if p == "/" {
		return "/", nil
	}
	for _, root := range dataRoots {
		if p == root || strings.HasPrefix(p, root+"/") {
			return p, nil
		}
	}
	return "", &ErrNotCovered{Path: livePath}
}

// ToSnapshot maps a live path to the same file inside a mounted snapshot.
func ToSnapshot(mountPoint, livePath string) (string, error) {
	p, err := Canonical(livePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(mountPoint, p), nil
}

// ToLive maps a path inside a mounted snapshot back to its live location, so
// the UI can show where a restored file would land.
func ToLive(mountPoint, snapshotPath string) (string, error) {
	mp := filepath.Clean(mountPoint)
	p := filepath.Clean(snapshotPath)
	if p == mp {
		return "/", nil
	}
	rest, ok := strings.CutPrefix(p, mp+"/")
	if !ok {
		return "", fmt.Errorf("vfs: %q is not inside the snapshot mounted at %s", snapshotPath, mountPoint)
	}
	return "/" + rest, nil
}

// Covered reports whether snapshots of the data volume can contain a path.
func Covered(livePath string) bool {
	_, err := Canonical(livePath)
	return err == nil
}
