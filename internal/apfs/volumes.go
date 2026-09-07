package apfs

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
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
	// Protocol is how the disk is attached, as diskutil words it: "Secure
	// Digital", "Apple Fabric", "USB". Empty when it would not say.
	//
	// Kept because it is the only honest way to decide how many folders to check
	// at once. See Lanes.
	Protocol string
}

// Lanes is how many folder checks this volume can usefully answer at once.
//
// Reading is the whole cost of a folder verdict, so the right number is a
// property of the disk rather than of the Mac. An SD card has one slow channel:
// asking it for twelve directories at once does not make it read faster, it makes
// every one of the twelve arrive late, which is exactly the "nothing is
// happening" the window was accused of. Internal storage is the opposite — the
// queue depth is what keeps it busy, and three was leaving most of it idle.
//
// The numbers are deliberately coarse. There is no measurement here that would
// survive a different Mac, and a wrong-but-conservative number costs time while a
// wrong-but-greedy one costs responsiveness.
func (v Volume) Lanes() int {
	switch {
	case strings.Contains(v.Protocol, "Secure Digital"):
		return 4
	case strings.Contains(v.Protocol, "Apple Fabric"), strings.Contains(v.Protocol, "PCI"):
		return 12
	case strings.Contains(v.Protocol, "USB"), strings.Contains(v.Protocol, "Thunderbolt"):
		return 6
	default:
		// Including the empty string, which is what a disk diskutil would not
		// describe leaves behind. Four is the cautious end: too few is slow, too
		// many is a window that stops answering.
		return 4
	}
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

// Volumes returns every mounted APFS volume that Time Machine would snapshot,
// deduplicated by volume, whether or not it holds any snapshots yet.
//
// Two ways in, and either is enough. A volume Time Machine says it includes is a
// place the next `tmutil localsnapshot` will write, which is worth showing while
// it is still empty — an empty disk is the state somebody is most likely to be
// looking at the application to change, and leaving it out made a plugged-in
// card indistinguishable from one the application could not see. A volume
// holding snapshots is listed regardless of what Time Machine says about it now,
// because those snapshots exist and have to be prunable: excluding a disk after
// the fact must not turn its history into something nothing can ever delete.
//
// Callers that mean strictly "volumes with snapshots on them" — the elevated
// helper's allowlist above all — say so with WithSnapshots.
func Volumes(ctx context.Context, r Runner) ([]Volume, error) {
	out, err := r.Run(ctx, "mount")
	if err != nil {
		return nil, fmt.Errorf("apfs: listing mounted volumes: %w: %s", err, strings.TrimSpace(out))
	}

	mounts := mountedAPFS(out)
	included := includedInBackup(ctx, r, mounts)

	var vols []Volume
	seen := map[string]bool{}
	for _, mount := range mounts {
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
		if len(vol.Snapshots) == 0 && !included[mount] {
			continue
		}
		// After the filter, never before. The name is one more subprocess per
		// volume and it is wanted only for the ones that appear on screen — asking
		// for all twelve mount points a Mac has, most of which hold nothing, was
		// most of the cost of this function.
		vol.Name, vol.Protocol = volumeInfo(ctx, r, mount)
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

// WithSnapshots narrows a volume list to the ones that actually hold snapshots.
//
// Volumes answers a wider question than it used to — it includes eligible
// volumes that are still empty, so the window can show a disk before anything
// has been written to it. Anywhere the older meaning is the load-bearing one,
// this says which meaning is intended rather than leaving it to be inferred from
// a length check written out longhand at each site.
func WithSnapshots(vols []Volume) []Volume {
	out := make([]Volume, 0, len(vols))
	for _, v := range vols {
		if len(v.Snapshots) > 0 {
			out = append(out, v)
		}
	}
	return out
}

// includedInBackup asks Time Machine which of these mount points it would
// snapshot, as the set of the ones it says it includes.
//
// tmutil is the only honest source. There is no property of a mount line that
// separates a snapshot target from the rest: the data volume is nobrowse, so
// hiding nobrowse would hide the main disk, and every other filter lets through
// Preboot, VM, xarts and the recovery mounts — nine rows of macOS plumbing in a
// list of somebody's disks. `tmutil isexcluded` answers for exactly the volumes
// localsnapshot writes to and nothing else.
//
// One call for every path, not one per path. tmutil takes any number of items
// and answers a line each, so the whole mount table costs a single subprocess —
// which matters because this function runs inside an enumeration that was
// already the expensive thing on the screen.
//
// A failure is an empty set, not an error. Not being able to ask leaves the
// listing exactly as it was before this existed — volumes that hold snapshots,
// which need no permission to recognise — rather than emptying a sidebar because
// one subprocess would not run.
func includedInBackup(ctx context.Context, r Runner, mounts []string) map[string]bool {
	included := map[string]bool{}
	if len(mounts) == 0 {
		return included
	}
	out, err := r.Run(ctx, "tmutil", append([]string{"isexcluded"}, mounts...)...)
	if err != nil {
		return included
	}
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "[Included]")
		if !ok {
			continue
		}
		// Everything after the marker, trimmed: tmutil separates them with a tab
		// on some releases and spaces on others, and a mount point can contain
		// spaces of its own — so the split is at the marker and nowhere else.
		if path := strings.TrimSpace(rest); path != "" {
			included[path] = true
		}
	}
	return included
}

// volumeInfo asks diskutil what the volume is called and how it is attached,
// falling back to the last component of its mount point for the name.
//
// A fallback rather than an error: the name is a heading, and a listing that
// refuses to appear because one disk would not say its name is a worse answer
// than a heading reading "sdcard256gb" because that is where it is mounted.
func volumeInfo(ctx context.Context, r Runner, mount string) (name, protocol string) {
	name = filepath.Base(mount)
	out, err := r.Run(ctx, "diskutil", "info", mount)
	if err != nil {
		return name, ""
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := splitField(line)
		if !ok || value == "" {
			continue
		}
		switch key {
		case "Volume Name":
			name = value
		case "Protocol":
			// Taken from the call already being made. How the disk is attached
			// decides how many folders are worth checking at once, and asking a
			// second time for something already on screen would be a subprocess per
			// volume for a string this output is holding.
			protocol = value
		}
	}
	return name, protocol
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

// CreateOn takes a snapshot and leaves it on one volume only, which is the
// closest thing to a per-disk snapshot macOS allows.
//
// There is no per-disk create to call. `tmutil localsnapshot` takes no arguments
// — the binary carries a deleteLocalSnapshotsForDisk: and no create counterpart
// — so one is written to every eligible volume whatever anybody asked for.
// Deletion is the half Apple did make selectable, and DeleteOn removes one copy
// on one volume by UUID. Create, then remove the copies nobody wanted.
//
// The removals are held to a rule that makes this safe to offer: a copy is only
// deleted if the stamp was NOT on that volume before this call. So the worst a
// bug here can do is leave a snapshot behind. It cannot reach a snapshot that
// already existed, which is the only kind whose loss could not be undone by
// pressing the button again — and the enumeration is taken fresh rather than
// from a cache precisely so "before" means before.
//
// An empty device sweeps nothing and is the honest answer to "which disk did you
// mean" when the volumes could not be enumerated: a machine-wide snapshot, which
// is what the command does anyway.
//
// Returns the snapshot, and the volumes the new copy was removed from. A volume
// that would not give it up is reported in the error WITH the snapshot: the
// snapshot was taken, and saying otherwise would send somebody looking for one
// that is there.
func CreateOn(ctx context.Context, r Runner, device string) (Snapshot, []Volume, error) {
	if device == "" {
		snap, err := Create(ctx, r)
		return snap, nil, err
	}

	// Before, so a stamp that somehow already exists elsewhere is never mistaken
	// for one this call produced. Two snapshots cannot share a second in
	// practice, which is exactly why this costs nothing and is worth having: the
	// check is free and the thing it prevents is unrecoverable.
	before, err := Volumes(ctx, r)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("apfs: cannot tell which volumes hold snapshots, so none was taken: %w", err)
	}
	held := map[string]map[string]bool{}
	for _, v := range before {
		stamps := map[string]bool{}
		for _, s := range v.Snapshots {
			stamps[s.Stamp] = true
		}
		held[v.Device] = stamps
	}

	snap, err := Create(ctx, r)
	if err != nil {
		return Snapshot{}, nil, err
	}

	after, err := Volumes(ctx, r)
	if err != nil {
		// The snapshot exists and is returned. Not being able to enumerate is a
		// reason to leave the other disks' copies alone, not a reason to report a
		// snapshot that was taken as one that was not.
		return snap, nil, fmt.Errorf("apfs: %s was taken on every disk, and the copies on the others could not be removed: %w", snap.Stamp, err)
	}

	// Nothing is swept unless the disk somebody asked for actually got one.
	//
	// A volume excluded from Time Machine is still listed here while it holds
	// snapshots, and localsnapshot does not write to it — so pressing its camera
	// would take a snapshot everywhere, find none on the disk that was asked for,
	// and delete every copy that was made. A snapshot taken and then entirely
	// destroyed, which is the one outcome this button must never have.
	var landed bool
	for _, v := range after {
		if v.Device != device {
			continue
		}
		for _, s := range v.Snapshots {
			if s.Stamp == snap.Stamp {
				landed = true
			}
		}
	}
	if !landed {
		return snap, nil, fmt.Errorf("apfs: %s was taken, but not on %s — macOS does not snapshot that disk, so the copies on the others were left alone", snap.Stamp, device)
	}

	var swept []Volume
	var failed []string
	for _, v := range after {
		if v.Device == device {
			continue
		}
		for _, s := range v.Snapshots {
			if s.Stamp != snap.Stamp || held[v.Device][s.Stamp] {
				continue
			}
			// One failure does not abandon the rest. Every copy left behind is a
			// disk quietly keeping something nobody asked for, so the others are
			// still worth removing, and all of them are named at the end.
			if err := DeleteOn(ctx, r, v.Device, s.UUID); err != nil {
				failed = append(failed, fmt.Sprintf("%s (%s)", v.Name, v.Device))
				continue
			}
			swept = append(swept, v)
		}
	}
	if len(failed) > 0 {
		return snap, swept, fmt.Errorf("apfs: %s was taken, and the copy could not be removed from %s",
			snap.Stamp, strings.Join(failed, ", "))
	}
	return snap, swept, nil
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

// Cache holds the volume list for a short while.
//
// Enumerating is not cheap: one `mount`, a `diskutil apfs listSnapshots` per
// mounted APFS filesystem — which includes every snapshot this application has
// opened — and a `diskutil info` for each volume that holds any. Twenty-odd
// subprocesses, several seconds.
//
// That is fine once and catastrophic per call, and per call is what it became:
// translating a path needs to know the volume, and translating happens once per
// directory entry. A listing of two hundred files asked the machine to enumerate
// its disks two hundred times, and the window stopped answering.
//
// The window is the caller that needs this. The command line builds no cache and
// pays the full cost once, which is the right trade for a process that exits.
type Cache struct {
	mu     sync.Mutex
	ttl    time.Duration
	at     time.Time
	vols   []Volume
	cached bool
}

// NewCache builds a cache. A zero or negative ttl gets DefaultVolumeTTL.
//
// Short, because the answer changes when a disk is plugged in or a snapshot is
// opened, and a stale list would offer a volume that has gone or hide one that
// has arrived. Long enough that a single listing enumerates once.
func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultVolumeTTL
	}
	return &Cache{ttl: ttl}
}

// DefaultVolumeTTL is how long a volume list is reused.
const DefaultVolumeTTL = 10 * time.Second

// Volumes returns the cached list, enumerating if it is missing or stale.
//
// A nil Cache enumerates every time, so a caller that has not been given one
// still works rather than having to check.
func (c *Cache) Volumes(ctx context.Context, r Runner, now time.Time) ([]Volume, error) {
	if c == nil {
		return Volumes(ctx, r)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cached && now.Sub(c.at) < c.ttl {
		return c.vols, nil
	}
	vols, err := Volumes(ctx, r)
	if err != nil {
		// The previous answer is kept rather than cleared. A momentary failure to
		// run diskutil is not evidence that the disks have gone, and answering
		// "no volumes" would empty the sidebar and refuse every path translation.
		if c.cached {
			return c.vols, nil
		}
		return nil, err
	}
	c.vols, c.at, c.cached = vols, now, true
	return vols, nil
}

// Forget drops the cached list, for the moments something is known to have
// changed — a snapshot opened or closed, a disk mounted — so the next question
// is answered by the machine rather than by a memory of it.
func (c *Cache) Forget() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cached = false
}
