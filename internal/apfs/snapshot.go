// Package apfs wraps the tmutil commands that create, list and delete APFS
// local snapshots.
//
// All three run as the ordinary user: tmutil is a client of backupd, which is
// the privileged process that does the work. Only mounting a snapshot needs
// root, and that lives in the mountmgr package.
package apfs

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DataVolume is the volume holding everything a user owns. The system volume
// is sealed and read-only, so it is never a restore target.
const DataVolume = "/System/Volumes/Data"

const (
	namePrefix = "com.apple.TimeMachine."
	nameSuffix = ".local"

	// stampLayout matches tmutil's zero-padded local-time stamp, which means a
	// lexical sort of stamps is also a chronological one.
	stampLayout = "2006-01-02-150405"
)

// stampPattern guards every value handed back to tmutil. It matters most for
// deletion: `tmutil deletelocalsnapshots` accepts either a stamp or a mount
// point, and given a mount point it deletes every snapshot on that volume. A
// stray "/" reaching that argument would wipe the entire restore history, so
// nothing that fails this pattern is ever passed through.
var stampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{6}$`)

// Snapshot is one Time Machine local snapshot of an APFS volume.
type Snapshot struct {
	// Name is the full identifier, com.apple.TimeMachine.<stamp>.local, and is
	// what mount_apfs expects.
	Name string
	// Stamp is the bare YYYY-MM-DD-HHMMSS date, and is what tmutil
	// deletelocalsnapshots expects. The two commands disagree; keeping both
	// forms on the struct stops callers guessing.
	Stamp string
	// Taken is Stamp parsed in the machine's local zone, which is the zone
	// tmutil wrote it in.
	Taken time.Time
}

// ParseName turns a tmutil snapshot identifier into a Snapshot. It reports
// false for anything that is not a Time Machine local snapshot, which includes
// the header line tmutil prints above its listing and the sealed system
// snapshot (com.apple.os.update-<hash>) that appears when querying "/".
func ParseName(name string) (Snapshot, bool) {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, namePrefix) || !strings.HasSuffix(name, nameSuffix) {
		return Snapshot{}, false
	}
	stamp := strings.TrimSuffix(strings.TrimPrefix(name, namePrefix), nameSuffix)
	if !stampPattern.MatchString(stamp) {
		return Snapshot{}, false
	}
	taken, err := time.ParseInLocation(stampLayout, stamp, time.Local)
	if err != nil {
		return Snapshot{}, false
	}
	return Snapshot{Name: name, Stamp: stamp, Taken: taken}, true
}

// NameForStamp rebuilds the full identifier from a bare stamp.
func NameForStamp(stamp string) (string, error) {
	if !stampPattern.MatchString(stamp) {
		return "", fmt.Errorf("apfs: %q is not a snapshot date", stamp)
	}
	return namePrefix + stamp + nameSuffix, nil
}

// List returns the snapshots of a volume, newest first.
func List(ctx context.Context, r Runner, volume string) ([]Snapshot, error) {
	out, err := r.Run(ctx, "tmutil", "listlocalsnapshots", volume)
	if err != nil {
		return nil, fmt.Errorf("apfs: listing snapshots of %s: %w: %s", volume, err, strings.TrimSpace(out))
	}
	return parseList(out), nil
}

// parseList keeps only well-formed Time Machine snapshot names. tmutil prints a
// "Snapshots for disk <volume>:" header first, and acting on that line produces
// a confusing "is not a valid disk" error further down.
func parseList(out string) []Snapshot {
	var snaps []Snapshot
	for _, line := range strings.Split(out, "\n") {
		if s, ok := ParseName(line); ok {
			snaps = append(snaps, s)
		}
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Taken.After(snaps[j].Taken) })
	return snaps
}

// Create takes a new snapshot and returns it.
func Create(ctx context.Context, r Runner) (Snapshot, error) {
	out, err := r.Run(ctx, "tmutil", "localsnapshot")
	if err != nil {
		return Snapshot{}, fmt.Errorf("apfs: creating snapshot: %w: %s", err, strings.TrimSpace(out))
	}
	stamp, ok := parseCreated(out)
	if !ok {
		return Snapshot{}, fmt.Errorf("apfs: could not find a snapshot date in tmutil output: %q", strings.TrimSpace(out))
	}
	name, err := NameForStamp(stamp)
	if err != nil {
		return Snapshot{}, err
	}
	taken, err := time.ParseInLocation(stampLayout, stamp, time.Local)
	if err != nil {
		return Snapshot{}, fmt.Errorf("apfs: parsing new snapshot date %q: %w", stamp, err)
	}
	return Snapshot{Name: name, Stamp: stamp, Taken: taken}, nil
}

// parseCreated pulls the stamp out of "Created local snapshot with date: <stamp>".
// tmutil sometimes prefixes that with a note about snapshots being purgeable,
// so the whole output is scanned rather than just the first line.
func parseCreated(out string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		idx := strings.LastIndex(line, ": ")
		if idx < 0 {
			continue
		}
		if stamp := strings.TrimSpace(line[idx+2:]); stampPattern.MatchString(stamp) {
			return stamp, true
		}
	}
	return "", false
}

// Delete removes one snapshot, identified by its bare date stamp.
func Delete(ctx context.Context, r Runner, stamp string) error {
	if !stampPattern.MatchString(stamp) {
		return fmt.Errorf("apfs: refusing to delete %q: not a snapshot date", stamp)
	}
	out, err := r.Run(ctx, "tmutil", "deletelocalsnapshots", stamp)
	if err != nil {
		return fmt.Errorf("apfs: deleting snapshot %s: %w: %s", stamp, err, strings.TrimSpace(out))
	}
	return nil
}

// Prune deletes every snapshot older than retain, and returns those deleted.
// Snapshots are purgeable, so the retention window is an upper bound: macOS
// reclaims the oldest under space pressure rather than failing a write.
func Prune(ctx context.Context, r Runner, volume string, retain time.Duration, now time.Time) ([]Snapshot, error) {
	snaps, err := List(ctx, r, volume)
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-retain)
	var pruned []Snapshot
	for _, s := range snaps {
		if !s.Taken.Before(cutoff) {
			continue
		}
		if err := Delete(ctx, r, s.Stamp); err != nil {
			return pruned, err
		}
		pruned = append(pruned, s)
	}
	return pruned, nil
}
