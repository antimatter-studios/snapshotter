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

// ErrNotCovered reports a path that no snapshot of the volume can contain.
type ErrNotCovered struct {
	Path string
	// Volume is where the volume being translated against is mounted, empty for
	// the data volume. It is carried so the message can name the disk someone is
	// actually looking at: "not on the data volume" is a confusing thing to be
	// told about a file on an SD card that is plainly being browsed.
	Volume string
}

func (e *ErrNotCovered) Error() string {
	if e.Volume != "" {
		return fmt.Sprintf("%s is not on %s, so that volume's snapshots do not cover it", e.Path, e.Volume)
	}
	return fmt.Sprintf("%s is not on the data volume, so snapshots do not cover it", e.Path)
}

// Volume is the live root that a snapshot was taken of.
//
// It exists because there is more than one. `tmutil localsnapshot` takes no
// arguments and writes to every mounted APFS volume at once, so a snapshot's
// contents are rooted wherever its volume is — and this package translated every
// path as though that were always the data volume. Browsing an external disk's
// snapshot asked whether its files were under /Users and was told they were not
// covered, which is true of the data volume and irrelevant to the disk on screen.
//
// The zero value is the data volume, which is the one root that is not a plain
// prefix: it is presented at "/" with a fixed set of top-level directories, and
// the system volume points into it through symlinks.
type Volume struct {
	// Root is where the volume is mounted live, "/Volumes/sdcard256gb". Empty
	// means the data volume.
	Root string
}

// At names a volume by its live mount point. The data volume is passed as itself
// rather than as a path, because its layout is a special case rather than a
// prefix.
func At(mountPoint string) Volume {
	if mountPoint == "" || filepath.Clean(mountPoint) == dataVolumePrefix {
		return Volume{}
	}
	return Volume{Root: filepath.Clean(mountPoint)}
}

// Canonical rewrites a live path into its position inside a snapshot of v.
func (v Volume) Canonical(livePath string) (string, error) {
	if v.Root == "" {
		return canonicalOnData(livePath)
	}
	if !filepath.IsAbs(livePath) {
		return "", fmt.Errorf("vfs: %q is not an absolute path", livePath)
	}
	p := filepath.Clean(livePath)
	if p == v.Root {
		return "/", nil
	}
	rest, ok := strings.CutPrefix(p, v.Root+"/")
	if !ok {
		return "", &ErrNotCovered{Path: livePath, Volume: v.Root}
	}
	return "/" + rest, nil
}

// ToSnapshot maps a live path to the same file inside a mounted snapshot of v.
func (v Volume) ToSnapshot(mountPoint, livePath string) (string, error) {
	p, err := v.Canonical(livePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(mountPoint, p), nil
}

// ToLive maps a path inside a mounted snapshot of v back to its live location,
// so the interface can show where a restored file would land.
//
// The volume's root is put back on the front. Returning the snapshot-relative
// path, which is what this did, produced "/projects" for a file that lives at
// "/Volumes/sdcard256gb/projects" — a path on the startup disk, and a restore
// aimed at the wrong volume entirely.
func (v Volume) ToLive(mountPoint, snapshotPath string) (string, error) {
	mp := filepath.Clean(mountPoint)
	p := filepath.Clean(snapshotPath)
	if p == mp {
		if v.Root == "" {
			return "/", nil
		}
		return v.Root, nil
	}
	rest, ok := strings.CutPrefix(p, mp+"/")
	if !ok {
		return "", fmt.Errorf("vfs: %q is not inside the snapshot mounted at %s", snapshotPath, mountPoint)
	}
	if v.Root == "" {
		return "/" + rest, nil
	}
	return filepath.Join(v.Root, rest), nil
}

// Top is the highest path this volume's snapshots have anything to say about.
//
// The data volume's is "/", because its snapshots cover the whole running system.
// Any other volume's is where it is mounted: a snapshot of an SD card knows
// nothing about /Volumes, which is on the startup disk, and nothing about the
// other disks mounted beside it.
//
// It exists because the interface was offering paths it would then have to
// refuse. Browsing a snapshot of /Volumes/sdcard256gb, the trail of folders
// across the top read "/ › Volumes › sdcard256gb › projects", and the first two
// were clickable — leading to an error saying that volume's snapshots do not
// cover them. A control that cannot work should not be drawn.
func (v Volume) Top() string {
	if v.Root == "" {
		return "/"
	}
	return v.Root
}

// Parent is the folder above one this volume covers, and never above Top.
//
// Clamped rather than refused: somebody at the top of a volume pressing "up" has
// asked for something reasonable, and the answer is that they are already there.
func (v Volume) Parent(livePath string) string {
	top := v.Top()
	clean := filepath.Clean(livePath)
	if clean == top || !v.Covered(clean) {
		return top
	}
	parent := filepath.Dir(clean)
	if !v.Covered(parent) {
		return top
	}
	return parent
}

// Covered reports whether a snapshot of v can contain a path.
func (v Volume) Covered(livePath string) bool {
	_, err := v.Canonical(livePath)
	return err == nil
}

// canonicalOnData rewrites a live path into its data-volume form: absolute,
// cleaned, with the /System/Volumes/Data prefix and the system volume's symlinks
// resolved. It reports ErrNotCovered for anything on another volume — which is
// correct about the data volume and says nothing about whether that other volume
// has snapshots of its own. Volume.Canonical is what decides which question is
// being asked.
func canonicalOnData(livePath string) (string, error) {
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
