package mountmgr

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"snapshotter/internal/apfs"
)

// rootTail is where a mountpoint directory is allowed to live, relative to a
// home directory.
//
// The helper cannot simply recompute the expected path: it runs as root, so
// os.UserHomeDir would hand back root's home rather than the home of the user
// who asked. So the shape is enforced instead of the exact value — a mounts
// directory belonging to some user's Application Support, and nowhere else.
var rootTail = filepath.Join("Library", "Application Support", "Snapshotter", "mounts")

// checkVolume refuses any source but the data volume. An arbitrary one is not
// something this ever needs, and "the volume to read from" is exactly the
// argument you would want to control if you could reach an elevated process.
func checkVolume(volume string) error {
	if volume != apfs.DataVolume {
		return fmt.Errorf("mountmgr: the helper only mounts snapshots of %s, not %q", apfs.DataVolume, volume)
	}
	return nil
}

// checkRoot confines the mountpoints to a user's own Application Support.
//
// Unconstrained, this is an elevated process mounting a filesystem over any
// path a caller names. The mount is read-only, so it cannot corrupt what it
// covers, but shadowing a directory as root is still not something to hand out:
// a read-only tree over the wrong path can hide files from everything that looks
// there afterwards.
func checkRoot(root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("mountmgr: the helper needs an absolute mount root, not %q", root)
	}
	// Cleaning changes the path only if it contained traversal or redundant
	// separators, and either means the caller is not the parent.
	if filepath.Clean(root) != root {
		return fmt.Errorf("mountmgr: %q is not a clean path", root)
	}
	if !strings.HasSuffix(root, string(filepath.Separator)+rootTail) {
		return fmt.Errorf("mountmgr: the helper only mounts under %s, not %q", rootTail, root)
	}
	// Deliberately not also anchored to /Users. That would read as tighter but
	// only rules out paths this tail already rules out, while breaking a home
	// directory that is not under /Users — a network or relocated account — for
	// no gain. The tail is the part that matters.
	return nil
}

// HelperFlag is the flag that turns this application into its own privileged
// helper. It is deliberately not a documented command: nothing but Manager
// should ever run it, and it is useless without an authorization prompt already
// having been answered.
const HelperFlag = "mount-helper"

// The modes the helper understands. There are only two, and naming them keeps
// the elevated command readable in a process listing — which matters, because
// a user who sees an admin prompt deserves to be able to tell what it is for.
const (
	helperMount   = "mount"
	helperUnmount = "unmount"
)

// helperScript builds the command that gets elevated: this binary, re-invoked
// against itself.
//
// The indirection exists for TCC, not for tidiness — see Manager.Program. The
// mount_apfs invocation itself is still built by mountScript, on the far side of
// the prompt, so there is exactly one place that decides how a snapshot is
// attached.
func (m *Manager) helperScript(mode string, names []string) (string, error) {
	if m.Program == "" {
		return "", errors.New("mountmgr: no program path, so there is nothing to elevate")
	}
	q, err := quoteAll(append([]string{m.Program, m.Root, m.Volume}, names...)...)
	if err != nil {
		return "", err
	}
	program, root, volume := q[0], q[1], q[2]
	return fmt.Sprintf("%s -%s=%s -helper-root %s -helper-volume %s -- %s",
		program, HelperFlag, mode, root, volume, strings.Join(q[3:], " ")), nil
}

// IsHelperInvocation reports whether these arguments are the elevated helper's.
//
// It is checked before the ordinary flags are parsed, because the helper's flag
// set is not this program's: letting flag.Parse see -helper-root would abort on
// an unknown flag, and letting the helper see the window's flags would be no
// better. One of the two has to go first, and this is the cheaper test.
func IsHelperInvocation(args []string) bool {
	for _, a := range args {
		switch {
		case a == "-"+HelperFlag, a == "--"+HelperFlag:
			return true
		case strings.HasPrefix(a, "-"+HelperFlag+"="), strings.HasPrefix(a, "--"+HelperFlag+"="):
			return true
		}
	}
	return false
}

// RunHelper is the elevated half. It runs as root, having been re-execed by
// helperScript through the authorization prompt, and does the one thing that
// needed the privilege.
//
// args are the arguments after the program name. Output goes to out, which the
// unprivileged parent captures: mount_apfs explains a TCC refusal on stderr and
// classifyMount reads that text, so swallowing it here would turn a diagnosable
// refusal into a bare failure.
func RunHelper(ctx context.Context, args []string, out io.Writer) error {
	script, err := helperPlan(args, out)
	if err != nil {
		return err
	}

	// The script is a ';'-joined list so one snapshot failing does not abandon
	// the rest, exactly as it was when osascript ran it directly. The parent
	// verifies the mount table afterwards and decides the outcome, so the exit
	// status here is not load-bearing — but the output is.
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// helperPlan validates the elevated arguments and returns the command they
// authorise, running nothing.
//
// It is separate from RunHelper so the refusals can be tested for what actually
// matters: that a rejected argument yields no command at all, rather than a
// command that merely happened to fail.
func helperPlan(args []string, out io.Writer) (string, error) {
	fs := flag.NewFlagSet("snapshotter "+HelperFlag, flag.ContinueOnError)
	fs.SetOutput(out)
	mode := fs.String(HelperFlag, "", "mount or unmount")
	root := fs.String("helper-root", "", "directory holding the mountpoints")
	volume := fs.String("helper-volume", "", "the volume whose snapshots these are")
	if err := fs.Parse(args); err != nil {
		return "", err
	}

	names := fs.Args()
	if len(names) == 0 {
		return "", errors.New("mountmgr: the helper was given no snapshots")
	}
	// Everything below re-validates what the parent already validated, because
	// after this change the parent is no longer upstream of it. This is a
	// root-privileged entry point taking its arguments from argv, so anyone able
	// to run this binary can reach it, and the checks in Manager are beside it
	// rather than in front of it. The same reasoning as apfs.Delete refusing
	// anything but a bare date stamp: the guard lives next to the dangerous call.
	if err := checkVolume(*volume); err != nil {
		return "", err
	}
	if err := checkRoot(*root); err != nil {
		return "", err
	}
	// The whole batch is refused on the first bad name rather than the bad one
	// skipped, so a crafted argument cannot ride along with valid ones.
	for _, name := range names {
		if _, ok := apfs.ParseName(name); !ok {
			return "", fmt.Errorf("mountmgr: %q is not a snapshot name, so nothing was mounted", name)
		}
	}

	// No Elevator: this process is already root, and building one here would
	// invite a second prompt for work the first one already authorized.
	m := &Manager{Volume: *volume, Root: *root, isMounted: isMountPoint}

	// quoteAll stays in the path via these two, and is the last line rather than
	// the first: the checks above decide what is allowed, and it refuses to build
	// a command out of anything that slipped through with a quote in it.
	switch *mode {
	case helperMount:
		return m.mountScript(names)
	case helperUnmount:
		return m.unmountScript(names)
	default:
		return "", fmt.Errorf("mountmgr: %q is not a helper mode", *mode)
	}
}
