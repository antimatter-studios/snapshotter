package apfs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeRunner records the commands it is asked to run and replays canned output.
type fakeRunner struct {
	out  map[string]string
	err  map[string]error
	call [][]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.call = append(f.call, append([]string{name}, args...))
	key := strings.Join(append([]string{name}, args...), " ")
	return f.out[key], f.err[key]
}

func (f *fakeRunner) ran(want string) bool {
	for _, c := range f.call {
		if strings.Join(c, " ") == want {
			return true
		}
	}
	return false
}

const listing = `Snapshots for disk /System/Volumes/Data:
com.apple.TimeMachine.2026-08-11-100000.local
com.apple.TimeMachine.2026-08-13-172036.local
com.apple.TimeMachine.2026-08-12-040000.local
`

func TestParseListSkipsHeaderAndSortsNewestFirst(t *testing.T) {
	got := parseList(listing)
	if len(got) != 3 {
		t.Fatalf("want 3 snapshots, got %d: %+v", len(got), got)
	}
	want := []string{"2026-08-13-172036", "2026-08-12-040000", "2026-08-11-100000"}
	for i, w := range want {
		if got[i].Stamp != w {
			t.Errorf("position %d: want %s, got %s", i, w, got[i].Stamp)
		}
	}
}

func TestParseNameRejectsNonSnapshots(t *testing.T) {
	for _, in := range []string{
		"Snapshots for disk /System/Volumes/Data:",
		"com.apple.os.update-8A9F2C1B4D",
		"com.apple.TimeMachine.2026-8-13-172036.local",
		"com.apple.TimeMachine.2026-08-13-172036",
		"2026-08-13-172036",
		"",
	} {
		if _, ok := ParseName(in); ok {
			t.Errorf("accepted %q as a snapshot name", in)
		}
	}
}

func TestParseCreatedIgnoresPurgeableNote(t *testing.T) {
	out := "NOTE: local snapshots are considered purgeable and may be removed at any time by deleted(8).\n" +
		"Created local snapshot with date: 2026-08-13-180230\n"
	stamp, ok := parseCreated(out)
	if !ok || stamp != "2026-08-13-180230" {
		t.Fatalf("want 2026-08-13-180230, got %q (ok=%v)", stamp, ok)
	}
}

// Deleting by anything other than a bare date is the one destructive mistake
// available here: tmutil deletelocalsnapshots given a mount point deletes every
// snapshot on the volume.
func TestDeleteRefusesAnythingButADateStamp(t *testing.T) {
	for _, in := range []string{
		"/",
		"/System/Volumes/Data",
		"com.apple.TimeMachine.2026-08-13-172036.local",
		"2026-08-13-172036 ; rm -rf /",
		"",
	} {
		f := &fakeRunner{}
		if err := Delete(context.Background(), f, in); err == nil {
			t.Errorf("accepted %q as a deletion target", in)
		}
		if len(f.call) != 0 {
			t.Errorf("ran a command for rejected input %q: %v", in, f.call)
		}
	}
}

func TestDeleteUsesTheBareStamp(t *testing.T) {
	f := &fakeRunner{}
	if err := Delete(context.Background(), f, "2026-08-13-172036"); err != nil {
		t.Fatal(err)
	}
	if !f.ran("tmutil deletelocalsnapshots 2026-08-13-172036") {
		t.Fatalf("wrong command: %v", f.call)
	}
}

func TestPruneDeletesOnlySnapshotsPastTheWindow(t *testing.T) {
	f := &fakeRunner{out: map[string]string{
		"tmutil listlocalsnapshots " + DataVolume: listing,
	}}
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.Local)

	pruned, err := Prune(context.Background(), f, DataVolume, 48*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != 1 || pruned[0].Stamp != "2026-08-11-100000" {
		t.Fatalf("want only the 11th pruned, got %+v", pruned)
	}
	if !f.ran("tmutil deletelocalsnapshots 2026-08-11-100000") {
		t.Errorf("did not delete the expired snapshot: %v", f.call)
	}
	if f.ran("tmutil deletelocalsnapshots 2026-08-13-172036") {
		t.Errorf("deleted a snapshot inside the window: %v", f.call)
	}
}

func TestPruneKeepsEverythingWhenWindowIsWide(t *testing.T) {
	f := &fakeRunner{out: map[string]string{
		"tmutil listlocalsnapshots " + DataVolume: listing,
	}}
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.Local)

	pruned, err := Prune(context.Background(), f, DataVolume, 14*24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned inside the retention window: %+v", pruned)
	}
}

func TestCreateReturnsTheNewSnapshot(t *testing.T) {
	f := &fakeRunner{out: map[string]string{
		"tmutil localsnapshot": "Created local snapshot with date: 2026-08-14-013000",
	}}
	s, err := Create(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "com.apple.TimeMachine.2026-08-14-013000.local" {
		t.Errorf("wrong name: %s", s.Name)
	}
	if s.Taken.Year() != 2026 || s.Taken.Month() != time.August || s.Taken.Day() != 14 {
		t.Errorf("wrong date: %s", s.Taken)
	}
}

func TestCreateReportsUnparseableOutput(t *testing.T) {
	f := &fakeRunner{out: map[string]string{"tmutil localsnapshot": "something unexpected"}}
	if _, err := Create(context.Background(), f); err == nil {
		t.Fatal("accepted output with no snapshot date")
	}
}

func TestDestinationInfoTreatsFailureAsNoDestination(t *testing.T) {
	f := &fakeRunner{
		out: map[string]string{"tmutil destinationinfo": "tmutil: No destinations configured."},
		err: map[string]error{"tmutil destinationinfo": errors.New("exit status 1")},
	}
	tm := DestinationInfo(context.Background(), f)
	if tm.HasDestination {
		t.Error("reported a destination when none is configured")
	}
	if tm.Detail == "" {
		t.Error("dropped the tmutil message")
	}
}

func TestDestinationInfoDetectsConfiguredDestination(t *testing.T) {
	f := &fakeRunner{out: map[string]string{
		"tmutil destinationinfo": "Name          : Backups\nKind          : Local\nID            : 0F1E2D3C",
	}}
	if !DestinationInfo(context.Background(), f).HasDestination {
		t.Error("missed a configured destination")
	}
}
