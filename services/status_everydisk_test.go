package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"path/filepath"

	"snapshotter/internal/apfs"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/schedule"
)

// Home said "No snapshots — nothing to roll back to" on a machine holding one on
// an external disk, with that snapshot visible in the sidebar at the same moment.
//
// Health counted the data volume alone. The sidebar, the command line and
// retention were all widened when `tmutil localsnapshot` turned out to write to
// every mounted volume; this was not, so the screen somebody opens to find out
// whether they are covered was the last place still answering for one disk.
// Telling them they have nothing while they are looking at something is the
// worst thing it can do, and it is why this has a test of its own rather than
// riding along with the scenario presets, none of which model an external disk.

// oneEmptyOneNot describes a Mac whose startup disk holds nothing and whose card
// holds a snapshot — the exact state that produced the wrong answer.
type oneEmptyOneNot struct{ dataSnapshots, cardSnapshots []string }

func (m oneEmptyOneNot) Run(_ context.Context, name string, args ...string) (string, error) {
	listing := func(device string, names []string) string {
		if len(names) == 0 {
			return "No snapshots for " + device + "\n"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Snapshots for %s (%d found)\n", device, len(names))
		for i, n := range names {
			fmt.Fprintf(&b, "+-- %08X-0000-0000-0000-000000000000\n    Name:        %s\n    Purgeable:   Yes\n", i+1, n)
		}
		return b.String()
	}

	switch {
	case name == "mount":
		return "/dev/disk3s1 on " + apfs.DataVolume + " (apfs, local, journaled)\n" +
			"/dev/disk8s1 on /Volumes/sdcard256gb (apfs, local, nodev)\n", nil
	case name == "tmutil" && len(args) > 1 && args[0] == "isexcluded":
		var b strings.Builder
		for _, item := range args[1:] {
			fmt.Fprintf(&b, "[Included]\t%s\n", item)
		}
		return b.String(), nil
	case name == "tmutil" && len(args) > 0 && args[0] == "listlocalsnapshots":
		return "Snapshots for disk " + apfs.DataVolume + ":\n" + strings.Join(m.dataSnapshots, "\n") + "\n", nil
	case name == "tmutil":
		// destinationinfo and anything else: nothing configured.
		return "No destinations configured", fmt.Errorf("exit status 1")
	case name == "diskutil" && len(args) > 0 && args[0] == "info":
		return "   Volume Name:               sdcard256gb\n   Protocol:                  Secure Digital\n", nil
	case name == "diskutil" && len(args) == 3 && args[1] == "listSnapshots":
		if args[2] == apfs.DataVolume {
			return listing("disk3s1", m.dataSnapshots), nil
		}
		return listing("disk8s1", m.cardSnapshots), nil
	}
	return "", nil
}

func healthOf(t *testing.T, machine oneEmptyOneNot) Health {
	t.Helper()
	agentDir := t.TempDir()
	deps := Deps{
		Runner: machine,
		Volume: apfs.DataVolume,
		Mounts: mountmgr.NewFake(t.TempDir(), t.TempDir()),
		// Asked rather than read from the kernel, so this describes the machine
		// under test and not the one running the test.
		Space: func(string) (uint64, uint64, error) { return 1000, 400, nil },
		// Check reads whether a schedule and a watcher are installed, and both
		// are pointers it dereferences. Given a directory of their own, so a test
		// cannot see or write the developer's real agents.
		Agent: &schedule.Agent{
			Runner: machine, AgentDir: agentDir,
			Program: "/usr/bin/true", LogPath: filepath.Join(agentDir, "log"), UID: os.Getuid(),
		},
		Tripwire: &schedule.Tripwire{
			Runner: machine, AgentDir: agentDir,
			Program: "/usr/bin/true", LogPath: filepath.Join(agentDir, "tripwire.log"), UID: os.Getuid(),
		},
	}
	h, err := NewStatusService(deps).Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	return h
}

func TestHealthCountsSnapshotsOnEveryDisk(t *testing.T) {
	h := healthOf(t, oneEmptyOneNot{
		dataSnapshots: nil,
		cardSnapshots: []string{"com.apple.TimeMachine.2026-09-09-130736.local"},
	})

	if h.SnapshotCount == 0 {
		t.Fatal("reported no snapshots while one exists on another disk")
	}
	if h.Newest == nil {
		t.Error("no newest snapshot, so the screen cannot say how far back cover reaches")
	}
	for _, f := range h.Findings {
		if strings.Contains(strings.ToLower(f.Title), "no snapshot") {
			t.Errorf("raised %q with a snapshot on disk", f.Title)
		}
	}
}

// A machine with genuinely nothing must still say so, or the fix above would
// have replaced one wrong answer with another.
func TestHealthStillSaysWhenNoDiskHasAnything(t *testing.T) {
	h := healthOf(t, oneEmptyOneNot{})

	if h.SnapshotCount != 0 {
		t.Fatalf("counted %d snapshots on a machine with none", h.SnapshotCount)
	}
	var said bool
	for _, f := range h.Findings {
		if strings.Contains(strings.ToLower(f.Title), "no snapshot") {
			said = true
		}
	}
	if !said {
		t.Error("a machine with no snapshots anywhere was not told so")
	}
}
