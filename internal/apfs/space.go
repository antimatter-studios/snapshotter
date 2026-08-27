package apfs

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Detail is what diskutil knows about a snapshot that tmutil does not.
//
// APFS does not report a size per snapshot, and no amount of asking will
// produce one: a snapshot shares blocks with the live volume and with its
// neighbours, so "how big is it" has no single answer. These two flags are the
// honest substitute — whether macOS may delete it, and whether it is the one
// holding the container's floor up.
type Detail struct {
	Name  string
	Stamp string
	// UUID identifies this snapshot ON THIS VOLUME, which is what makes deleting
	// one copy possible.
	//
	// `tmutil deletelocalsnapshots <date>` removes that date from every volume
	// holding it, which is right for retention — the policy's verdict on a date is
	// the same everywhere — and wrong for a button beside one row. Deleting the
	// SD card's copy of a date must not take the startup disk's with it.
	// `diskutil apfs deleteSnapshot <device> -uuid` is the only call that can tell
	// them apart.
	UUID string
	// Purgeable reports that macOS may reclaim this snapshot on its own under
	// space pressure. Every Time Machine local snapshot normally is, which is
	// why a retention window is an upper bound rather than a promise.
	Purgeable bool
	// LimitsContainer reports the snapshot diskutil names as limiting the
	// minimum size of the container. That is as close as APFS comes to saying
	// "this is the one costing you space", and it is usually the oldest.
	LimitsContainer bool
}

// Details returns what diskutil reports for each snapshot of a volume, keyed by
// the full snapshot name.
//
// A failure here is not fatal to the caller: the flags decorate a listing that
// tmutil has already produced, so an empty map degrades to showing less rather
// than showing nothing.
func Details(ctx context.Context, r Runner, volume string) (map[string]Detail, error) {
	out, err := r.Run(ctx, "diskutil", "apfs", "listSnapshots", volume)
	if err != nil {
		return nil, fmt.Errorf("apfs: listing snapshot details for %s: %w: %s", volume, err, strings.TrimSpace(out))
	}
	return parseDetails(out), nil
}

// parseDetails reads diskutil's indented listing. The format is one block per
// snapshot, and a block's fields follow its Name line, so the parser carries the
// current snapshot forward rather than trying to match fields to names by
// position.
func parseDetails(out string) map[string]Detail {
	details := make(map[string]Detail)
	var current string
	// pending holds the UUID seen since the last Name. diskutil writes it on the
	// block's opening line — "+-- 85095869-…" — which is not a "Key: value" line
	// and so arrives before there is anything to attach it to.
	var pending string

	for _, line := range strings.Split(out, "\n") {
		if uuid, ok := blockUUID(line); ok {
			pending = uuid
			continue
		}
		key, value, ok := splitField(line)
		if !ok {
			continue
		}
		switch key {
		case "Name":
			snap, ok := ParseName(value)
			if !ok {
				current = ""
				pending = ""
				continue
			}
			current = snap.Name
			// The UUID heads the block, so it was read before the name that keys
			// this map was known.
			details[current] = Detail{Name: snap.Name, Stamp: snap.Stamp, UUID: pending}
			pending = ""
		case "Purgeable":
			if d, ok := details[current]; ok {
				d.Purgeable = strings.EqualFold(value, "Yes")
				details[current] = d
			}
		case "NOTE":
			// diskutil phrases this as prose rather than as a flag, so the
			// distinctive part of the sentence is matched instead of all of it.
			if d, ok := details[current]; ok && strings.Contains(value, "limits the minimum size") {
				d.LimitsContainer = true
				details[current] = d
			}
		}
	}
	return details
}

// uuidPattern matches the identifier diskutil heads each snapshot block with.
// Anchored and shaped, because it is handed back to diskutil as an argument.
var uuidPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// blockUUID reads the UUID off a block's opening line, "+-- <uuid>".
func blockUUID(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "+-- ") {
		return "", false
	}
	uuid := strings.TrimSpace(strings.TrimPrefix(trimmed, "+-- "))
	if !uuidPattern.MatchString(uuid) {
		return "", false
	}
	return uuid, true
}

// splitField pulls "Key: value" out of a line, ignoring the tree-drawing
// characters diskutil indents its blocks with.
func splitField(line string) (key, value string, ok bool) {
	trimmed := strings.TrimLeft(line, " |+-\t")
	idx := strings.Index(trimmed, ":")
	if idx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(trimmed[:idx]), strings.TrimSpace(trimmed[idx+1:]), true
}
