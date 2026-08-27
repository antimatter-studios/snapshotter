package mountmgr

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"snapshotter/internal/apfs"
)

// goodRoot is a mount root of the shape the helper insists on. The helper cannot
// recompute the real one, because as root it would resolve the wrong home, so it
// checks the shape instead.
const goodRoot = "/Users/someone/Library/Application Support/Snapshotter/mounts"

const goodName = "com.apple.TimeMachine.2026-08-13-172036.local"

// A described machine: the data volume, and one external disk that also holds
// local snapshots because tmutil put them there.
const (
	otherVolume = "/Volumes/sdcard256gb"
	otherDevice = "disk8s1"
	otherRoot   = goodRoot + "/" + otherDevice
)

// machine answers the two commands the allowlist is built from.
//
// Injected rather than reaching for the real machine: a build machine has no
// volumes holding local snapshots, so every case would be refused and every
// refusal test would pass for the wrong reason.
type machine struct{}

func (machine) Run(_ context.Context, name string, args ...string) (string, error) {
	switch {
	case name == "mount":
		return "/dev/disk3s1 on " + apfs.DataVolume + " (apfs, local, journaled)\n" +
			"/dev/disk8s1 on " + otherVolume + " (apfs, local, journaled)\n" +
			"/dev/disk3s3s1 on / (apfs, sealed, local, read-only, journaled)\n", nil
	case name == "diskutil" && len(args) == 3 && args[1] == "listSnapshots":
		switch args[2] {
		case apfs.DataVolume:
			return snapshotBlock("disk3s1", goodName), nil
		case otherVolume:
			return snapshotBlock(otherDevice, goodName), nil
		case "/":
			// macOS's own sealed snapshot, which is not ours and does not make the
			// system volume a place we may mount from.
			return snapshotBlock("disk3s3s1", "com.apple.os.update-8F1C4B2A9D3E5F60"), nil
		}
		return "", errNoVolume
	}
	return "", errNoVolume
}

var errNoVolume = errors.New("no such volume")

func snapshotBlock(device, name string) string {
	return "Snapshots for " + device + " (1 found)\n|\n+-- 00000000-0000-0000-0000-000000000000\n" +
		"|   Name:        " + name + "\n|   XID:         1\n|   Purgeable:   Yes\n"
}

// helperArgs assembles an elevated invocation the way helperScript does.
func helperArgs(mode, root, volume string, names ...string) []string {
	args := []string{"-" + HelperFlag + "=" + mode, "-helper-root", root, "-helper-volume", volume, "--"}
	return append(args, names...)
}

// TestHelperRefusesAndRunsNothing is the assertion that matters for a guard on a
// root-privileged entry point: a rejected argument must produce no command, not
// a command that merely fails. helperPlan returns the command it would run, so
// an empty string is proof nothing would have been executed.
func TestHelperRefusesAndRunsNothing(t *testing.T) {
	for _, tc := range []struct {
		why  string
		args []string
	}{
		{"no names", helperArgs(helperMount, goodRoot, apfs.DataVolume)},
		{"unknown mode", helperArgs("remount", goodRoot, apfs.DataVolume, goodName)},
		{"empty mode", helperArgs("", goodRoot, apfs.DataVolume, goodName)},
		{"not a snapshot name", helperArgs(helperMount, goodRoot, apfs.DataVolume, "/")},
		{"mount point instead of a name", helperArgs(helperMount, goodRoot, apfs.DataVolume, apfs.DataVolume)},
		{"shell metacharacters in the name", helperArgs(helperMount, goodRoot, apfs.DataVolume, goodName+" ; rm -rf /")},
		{"a crafted name riding along with a valid one", helperArgs(helperMount, goodRoot, apfs.DataVolume, goodName, "not-a-snapshot")},
		{"the system volume", helperArgs(helperMount, goodRoot, "/", goodName)},
		{"an arbitrary volume", helperArgs(helperMount, goodRoot, "/Volumes/Someone Elses Disk", goodName)},
		{"no volume", helperArgs(helperMount, goodRoot, "", goodName)},
		{"a relative root", helperArgs(helperMount, "mounts", apfs.DataVolume, goodName)},
		{"a traversing root", helperArgs(helperMount, "/Users/someone/../../etc", apfs.DataVolume, goodName)},
		{"the filesystem root", helperArgs(helperMount, "/", apfs.DataVolume, goodName)},
		{"a root outside Application Support", helperArgs(helperMount, "/tmp/mounts", apfs.DataVolume, goodName)},
		{"no root", helperArgs(helperMount, "", apfs.DataVolume, goodName)},
	} {
		script, err := helperPlan(context.Background(), machine{}, tc.args, io.Discard)
		if err == nil {
			t.Errorf("%s: accepted, and would have run %q", tc.why, script)
		}
		if script != "" {
			t.Errorf("%s: refused but still produced a command: %q", tc.why, script)
		}
	}
}

func TestHelperMountsWhatItIsGiven(t *testing.T) {
	script, err := helperPlan(context.Background(), machine{}, helperArgs(helperMount, goodRoot, apfs.DataVolume, goodName), io.Discard)
	if err != nil {
		t.Fatalf("refused a valid invocation: %v", err)
	}
	// The stamp names the directory, and the full name is what mount_apfs takes.
	for _, want := range []string{
		"/sbin/mount_apfs",
		"-o ro,nobrowse",
		"'" + goodName + "'",
		"'" + apfs.DataVolume + "'",
		"'" + goodRoot + "/2026-08-13-172036'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("mount command missing %q: %s", want, script)
		}
	}
}

func TestHelperUnmountsByMountPoint(t *testing.T) {
	script, err := helperPlan(context.Background(), machine{}, helperArgs(helperUnmount, goodRoot, apfs.DataVolume, goodName), io.Discard)
	if err != nil {
		t.Fatalf("refused a valid invocation: %v", err)
	}
	if !strings.Contains(script, "/sbin/umount") {
		t.Errorf("unmount command does not umount: %s", script)
	}
	// A snapshot name reaching umount would be a bug: it detaches a path.
	if strings.Contains(script, goodName) {
		t.Errorf("unmount command names the snapshot rather than its mountpoint: %s", script)
	}
}

// TestHelperScriptElevatesThisBinary guards the whole point of the indirection:
// what gets elevated must be our own executable, because that is the identity
// the Full Disk Access grant is attached to. Elevating /sbin/mount_apfs instead
// is what this change existed to stop, and it would fail only at runtime, on a
// machine where the grant had been given.
func TestHelperScriptElevatesThisBinary(t *testing.T) {
	m, _ := newTestManager(&fakeElevator{}, nil)
	script, err := m.helperScript(helperMount, []string{goodName})
	if err != nil {
		t.Fatalf("building the elevated command: %v", err)
	}
	if !strings.HasPrefix(script, "'"+m.Program+"'") {
		t.Errorf("elevates something other than this binary: %s", script)
	}
	if strings.Contains(script, "mount_apfs") {
		t.Errorf("elevates mount_apfs directly, which the TCC grant cannot reach: %s", script)
	}
	if !strings.Contains(script, "-"+HelperFlag+"="+helperMount) {
		t.Errorf("does not ask for the helper: %s", script)
	}
}

func TestHelperScriptNeedsAProgram(t *testing.T) {
	m := &Manager{Volume: apfs.DataVolume, Root: goodRoot, isMounted: isMountPoint}
	if _, err := m.helperScript(helperMount, []string{goodName}); err == nil {
		t.Error("built an elevated command with nothing to elevate")
	}
}

func TestIsHelperInvocation(t *testing.T) {
	for _, args := range [][]string{
		{"-" + HelperFlag + "=mount"},
		{"--" + HelperFlag + "=mount"},
		{"-" + HelperFlag},
		{"-helper-root", "/x", "--" + HelperFlag + "=unmount"},
	} {
		if !IsHelperInvocation(args) {
			t.Errorf("did not recognise the helper in %v", args)
		}
	}
	// The ordinary entry points must not be mistaken for it, or the window would
	// never open.
	for _, args := range [][]string{
		{},
		{"--take-snapshot"},
		{"--watch"},
		{"status"},
		{"run", "--", "ls"},
	} {
		if IsHelperInvocation(args) {
			t.Errorf("mistook %v for the helper", args)
		}
	}
}

// The authorization dialog is the one place this application asks for a
// password, and the only part of it we control is the sentence. A prompt that
// does not say what it is for trains people to approve prompts unread, so these
// assert it names the application, the act, and why root is involved at all.
func TestMountReasonSaysWhatItIsFor(t *testing.T) {
	one := mountReason([]string{goodName})
	for _, want := range []string{"Snapshotter", "opening", "read-only", "administrator", "13 Aug"} {
		if !strings.Contains(one, want) {
			t.Errorf("single-snapshot reason missing %q: %s", want, one)
		}
	}
	// One snapshot is named by its date, because the user has just clicked one row.
	if strings.Contains(one, "1 snapshot") {
		t.Errorf("named a single snapshot by count rather than by date: %s", one)
	}

	many := mountReason([]string{goodName, "com.apple.TimeMachine.2026-08-12-040000.local"})
	if !strings.Contains(many, "2 snapshots") {
		t.Errorf("batch reason does not say how many: %s", many)
	}
	// Grammar has to follow the count, since this is read by a person deciding
	// whether to type their password.
	if strings.Contains(many, "It is") || strings.Contains(many, "its files") {
		t.Errorf("batch reason reads as singular: %s", many)
	}
}

func TestUnmountReasonSaysWhatItIsFor(t *testing.T) {
	r := unmountReason([]string{goodName})
	for _, want := range []string{"Snapshotter", "closing", "administrator"} {
		if !strings.Contains(r, want) {
			t.Errorf("unmount reason missing %q: %s", want, r)
		}
	}
	if strings.Contains(r, "read-only") {
		t.Errorf("unmount reason reassures about something it is not doing: %s", r)
	}
}

// A reason that never reaches the dialog is decoration, so this checks the wiring
// rather than the wording.
func TestMountPassesAReasonToTheElevator(t *testing.T) {
	elev := &fakeElevator{}
	m, _ := newTestManager(elev, map[string]bool{})
	m.isMounted = func(string) (bool, error) { return len(elev.scripts) > 0, nil }

	if err := m.Mount(context.Background(), []string{goodName}); err != nil {
		t.Fatal(err)
	}
	if len(elev.reasons) != 1 || elev.reasons[0] == "" {
		t.Fatalf("no reason reached the authorization dialog: %#v", elev.reasons)
	}
	if !strings.Contains(elev.reasons[0], "Snapshotter") {
		t.Errorf("reason does not name the application: %s", elev.reasons[0])
	}
}

// Widening the allowlist is the riskiest change this package has had, so what it
// does and does not now accept is pinned here.
//
// It used to be one constant: the data volume, and nothing else. It no longer can
// be — `tmutil localsnapshot` writes to every mounted APFS volume at once, and a
// snapshot you can see and cannot open is a row that reads as broken. What has
// not changed is that "the volume to read from" is exactly the argument you would
// want to control if you could reach an elevated process. So it is still an
// allowlist; it is just discovered from the machine instead of written down.

func TestAVolumeHoldingSnapshotsMayBeMounted(t *testing.T) {
	script, err := helperPlan(context.Background(), machine{},
		helperArgs(helperMount, otherRoot, otherVolume, goodName), io.Discard)
	if err != nil {
		t.Fatalf("refused a volume that holds local snapshots: %v", err)
	}
	// Mounted from that volume, into that volume's own directory.
	if !strings.Contains(script, "'"+otherVolume+"'") {
		t.Errorf("the command does not read from %s: %s", otherVolume, script)
	}
	if !strings.Contains(script, "'"+otherRoot+"/2026-08-13-172036'") {
		t.Errorf("the command does not mount under %s: %s", otherRoot, script)
	}
}

// The allowlist is built here, as root, from the machine. A caller may name a
// volume; it may not add one.
func TestAVolumeTheMachineDoesNotHaveIsRefused(t *testing.T) {
	for _, volume := range []string{
		"/Volumes/Someone Elses Disk",
		"/",                     // the sealed system volume: its snapshot is macOS's own
		"/System/Volumes/VM",    // APFS, mounted, and holds no local snapshots
		"/Volumes/sdcard256gb/", // a trailing separator is a different string
		"../../etc",
		"",
	} {
		script, err := helperPlan(context.Background(), machine{},
			helperArgs(helperMount, goodRoot, volume, goodName), io.Discard)
		if err == nil {
			t.Errorf("%q was accepted, and would have run %q", volume, script)
		}
		if script != "" {
			t.Errorf("%q was refused but still produced a command: %q", volume, script)
		}
	}
}

// A machine that cannot be interrogated refuses rather than falling back.
//
// A fallback to the data volume would mean an unreadable mount table silently
// narrows what can be opened, and the failure would look like a broken button
// rather than a broken query.
type mute struct{}

func (mute) Run(context.Context, string, ...string) (string, error) { return "", errNoVolume }

func TestAnUnreadableMachineMountsNothing(t *testing.T) {
	script, err := helperPlan(context.Background(), mute{},
		helperArgs(helperMount, goodRoot, apfs.DataVolume, goodName), io.Discard)
	if err == nil {
		t.Fatalf("mounted without being able to tell which volumes hold snapshots: %q", script)
	}
	if script != "" {
		t.Errorf("refused but still produced a command: %q", script)
	}
}

// The mount root may gain exactly one component, and it has to look like a volume
// identifier. Without that the rule would accept the tail plus arbitrary depth,
// which is most of the way back to accepting anything.
func TestTheMountRootAllowsOneVolumeDirectoryAndNoMore(t *testing.T) {
	for _, root := range []string{
		goodRoot + "/disk8s1/deeper",
		goodRoot + "/not-a-device",
		goodRoot + "/disk8s1/../../../../etc",
		goodRoot + "/..",
	} {
		script, err := helperPlan(context.Background(), machine{},
			helperArgs(helperMount, root, otherVolume, goodName), io.Discard)
		if err == nil {
			t.Errorf("%q was accepted, and would have run %q", root, script)
		}
		if script != "" {
			t.Errorf("%q was refused but still produced a command: %q", root, script)
		}
	}
}

// Two volumes' snapshots of the same moment share a date, so they would share a
// mountpoint. The second mount would land on top of the first, and the row that
// opened it would show someone another disk's files.
func TestOneDateOnTwoVolumesMountsInTwoPlaces(t *testing.T) {
	onData, err := helperPlan(context.Background(), machine{},
		helperArgs(helperMount, goodRoot, apfs.DataVolume, goodName), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	onOther, err := helperPlan(context.Background(), machine{},
		helperArgs(helperMount, otherRoot, otherVolume, goodName), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if onData == onOther {
		t.Fatal("the same date on two volumes produced the same command")
	}
	if strings.Contains(onData, otherDevice) {
		t.Errorf("the data volume's mountpoint names another volume: %s", onData)
	}
}
