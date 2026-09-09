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

// runScheduledSnapshot is the whole of the scheduled task: make sure this period
// has a snapshot, reap the ones this schedule created and no longer wants, and
// report what happened to the log launchd captures. It needs no privileges,
// because tmutil asks backupd to do the work.
//
// Two changes of principle, both learned the hard way on a real machine.
//
// It ASKS whether a snapshot is needed rather than taking one regardless. A
// person who takes a snapshot by hand at 06:00 has covered the day; a scheduled
// run at 06:16 that takes another has added nothing and, under the old
// behaviour, then deleted one of the two.
//
// And it reaps only the snapshots it created — the managed ones. It used to plan
// over every snapshot on the machine and delete whatever the policy did not
// keep, which meant "one a day" quietly ate a hand-made snapshot the next
// morning for sharing a day with a scheduled one. The person who took it had no
// way to know that would happen and nothing said so afterwards. A snapshot this
// schedule did not create is not its business: not to delete, and not to count
// against the policy either.
//
// What neither change can do is protect anything from macOS. Every local
// snapshot is purgeable and the system reclaims them under space pressure
// without asking, managed or not.
func runScheduledSnapshot(ctx context.Context, runner apfs.Runner, p paths) error {
	// Read first, because it decides whether to create at all. The plist carries
	// it, so the run behaves as it was installed rather than as this binary's
	// defaults would.
	policy, err := schedule.PolicyFromEnv()
	if err != nil {
		// A policy this build cannot read reaps NOTHING rather than reaping on a
		// guess, and still takes a snapshot, because failing to protect the disk
		// is the worse of the two failures.
		log.Print(err)
	}

	dir, err := config.Dir()
	if err != nil {
		return err
	}
	// Once per installation, and exact rather than a guess: this task logs
	// "created <stamp>" and nothing else does, so a stamp in that file was made
	// by this schedule. Without it every snapshot already on disk would be
	// unmanaged and therefore permanent, and upgrading would silently stop the
	// history thinning.
	if err := schedule.Adopt(dir, p.logPath); err != nil {
		log.Printf("could not adopt the existing snapshots, so none will be reaped: %v", err)
	}

	vols, err := apfs.Volumes(ctx, runner)
	if err != nil {
		return err
	}
	existing := apfs.EverySnapshot(vols)

	// Covered by ANY snapshot, not only a managed one. This is the whole point:
	// the question is whether the period has a restore point, and one somebody
	// took themselves answers it exactly as well as one this task took.
	if covering, ok := schedule.Covering(existing, policy, time.Now()); ok {
		log.Printf("nothing to do: %s already covers this period", covering.Stamp)
	} else {
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
		// Recorded before anything is reaped. A crash between the two leaves a
		// snapshot recorded and not yet planned over, which costs nothing; the
		// other order would leave one this task made and will never reap.
		if err := schedule.Manage(dir, snap.Stamp); err != nil {
			log.Printf("could not record %s as this schedule's, so it will not be reaped: %v", snap.Stamp, err)
		}
		// Re-read, because the volumes changed. Reaping against the list from
		// before the create would plan without the snapshot just taken.
		if vols, err = apfs.Volumes(ctx, runner); err != nil {
			return err
		}
		existing = apfs.EverySnapshot(vols)
	}

	// Managed only. Every volume too: localsnapshot writes to all of them, so a
	// date this task created exists on all of them, and reaping one volume's is
	// how the others filled up.
	pruned, err := schedule.ReapManaged(ctx, runner, dir, existing, policy, time.Now())
	for _, p := range pruned {
		log.Printf("reaped %s", p.Stamp)
	}
	if err != nil {
		return err
	}

	for _, v := range vols {
		managed, unmanaged := 0, 0
		set := schedule.Managed(dir)
		for _, s := range v.Snapshots {
			if set[s.Stamp] {
				managed++
			} else {
				unmanaged++
			}
		}
		log.Printf("holding %d snapshots on %s (%d managed, %d left alone), aiming for %s",
			len(v.Snapshots), v.MountPoint, managed, unmanaged, schedule.Describe(policy))
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
