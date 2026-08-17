// Package mountmgr attaches APFS snapshots to the filesystem so their contents
// can be read.
//
// Mounts are read-only, which is what makes browsing a snapshot safe: nothing
// the user does through this application can alter the recorded state.
package mountmgr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"snapshotter/internal/i18n"
	"strings"
	"syscall"

	"snapshotter/internal/apfs"
	"snapshotter/internal/elevate"
)

// Manager owns the mountpoints for one volume's snapshots.
type Manager struct {
	Elev   elevate.Elevator
	Volume string
	// Root is the directory holding one mountpoint per snapshot. It lives
	// under the user's own Application Support directory so the directories
	// themselves can be created without authorization; only the mount needs it.
	Root string
	// Program is this application's own executable, which is what gets elevated.
	//
	// It has to be this binary and not /sbin/mount_apfs, and that is the whole
	// reason mounting works at all. Root is necessary but not sufficient: a
	// snapshot of the data volume holds the user's files, so reading it is gated
	// by TCC as well, and TCC decides by the identity of the process making the
	// call. Elevating mount_apfs directly asks a stock Apple binary to do it,
	// which carries none of our identity, so no amount of granting Full Disk
	// Access to this application can reach it. Elevating ourselves and calling
	// mount_apfs from there puts our own code signature on the call, and the
	// grant applies.
	//
	// The consequence is worth stating: this only works from a packaged,
	// FDA-granted .app. Under `go run` or a bare binary the identity is not the
	// bundle's and mounting is refused — correctly.
	Program string

	// isMounted is swappable so the batching logic can be tested without
	// mounting anything.
	isMounted func(string) (bool, error)
}

// New builds a Manager. root is created on demand. program is this application's
// own executable; see Manager.Program for why it is needed.
func New(elev elevate.Elevator, volume, root, program string) *Manager {
	return &Manager{Elev: elev, Volume: volume, Root: root, Program: program, isMounted: isMountPoint}
}

// MountPoint is where a snapshot is or would be mounted. The bare date stamp
// names the directory, which keeps restore paths readable.
func (m *Manager) MountPoint(name string) (string, error) {
	s, ok := apfs.ParseName(name)
	if !ok {
		return "", fmt.Errorf("mountmgr: %q is not a snapshot name", name)
	}
	return filepath.Join(m.Root, s.Stamp), nil
}

// IsMounted reports whether a snapshot is currently attached.
func (m *Manager) IsMounted(name string) (bool, error) {
	mp, err := m.MountPoint(name)
	if err != nil {
		return false, err
	}
	return m.isMounted(mp)
}

// Mount attaches every named snapshot that is not already attached, raising a
// single authorization prompt for the whole batch. Mounting an already-mounted
// snapshot costs nothing and prompts for nothing.
func (m *Manager) Mount(ctx context.Context, names []string) error {
	var pending []string
	for _, name := range names {
		mp, err := m.MountPoint(name)
		if err != nil {
			return err
		}
		mounted, err := m.isMounted(mp)
		if err != nil {
			return err
		}
		if mounted {
			continue
		}
		if err := os.MkdirAll(mp, 0o700); err != nil {
			return fmt.Errorf("mountmgr: creating mountpoint: %w", err)
		}
		pending = append(pending, name)
	}
	if len(pending) == 0 {
		return nil
	}

	script, err := m.helperScript(helperMount, pending)
	if err != nil {
		return err
	}
	out, err := m.Elev.RunPrivileged(ctx, script, mountReason(pending))
	if err != nil {
		return classifyMount(out, err)
	}
	// mount_apfs can be refused while the privileged shell still exits zero, so
	// the output is classified whatever the status was.
	if denied := classifyMount(out, nil); denied != nil {
		return denied
	}
	return m.verify(pending, true)
}

// ErrNeedsFullDiskAccess reports that mount_apfs was refused by TCC rather than
// by file permissions.
//
// Root is necessary to mount and is not sufficient: the process responsible for
// the call also needs Full Disk Access. mount_apfs reports that as "Operation
// not permitted", which reads like an ownership problem and is not one, so the
// distinction is made here rather than left to the user to guess.
var ErrNeedsFullDiskAccess = errors.New(
	"macOS refused to mount the snapshot. Mounting needs Full Disk Access as well as an " +
		"administrator password, and the permission is checked against the process that runs " +
		"mount_apfs rather than against this application")

// deniedMarkers are the ways mount_apfs words a TCC refusal. The errno it
// prints is not the one that matches its own message, so the text is matched
// rather than the number.
var deniedMarkers = []string{
	"operation not permitted",
	"permission denied",
}

// classifyMount recognises a TCC refusal in mount_apfs output. Anything else is
// returned unchanged, because guessing at an unfamiliar failure is worse than
// showing it.
func classifyMount(out string, err error) error {
	text := strings.ToLower(out)
	if err != nil {
		text += " " + strings.ToLower(err.Error())
	}
	if !strings.Contains(text, "mount_apfs") && !strings.Contains(text, "could not be mounted") {
		return err
	}
	for _, marker := range deniedMarkers {
		if strings.Contains(text, marker) {
			if err != nil {
				return fmt.Errorf("%w: %v", ErrNeedsFullDiskAccess, err)
			}
			return ErrNeedsFullDiskAccess
		}
	}
	return err
}

// Unmount detaches snapshots, again with one prompt for the batch.
func (m *Manager) Unmount(ctx context.Context, names []string) error {
	var pending []string
	for _, name := range names {
		mounted, err := m.IsMounted(name)
		if err != nil {
			return err
		}
		if mounted {
			pending = append(pending, name)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	script, err := m.helperScript(helperUnmount, pending)
	if err != nil {
		return err
	}
	if _, err := m.Elev.RunPrivileged(ctx, script, unmountReason(pending)); err != nil {
		return err
	}
	return m.verify(pending, false)
}

// MountedNames filters names down to those currently attached.
func (m *Manager) MountedNames(names []string) []string {
	var out []string
	for _, name := range names {
		if ok, err := m.IsMounted(name); err == nil && ok {
			out = append(out, name)
		}
	}
	return out
}

// verify re-checks the mount table, because a privileged script reports the
// exit status of its last command and a partial batch would otherwise pass.
func (m *Manager) verify(names []string, wantMounted bool) error {
	var failed []string
	for _, name := range names {
		mounted, err := m.IsMounted(name)
		if err != nil {
			return err
		}
		if mounted != wantMounted {
			failed = append(failed, name)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	verb := "mount"
	if !wantMounted {
		verb = "unmount"
	}
	return fmt.Errorf("mountmgr: failed to %s: %s", verb, strings.Join(failed, ", "))
}

func (m *Manager) mountScript(names []string) (string, error) {
	var cmds []string
	for _, name := range names {
		mp, err := m.MountPoint(name)
		if err != nil {
			return "", err
		}
		q, err := quoteAll(name, m.Volume, mp)
		if err != nil {
			return "", err
		}
		// Read-only, and nobrowse so a mounted snapshot does not appear as a
		// removable disk in the Finder sidebar for every snapshot opened.
		cmds = append(cmds, fmt.Sprintf("/sbin/mount_apfs -o ro,nobrowse -s %s %s %s", q[0], q[1], q[2]))
	}
	// Separated by ';' so one snapshot failing still lets the others mount;
	// verify() decides the outcome, not the exit status.
	return strings.Join(cmds, "; "), nil
}

func (m *Manager) unmountScript(names []string) (string, error) {
	var cmds []string
	for _, name := range names {
		mp, err := m.MountPoint(name)
		if err != nil {
			return "", err
		}
		q, err := quoteAll(mp)
		if err != nil {
			return "", err
		}
		cmds = append(cmds, fmt.Sprintf("/sbin/umount %s", q[0]))
	}
	return strings.Join(cmds, "; "), nil
}

// The reasons shown in the authorization dialog.
//
// These name the application, say which snapshots and what will happen to them,
// and answer the question the prompt otherwise provokes: why does reading my own
// files need a password? Because attaching a filesystem is privileged, not
// because the contents are. Saying so is the difference between a prompt someone
// reads and a prompt someone learns to click through.
//
// A single snapshot is named by its date, because at that point the user has
// clicked one row and the dialog should agree with what they clicked. Past one,
// the count is the only thing that fits.
// One message per plural form rather than a sentence assembled from pronouns.
// possessive() and subject() existed only to make English agree — "its"/"their",
// "It"/"They" — and no other language agrees along the same axis.
func mountReason(names []string) string {
	return i18n.N("mount.opening", len(names), "What", describe(names))
}

func unmountReason(names []string) string {
	return i18n.N("mount.closing", len(names), "What", describe(names))
}

// describe names one snapshot by date and any larger number by count.
func describe(names []string) string {
	if len(names) == 1 {
		if s, ok := apfs.ParseName(names[0]); ok {
			return i18n.T("mount.theSnapshotFrom", "When", s.Taken.Format("Mon 2 Jan, 15:04"))
		}
		return i18n.N("count.snapshots", 1)
	}
	return i18n.N("count.snapshots", len(names))
}

func possessive(names []string) string {
	if len(names) == 1 {
		return "its"
	}
	return "their"
}

func subject(names []string) string {
	if len(names) == 1 {
		return "It is"
	}
	return "They are"
}

// quoteAll single-quotes arguments for the privileged shell. Every value
// reaching here is already validated, so an embedded quote means a bug
// upstream and is refused rather than escaped.
func quoteAll(args ...string) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, "'\n") {
			return nil, fmt.Errorf("mountmgr: refusing to use %q in a privileged command", a)
		}
		out[i] = "'" + a + "'"
	}
	return out, nil
}

// isMountPoint reports whether path is the root of a mounted filesystem, by
// comparing its device number with its parent's. This reads the live mount
// state rather than tracking it in memory, so mounts left behind by a previous
// run of the application are still recognised.
func isMountPoint(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	a, ok1 := fi.Sys().(*syscall.Stat_t)
	b, ok2 := parent.Sys().(*syscall.Stat_t)
	if !ok1 || !ok2 {
		return false, errors.New("mountmgr: cannot read device numbers")
	}
	return a.Dev != b.Dev, nil
}
