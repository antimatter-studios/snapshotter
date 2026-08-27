// The two things launchd runs: the scheduled task and the bulk-deletion tripwire.
//
// Neither opens a window and neither needs privileges — tmutil asks backupd to do
// the work — and both report to a log nobody reads until something has already
// been lost.
package main

import (
	"context"
	"log"
	"snapshotter/internal/apfs"
	"snapshotter/internal/config"
	"snapshotter/internal/events"
	"snapshotter/internal/i18n"
	"snapshotter/internal/notify"
	"snapshotter/internal/schedule"
	"snapshotter/internal/watch"
	"time"
)

// runScheduledSnapshot is the whole of the scheduled task: take one snapshot,
// drop the ones past the retention window, and report what happened to the log
// launchd captures. It needs no privileges, because tmutil asks backupd to do
// the work.
func runScheduledSnapshot(ctx context.Context, runner apfs.Runner) error {
	snap, err := apfs.Create(ctx, runner)
	if err != nil {
		// A scheduled run that fails is invisible: launchd keeps the output and
		// nobody reads a log until something has already been lost.
		if nerr := notify.Send(ctx, i18n.T("notify.scheduledFailed"), err.Error()); nerr != nil {
			log.Printf("could not post a notification: %v", nerr)
		}
		return err
	}
	log.Printf("created %s", snap.Stamp)

	// The plist carries the policy, so the schedule prunes by whatever it was
	// installed with rather than by whatever this binary's default happens to be.
	policy, err := schedule.PolicyFromEnv()
	if err != nil {
		// A policy this build cannot read prunes NOTHING rather than pruning on a
		// guess. Keeping too much is corrected by the next run; deleting too much
		// is not correctable at all, because a snapshot records a past state of the
		// disk and cannot be recreated.
		log.Print(err)
	}
	// Every volume that holds snapshots, not the data volume: localsnapshot above
	// wrote to all of them, and pruning one of them is how the others filled up.
	pruned, err := schedule.PruneByPolicy(ctx, runner, policy, time.Now())
	for _, p := range pruned {
		log.Printf("pruned %s", p.Stamp)
	}
	if err != nil {
		return err
	}

	// Every volume, because the pruning above covered every volume. Counting the
	// data volume's alone reported a number the run had not acted on, which is
	// the shape of a log line that quietly stops being true.
	vols, err := apfs.Volumes(ctx, runner)
	if err != nil {
		return err
	}
	for _, v := range vols {
		log.Printf("holding %d snapshots on %s, keeping %s",
			len(v.Snapshots), v.MountPoint, schedule.Describe(policy))
	}
	return nil
}

// runWatch is the tripwire: it watches the directories the settings name and
// takes a snapshot as soon as something starts deleting in bulk in one of them.
//
// It cannot prevent a deletion. FSEvents reports what has already happened, so
// by the time a removal is seen that file is gone. What it prevents is a
// deletion running to completion unwitnessed — trip at the two-hundredth file
// of ten thousand and the rest are still recoverable.
//
// It used to watch the whole home directory. That watched ~/Library above all,
// which deletes in bulk as a matter of routine, so most of what it caught was
// housekeeping and each catch pinned another whole-volume snapshot on the disk.
// Now nothing is watched that was not named.
//
// Like the scheduled task it needs no privileges, because tmutil asks backupd
// to do the work.
func runWatch(ctx context.Context, runner apfs.Runner) error {
	// Read once at startup, before anything else: with no directories there is
	// nothing to build a watcher around. The tripwire is its own process and
	// launchd restarts it, so a changed list takes effect on the next run rather
	// than needing anything clever here.
	cfg, cerr := config.Load()
	if cerr != nil {
		// No fallback to the home directory. A settings file that cannot be read
		// says nothing about what someone wanted watched, and the old fallback
		// turned every such failure into watching everything — which is the
		// behaviour this list exists to end.
		log.Printf("configuration: %v", cerr)
	}

	roots := cfg.Tripwire.WatchRoots()
	if len(roots) == 0 {
		// Idle rather than exit. launchd is asked to keep this alive, so exiting
		// would have it relaunched every thirty seconds forever, filling the log
		// with the same line — and someone reading that log would reasonably
		// conclude the tripwire was broken rather than unconfigured.
		log.Print("no directories are configured to watch, so nothing is being watched. " +
			"Add them under \"Watching for bulk deletions\" and install the watcher again.")
		<-ctx.Done()
		return ctx.Err()
	}

	w := watch.New(roots, func(ctx context.Context, where []string) error {
		snap, err := apfs.Create(ctx, runner)
		if err != nil {
			// Recorded even though it failed — especially because it failed. A
			// bulk deletion nobody captured is the thing most worth being able to
			// look back at.
			if eerr := events.Append(events.Event{
				Kind: events.KindBulkDeletion, Where: where,
				Note: "no snapshot was taken: " + err.Error(),
			}); eerr != nil {
				log.Printf("could not record the event: %v", eerr)
			}
			if nerr := notify.Send(ctx, i18n.T("notify.deletingFrom", "Where", watch.Places(where)),
				i18n.T("notify.couldNotSnapshot", "Error", err.Error())); nerr != nil {
				log.Printf("could not post a notification: %v", nerr)
			}
			return err
		}
		log.Printf("created %s", snap.Stamp)

		// Recorded for the window, which is a different process and is almost
		// never running when this fires. Nothing this agent learns can be held in
		// memory for it, so it goes in the file both can read.
		if eerr := events.Append(events.Event{
			Kind: events.KindBulkDeletion, Where: where, Snapshot: snap.Stamp,
		}); eerr != nil {
			log.Printf("could not record the event: %v", eerr)
		}

		// Worth interrupting for: something is deleting in bulk, and the user
		// may not have asked for it.
		// The location is the point. "Something is deleting a lot of files" tells
		// someone to worry; naming the folder tells them whether it is the build
		// directory they just cleaned out or the one with their invoices in it.
		if nerr := notify.Send(ctx, i18n.T("notify.deletingFrom", "Where", watch.Places(where)),
			i18n.T("notify.tookSnapshotAt", "When", snap.Taken.Format("15:04"))); nerr != nil {
			log.Printf("could not post a notification: %v", nerr)
		}
		return nil
	})
	w.Ignore = cfg.Tripwire.Ignore
	// How many deletions count as a burst, in any ONE of the watched directories.
	// An unrecognised name gives the default rather than an error: this comes from
	// a file someone may have typed into, and refusing to watch over a misspelling
	// would trade the protection for the typo.
	sensitivity := watch.Sensitivity(cfg.Tripwire.Sensitivity)
	w.Trigger = watch.NewTrigger(watch.ThresholdFor(sensitivity), 0, 0)
	if !watch.Known(sensitivity) && cfg.Tripwire.Sensitivity != "" {
		log.Printf("sensitivity %q is not one this build knows; using %s",
			cfg.Tripwire.Sensitivity, watch.Balanced)
	}
	w.Log = log.Printf
	return w.Run(ctx)
}
