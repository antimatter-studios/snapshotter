// The two things launchd runs: the scheduled task and the bulk-deletion tripwire.
//
// Neither opens a window and neither needs privileges — tmutil asks backupd to do
// the work — and both report to a log nobody reads until something has already
// been lost.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
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
	pruned, err := schedule.PruneByPolicy(ctx, runner, apfs.DataVolume, policy, time.Now())
	for _, p := range pruned {
		log.Printf("pruned %s", p.Stamp)
	}
	if err != nil {
		return err
	}

	remaining, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		return err
	}
	log.Printf("holding %d snapshots, keeping %s", len(remaining), schedule.Describe(policy))
	return nil
}

// runWatch is the tripwire: it watches the home directory and takes a snapshot
// as soon as something starts deleting in bulk.
//
// It cannot prevent a deletion. FSEvents reports what has already happened, so
// by the time a removal is seen that file is gone. What it prevents is a
// deletion running to completion unwitnessed — trip at the two-hundredth file
// of ten thousand and the rest are still recoverable.
//
// Like the scheduled task it needs no privileges, because tmutil asks backupd
// to do the work.
func runWatch(ctx context.Context, runner apfs.Runner) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find the home directory: %w", err)
	}

	w := watch.New([]string{home}, func(ctx context.Context, where []string) error {
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
	// Read once at startup. The tripwire is its own process and launchd restarts
	// it, so a changed ignore list takes effect on the next run rather than
	// needing anything clever here.
	if cfg, cerr := config.Load(); cerr == nil {
		w.Ignore = cfg.Tripwire.Ignore
	} else {
		log.Printf("configuration: %v (watching everything)", cerr)
	}
	w.Log = log.Printf
	return w.Run(ctx)
}
