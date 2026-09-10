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
	// included is what `tmutil isexcluded` says, when a test cares. A nil map
	// refuses the command, which is the case worth having as the default: it is
	// what a Mac that will not answer looks like, and every test written before
	// eligibility existed goes down that path and must still list what it did.
	included map[string]bool
	asked    []string
}

func (v *volumeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name == "mount" {
		return v.mount, nil
	}
	if name == "tmutil" && len(args) > 1 && args[0] == "isexcluded" {
		if v.included == nil {
			return "", fmt.Errorf("tmutil: cannot answer")
		}
		var b strings.Builder
		for _, item := range args[1:] {
			verdict := "[Excluded]"
			if v.included[item] {
				verdict = "[Included]"
			}
			fmt.Fprintf(&b, "%s\t%s\n", verdict, item)
		}
		return b.String(), nil
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
	// mount, one isexcluded for every mount point at once, four listSnapshots,
	// and one info for the one volume that has any.
	if r.runs > 7 {
		t.Errorf("%d commands for four mount points, which is a name looked up for volumes that hold nothing", r.runs)
	}
}

// Eligibility costs one subprocess however many disks are mounted.
//
// tmutil takes any number of items and answers a line each. Asking per mount
// point would be a dozen subprocesses added to the function that was already the
// expensive thing behind the window, to answer a question tmutil will answer for
// the whole mount table at once.
func TestEligibilityIsOneCallForEveryMountPoint(t *testing.T) {
	r := &volumeRunner{
		mount: "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n" +
			"/dev/disk3s6 on /System/Volumes/VM (apfs, local, journaled)\n" +
			"/dev/disk8s1 on /Volumes/sdcard256gb (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/System/Volumes/Data": "No snapshots for disk3s1\n",
			"/System/Volumes/VM":   "No snapshots for disk3s6\n",
			"/Volumes/sdcard256gb": "No snapshots for disk8s1\n",
		},
		included: map[string]bool{"/System/Volumes/Data": true, "/Volumes/sdcard256gb": true},
	}
	counted := &countingRunner{inner: r}

	if _, err := Volumes(context.Background(), counted); err != nil {
		t.Fatal(err)
	}
	// mount, one isexcluded, three listSnapshots, two info for the two volumes
	// that are eligible. Seven, and none of them a second isexcluded.
	if counted.runs > 7 {
		t.Errorf("%d commands for three mount points, which is eligibility asked more than once", counted.runs)
	}
}

// A volume Time Machine would snapshot appears before anything has been written
// to it.
//
// It used to be left out, and an empty eligible disk is exactly the state
// somebody opens this application to change: a card plugged in and not yet
// snapshotted looked identical to a card the application could not see at all.
func TestAnEligibleVolumeIsListedWhileItIsStillEmpty(t *testing.T) {
	r := &volumeRunner{
		mount: "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n" +
			"/dev/disk8s1 on /Volumes/sdcard256gb (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-08-27-130450.local"),
			"/Volumes/sdcard256gb": "No snapshots for disk8s1\n",
		},
		included: map[string]bool{"/System/Volumes/Data": true, "/Volumes/sdcard256gb": true},
	}

	vols, err := Volumes(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 2 {
		t.Fatalf("got %d volumes, want both the data volume and the empty card", len(vols))
	}
	card := vols[1]
	if card.MountPoint != "/Volumes/sdcard256gb" {
		t.Fatalf("got %q, want the card", card.MountPoint)
	}
	if len(card.Snapshots) != 0 {
		t.Errorf("got %d snapshots on an empty card", len(card.Snapshots))
	}
	// WithSnapshots is what the callers that mean the older, narrower question
	// use, and it has to still answer it.
	if held := WithSnapshots(vols); len(held) != 1 || held[0].MountPoint != "/System/Volumes/Data" {
		t.Errorf("WithSnapshots gave %v, want the data volume alone", held)
	}
}

// Everything macOS mounts that is not a snapshot target stays out.
//
// A Mac mounts a dozen APFS filesystems and only one or two of them are disks
// anybody means: Preboot, VM, xarts, iSCPreboot, Hardware, the recovery mounts
// and the sealed system volume are all APFS, all mounted, and none of them are
// places a snapshot is ever written. Listing empty volumes without this filter
// would have put nine rows of macOS plumbing in a list of somebody's disks.
func TestExcludedVolumesStayOutWhenTheyAreEmpty(t *testing.T) {
	r := &volumeRunner{
		mount: "/dev/disk3s3s1 on / (apfs, sealed, local, read-only, journaled)\n" +
			"/dev/disk3s6 on /System/Volumes/VM (apfs, local, journaled)\n" +
			"/dev/disk3s4 on /System/Volumes/Preboot (apfs, local, journaled)\n" +
			"/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/":                       "No snapshots for disk3s3s1\n",
			"/System/Volumes/VM":      "No snapshots for disk3s6\n",
			"/System/Volumes/Preboot": "No snapshots for disk3s4\n",
			"/System/Volumes/Data":    "No snapshots for disk3s1\n",
		},
		included: map[string]bool{"/System/Volumes/Data": true},
	}

	vols, err := Volumes(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 || vols[0].MountPoint != "/System/Volumes/Data" {
		t.Fatalf("got %v, want the data volume alone", vols)
	}
}

// A disk excluded from Time Machine after its snapshots were taken keeps them
// visible, because they still have to be deletable.
//
// This is the invariant the empty-volume change must not break. Snapshots that
// reach a volume nothing lists are not merely unseen, they are permanent:
// retention plans over what it can enumerate, so a list that dropped them would
// leave them on the disk forever with nothing able to ask for their removal.
func TestSnapshotsOnAnExcludedVolumeAreStillListed(t *testing.T) {
	r := &volumeRunner{
		mount: "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n" +
			"/dev/disk8s1 on /Volumes/sdcard256gb (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/System/Volumes/Data": "No snapshots for disk3s1\n",
			"/Volumes/sdcard256gb": snapshotBlocks("disk8s1", "com.apple.TimeMachine.2026-08-27-130450.local"),
		},
		included: map[string]bool{"/System/Volumes/Data": true},
	}

	vols, err := Volumes(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range vols {
		if v.MountPoint == "/Volumes/sdcard256gb" && len(v.Snapshots) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("got %v, want the excluded card's snapshot still listed", vols)
	}
}

// tmutil's own output shape, including the mount point with a space in it that
// the mount(8) parsing already had to survive.
func TestReadingWhichVolumesTimeMachineIncludes(t *testing.T) {
	r := &fixedRunner{out: "[Excluded]\t/\n" +
		"[Included]  /System/Volumes/Data\n" +
		"[Included]\t/Volumes/My Backup Disk\n"}

	got := includedInBackup(context.Background(), r, []string{"/", "/System/Volumes/Data", "/Volumes/My Backup Disk"})
	if len(got) != 2 || !got["/System/Volumes/Data"] || !got["/Volumes/My Backup Disk"] {
		t.Errorf("got %v, want the data volume and the backup disk", got)
	}
}

// A refusal narrows the answer rather than emptying it. Not being able to ask
// leaves the listing as it was before eligibility existed — the volumes that
// hold snapshots, which need no permission to recognise.
func TestAnUnanswerableEligibilityQuestionIsNotAnError(t *testing.T) {
	if got := includedInBackup(context.Background(), &fixedRunner{err: errors.New("no")}, []string{"/"}); len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
}

// fixedRunner answers every command with the same thing.
type fixedRunner struct {
	out string
	err error
}

func (f *fixedRunner) Run(context.Context, string, ...string) (string, error) {
	return f.out, f.err
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

// The cost of a refresh, which is the thing that went wrong in the field.
//
// The window refreshes on a timer, and a full walk per tick meant one
// `diskutil apfs listSnapshots` subprocess per mounted APFS filesystem, for
// ever. Measured on one machine over eleven and a half hours: 12,469 calls,
// sixty-five an hour against each of ten filesystems that can never hold a Time
// Machine snapshot — Preboot, VM, xarts, iSCPreboot, Hardware, the recovery
// mounts and the sealed system volume. About nineteen thousand subprocesses a
// day to answer a question where two volumes could possibly matter.
func TestARefreshDoesNotInterrogateEveryFilesystem(t *testing.T) {
	inner := &volumeRunner{
		mount: "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n" +
			"/dev/disk8s1 on /Volumes/sdcard256gb (apfs, local, journaled)\n" +
			"/dev/disk3s6 on /System/Volumes/VM (apfs, local, journaled)\n" +
			"/dev/disk3s4 on /System/Volumes/Preboot (apfs, local, journaled)\n" +
			"/dev/disk1s2 on /System/Volumes/xarts (apfs, local, journaled)\n" +
			"/dev/disk1s1 on /System/Volumes/iSCPreboot (apfs, local, journaled)\n" +
			"/dev/disk1s3 on /System/Volumes/Hardware (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/System/Volumes/Data":       snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-09-09-061633.local"),
			"/Volumes/sdcard256gb":       snapshotBlocks("disk8s1", "com.apple.TimeMachine.2026-09-09-130736.local"),
			"/System/Volumes/VM":         "No snapshots for disk3s6\n",
			"/System/Volumes/Preboot":    "No snapshots for disk3s4\n",
			"/System/Volumes/xarts":      "No snapshots for disk1s2\n",
			"/System/Volumes/iSCPreboot": "No snapshots for disk1s1\n",
			"/System/Volumes/Hardware":   "No snapshots for disk1s3\n",
		},
		included: map[string]bool{"/System/Volumes/Data": true, "/Volumes/sdcard256gb": true},
	}
	c := NewCache(time.Millisecond)
	at := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)

	// The first answer is a full sweep: nothing is known yet.
	if _, err := c.Volumes(context.Background(), inner, at); err != nil {
		t.Fatal(err)
	}
	if len(inner.asked) != 7 {
		t.Fatalf("the first enumeration asked %d filesystems, want all 7", len(inner.asked))
	}

	// Every refresh after it asks only what can answer. Past the ttl and well
	// inside FullSweep, at the window's own thirty-second cadence, which is the
	// state the application actually lives in.
	inner.asked = nil
	for i := 1; i <= 10; i++ {
		if _, err := c.Volumes(context.Background(), inner, at.Add(time.Duration(i)*30*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	// Two volumes per refresh, not seven. The five that can never hold a snapshot
	// are not asked at all.
	if len(inner.asked) != 20 {
		t.Errorf("ten refreshes cost %d listSnapshots calls, want 20 — two a refresh", len(inner.asked))
	}
	for _, mount := range inner.asked {
		if mount != "/System/Volumes/Data" && mount != "/Volumes/sdcard256gb" {
			t.Errorf("a refresh interrogated %s, which cannot hold a snapshot", mount)
		}
	}
}

// The one state the narrow question cannot see, and the reason the sweep exists:
// a volume excluded from Time Machine that holds snapshots anyway. It must stay
// listed, or its history is unprunable.
func TestTheSweepFindsAnExcludedVolumeThatHoldsSnapshots(t *testing.T) {
	inner := &volumeRunner{
		mount: "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n" +
			"/dev/disk8s1 on /Volumes/sdcard256gb (apfs, local, journaled)\n",
		byPath: map[string]string{
			"/System/Volumes/Data": snapshotBlocks("disk3s1", "com.apple.TimeMachine.2026-09-09-061633.local"),
			"/Volumes/sdcard256gb": "No snapshots for disk8s1\n",
		},
		included: map[string]bool{"/System/Volumes/Data": true},
	}
	c := NewCache(time.Millisecond)
	at := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)

	if _, err := c.Volumes(context.Background(), inner, at); err != nil {
		t.Fatal(err)
	}
	// The excluded card acquires a snapshot from somewhere that is not this
	// application, so nothing here knows to look for it.
	inner.byPath["/Volumes/sdcard256gb"] = snapshotBlocks("disk8s1", "com.apple.TimeMachine.2026-09-09-130736.local")

	// A refresh inside the sweep window does not see it, which is the trade.
	vols, err := c.Volumes(context.Background(), inner, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(WithSnapshots(vols)) != 1 {
		t.Errorf("a narrow refresh found the excluded volume, so this test proves nothing")
	}

	// Past the sweep it does.
	vols, err = c.Volumes(context.Background(), inner, at.Add(FullSweep+time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range WithSnapshots(vols) {
		if v.MountPoint == "/Volumes/sdcard256gb" {
			found = true
		}
	}
	if !found {
		t.Error("the sweep did not find an excluded volume holding snapshots, so its history would never be pruned")
	}
}
