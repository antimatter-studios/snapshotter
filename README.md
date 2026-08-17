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

**Bulk-deletion tripwire** — watches FSEvents and snapshots as soon as something
starts deleting in bulk, from its own LaunchAgent so it keeps watching with the
window closed. It cannot *prevent* a deletion — FSEvents reports what has already
happened — but it stops one running to completion unwitnessed.

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
`main.go` embeds it — otherwise a fresh checkout could not commit at all. Build
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
- **Snapshots are whole-volume.** APFS has no per-path snapshot and no exclusion
  mechanism. A separate APFS volume in the same container shares free space and
  mounts anywhere, but `tmutil localsnapshot` takes no volume argument and
  `diskutil apfs` has no create verb — so moving a folder to its own volume
  *excludes* it from coverage rather than giving it a schedule.
- **No per-snapshot size, ever.** A snapshot shares blocks with the live volume
  and with its neighbours, so the question has no single answer. What is reported
  instead is which snapshots are purgeable and which one pins the container.
- **Mounts are left attached on quit.** Detaching needs root, and a password
  prompt on the way out is a poor trade for tidying something read-only. A mounted
  snapshot cannot be deleted, so leftover mounts block pruning — *Close all* is in
  the sidebar.
- **This source tree is not protected by the snapshots it manages.** It lives on
  an SD card, which is a different volume.

## Changelog

Most recent releases; the full history lives in [CHANGELOG.md](CHANGELOG.md).

### v0.45.0

Switching language updates the menu bar immediately rather than at the next
refresh.

### v0.44.0

The command line is translated too, following `appearance.language` like
everything else. Command syntax stays in English, being what you type.

### v0.43.0

Every piece of text a person reads is translated, including the macOS password
prompts, the notifications the background agents post, and the retention sentence.

### v0.42.0

Translation is handled by i18next and go-i18n, and image comparison by pixelmatch,
in place of hand-written versions. Plurals now follow each language's own rules.

### v0.41.0

The health findings, the menu bar and notifications are translated, so the whole
application follows the language rather than only the window.

### v0.40.0

Comparing a file that is in neither version explains itself instead of reporting
a failure.

### v0.39.0

A file is only shown as a picture when its contents agree with its name, so a zip
called photo.png is not handed to an image tag.

### v0.38.0

Images can be compared: side by side, cross-faded, or as a mask of exactly which
pixels changed.

### v0.37.0

Files up to 16 MiB can be compared, rather than 1 MiB, and a large binary now
reports that it is binary instead of blaming its size.

### v0.36.0

Fixes a sentence that was cut short in German, Spanish and French, and adds tests
for the shape of a translation rather than merely its presence.


## Design decisions

[docs/DECISIONS.md](docs/DECISIONS.md) records why things are the way they are,
including the traps that cost real debugging. [docs/ROADMAP.md](docs/ROADMAP.md)
orders the outstanding work by what can be verified rather than by what is worth
most.
