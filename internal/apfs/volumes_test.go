package apfs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Enumerating the volumes local snapshots actually reach.
//
// `tmutil localsnapshot` takes no arguments and writes to every eligible mounted
// APFS volume at once, and this package used to list and prune the data volume
// alone. Everything else accumulated snapshots nothing would ever delete: the
// machine that found it had an SD card at 98% full holding eight that existed
// nowhere else, one of them pinning its container's minimum size.

// realMount is this machine's mount(8) output, trimmed to the interesting lines.
// The shape matters more than the contents: a mount point can contain spaces,
// and the options list is what identifies the filesystem.
const realMount = `/dev/disk3s3s1 on / (apfs, sealed, local, read-only, journaled)
/dev/disk3s6 on /System/Volumes/VM (apfs, local, noexec, journaled, noatime, nobrowse)
devfs on /dev (devfs, local, nobrowse)
/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled, nobrowse, protect, root data)
map auto_home on /System/Volumes/Data/home (autofs, automounted, nobrowse)
/dev/disk8s1 on /Volumes/sdcard256gb (apfs, local, nodev, nosuid, journaled, noowners)
/dev/disk9s1 on /Volumes/My Backup Disk (apfs, local, journaled)`

// snapshotBlocks renders diskutil's block-per-snapshot format for one volume.
func snapshotBlocks(device string, snaps ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Snapshots for %s (%d found)\n", device, len(snaps))
	for _, name := range snaps {
		b.WriteString("|\n+-- 00000000-0000-0000-0000-000000000000\n")
		fmt.Fprintf(&b, "|   Name:        %s\n|   XID:         1\n|   Purgeable:   Yes\n", name)
	}
	return b.String()
}

// volumeRunner answers mount and diskutil for a machine described as a map of
// mount point to listing.
type volumeRunner struct {
	mount  string
	byPath map[string]string
	asked  []string
}

func (v *volumeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name == "mount" {
		return v.mount, nil
	}
	if name == "diskutil" && len(args) == 3 && args[1] == "listSnapshots" {
		v.asked = append(v.asked, args[2])
		if out, ok := v.byPath[args[2]]; ok {
			return out, nil
		}
		return "", fmt.Errorf("diskutil: no such volume")
	}
	return "", fmt.Errorf("unexpected %s %v", name, args)
}

func TestOnlyAPFSVolumesAreAskedAbout(t *testing.T) {
	got := mountedAPFS(realMount)

	want := []string{
		"/", "/System/Volumes/VM", "/System/Volumes/Data",
		"/Volumes/sdcard256gb", "/Volumes/My Backup Disk",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v,\nwant %v", got, want)
	}
	// devfs and autofs are filesystems too, and asking diskutil about them is a
	// command that can only fail.
	for _, m := range got {
		if m == "/dev" || strings.HasSuffix(m, "/home") {
			t.Errorf("%s is not APFS and was included", m)
		}
	}
}

// A mount point can contain spaces, which is why the line is cut at " on " and
// the last " (" rather than split on whitespace.
func TestAVolumeNamedWithSpacesSurvives(t *testing.T) {
	for _, m := range mountedAPFS(realMount) {
		if m == "/Volumes/My Backup Disk" {
			return
		}
	}
	t.Error("a volume whose name contains spaces was lost")
}

// Two mount points can name one volume: tmutil answers for the volume group, so
// "/" and the data volume return an identical snapshot list. Deduplicating on
// the path would count them twice and try to delete each snapshot twice.
func TestOneVolumeUnderTwoMountPointsIsCountedOnce(t *testing.T) {
	r := &volumeRunner{
		mount: "/dev/disk3s3s1 on / (apfs, sealed, local, read-only, journaled)\n" +
			"/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled, nobrowse)\n",
		byPath: map[string]string{
			"/":                    snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local"),
			"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local"),
		},
	}

	vols, err := Volumes(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("got %d volumes, want 1: %+v", len(vols), vols)
	}
	if vols[0].Device != "disk3s1" {
		t.Errorf("device %q", vols[0].Device)
	}
}

// The sealed system volume carries macOS's own snapshot. It is not a Time
// Machine local snapshot, it is not ours, and deleting it is not a thing to
// attempt.
func TestTheSealedSystemSnapshotIsNotOurs(t *testing.T) {
	r := &volumeRunner{
		mount: "/dev/disk3s3s1 on / (apfs, sealed, local, read-only, journaled)\n",
		byPath: map[string]string{
			"/": snapshotBlocks("disk3s3s1", "com.apple.os.update-8F1C4B2A9D3E5F607182930A4B5C6D7E8F90A1B2"),
		},
	}

	vols, err := Volumes(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 0 {
		t.Errorf("the OS seal was reported as a volume to manage: %+v", vols)
	}
}

func TestAVolumeWithNoSnapshotsIsLeftOut(t *testing.T) {
	r := &volumeRunner{
		mount: "/dev/disk3s6 on /System/Volumes/VM (apfs, local, journaled)\n" +
			"/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/System/Volumes/VM":   "No snapshots for disk3s6\n",
			"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local"),
		},
	}

	vols, err := Volumes(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 || vols[0].Device != "disk3s1" {
		t.Errorf("got %+v, want the data volume alone", vols)
	}
}

// A volume diskutil will not answer for must not stop the ones it will from
// being enumerated — and therefore from being pruned.
func TestAVolumeThatCannotBeInterrogatedIsSkipped(t *testing.T) {
	r := &volumeRunner{
		mount: "/dev/disk9s1 on /Volumes/gone (apfs, local, journaled)\n" +
			"/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local"),
		},
	}

	vols, err := Volumes(context.Background(), r)
	if err != nil {
		t.Fatalf("one unreadable volume failed the whole enumeration: %v", err)
	}
	if len(vols) != 1 {
		t.Errorf("got %+v", vols)
	}
}

// The flags come from the same listing as the snapshots, per volume, because the
// container is per volume: an external disk has its own pinning snapshot and the
// boot volume's numbers say nothing about it.
func TestEachVolumeCarriesItsOwnPinningSnapshot(t *testing.T) {
	sdcard := snapshotBlocks("disk8s1",
		"com.apple.TimeMachine.2026-08-26-134707.local",
		"com.apple.TimeMachine.2026-08-27-130450.local")
	sdcard = strings.Replace(sdcard,
		"|   Name:        com.apple.TimeMachine.2026-08-26-134707.local\n|   XID:         1\n|   Purgeable:   Yes\n",
		"|   Name:        com.apple.TimeMachine.2026-08-26-134707.local\n|   XID:         1\n|   Purgeable:   Yes\n"+
			"|   NOTE:        This snapshot limits the minimum size of APFS Container disk8\n", 1)

	r := &volumeRunner{
		mount: "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n" +
			"/dev/disk8s1 on /Volumes/sdcard256gb (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local"),
			"/Volumes/sdcard256gb": sdcard,
		},
	}

	vols, err := Volumes(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 2 {
		t.Fatalf("got %d volumes, want 2", len(vols))
	}
	// Sorted by device, so disk3s1 then disk8s1.
	if vols[0].PinningStamp != "" {
		t.Errorf("the data volume was given the other disk's pinning snapshot: %q", vols[0].PinningStamp)
	}
	if vols[1].PinningStamp != "2026-08-26-134707" {
		t.Errorf("the external volume's pinning snapshot is %q", vols[1].PinningStamp)
	}
	if vols[1].Purgeable != 2 {
		t.Errorf("purgeable count is %d, want 2", vols[1].Purgeable)
	}
}

// The union is what pruning decides over. A date on one volume and not another
// has to appear, or nothing ever asks for its deletion — which is exactly how a
// disk fills with snapshots that cannot be removed.
func TestTheUnionHoldsEveryDateOnceAcrossVolumes(t *testing.T) {
	shared, _ := ParseName("com.apple.TimeMachine.2026-08-27-130450.local")
	only, _ := ParseName("com.apple.TimeMachine.2026-08-26-134707.local")

	got := EverySnapshot([]Volume{
		{Device: "disk3s1", Snapshots: []VolumeSnapshot{{Snapshot: shared}}},
		{Device: "disk8s1", Snapshots: []VolumeSnapshot{{Snapshot: shared}, {Snapshot: only}}},
	})

	if len(got) != 2 {
		t.Fatalf("got %d snapshots, want 2 — the shared date once and the other's own: %+v", len(got), got)
	}
	// Newest first, which is the order every other listing here uses.
	if !got[0].Taken.After(got[1].Taken) {
		t.Errorf("not newest first: %v then %v", got[0].Stamp, got[1].Stamp)
	}
	var found bool
	for _, s := range got {
		if s.Stamp == only.Stamp {
			found = true
		}
	}
	if !found {
		t.Error("a date held by only one volume is missing from the union, so nothing would ever prune it")
	}
}

func TestTheDeviceIsReadFromEitherWordingDiskutilUses(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{"Snapshots for disk8s1 (14 found)", "disk8s1"},
		{"No snapshots for disk3s6", "disk3s6"},
		{"Snapshot for disk3s3s1 (1 found)", "disk3s3s1"},
	} {
		got, ok := snapshotListDevice(c.line)
		if !ok || got != c.want {
			t.Errorf("%q gave %q (ok=%v), want %q", c.line, got, ok, c.want)
		}
	}
	if _, ok := snapshotListDevice("something else entirely"); ok {
		t.Error("a line naming no device reported one")
	}
}

// Enumerating volumes is expensive, and what it cost was not visible until it
// was on a hot path.
//
// One `mount`, a `diskutil apfs listSnapshots` per mounted APFS filesystem —
// which includes every snapshot the application has opened — and a `diskutil
// info` for each volume that holds any. Twenty-odd subprocesses, seconds of
// wall clock. Fine once; catastrophic per call, and per call is what it became:
// translating a path needs the volume, and translating happens once per
// directory entry, so a listing of two hundred files asked the machine to
// enumerate its disks two hundred times and the window stopped answering.

// countingRunner reports how many commands an operation costs.
type countingRunner struct {
	inner Runner
	runs  int
}

func (c *countingRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	c.runs++
	return c.inner.Run(ctx, name, args...)
}

// A volume holding no snapshots is never asked its name, which is one subprocess
// each and wanted only for the volumes that reach the screen. Most of a Mac's
// mount points hold nothing.
func TestOnlyVolumesWithSnapshotsAreNamed(t *testing.T) {
	r := &countingRunner{inner: &volumeRunner{
		mount: "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n" +
			"/dev/disk3s6 on /System/Volumes/VM (apfs, local, journaled)\n" +
			"/dev/disk3s4 on /System/Volumes/Preboot (apfs, local, journaled)\n" +
			"/dev/disk3s5 on /System/Volumes/Update (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/System/Volumes/Data":    snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local"),
			"/System/Volumes/VM":      "No snapshots for disk3s6\n",
			"/System/Volumes/Preboot": "No snapshots for disk3s4\n",
			"/System/Volumes/Update":  "No snapshots for disk3s5\n",
		},
	}}

	vols, err := Volumes(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("got %d volumes, want 1", len(vols))
	}
	// mount, four listSnapshots, and one info for the one volume that has any.
	if r.runs > 6 {
		t.Errorf("%d commands for four mount points, which is a name looked up for volumes that hold nothing", r.runs)
	}
}

// The cache is what keeps the cost off the hot path. Without it every path
// translation pays for a full enumeration.
func TestTheCacheEnumeratesOnceWithinItsWindow(t *testing.T) {
	inner := &volumeRunner{
		mount:  "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n",
		byPath: map[string]string{"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local")},
	}
	r := &countingRunner{inner: inner}
	c := NewCache(10 * time.Second)
	at := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)

	if _, err := c.Volumes(context.Background(), r, at); err != nil {
		t.Fatal(err)
	}
	first := r.runs
	if first == 0 {
		t.Fatal("the first call ran nothing, so this test is checking nothing")
	}

	// Two hundred translations, which is one ordinary directory listing.
	for i := 0; i < 200; i++ {
		if _, err := c.Volumes(context.Background(), r, at.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if r.runs != first {
		t.Errorf("a listing's worth of lookups cost %d commands beyond the first enumeration", r.runs-first)
	}

	// Past the window it asks again, or a disk plugged in would never appear.
	if _, err := c.Volumes(context.Background(), r, at.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	if r.runs == first {
		t.Error("the cache never expires, so a volume that appears is never seen")
	}
}

// Opening a snapshot adds an APFS filesystem, so the list is stale the moment it
// happens rather than ten seconds later.
func TestForgettingMakesTheNextQuestionReachTheMachine(t *testing.T) {
	r := &countingRunner{inner: &volumeRunner{
		mount:  "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n",
		byPath: map[string]string{"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local")},
	}}
	c := NewCache(time.Hour)
	at := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)

	c.Volumes(context.Background(), r, at)
	first := r.runs
	c.Forget()
	c.Volumes(context.Background(), r, at)

	if r.runs == first {
		t.Error("forgetting did not send the next question to the machine")
	}
}

// A momentary failure to run diskutil is not evidence that the disks have gone.
// Answering "no volumes" would empty the sidebar and refuse every translation.
func TestAFailedRefreshKeepsTheAnswerItHad(t *testing.T) {
	inner := &volumeRunner{
		mount:  "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n",
		byPath: map[string]string{"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local")},
	}
	c := NewCache(time.Second)
	at := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)

	got, err := c.Volumes(context.Background(), &countingRunner{inner: inner}, at)
	if err != nil || len(got) != 1 {
		t.Fatalf("first enumeration: %d volumes, %v", len(got), err)
	}

	// The machine stops answering, and the window still knows what it knew.
	got, err = c.Volumes(context.Background(), mute{}, at.Add(2*time.Second))
	if err != nil {
		t.Errorf("a failed refresh returned an error rather than the answer it had: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("a failed refresh emptied the list: %v", got)
	}
}

// A caller with no cache still works, so nothing has to check for one.
func TestNoCacheStillAnswers(t *testing.T) {
	var c *Cache
	got, err := c.Volumes(context.Background(), &volumeRunner{
		mount:  "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n",
		byPath: map[string]string{"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local")},
	}, time.Now())
	if err != nil || len(got) != 1 {
		t.Errorf("a nil cache answered %d volumes, %v", len(got), err)
	}
}

type mute struct{}

func (mute) Run(context.Context, string, ...string) (string, error) {
	return "", errors.New("nothing here answers")
}
