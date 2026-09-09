package schedule

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"snapshotter/internal/apfs"
)

// A retention policy reaps only the snapshots it created: the managed ones.
//
// It used to reconcile: every run planned over every snapshot on the machine and
// deleted whatever the policy did not keep. That is defensible for a history the
// schedule owns entirely, and wrong the moment somebody takes one themselves —
// a snapshot taken by hand is a deliberate act, and "one a day" would quietly
// eat it the next morning because it shared a day with the scheduled one. The
// person who took it had no way to know that, and nothing said so afterwards.
//
// So the schedule keeps a record of the stamps it created — the managed set — and
// prunes within it. Everything else is unmanaged and left entirely alone. A snapshot it did not make is not its business: not to delete, and
// not to count against the policy either.
//
// The record is a plain list of stamps, one per line. Not JSON, not a database:
// it is read by a scheduled task that must never fail to take a snapshot because
// a file would not parse, and it is worth being readable by anyone wondering why
// a snapshot did or did not go away.
const managedFile = "managed-snapshots"

// ManagedPath is where the record lives, given the configuration directory.
func ManagedPath(dir string) string { return filepath.Join(dir, managedFile) }

// Managed reads the stamps the schedule created, which are the ones it may reap.
//
// A missing file is an empty set and not an error: that is what a fresh
// installation looks like, and it is also the safe answer — an empty set owns
// nothing, so nothing is pruned. Every failure here has to fall that way. The
// asymmetry is the whole reason this is careful: keeping too much is corrected
// by the next run, and a snapshot deleted by mistake cannot be recreated,
// because it recorded a state of the disk that has passed.
func Managed(dir string) map[string]bool {
	managed := map[string]bool{}
	f, err := os.Open(ManagedPath(dir))
	if err != nil {
		return managed
	}
	defer f.Close()

	scan := bufio.NewScanner(f)
	for scan.Scan() {
		// Only well-formed stamps. A truncated write or an edited file must not
		// turn a partial line into an argument for a command that deletes.
		if stamp := strings.TrimSpace(scan.Text()); apfs.IsStamp(stamp) {
			managed[stamp] = true
		}
	}
	return managed
}

// Manage records that the schedule created this stamp, making it managed.
//
// Appended rather than rewritten, so a crash midway leaves the file short by one
// line instead of empty. Short means one snapshot the schedule will not prune,
// which is the harmless direction.
func Manage(dir, stamp string) error {
	if !apfs.IsStamp(stamp) {
		return fmt.Errorf("schedule: refusing to record %q as a snapshot this schedule created: not a date stamp", stamp)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(ManagedPath(dir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, stamp)
	return err
}

// Forget drops stamps from the record, for the ones that no longer exist.
//
// Without this the file grows forever and, worse, would eventually claim
// management of a date some future snapshot happens to reuse. Called with the
// stamps that are still on disk, so the record converges on reality.
func Forget(dir string, keep map[string]bool) error {
	managed := Managed(dir)
	var lines []string
	for stamp := range managed {
		if keep[stamp] {
			lines = append(lines, stamp)
		}
	}
	sort.Strings(lines)

	// Written beside and renamed, so a reader never sees a half-written file.
	tmp := ManagedPath(dir) + ".tmp"
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, ManagedPath(dir))
}

// Adopt seeds the record from a log the scheduled task has already written,
// once, for an installation that predates the record existing.
//
// Without it every snapshot already on disk would be unowned and therefore
// permanent, and the history would stop thinning for everyone upgrading. With
// it the answer is exact rather than a guess: the scheduled task logs
// "created <stamp>" and nothing else does — the tripwire writes to a log of its
// own and the window writes to neither — so a stamp in that file was created by
// this schedule.
//
// Does nothing if the record already exists. Adopting twice would be harmless,
// being a set, but "run once at first use" is easier to reason about than "runs
// every time and happens not to matter".
func Adopt(dir, logPath string) error {
	if _, err := os.Stat(ManagedPath(dir)); err == nil {
		return nil
	}
	f, err := os.Open(logPath)
	if err != nil {
		// No log is not a failure. It means there is nothing to adopt, and an
		// empty record manages nothing, which is the safe end.
		return nil
	}
	defer f.Close()

	var stamps []string
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		_, rest, found := strings.Cut(scan.Text(), " created ")
		if !found {
			continue
		}
		if stamp := strings.TrimSpace(rest); apfs.IsStamp(stamp) {
			stamps = append(stamps, stamp)
		}
	}
	if len(stamps) == 0 {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sort.Strings(stamps)
	body := ""
	for _, s := range stamps {
		body += s + "\n"
	}
	return os.WriteFile(ManagedPath(dir), []byte(body), 0o644)
}

// Covering reports the snapshot that already satisfies the current period, if
// one exists.
//
// This is what turns the scheduled task from "take a snapshot" into "make sure
// there is one". The period is the first tier's, because that is the tier the
// present falls in — "one a day for 14 days, then one a week" means today wants
// a snapshot today, and what happens to it in three weeks is a question for
// then.
//
// EVERY snapshot counts, managed or not. The question is whether this period has
// a restore point, and one somebody took themselves answers it exactly as well
// as one this task took. Answering otherwise is how a hand-made snapshot at
// 06:00 got a redundant scheduled one at 06:16 beside it, and then lost the
// coin toss between them.
//
// A policy with no usable tier covers nothing, so a snapshot is taken. That is
// the safe end: an unreadable policy must not be a reason to stop protecting the
// disk.
func Covering(snaps []apfs.Snapshot, policy Policy, now time.Time) (apfs.Snapshot, bool) {
	tiers := policy.Tiers
	if len(tiers) == 0 {
		return apfs.Snapshot{}, false
	}
	every := tiers[0].Every
	if every <= 0 {
		// A keep-everything tier states no period, so there is nothing to be
		// "already covered" for and a snapshot is always taken. The cadence is
		// then launchd's alone, which is what a flat policy asks for.
		return apfs.Snapshot{}, false
	}

	current := BucketStart(now, every)
	for _, s := range snaps {
		if BucketStart(s.Taken, every) == current {
			return s, true
		}
	}
	return apfs.Snapshot{}, false
}

// ReapManaged deletes the snapshots this schedule created and no longer wants,
// and nothing else.
//
// existing is every snapshot on the machine; the plan is made over the managed
// subset of it. Planning over all of them is what this replaced, and it deleted
// people's own snapshots for sharing a period with a scheduled one.
//
// Deletion is still by date, which removes it from every volume holding it. That
// is right for these: a managed date was created by `tmutil localsnapshot`,
// which wrote it to every eligible volume at once, so every copy of it is this
// schedule's to reap.
//
// The record is trimmed to what remains afterwards, so it converges on reality
// rather than growing forever and eventually claiming a date that some future
// snapshot happens to reuse.
func ReapManaged(ctx context.Context, r apfs.Runner, dir string, existing []apfs.Snapshot, policy Policy, now time.Time) ([]apfs.Snapshot, error) {
	managed := Managed(dir)

	var mine []apfs.Snapshot
	for _, s := range existing {
		if managed[s.Stamp] {
			mine = append(mine, s)
		}
	}
	if len(mine) == 0 {
		return nil, nil
	}

	deleted, err := prune(ctx, r, mine, policy, now)

	// Trimmed from what the plan says survives rather than by re-enumerating the
	// disks: a delete that failed leaves its stamp on disk and in the record,
	// which is correct, and re-reading would cost another dozen subprocesses.
	gone := map[string]bool{}
	for _, d := range deleted {
		gone[d.Stamp] = true
	}
	keep := map[string]bool{}
	for _, s := range existing {
		if !gone[s.Stamp] {
			keep[s.Stamp] = true
		}
	}
	if ferr := Forget(dir, keep); ferr != nil && err == nil {
		err = ferr
	}
	return deleted, err
}
