package apfs

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Taking a snapshot of ONE disk, which macOS does not offer.
//
// `tmutil localsnapshot` takes no arguments and writes to every eligible volume,
// so the only way to end up with a snapshot on one disk is to take it everywhere
// and remove the copies nobody wanted. That is a create followed by deletes, and
// deletes are the irreversible half of this application — so what these cover is
// mostly what must NOT be deleted.

// machine is a Mac with disks, snapshots and a tmutil that behaves like the real
// one: creation writes to every eligible volume at once and returns a single
// date, and deletion takes one copy on one volume.
type machine struct {
	// vols is mount point to volume, in the order mount(8) would list them.
	order []string
	vols  map[string]*fakeVolume
	// created is what the next localsnapshot will call itself. Fixed rather than
	// clock-derived so a test can say what it expects.
	created string
	deletes []string
}

type fakeVolume struct {
	device string
	// eligible is whether localsnapshot writes here, which is what
	// `tmutil isexcluded` reports.
	eligible bool
	// snaps is stamp to UUID.
	snaps map[string]string
	// tag is this volume's share of every UUID it hands out, so two disks' copies
	// of one date are different identifiers — which is the whole reason deleting
	// is done by UUID, and a fake that reused one could not catch the wrong copy
	// being deleted. Hex, because DeleteOn refuses anything that is not.
	tag string
}

func newMachine(created string) *machine {
	return &machine{created: created, vols: map[string]*fakeVolume{}}
}

func (m *machine) disk(mount, device string, eligible bool, stamps ...string) *machine {
	v := &fakeVolume{
		device: device, eligible: eligible, snaps: map[string]string{},
		tag: fmt.Sprintf("%08X", len(m.order)+0xA0),
	}
	for i, s := range stamps {
		v.snaps[s] = v.uuid(i + 1)
	}
	m.vols[mount] = v
	m.order = append(m.order, mount)
	return m
}

func (v *fakeVolume) uuid(n int) string {
	return fmt.Sprintf("%s-0000-0000-0000-%012X", v.tag, n)
}

func (m *machine) stamps(mount string) []string {
	var out []string
	for s := range m.vols[mount].snaps {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func (m *machine) Run(_ context.Context, name string, args ...string) (string, error) {
	switch {
	case name == "mount":
		var b strings.Builder
		for _, mount := range m.order {
			fmt.Fprintf(&b, "/dev/%s on %s (apfs, local, journaled)\n", m.vols[mount].device, mount)
		}
		return b.String(), nil

	case name == "tmutil" && len(args) > 1 && args[0] == "isexcluded":
		var b strings.Builder
		for _, item := range args[1:] {
			verdict := "[Excluded]"
			if v, ok := m.vols[item]; ok && v.eligible {
				verdict = "[Included]"
			}
			fmt.Fprintf(&b, "%s\t%s\n", verdict, item)
		}
		return b.String(), nil

	case name == "tmutil" && len(args) == 1 && args[0] == "localsnapshot":
		for _, v := range m.vols {
			if !v.eligible {
				continue
			}
			if _, taken := v.snaps[m.created]; !taken {
				v.snaps[m.created] = v.uuid(len(v.snaps) + 90)
			}
		}
		return "Created local snapshot with date: " + m.created + "\n", nil

	case name == "diskutil" && len(args) == 3 && args[0] == "apfs" && args[1] == "listSnapshots":
		v, ok := m.vols[args[2]]
		if !ok {
			return "", fmt.Errorf("diskutil: no such volume")
		}
		stamps := m.stamps(args[2])
		if len(stamps) == 0 {
			return "No snapshots for " + v.device + "\n", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Snapshots for %s (%d found)\n", v.device, len(stamps))
		for _, s := range stamps {
			fmt.Fprintf(&b, "|\n+-- %s\n", v.snaps[s])
			fmt.Fprintf(&b, "|   Name:        com.apple.TimeMachine.%s.local\n|   XID:         1\n", s)
		}
		return b.String(), nil

	case name == "diskutil" && len(args) == 5 && args[0] == "apfs" && args[1] == "deleteSnapshot" && args[3] == "-uuid":
		m.deletes = append(m.deletes, args[2]+"/"+args[4])
		for _, v := range m.vols {
			if v.device != args[2] {
				continue
			}
			for stamp, uuid := range v.snaps {
				if uuid == args[4] {
					delete(v.snaps, stamp)
					return "Deleted APFS Snapshot\n", nil
				}
			}
			return "", fmt.Errorf("diskutil: no snapshot with that uuid on %s", args[2])
		}
		return "", fmt.Errorf("diskutil: no such volume %s", args[2])
	}
	return "", fmt.Errorf("unexpected %s %v", name, args)
}

// The whole point: ask for a snapshot of the card, end up with one on the card
// and nowhere else, without touching anything that was already there.
func TestASnapshotOfOneDiskIsLeftOnlyOnThatDisk(t *testing.T) {
	m := newMachine("2026-09-07-190412").
		disk("/System/Volumes/Data", "disk3s1", true, "2026-09-01-100000").
		disk("/Volumes/sdcard256gb", "disk8s1", true, "2026-08-20-100000")

	snap, swept, err := CreateOn(context.Background(), m, "disk8s1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Stamp != "2026-09-07-190412" {
		t.Fatalf("got %q, want the new stamp", snap.Stamp)
	}
	if got := m.stamps("/Volumes/sdcard256gb"); strings.Join(got, ",") != "2026-08-20-100000,2026-09-07-190412" {
		t.Errorf("the card holds %v, want its old snapshot and the new one", got)
	}
	// The startup disk got one too — it always does — and it is gone again.
	if got := m.stamps("/System/Volumes/Data"); strings.Join(got, ",") != "2026-09-01-100000" {
		t.Errorf("the startup disk holds %v, want only what it had before", got)
	}
	if len(swept) != 1 || swept[0].Device != "disk3s1" {
		t.Errorf("swept %v, want the startup disk named", swept)
	}
}

// The guard that makes this safe to offer at all.
//
// Only a copy this call produced is ever removed. A stamp that was already on
// another disk is not ours to delete however exactly it matches — and unlike
// everything else here, deleting it could not be undone by pressing the button
// again, because a snapshot records a state of the disk that has passed.
func TestACopyThatWasAlreadyThereIsNeverRemoved(t *testing.T) {
	// The startup disk already holds the very date localsnapshot is about to
	// produce, which cannot happen twice in one second on a real Mac and is
	// exactly why the check costs nothing.
	m := newMachine("2026-09-07-190412").
		disk("/System/Volumes/Data", "disk3s1", true, "2026-09-07-190412").
		disk("/Volumes/sdcard256gb", "disk8s1", true)

	if _, swept, err := CreateOn(context.Background(), m, "disk8s1"); err != nil {
		t.Fatal(err)
	} else if len(swept) != 0 {
		t.Errorf("swept %v, want nothing", swept)
	}
	if got := m.stamps("/System/Volumes/Data"); strings.Join(got, ",") != "2026-09-07-190412" {
		t.Errorf("the startup disk holds %v, want the snapshot it already had", got)
	}
	if len(m.deletes) != 0 {
		t.Errorf("deleted %v, want nothing deleted", m.deletes)
	}
}

// A disk macOS will not snapshot must not cost the machine the snapshot it took.
//
// The volume is listed because it holds snapshots, and localsnapshot skips it
// because Time Machine excludes it. Sweeping on that would take a snapshot
// everywhere, find none on the disk that was asked for, and delete every copy
// that was made — a snapshot taken and then entirely destroyed.
func TestNothingIsSweptWhenTheDiskAskedForGetsNoSnapshot(t *testing.T) {
	m := newMachine("2026-09-07-190412").
		disk("/System/Volumes/Data", "disk3s1", true).
		disk("/Volumes/sdcard256gb", "disk8s1", false, "2026-08-20-100000")

	snap, swept, err := CreateOn(context.Background(), m, "disk8s1")
	if err == nil {
		t.Fatal("want an error saying the disk was not snapshotted")
	}
	// The snapshot is reported even so. It exists, and saying otherwise would
	// send somebody looking for one that is there.
	if snap.Stamp != "2026-09-07-190412" {
		t.Errorf("got %q, want the snapshot that was taken", snap.Stamp)
	}
	if len(swept) != 0 || len(m.deletes) != 0 {
		t.Errorf("swept %v / deleted %v, want nothing touched", swept, m.deletes)
	}
	if got := m.stamps("/System/Volumes/Data"); strings.Join(got, ",") != "2026-09-07-190412" {
		t.Errorf("the startup disk holds %v, want the new snapshot kept", got)
	}
}

// No disk named means the volumes could not be enumerated, and then this is the
// plain machine-wide snapshot. Guessing which disk was meant is the one thing
// that could delete somebody's only new copy.
func TestNoDiskNamedSweepsNothing(t *testing.T) {
	m := newMachine("2026-09-07-190412").
		disk("/System/Volumes/Data", "disk3s1", true).
		disk("/Volumes/sdcard256gb", "disk8s1", true)

	if _, swept, err := CreateOn(context.Background(), m, ""); err != nil {
		t.Fatal(err)
	} else if len(swept) != 0 {
		t.Errorf("swept %v, want nothing", swept)
	}
	for _, mount := range m.order {
		if got := m.stamps(mount); strings.Join(got, ",") != "2026-09-07-190412" {
			t.Errorf("%s holds %v, want the new snapshot", mount, got)
		}
	}
}
