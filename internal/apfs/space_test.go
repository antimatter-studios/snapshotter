package apfs

import "testing"

// diskutilListing is a real `diskutil apfs listSnapshots` block, including the
// tree-drawing characters and the NOTE that only appears on one snapshot.
const diskutilListing = `Snapshots for disk3s1 (2 found)
|
+-- F23B1DD5-CD96-4EA0-822D-E75612ACA981
|   Name:        com.apple.TimeMachine.2026-08-13-172036.local
|   XID:         18031176
|   Purgeable:   Yes
|   NOTE:        This snapshot limits the minimum size of APFS Container disk3
|
+-- D5493265-A45C-45B7-AA5F-D1EDB475D645
    Name:        com.apple.TimeMachine.2026-08-14-003200.local
    XID:         18045960
    Purgeable:   No
`

func TestParseDetailsReadsFlagsPerSnapshot(t *testing.T) {
	details := parseDetails(diskutilListing)

	if len(details) != 2 {
		t.Fatalf("got %d snapshots, want 2", len(details))
	}

	older := details["com.apple.TimeMachine.2026-08-13-172036.local"]
	if !older.Purgeable {
		t.Error("the older snapshot is marked Purgeable: Yes and did not parse as purgeable")
	}
	if !older.LimitsContainer {
		t.Error("the older snapshot carries the container NOTE and did not parse as limiting")
	}
	if older.Stamp != "2026-08-13-172036" {
		t.Errorf("stamp = %q, want the bare date", older.Stamp)
	}

	// The NOTE belongs to the first block only. Carrying it forward would name
	// the wrong snapshot as the one worth deleting.
	newer := details["com.apple.TimeMachine.2026-08-14-003200.local"]
	if newer.LimitsContainer {
		t.Error("the NOTE from the first block leaked into the second")
	}
	if newer.Purgeable {
		t.Error("Purgeable: No parsed as purgeable")
	}
}

// The sealed system snapshot appears in some listings and is not a Time Machine
// local snapshot. Including it would put an undeletable entry in the interface.
func TestParseDetailsIgnoresTheSystemSnapshot(t *testing.T) {
	const listing = `Snapshots for disk3s3s1 (1 found)
+-- 09CB8AC7-97C6-4989-B4A3-A863FFAF3511
    Name:        com.apple.os.update-7D0A8EB9C76A76AAF99D0FC872576F61
    XID:         7090234
    Purgeable:   No
`
	if details := parseDetails(listing); len(details) != 0 {
		t.Fatalf("got %d snapshots, want none: %v", len(details), details)
	}
}

// A field arriving before any Name would otherwise be attributed to whatever
// snapshot was parsed last, or panic on an empty map key.
func TestParseDetailsIgnoresFieldsBeforeAnyName(t *testing.T) {
	const listing = `Purgeable:   Yes
+-- 1
    Name:        com.apple.TimeMachine.2026-08-14-003200.local
    Purgeable:   Yes
`
	details := parseDetails(listing)
	if len(details) != 1 {
		t.Fatalf("got %d snapshots, want 1", len(details))
	}
	if _, ok := details[""]; ok {
		t.Error("a field before the first Name was recorded under an empty key")
	}
}

func TestParseDetailsHandlesEmptyOutput(t *testing.T) {
	if details := parseDetails(""); len(details) != 0 {
		t.Fatalf("got %d snapshots from empty output", len(details))
	}
}
