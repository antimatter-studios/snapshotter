# Decisions

Written during the first build, 2026-08-13/14. Each entry records what was
chosen and why, so the reasoning survives without the conversation.

## Shell out to tmutil rather than call fs_snapshot_create

`fs_snapshot_create()` is declared in `sys/snapshot.h` and fails with EPERM as an
ordinary user. `tmutil` succeeds, because it is an XPC client of `backupd` and
the daemon does the privileged work.

Calling the syscall directly would mean a root helper tool for *every* operation.
Going through `tmutil` means the application needs no privileges at all except
for mounting. That single decision removes a signed privileged helper, an
`SMAppService` registration, and its re-signing burden when certificates roll.

## Delete only ever takes a bare date stamp

`tmutil deletelocalsnapshots` accepts either a snapshot date **or a mount
point** — and given a mount point it deletes every snapshot on that volume. A
stray `/` reaching that argument would destroy the entire restore history.

So `apfs.Delete` validates against `^\d{4}-\d{2}-\d{2}-\d{6}$` and refuses
anything else before running a command. `TestDeleteRefusesAnythingButADateStamp`
covers `/`, `/System/Volumes/Data`, the full `com.apple.TimeMachine.*.local`
form, and a shell-injection attempt, and asserts that no command ran at all.

Note the asymmetry, which is easy to get wrong: **delete** wants the bare stamp,
**mount_apfs** wants the full name. `Snapshot` carries both so callers never guess.

## The listing header line is filtered, not skipped by position

`tmutil listlocalsnapshots` prints `Snapshots for disk <volume>:` first. Anything
that takes it for a snapshot name produces a baffling `is not a valid disk` error
further downstream. Rather than dropping line 1 by position, every line must
parse as `com.apple.TimeMachine.<stamp>.local`. That also rejects the sealed
system snapshot (`com.apple.os.update-<hash>`), which appears when querying `/`
and is not a restore point.

## One authorization prompt per batch

`Manager.Mount` takes a slice, not a single name, and builds one shell script for
the whole batch. Mounting something already mounted costs no prompt at all,
because the mount table is checked first.

The privileged script joins its commands with `;` rather than `&&`, so one
snapshot failing does not block the rest — and success is then decided by
re-reading the mount table, not by the script's exit status, which only reports
its last command.

## Mount state is read, never remembered

`isMountPoint` compares a directory's device number with its parent's. Nothing is
cached in memory, so mounts left behind by a previous run of the application are
recognised on the next launch instead of being invisible and unmountable.

## Restores are non-destructive by default

A restore usually happens in order to *compare* — the file on disk may well be
the one worth keeping. So the default writes `<path>.restored-<snapshot-date>`
and leaves the original untouched. `Replace` mode moves the current file to
`.bak-<date>` before writing. Neither mode deletes anything, and repeating a
restore appends a counter rather than overwriting the previous copy.

The snapshot's own date tags the copy, so a file recovered from last Tuesday says
so in its name.

## Two comparison depths, because they answer different questions

`diffs.Level` merges one directory from each side — two directory reads, instant
on any tree — and backs the browser, where each entry needs a verdict as you walk
around.

`diffs.Compare` walks recursively and answers "what did I lose anywhere under
here", which is the question after a deletion of unknown extent. It reports a
directory missing from one side as a *single* row naming that directory, rather
than expanding it, so losing a large tree produces one legible row instead of
thousands that bury it.

Shallow comparison (size + mtime) is the default; deep comparison hashes
contents. Both blind spots are covered by tests: shallow misses a same-length
rewrite with a preserved timestamp, deep sees it; shallow reports a
timestamp-only difference as changed, deep does not.

A test fixture stamps a fixed mtime on every file it creates, because a real
snapshot shares blocks with the live volume and so has *identical* timestamps —
building two trees seconds apart would make every file look modified for a reason
that never happens in production.

## The scheduled task is the same binary

`--take-snapshot` takes a snapshot, prunes, logs, and exits before any window is
created. One binary to install, no shell script to keep in step with the Go code,
and the scheduled path is testable directly:

```sh
./bin/snapshotter --take-snapshot
```

Retention travels in the plist as `SNAPSHOTTER_RETENTION_HOURS`, so changing it
rewrites a file rather than rebuilding the binary.

## The schedule detects a conflicting agent

A second LaunchAgent taking snapshots would double the rate and apply two
retention windows to one shared set. `Status` scans `~/Library/LaunchAgents` for
other plists mentioning `localsnapshot`, `apfs-snapshot` or `tmutil`.

Matching on `localsnapshot` alone was a real bug, caught by a test: a plist that
drives a shell script names only the script, so the standalone
`com.christhomas.apfs-snapshot` agent — the one most likely to be installed on
this machine — has its `tmutil` calls inside `~/bin/apfs-snapshot`, not in the
plist at all.

## Retention must exceed the interval

`Install` refuses a retention window shorter than the interval, which would
delete each snapshot about as fast as it was taken and leave nothing to restore
from. It also refuses intervals under a minute.

## Default 6 hours, 14 days

Days of depth against an accidental deletion, not intra-day granularity. Each
snapshot pins another generation of any large file rewritten between them, and
this machine routinely rewrites 30 GB disk images — hourly snapshots would pin
24 generations a day of whatever was being rebuilt.

Retention is an upper bound rather than a reservation: snapshots are purgeable,
and macOS reclaims the oldest under space pressure rather than failing a write.
So a heavy image-building day shortens history instead of breaking something.

## Time Machine's state is surfaced, not assumed

Every snapshot named `com.apple.TimeMachine.*.local` belongs to `backupd`. With
no destination configured, backupd never runs a cycle and never thins them, so a
14-day window holds. Attach a destination and it thins local snapshots to roughly
24 hours as part of each backup.

The overview therefore checks `tmutil destinationinfo` and warns, because
otherwise the retention the settings screen displays would be a promise the
system quietly breaks.

**Corrected 2026-08-14.** This entry used to say `tmutil` exits non-zero when
nothing is configured. It does not, at least on macOS 26: it prints "No
destinations configured." and exits **zero**. The code was right anyway because it
tested the message as well as the status, but the stated reason was wrong — and a
decision record that explains a correct behaviour with a false fact invites
somebody to simplify it back to the bug. Both signals are read as "no
destination", and a positive answer requires a Name or an ID in the output rather
than merely the absence of a failure.

## macOS only

APFS snapshots, `tmutil`, `mount_apfs`, `launchd`, `osascript`. The
ios/android/windows/linux build directories were deleted rather than left to rot,
and `build/ios` was breaking `go build ./...` in any case.

## Mounting elevates this binary, not mount_apfs

`mount_apfs` on a Time Machine local snapshot fails with

```
mount_apfs: volume could not be mounted: Operation not permitted (77)
```

even as root. Root is necessary and **not sufficient**: a snapshot of the data
volume holds the user's files, so reading it is gated by TCC as well, and the
message is misleading because nothing about file ownership is involved.

**The identity TCC judges is that of the binary being elevated.** Not the
application that asked, and not `osascript` that carried the request. Elevating
`/sbin/mount_apfs` asks a stock Apple binary to do the work, and it carries none
of our identity, so no way of aiming a Full Disk Access grant at this application
can ever reach it.

So `Manager` elevates **this binary**, re-invoked as its own helper
(`--mount-helper`), and calls `mount_apfs` from there. The call then carries our
own code signature and the grant applies.

### How this was arrived at, including two wrong turns

Worth recording, because both wrong turns were reasonable and one of them
recommended a much larger change than was needed.

1. Granting Full Disk Access to the bundle and relaunching **did not fix it**. The
   authorization dialog succeeded — no `ErrCancelled` — and the mount was refused
   afterwards. Correct observation.
2. The conclusion drawn from it was that TCC attributes the call to
   `/usr/bin/osascript`, and therefore that the fix had to be an `SMAppService`
   helper with its own stable identity. **Both halves were wrong.** `osascript` is
   only the conduit; what matters is the process it execs. And because the fix is
   to exec *ourselves*, no persistent privileged daemon and no separate signing
   identity is required.
3. What settled it was Chris's `diskcutter` project, which does privileged
   raw-device I/O through the *same* `osascript` route and works on this machine.
   Its FAQ is explicit that its helper cannot touch removable raw devices until the
   **bundle** is granted Full Disk Access — proving a grant does reach an
   osascript-elevated process, provided that process is your own binary. A working
   example on the same machine beat reasoning from the API surface.

The general lesson is the one worth keeping: two independent runs agreeing that
"the app's path is refused" said nothing about *why*, because both went through the
same route. Agreement between identical experiments is not corroboration.

### Do not add a Full Disk Access pre-check

The tempting shortcut is to `stat` the TCC database before mounting and refuse
early with a friendlier message. It does not work: `stat` succeeds while *reading*
is refused, so it yields a false pass, and `diskcutter` records having tried it and
dropped it because it "was unreliable and intercepted legitimate retries".

Letting the helper attempt the mount and report honestly is the correct shape,
which is what `classifyMount` and `ErrNeedsFullDiskAccess` already do.

### Why the helper re-validates arguments it was already given

The helper is a **root-privileged entry point taking arguments from argv**, where
before the command was built in-process from values already checked. Crafted names
were in fact already refused transitively — `mountScript` calls `MountPoint`, which
calls `apfs.ParseName` — and `quoteAll`'s single-quoting already closed injection.

So the re-validation fixes no hole. It puts the guard *local to the privileged
entry point* rather than three calls away, where a later edit to `mountScript`
could remove it without anyone noticing. Defence in depth, not a fix. The volume is
pinned to the data volume, which is the only value anything ever passes.

### Consequences

- Real mounts work only from a **packaged `.app` that has been granted Full Disk
  Access**. Under `go run`, `wails3 task run` or a bare binary the identity is not
  the bundle's and mounting is refused — correctly, and it will read as a
  regression the first time somebody meets it in development.
- TCC keys a grant for an ad-hoc signed bundle to its **cdhash**, and every
  `codesign --force --sign -` produces a new one. So for development builds the
  grant must be given *after* the final package and re-given after each rebuild.
  "Grant it once" is true only once the application has a stable signing identity,
  which is why that remains a prerequisite rather than a distribution nicety.
- A grant aimed at a bundle on removable media is not durable either, which is a
  second reason the application should not live on the SD card.

## Mounts can be faked, so browsing does not wait on any of that

Nothing in `vfs`, `diffs` or `restore` knows how the tree under a mountpoint got
there. `mountmgr.Fake` populates a directory instead of calling `mount_apfs`,
which makes the whole browse and compare surface exercisable — and testable in CI
— without root and without a password.

It is deliberately awkward to enable: `SNAPSHOTTER_FAKE_MOUNTS=1`, a marker file
inside every fake mountpoint, a refusal to remove any directory lacking that
marker, and a banner in the interface. Fake data reaching a real *Replace* restore
would overwrite real files with invented ones, so `Replace` is refused outright
while the mode is on.

The stand-in is sealed **read-only** once populated, and unsealed again before it
is removed. A real snapshot is mounted read-only, so without this, code that wrote
into a snapshot would pass against the stand-in and fail against a mount — the one
respect in which a populated directory was still unlike a mount. Closing it is what
makes work built on the stand-in trustworthy while mounting itself stays unverified:
everything that merely walks and compares two directory trees is now genuinely
covered, and only mounting is not.

## Open questions for the morning

1. **Mounting is fixed but unproven.** The cause is understood and the fix is in
   — this binary is elevated rather than `mount_apfs`, so the grant can reach it.
   What has not happened yet is a real mount: that needs Full Disk Access granted
   to the freshly packaged bundle, and re-granted after each rebuild while the
   signature is ad-hoc. Until somebody does that and opens a snapshot, browse,
   compare and restore remain exercised only against `mountmgr.Fake`.

2. **The name.** "Snapshotter" collides with the containerd/Kubernetes term of
   art. Nothing is installed yet, so renaming is cheap now and expensive later —
   the launchd `Label` identifies an installed agent, so renaming after an
   install orphans it.
3. **Where the source lives, and the missing remote.** `/Volumes/sdcard256gb` is
   `/dev/disk5s1` — APFS on removable Secure Digital media, while the root
   volume is `disk3s3s1`. The project is genuinely off the snapshotted volume,
   on a card that gets reflashed and physically pulled. There is a local git
   repository but **no remote**, so this tree is still the only copy. Pushing it
   somewhere is the first thing to do in the morning.
4. **Per-path snapshots are impossible, and a separate volume does not fix it.**
   An APFS volume in the same container does share free space, does take
   `-reserve` and `-quota`, and does mount at any path — all confirmed. But
   `tmutil localsnapshot` takes no volume argument, and `diskutil apfs` offers
   `listSnapshots` and `deleteSnapshot` with **no create verb**. There is no
   user-space way to snapshot an arbitrary volume; only `fs_snapshot_create`,
   which needs root and would break this application's one real architectural
   commitment.

   So moving a folder to its own volume cannot give it "its own schedule". It
   removes it from snapshot coverage and offers nothing to put back. That is
   still useful — a folder of large, frequently rewritten files stops pinning a
   new generation on every snapshot — but it is an *exclusion*, and describing
   it as protection would be a lie that costs someone their files.
5. **Authorization model — partly settled.** Per-action `osascript` prompts are
   still what ships, and the earlier recommendation of an `SMAppService` helper is
   **withdrawn**: elevating this binary achieves the same access without a
   persistent privileged daemon or a second signing identity. What `SMAppService`
   would still buy is one prompt at install instead of one per batch, which is a
   comfort rather than a capability. A stable signing identity remains needed, for
   the cdhash reason above.
