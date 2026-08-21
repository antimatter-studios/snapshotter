package apfs

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The flags diskutil reports about each snapshot: whether macOS may reclaim it on
// its own, and whether it is the one pinning the container's minimum size.
//
// The fixture is diskutil's real phrasing, which matters for the container flag:
// it is not a field with a yes or no but a NOTE line in prose, so the parser
// matches the distinctive part of the sentence. A fixture inventing a tidy
// "Limiting Container: Yes" field would pass against a parser that never sees
// one.
//
// Both matter more than they look. Purgeable is why a retention window is an
// upper bound rather than a promise — on a full disk macOS takes them back
// whatever the policy says — and the container flag names the single snapshot
// holding space that cannot otherwise be freed.

const diskutilOutput = `Snapshots for disk3s1 (3 found)
|
+-- 2026-08-20-120000
|   Name:            com.apple.TimeMachine.2026-08-20-120000.local
|   XID:             1234
|   Purgeable:       Yes
|
+-- 2026-08-19-120000
|   Name:            com.apple.TimeMachine.2026-08-19-120000.local
|   XID:             1233
|   Purgeable:       No
|   NOTE:            This snapshot limits the minimum size of APFS Container disk3
|
+-- not-a-snapshot-name
|   Name:            something-else-entirely
|   Purgeable:       Yes
`

func TestDetailsReadsTheFlagsPerSnapshot(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		"diskutil apfs listSnapshots " + DataVolume: diskutilOutput,
	}}

	got, err := Details(context.Background(), r, DataVolume)
	if err != nil {
		t.Fatalf("listing details: %v", err)
	}

	// Keyed by the full snapshot name, which is what every other call addresses
	// them by.
	purgeable, ok := got["com.apple.TimeMachine.2026-08-20-120000.local"]
	if !ok {
		t.Fatalf("the newest snapshot is missing from %v", keys(got))
	}
	if !purgeable.Purgeable {
		t.Error("a snapshot diskutil calls purgeable was not reported as one")
	}
	if purgeable.LimitsContainer {
		t.Error("a snapshot with no container flag was reported as limiting it")
	}

	pinning, ok := got["com.apple.TimeMachine.2026-08-19-120000.local"]
	if !ok {
		t.Fatal("the older snapshot is missing")
	}
	if pinning.Purgeable {
		t.Error("a snapshot diskutil calls not purgeable was reported as purgeable")
	}
	if !pinning.LimitsContainer {
		t.Error("the snapshot pinning the container was not reported as doing so")
	}
}

// Anything that is not one of this application's snapshots is skipped rather
// than half-read. diskutil lists every snapshot on the volume, including ones
// taken by other software.
func TestDetailsSkipsWhatIsNotOneOfOurs(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		"diskutil apfs listSnapshots " + DataVolume: diskutilOutput,
	}}

	got, err := Details(context.Background(), r, DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("read %d snapshots, want the 2 with parseable names: %v", len(got), keys(got))
	}
	for name := range got {
		if strings.Contains(name, "not-a-snapshot") || strings.Contains(name, "something-else") {
			t.Errorf("kept an entry that is not one of ours: %q", name)
		}
	}
}

// A failure to run diskutil is reported rather than read as "no snapshots have
// any flags", which would show a full disk as having nothing purgeable on it.
func TestDetailsReportsAFailureRatherThanEmptiness(t *testing.T) {
	r := &fakeRunner{err: map[string]error{
		"diskutil apfs listSnapshots " + DataVolume: errors.New("diskutil went missing"),
	}}

	if _, err := Details(context.Background(), r, DataVolume); err == nil {
		t.Fatal("a failed command was read as an empty result")
	}
}

func TestDetailsOnNoSnapshotsIsEmptyRatherThanAnError(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		"diskutil apfs listSnapshots " + DataVolume: "Snapshots for disk3s1 (0 found)\n",
	}}

	got, err := Details(context.Background(), r, DataVolume)
	if err != nil {
		t.Fatalf("a machine with no snapshots reported an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("read %d details from nothing", len(got))
	}
}

// The name a stamp turns into, which is what every delete is addressed to. A
// stamp that is not a date must be refused rather than made into a name that
// matches nothing — or, worse, something.
func TestNameForStampRefusesAnythingThatIsNotADate(t *testing.T) {
	if _, err := NameForStamp("2026-08-20-120000"); err != nil {
		t.Errorf("a real stamp was refused: %v", err)
	}
	for _, bad := range []string{
		"", "yesterday", "2026-08-20", "2026-08-20-12000",
		"2026-08-20-120000 ; rm -rf /", "../../../etc/passwd",
	} {
		if got, err := NameForStamp(bad); err == nil {
			t.Errorf("%q was accepted as %q", bad, got)
		}
	}
}

func keys(m map[string]Detail) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
