package apfs

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
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
	// Name is the volume's own name, "sdcard256gb" rather than its mount point.
	// It is what a person calls the disk, and what a heading over its snapshots
	// should say.
	Name string
	// Snapshots are its Time Machine local snapshots, newest first.
	//
	// Only those: the sealed system volume carries a com.apple.os.update-<hash>
	// snapshot which is macOS's own and is not ours to count or delete.
	Snapshots []VolumeSnapshot
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

// VolumeSnapshot is one snapshot as it exists on one volume.
//
// The same date can exist on several — `tmutil localsnapshot` writes to all of
// them at once — and each copy is a separate thing that can be deleted on its
// own. So the identity here is the UUID, which is per volume, and not the date,
// which is not.
type VolumeSnapshot struct {
	Snapshot
	// UUID identifies this copy, and is what deletes only this copy.
	UUID string
	// Purgeable reports that macOS may reclaim it without being asked.
	Purgeable bool
	// LimitsContainer reports it as the one holding this container's floor up.
	LimitsContainer bool
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

		vol := Volume{MountPoint: mount, Device: device, Name: volumeName(ctx, r, mount)}
		// diskutil's listing, not tmutil's: the same call answers what is there and
		// what macOS thinks of each one, so the flags cost no second command.
		for _, d := range parseDetails(listing) {
			snap, ok := ParseName(d.Name)
			if !ok {
				continue
			}
			vol.Snapshots = append(vol.Snapshots, VolumeSnapshot{
				Snapshot: snap, UUID: d.UUID,
				Purgeable: d.Purgeable, LimitsContainer: d.LimitsContainer,
			})
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

// volumeName asks diskutil what the volume is called, falling back to the last
// component of its mount point.
//
// A fallback rather than an error: the name is a heading, and a listing that
// refuses to appear because one disk would not say its name is a worse answer
// than a heading reading "sdcard256gb" because that is where it is mounted.
func volumeName(ctx context.Context, r Runner, mount string) string {
	fallback := filepath.Base(mount)
	out, err := r.Run(ctx, "diskutil", "info", mount)
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := splitField(line); ok && key == "Volume Name" && value != "" {
			return value
		}
	}
	return fallback
}

// DeleteOn removes one snapshot from one volume, leaving every other volume's
// copy of the same date alone.
//
// This is the difference between a retention policy and a button. Retention
// decides about a DATE — the policy's verdict is the same on every volume, so
// `tmutil deletelocalsnapshots <date>`, which removes it everywhere, is exactly
// right. A button beside one row is about one COPY, and deleting the SD card's
// snapshot must not take the startup disk's with it.
//
// It needs no privileges. diskutil says "Ownership of the affected disks is
// required", which the console user has for their own disks; this was checked
// against both an internal volume and an external one, and neither raised a
// prompt.
func DeleteOn(ctx context.Context, r Runner, device, uuid string) error {
	if !devicePattern.MatchString(device) {
		return fmt.Errorf("apfs: refusing to delete from %q: not a volume identifier", device)
	}
	if !uuidPattern.MatchString(uuid) {
		return fmt.Errorf("apfs: refusing to delete %q: not a snapshot identifier", uuid)
	}
	out, err := r.Run(ctx, "diskutil", "apfs", "deleteSnapshot", device, "-uuid", uuid)
	if err != nil {
		return fmt.Errorf("apfs: deleting snapshot %s from %s: %w: %s", uuid, device, err, strings.TrimSpace(out))
	}
	return nil
}

// devicePattern guards the volume handed to diskutil, for the same reason
// stampPattern guards a date: it is an argument to a command that deletes, and
// "disk3s1" is the only shape it is ever meant to take.
var devicePattern = regexp.MustCompile(`^disk\d+(s\d+)+$`)

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
			all = append(all, s.Snapshot)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Taken.After(all[j].Taken) })
	return all
}
