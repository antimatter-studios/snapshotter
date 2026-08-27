package apfs

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Volume is a mounted APFS volume that holds Time Machine local snapshots.
//
// More than one exists on most machines, which was not always accounted for
// here. `tmutil localsnapshot` takes no arguments at all — not a volume, not a
// flag — so it snapshots every eligible mounted APFS volume at once. This
// package used to create those snapshots and then list, prune and report on the
// data volume alone, so every other volume accumulated snapshots nothing would
// ever delete. On the machine that found it, an SD card sat at 98% full holding
// eight snapshots that existed nowhere else, one of them pinning the container's
// minimum size.
type Volume struct {
	// MountPoint is what tmutil and diskutil are addressed with.
	MountPoint string
	// Device is the volume's own identifier, like "disk8s1".
	//
	// It is the identity rather than the mount point, because two mount points
	// can name one volume: `tmutil listlocalsnapshots /` and the same for
	// /System/Volumes/Data return an identical list, since tmutil answers for
	// the volume group rather than the volume asked about. Deduplicating on the
	// path would count those snapshots twice and prune them twice.
	Device string
	// Snapshots are its Time Machine local snapshots, newest first.
	//
	// Only those: the sealed system volume carries a com.apple.os.update-<hash>
	// snapshot which is macOS's own and is not ours to count or delete.
	Snapshots []Snapshot
	// Purgeable is how many of them macOS may reclaim on its own.
	Purgeable int
	// PinningStamp names the one diskutil reports as holding the container's
	// minimum size up, empty when there is none.
	//
	// Per volume, because the container is per volume: the SD card that found
	// this bug had its own pinning snapshot, on its own container, and the only
	// one ever reported was the boot volume's.
	PinningStamp string
}

// Volumes returns every mounted APFS volume that holds at least one Time Machine
// local snapshot, deduplicated by volume.
//
// A volume with none is left out rather than reported empty: the answer this is
// used for is "what did localsnapshot write to, and what has to be pruned", and
// a volume with nothing on it is neither.
func Volumes(ctx context.Context, r Runner) ([]Volume, error) {
	out, err := r.Run(ctx, "mount")
	if err != nil {
		return nil, fmt.Errorf("apfs: listing mounted volumes: %w: %s", err, strings.TrimSpace(out))
	}

	var vols []Volume
	seen := map[string]bool{}
	for _, mount := range mountedAPFS(out) {
		// diskutil rather than tmutil, for the device identifier. tmutil names no
		// volume in its output, so there would be nothing to deduplicate on.
		listing, err := r.Run(ctx, "diskutil", "apfs", "listSnapshots", mount)
		if err != nil {
			// Skipped rather than fatal. A volume that cannot be interrogated —
			// unmounted a moment ago, or one diskutil declines to answer for — must
			// not stop the volumes that can be from being pruned.
			continue
		}
		device, ok := snapshotListDevice(listing)
		if !ok || seen[device] {
			continue
		}
		seen[device] = true

		vol := Volume{MountPoint: mount, Device: device}
		// diskutil's listing, not tmutil's: the same call answers what is there and
		// what macOS thinks of each one, so the flags cost no second command.
		for _, d := range parseDetails(listing) {
			snap, ok := ParseName(d.Name)
			if !ok {
				continue
			}
			vol.Snapshots = append(vol.Snapshots, snap)
			if d.Purgeable {
				vol.Purgeable++
			}
			if d.LimitsContainer {
				vol.PinningStamp = d.Stamp
			}
		}
		if len(vol.Snapshots) == 0 {
			continue
		}
		// parseDetails hands back a map, so the order it arrives in is not one.
		sort.Slice(vol.Snapshots, func(i, j int) bool {
			return vol.Snapshots[i].Taken.After(vol.Snapshots[j].Taken)
		})
		vols = append(vols, vol)
	}

	// By device, so two runs over one machine read the same way. The order is
	// otherwise the mount table's, which is not stable across a remount.
	sort.Slice(vols, func(i, j int) bool { return vols[i].Device < vols[j].Device })
	return vols, nil
}

// mountedAPFS pulls the mount points of every APFS filesystem out of mount(8).
//
// The line shape is "<device> on <mount point> (apfs, local, journaled)", and
// the mount point can contain spaces, so it is taken as everything between
// " on " and the last " (" rather than by splitting on whitespace.
func mountedAPFS(out string) []string {
	var mounts []string
	for _, line := range strings.Split(out, "\n") {
		on := strings.Index(line, " on ")
		open := strings.LastIndex(line, " (")
		if on < 0 || open <= on {
			continue
		}
		options := line[open+2:]
		if !strings.HasPrefix(options, "apfs,") && !strings.HasPrefix(options, "apfs)") {
			continue
		}
		if mount := strings.TrimSpace(line[on+4 : open]); mount != "" {
			mounts = append(mounts, mount)
		}
	}
	return mounts
}

// snapshotListDevice reads the volume out of diskutil's first line, which is
// either "Snapshots for disk8s1 (14 found)" or "No snapshots for disk3s6".
func snapshotListDevice(listing string) (string, bool) {
	for _, line := range strings.Split(listing, "\n") {
		idx := strings.Index(line, " for ")
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+5:])
		if cut := strings.Index(rest, " "); cut > 0 {
			rest = rest[:cut]
		}
		if strings.HasPrefix(rest, "disk") {
			return rest, true
		}
	}
	return "", false
}

// EverySnapshot is the union of the local snapshots on every volume, newest
// first, with each date appearing once.
//
// The union rather than one volume's list, because that is what pruning has to
// decide over. `tmutil deletelocalsnapshots <date>` removes that date wherever
// it lives, so a date is kept or dropped everywhere at once — but a date can
// only be dropped if something knows it is there. Snapshots reaching a volume
// this never listed were invisible, and therefore permanent.
//
// They diverge in practice because snapshots are purgeable: macOS reclaims them
// per volume under space pressure, so a date it drops from a full volume before
// the retention window expires survives on every other volume, unseen.
func EverySnapshot(vols []Volume) []Snapshot {
	seen := map[string]bool{}
	var all []Snapshot
	for _, v := range vols {
		for _, s := range v.Snapshots {
			if seen[s.Stamp] {
				continue
			}
			seen[s.Stamp] = true
			all = append(all, s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Taken.After(all[j].Taken) })
	return all
}
