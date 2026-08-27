# Snapshotter

A macOS desktop application for APFS local snapshots: browse them, compare them
against the live disk, restore files out of them, and keep them being taken —
without a backup disk.

![Home: whether this Mac currently has usable restore points, and what is wrong if
it does not](assets/screenshots/home.png)

| Browsing a snapshot, read-only | Scheduling and retention |
| --- | --- |
| [![A mounted snapshot's contents beside the live disk](assets/screenshots/browse.png)](assets/screenshots/browse.png) | [![How often snapshots are taken and what is kept](assets/screenshots/options.png)](assets/screenshots/options.png) |

> **Status: pre-1.0.** Every path has now been run against a real system, mounting
> included. What stands between this and 1.0 is listed in
> [Known limits](#known-limits).

## Install

```sh
brew install --cask antimatter-studios/tap/snapshotter
```

That installs the application and puts `snapshotter` on PATH. The command line
is not a second copy: it is a symlink to the executable inside the bundle, so
`snapshotter version` and the window can never disagree, and — more usefully —
running it uses the application's identity. macOS attributes Full Disk Access to
whichever executable makes the call, so the grant below covers both ways of
running it. A separately installed copy would have needed its own.

The command line alone is enough to take snapshots, list them and check whether
this Mac is protected. Browsing or restoring from one mounts a filesystem, which
is what needs that grant.

Anything the window does not offer is set in `$HOME/.config/snapshotter/config.yaml`.
`snapshotter config` says where that file is and what is in it; `snapshotter config
--write` creates it with the defaults to edit. Single settings can be read and
changed by name, which is the scriptable way in:

```sh
snapshotter config keys                              # everything that can be set
snapshotter config get schedule.interval_hours       # 6
snapshotter config set schedule.interval_hours 3
```

Then grant it Full Disk Access, which mounting a snapshot cannot work without:

> System Settings → Privacy & Security → Full Disk Access → add Snapshotter

Root alone is not sufficient. macOS checks that permission against the
application making the call, so until it is granted every attempt to open a
snapshot is refused with `Operation not permitted`. Opening one also asks for an
administrator password, once per batch.

Releases are signed with Developer ID, notarized and stapled, so nothing has to be
right-clicked past Gatekeeper. How that is produced is in
[docs/RELEASING.md](docs/RELEASING.md).

## Why it exists

macOS takes local snapshots only when Time Machine has a backup destination
configured. Without a backup disk, nothing takes them — and nothing browses them
either, because Time Machine's own interface needs a configured destination and
mounting a snapshot by hand needs root.

That gap has a cost. A tool that cleaned up after itself deleted a directory that
was gitignored, and so was in no repository either. Recovery was impossible:
FileVault plus TRIM rules out undelete, and with no Time Machine destination there
were no local snapshots to fall back on. This application is the missing half —
rollback points against accidental deletion, using no hardware.

It is **not** a backup. Snapshots live on the same disk as the data. They protect
against `rm -rf`, a bad script, a tool that cleans up too eagerly. They protect
against nothing if the SSD fails. Time Machine and snapshots are complementary,
not alternatives.

## What it does

**Health** — one answer to *am I protected right now*: a verdict, the specific
things to act on, and the numbers behind them. Each finding carries its own fix,
so none of them sends you to another tab.

**Browse** — pick a snapshot, walk the folder tree, and see every entry already
marked *unchanged*, *changed*, *deleted since* or *new since*. Reading a folder is
two directory reads, so it stays immediate on any tree.

**Compare** — walk a whole folder recursively and list everything that differs.
Shallow by default (size and timestamp); the *Compare contents* option hashes
files instead, which is slower and certain.

**Search** — find a file by name across every open snapshot. Every other screen
is organised by place, which assumes you know where to look; the moment this
application exists for is the one where you do not. Case-insensitive, shallowest
match first, and it names the snapshots it could *not* search — finding nothing
in the ones that happen to be open must never read as proof that nothing is
there.

**Compare two snapshots** — Compare works snapshot-to-live or snapshot-to-snapshot,
for *what changed between Tuesday and Wednesday*. Which of the two is older is
decided by their own timestamps rather than by the order they were picked in,
because getting that backwards does not fail — it inverts every row, so a file you
recovered reads as one you lost.

**Restore** — copy a file or folder back. By default the copy lands beside the
current file as `<name>.restored-<snapshot-date>`, leaving what is on disk
untouched. *Replace* puts it back at the original path, moving the current file to
a `.bak-` copy first. Nothing is ever deleted.

**Schedule** — install a LaunchAgent that takes a snapshot on an interval and
prunes as they age. Every 6 hours kept a flat 14 days by default, or a tiered
policy: keep everything recent, then thin with age. The same snapshot count
reaches much further — 6h/14d flat is 57 snapshots covering a fortnight, where
tiering reaches thirteen weeks in 34 or a year in 41. The settings screen shows
both numbers for each policy, counted by planning it rather than by arithmetic,
because those two numbers are the whole argument.

**Bulk-deletion tripwire** — watches the directories you name and snapshots as
soon as something starts deleting in bulk in one of them, from its own LaunchAgent
so it keeps watching with the window closed. It cannot *prevent* a deletion —
FSEvents reports what has already happened — but it stops one running to
completion unwitnessed.

Off until you name a directory, and each directory is counted on its own: 200
deletions in `~/projects` trips it, 100 there and 100 in `~/Documents` does not.
It used to watch the whole home directory, which meant most of what it caught was
`~/Library` doing its housekeeping and every catch pinned another whole-volume
snapshot on the disk.

**Menu bar** — the same verdict as the Health tab, visible without the window.

## Running it

```sh
wails3 task package     # builds bin/snapshotter.app
open bin/snapshotter.app

wails3 task dev         # live-reloading development build
go test ./...           # unit tests; no snapshots are touched
```

### Command line

```
snapshotter                   open the window
snapshotter list              the snapshots of the data volume, newest first
snapshotter status            whether this Mac has usable restore points
snapshotter take              take one now
snapshotter run -- <command>  take a snapshot, then run the command
```

`run` is the one thing the window cannot express: it puts the restore point
immediately before the risky thing, rather than up to six hours earlier. If the
snapshot cannot be taken, the command does not run at all, and the command's own
exit status passes through unchanged.

```sh
snapshotter run -- ./migrate.sh
snapshotter run -- git clean -fdx
```

The scheduled task and the tripwire are the same binary, run by launchd as
`--take-snapshot` and `--watch`.

### Integration tests

Guarded, because they change the machine's state:

```sh
SNAPSHOTTER_INTEGRATION=1 go test ./internal/apfs/ -run Integration -v
```

They create a snapshot, list it, delete it, and clean up after themselves. They
never prune by age, because that would destroy restore points belonging to whoever
is running them.

### Driving it by code

The interface cannot be driven by accessibility — presses do not reach into a
WKWebView — so verifying a screen meant launching the app and looking at it. Two
things fix that, and they compose:

```sh
wails3 task server                              # headless: real frontend, real bindings, over HTTP
SNAPSHOTTER_SCENARIO=full-disk wails3 task server
```

Server mode serves the real application with no native window, so a browser or a
`curl` can press things. Scenarios replace this Mac with a described one: a fake
`apfs.Runner` covering tmutil, diskutil and launchctl alike, so any screen can be
put into any state — including states that are impossible or destructive to
produce for real. `empty`, `healthy`, `overdue`, `full-disk`, `time-machine` and
`conflict` are built in; `SNAPSHOTTER_SCENARIO_FILE` takes a JSON one.

A scenario writes its LaunchAgent plists through the real install paths into a
per-process sandbox, never into your real `~/Library/LaunchAgents`, and it says so
in the window, the menu bar and the log — a simulated machine that looked real
would be indistinguishable from one that lies.

See [docs/AUTOMATION.md](docs/AUTOMATION.md).

### Developing without the ability to mount

Mounting needs root *and* Full Disk Access. Nothing in the path translation,
diffing or restore code knows how the tree under a mountpoint arrived there, so a
directory can stand in for one:

```sh
open bin/snapshotter.app --env SNAPSHOTTER_FAKE_MOUNTS=1
```

Every snapshot is "opened" by cloning a seed directory (`SNAPSHOTTER_FAKE_SEED`,
default `$HOME`) and injecting one of each difference the interface renders, so
Browse, Compare and Search have every state to show.

The stand-in is **sealed read-only** after populating, because a real mount is
read-only and code that wrote into a snapshot would otherwise pass here and fail
against a mount. That was the only respect in which a populated directory was
still unlike a mount; closing it is what makes the rest of the work trustworthy
before mounting is verified.

It also announces itself in the interface, refuses to remove any directory lacking
its marker, and refuses *Replace* restores — fake contents overwriting a real file
would destroy real work to demonstrate a feature.

## Permissions

Creating, listing and deleting snapshots need **no privileges**: `tmutil` is a
client of `backupd`, which does the privileged work. The same is true of the
schedule and the tripwire.

Mounting needs **root**, and that is the only prompt this application raises.
`mount(2)` attaches a filesystem to the global namespace, which is privileged
regardless of who owns the files inside — and an unprivileged mount could use
`-o noowners` to read every user's home directory. Snapshots are mounted
read-only, so browsing cannot alter what was recorded.

Mounting is batched: *Open all* attaches every snapshot behind a single password
prompt.

What gets elevated is **this application's own binary**, re-invoked as a helper,
which then calls `mount_apfs`. That is not tidiness: TCC judges the identity of the
elevated process, and `/sbin/mount_apfs` carries Apple's identity rather than ours,
so a Full Disk Access grant on this application could never reach it.

Two consequences worth knowing before they look like faults:

- **Real mounts only work from a packaged, FDA-granted `.app`.** Under `go run`,
  `wails3 task run` or a bare binary the identity is not the bundle's and mounting
  is refused. That is correct, and it is the first thing to check.
- **The grant survives rebuilds, but only because the signature is stable.**
  `task package` looks up a Developer ID in the keychain and signs with it, so the
  requirement TCC records names the bundle identifier and the certificate. An
  ad-hoc signature is keyed to the build's cdhash instead, and every repackage
  produces a new one — a grant that worked yesterday then stops silently, which
  presents as the mount path breaking rather than as a lapsed permission. A machine
  with no Developer ID still builds; it falls back to ad-hoc and says why.

## Layout

| Path | What lives there |
| --- | --- |
| `internal/apfs` | `tmutil` — list, create, delete, prune, Time Machine state |
| `internal/cli` | the command line: `list`, `status`, `take`, `run` |
| `internal/diffs` | recursive `Compare`, one-level `Level` |
| `internal/elevate` | the one authorization prompt |
| `internal/find` | searching inside a snapshot by name |
| `internal/scenario` | a fake machine, for driving the interface |
| `internal/mountmgr` | mounting snapshots, and the fake that stands in for it |
| `internal/notify` | notifications when protection lapses |
| `internal/restore` | copying files back out, non-destructively |
| `internal/schedule` | the two LaunchAgents, and retention policy |
| `internal/vfs` | live ⇄ snapshot path translation, directory reads |
| `internal/watch` | the bulk-deletion tripwire |
| `services/` | the Wails bridge to the frontend |
| `frontend/src` | React interface |

Every package that shells out takes a `Runner`, so the parsing and the safety
guards are tested without invoking `tmutil`.

## Development

Git hooks come from
[github-guard](https://github.com/antimatter-studios/agent-skills): squash-only
merges, a protected default branch, no merge commits, plus `gofmt` and `go vet` on
commit and `go test` on push. A fresh clone needs one command to activate them:

```sh
git config core.hooksPath .githooks
```

The Go guards skip themselves when `frontend/dist` has not been built, because
`frontend/embed.go` embeds it — otherwise a fresh checkout could not commit at all. Build
the frontend once (`wails3 task build`) and they engage.

One project-local guard exists for an irritation rather than a risk:
`wails3 generate bindings` emits trailing whitespace in its doc comments, which
the whitespace guard rightly blocks. `generated-normalise` strips it from
`frontend/bindings/` only and never blocks, so regenerating bindings does not
tempt anyone into `--no-verify` — a flag that would switch off every other guard
at the same time.

## Known limits

- **Mounting needs a permission only the user can give.** It works, and is now
  exercised against real snapshots, but every machine needs Full Disk Access
  granted to the bundle by hand before a snapshot will attach — root is necessary
  and not sufficient. Until that is granted, `mount_apfs` fails with `Operation
  not permitted`, which reads like an ownership problem and is not one.

  This was diagnosed the long way round, so the wrong turns are worth recording:
  granting FDA to the bundle appeared not to help, and the conclusion drawn was
  that TCC blamed `/usr/bin/osascript` and that an `SMAppService` helper was the
  only fix. Both were wrong. What matters is the identity of the binary that is
  *exec'd*, and elevating `/sbin/mount_apfs` runs a binary carrying Apple's
  identity rather than ours, which no grant on this application can reach.
  Elevating our own binary instead fixed it, with no privileged daemon.
- **Ad-hoc signatures do not keep a TCC grant.** TCC keys an ad-hoc signed bundle
  on its cdhash, and every `wails3 task package` produces a new one — so a Full
  Disk Access grant works once and stops silently after the next build, which
  presents as a regression in the mount path rather than as a lapsed grant. A
  stable signing identity is a prerequisite, not a distribution nicety. For the
  same reason a grant aimed at a bundle on removable media is not durable.
- **Snapshots are whole-volume, and every volume at once.** APFS has no per-path
  snapshot and no exclusion mechanism. `tmutil localsnapshot` takes no arguments
  at all — not a volume, not a flag — so it snapshots every eligible mounted APFS
  volume simultaneously, and there is no way to ask for one. Every neighbouring
  verb takes a mount point (`listlocalsnapshots`, `thinlocalsnapshots`,
  `deletelocalsnapshots`); creation is the one Apple did not make selectable.

  So an external APFS disk is snapshotted whenever the startup disk is, whether
  or not anyone wanted that. Snapshotter lists and prunes every volume for this
  reason, and the Health screen reports each one's own free space and pinning
  snapshot — a container is per volume, so neither is knowable from the boot
  volume's numbers. `tmutil addexclusion -v <volume>` is the only lever for
  keeping a disk out of it altogether.
- **No per-snapshot size, ever.** A snapshot shares blocks with the live volume
  and with its neighbours, so the question has no single answer. What is reported
  instead is which snapshots are purgeable and which one pins the container.
- **Mounts are left attached on quit.** Detaching needs root, and a password
  prompt on the way out is a poor trade for tidying something read-only. A mounted
  snapshot cannot be deleted, so leftover mounts block pruning — *Close all* is in
  the sidebar.
- **This source tree is snapshotted, but not reachable from inside the
  application.** It lives on an SD card, which is a different volume — and an
  APFS one, so `tmutil localsnapshot` sweeps it up with everything else. The
  snapshots exist and are pruned along with the rest.

  What they are not is browsable or restorable here: `mountmgr` refuses any
  source but the data volume, deliberately, because "the volume to read from" is
  exactly the argument worth controlling in a process that mounts as root. Recover
  from them with `mount_apfs` by hand.

## Changelog

Most recent releases; the full history lives in [CHANGELOG.md](CHANGELOG.md).

### v0.59.0

A snapshot on another volume can be looked inside, not only opened. Browsing,
comparing, searching and restoring all name the volume now, because a snapshot
name does not identify a copy — the same date exists on every volume that was
mounted when it was taken. Browsing starts at a home directory on the startup
disk and at the volume's own root anywhere else.

### v0.58.0

A snapshot on any volume can be opened, not just the startup disk's. The
privileged helper still keeps an allowlist of what it may read from — it is now
discovered as root from the machine rather than written down as one constant, so
a caller may name a volume but not add one. Each volume mounts into its own
directory, because two volumes' snapshots of the same moment share a date. The
home screen also gained one spacing rule where it had several, and the volumes
table moved into the part of it that scrolls.

### v0.57.1

Grouping the snapshot list moved a wrapper between the sidebar column and the
list that was its scrolling region, so nothing scrolled and the footer was pushed
out of the column and off screen. One scrolling region now holds every group.

### v0.57.0

The snapshot list is grouped by the volume the snapshots are on, so the ones on an
external disk are visible at all — there was no way to see them before. Deleting
is now per copy: it deleted by date, and a date exists on every volume mounted
when it was taken, so one press quietly took an external disk's snapshot of the
same moment too. `diskutil apfs deleteSnapshot -uuid` is the only call that tells
two copies apart.

### v0.56.0

Snapshots were taken on every APFS volume and pruned on one. `tmutil
localsnapshot` takes no arguments at all, so it writes to every mounted APFS
volume at once — and listing, pruning and reporting all ran on the startup disk
alone, so everything else accumulated snapshots nothing would ever delete. Found
on an SD card at 98% full holding eight that existed nowhere else. Pruning now
plans over every volume, and the Health screen reports each one's own free space
and pinning snapshot.

### v0.55.1

A mistyped verb opened a window. `snapshotter health` is not a command and it
silently launched the application, with only the one-window guard — which
describes a different problem — saying anything at all. Anything on the command
line now goes to the command line, which already refused an unknown verb by name.
`snapshotter --help` is fixed by the same change: it printed Go's usage for the two
flags the launchd agents are installed with, and never mentioned a command.

### v0.55.0

The bulk-deletion watcher watches directories you name, and nothing else. It
watched the whole home directory with an ignore list to quiet the rest, which is
the wrong way round — `~/Library` deletes in bulk as a matter of routine, so most
of what it caught was housekeeping and every catch pinned another whole-volume
snapshot on the disk. It is now off with an empty list until a directory is named,
each directory is counted on its own, and the cooldown stays shared because a
snapshot is of the whole volume anyway.

### v0.54.1

v0.54.0 shipped with no window in it on Apple Silicon — the universal build
compiled both architectures at once, and each half emptied the frontend's output
directory under the other. Fixed, and checked three ways, including thinning the
binary to inspect each slice on its own.

### v0.54.0

How readily the bulk-deletion watcher trips is a setting: Cautious, Balanced,
Sensitive or Very sensitive, each showing the count it stands for. The retention
profile is now chosen before the two numbers it uses, which are labelled for the
profile in force rather than for the flat one alone. And a translated message
nobody asks for is a test failure — which found five screens still hardcoded in
English.

### v0.53.1

Upgrading through Homebrew removes both launchd agents, and the application puts
them back at startup — but v0.53.0 renamed the retention presets without
translating the old names, so a settings file naming one restored nothing, and a
shared early return took the bulk-deletion watcher down with the schedule. Both
fixed, a failed restore now says so out loud, and only one window may run at a
time whatever it was built from.

## Design decisions

[docs/DECISIONS.md](docs/DECISIONS.md) records why things are the way they are,
including the traps that cost real debugging.
[docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) records the faults that have
actually happened, with the fix for each — start there when something is wrong
rather than reading the decisions. [docs/ROADMAP.md](docs/ROADMAP.md) orders the
outstanding work by what can be verified rather than by what is worth most.
