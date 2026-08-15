# Changelog

All notable changes, per released version. The most recent releases are also
summarized in the README; the full history lives here.

## Unreleased

Nothing yet.

## v0.5.0 — 2026-08-15

**The menu bar menu is legible, and says something a count cannot.**

Everything informational in that menu was disabled, which macOS draws in the grey
it uses for "you cannot have this". That is the wrong thing to say about the
state of your own machine, and it made the text most worth reading the hardest to
read. Nothing is disabled now: each row opens the window, so it is honestly
clickable rather than merely enabled.

A menu is not only a list of things to click, so two things are drawn rather than
written:

- **A coverage strip**, one cell per hour across the last two days, filled where a
  snapshot exists. The gaps are the point. A count says how many restore points
  there are; the strip says *when* they are, and a machine with twelve snapshots
  taken in one hour is not covered by any useful definition.
- **A glyph per finding**, chosen by what the finding is about rather than by how
  bad it is. Findings previously took their icon from their level, so three
  warnings were drawn with three identical warning icons — which tells a reader
  only that there are three. Each finding now carries a `Kind`, and the shape
  follows the kind while the colour still follows the level.

The one exception is the cross, which is always red and smaller than the rest: it
marks something absent, it is the strongest shape in the set, and in the same
amber as everything else it read as decoration.

Both are drawn in Go rather than shipped as assets, so they adapt to the light or
dark background the menu is being drawn on, and both are covered by tests that
fail if two things ever render identically or if a glyph draws an empty square.

## v0.4.0 — 2026-08-15

**The settings file can be found, read and changed from the command line.**

0.3.0 moved everything configurable into `$HOME/.config/snapshotter/config.yaml`
and then told nobody. It was the only way to change what the window does not
offer, and finding it — or learning what keys go in it — meant reading the
source.

    snapshotter config                       where it is, and what it says
    snapshotter config --write               create it with the defaults
    snapshotter config keys                  everything that can be set
    snapshotter config get <key>             read one setting
    snapshotter config set <key> <value>     change one setting

The names are the ones in the file — `schedule.interval_hours`, not a second
vocabulary invented for the command line — and they are derived by walking the
file's own structure rather than listed by hand, so a setting added later is
addressable the moment it exists.

This is the scripting surface the application did not have. It also means a test
can put a machine into a known state without hand-writing YAML.

Care where it writes to a file somebody owns:

- `--write` refuses when the file already exists. Settings outlive the version
  that wrote them, so rewriting one would drop anything this build does not
  recognise, and deleting somebody's settings to "reset" them is not something a
  flag should do quietly.
- `set` refuses a value the field cannot hold, and changes nothing when it does,
  so a script that fails does not leave the settings half applied.
- `set` refuses outright if the existing file will not parse, rather than saving
  the defaults over whatever it was trying to say.
- What `get` prints is what `set` accepts, so a value can be read, decided on and
  written back.

Type is checked; meaning is not. `appearance.theme` will accept a colour that is
not a theme — the window validates those, this does not.

### The command line tool is no longer published on its own

It was published as a separate archive for a companion formula that no longer
exists: the cask installs the application and symlinks the bundle's own
executable onto PATH, so one `brew install --cask` gives both.

They are deliberately not separable. The command line **is** the application's
executable, and browsing or restoring from a snapshot mounts a filesystem, which
needs the Full Disk Access grant that only the installed bundle carries. It also
removes a step that had to be kept correct: a binary copied out of a bundle
fails verification until it is re-signed, because it was signed as part of that
bundle.

## v0.3.0 — 2026-08-15

**One version number, one settings file, and a test suite that runs in CI.**

The version is stamped into the binary at build time from a single source, shown
on the home screen, and answered by `snapshotter version`. Before this there was
nothing to ask: an installed copy could not tell you what it was.

Everything that used to be hard-coded now lives in
`$HOME/.config/snapshotter/config.yaml` — schedule, tripwire, appearance, window
size, refresh intervals and paths — so every copy running on a machine agrees.
A missing file means defaults and no complaint. A file that cannot be parsed
means defaults, an error you can see, and the file left exactly as it is:
someone with a broken configuration still needs the window in order to fix it.

The command line tool is published as its own signed and notarized tarball, so
it can be installed onto PATH without the application bundle.

### Tests

Every package clears 70% except `main`, whose remainder is the Wails event loop
and process wiring — the parts exercised by launching the application rather
than by `go test`.

The two launchd entry points are now tested against a scenario rather than left
to the machine. They run unattended, and the first anyone hears of a failure is
a snapshot that is not there. That includes the rule that a retention policy
this build cannot read prunes **nothing**: keeping too much is corrected by the
next run, and deleting too much is not correctable at all.

Writing the tests found three real faults:

- the status line said "1 hours", because the number being pluralised was not
  the number being printed;
- `notify` and `elevate` had no seam, so what they conclude from a dismissed
  password dialog — a decision, not a failure — could not be tested at all.

The frontend gains a test suite of its own over formatting, theme handling and
the comparison labels.

### Fewer values written twice

The window background colour and the title bar height were each written once in
Go and once in CSS, with nothing connecting them. The second pair is what keeps
the traffic lights off the title, and it was held together only by both numbers
happening to be 50. Both are named on each side now, and a test reads the
stylesheet and fails if they drift apart.

The two screens showing the schedule's log asked for different amounts of it for
no stated reason; the size lives in one place now. `runWindow` came down from 149
lines to 78, and the rule that a scenario must never write a launchd plist into
the real `~/Library/LaunchAgents` — where it would outlive the run and start
taking real snapshots on a real timer — has a test for the first time.

### CI

Pull requests and `main` run gofmt, `go vet`, `go test -race`, a TypeScript
typecheck and the frontend tests. The release workflow runs the suite before it
imports the signing certificate, so a tag whose tests fail never reaches Apple
and never becomes a release.

## v0.1.1 — 2026-08-14

**Packaging only; the application is unchanged from v0.1.0.**

The application bundle now carries its own notarization ticket rather than relying
on the disk image's. Homebrew does not install the image — it copies the bundle out
of it, and that copy had no ticket of its own, so Gatekeeper had to ask Apple
whether it had been notarized. With a network that succeeds silently; without one it
can refuse, which is the machine this application exists to rescue.

Fixing it meant reordering the release rather than adding a flag, because a ticket
is stapled to one specific thing: the bundle is submitted and stapled first, the
image is built around the already-stapled copy, and the image is then submitted and
stapled in its own right — Gatekeeper assesses the downloaded file as well as what
is inside it.

## v0.1.0 — 2026-08-14

First release. Signed with Developer ID, notarized, and installable with
`brew install antimatter-studios/tap/snapshotter`.


**Browse, compare and restore APFS local snapshots without a backup disk:** the
first working version. macOS only schedules local snapshots when Time Machine has
a destination configured, and Time Machine's own interface refuses to show you
anything without one — so on a Mac with no backup disk the snapshots either do not
exist or cannot be reached. This opens them: pick a snapshot, walk the tree with
every entry already marked *unchanged*, *changed*, *deleted since* or *new since*,
compare a whole folder recursively (shallow on size and timestamp, or hashed and
certain), and copy files back out. Restores are non-destructive by default — the
recovered copy lands beside the original as `<name>.restored-<date>` — and
*Replace* moves the current file to a `.bak-` copy first. Nothing is ever deleted.

**A schedule, because nothing else will take them:** installs a LaunchAgent that
snapshots on an interval and prunes past a retention window; six hours and a
fortnight by default, since the aim is days of depth against an accidental
deletion rather than intra-day granularity, and each snapshot pins another
generation of every large file rewritten between them. It refuses a retention
shorter than the interval, which would delete each snapshot as fast as it was
taken, and it detects another agent already taking local snapshots rather than
silently doubling the rate.

**Health — one answer to "am I protected right now":** every input for this
already existed and was scattered across three tabs and a warning banner, which
is the problem. The failure this application exists to prevent is *believing* you
are covered, and that belief survives any amount of information that has to be
assembled by hand. One verdict, then the specific things to act on, then the
numbers behind them. Findings carry their own fix — install the schedule, take a
snapshot, show the failing task's log — so none of them is a dead end.

**A menu bar item:** the same verdict where it is visible without the window
being open. The window is where you act; the menu bar is where you find out
whether you need to.

**A tripwire for bulk deletion:** watches FSEvents and takes a snapshot as soon as
something starts removing files in bulk — two hundred inside five seconds, on a
ten-minute cooldown so one long deletion cannot fill the disk with snapshots of a
disk being emptied. It runs from its own LaunchAgent, because a watcher is
worthless while the window is closed and a deletion at 3am is exactly the one
nobody is watching.

It **cannot** prevent a deletion, and nothing here claims otherwise: FSEvents
reports what has already happened, so the file that trips it is already gone. It
is a tripwire, not an interlock — it stops a deletion running to completion
unwitnessed. Tripped at the two-hundredth file of ten thousand, the rest survive;
an `rm -rf` of one small folder is over before anything can react.

**Notifications when protection lapses:** a scheduled run that fails and a
watcher that trips both say so. Both of this application's failure modes are
silent ones, and the loss that prompted the project happened to someone who
believed they were covered.

**Snapshot space, reported honestly:** how many snapshots macOS may purge on its
own, and which one is holding the container's minimum size up — usually the oldest,
and the one worth deleting first when space runs short. There is deliberately no
per-snapshot byte figure, because APFS does not expose one and cannot: a snapshot
shares blocks with the live volume and with its neighbours, so "how big is it" has
no single answer. Inventing a number would be worse than showing none.

**A command line:** `list`, `status`, `take`, and `run`. `snapshotter run --
<command>` takes a snapshot and then runs the command, so the restore point exists
at the moment it matters rather than up to six hours earlier — and if the snapshot
cannot be taken, the command does not run at all. The command's exit status passes
through, so it can be dropped in front of anything.

**Find a file by name, across every open snapshot:** every other screen is
organised by place, which assumes you know where to look. The moment this
application exists for is the one where you do not — you know what the file was
called and roughly when it was still there, not which directory held it. Search is
case-insensitive on the name, shallowest match first, and bounded; when the bound
stops it, it says so, because a search that quietly stopped early reads as "that
is all there is". It matches names and never contents, so a phrase cannot silently
become a grep over the volume. Unreadable directories are skipped rather than
fatal, since a snapshot contains other users' home directories.

Crucially it names the snapshots it could **not** search. Only mounted snapshots
can be looked inside, and finding nothing in the two that happen to be open must
never read as proof that nothing is there.

**A deleted-since view:** a comparison filtered to what the snapshot held and the
disk no longer does. Compare shows everything that differs, which after a week of
ordinary work is mostly noise; when something has gone missing the only rows that
matter are the ones that are gone.

**Simulated mounts, for development and for tests:** mounting a snapshot needs
root *and* Full Disk Access, and is refused outright on a machine without both.
Nothing in the path translation, diffing or restore code knows how the tree under
a mountpoint arrived there, so `SNAPSHOTTER_FAKE_MOUNTS=1` populates a directory
by cloning a seed instead — which makes the whole browse and compare surface
workable, and testable in CI without root. It announces itself in the interface,
refuses to delete any directory lacking its marker, and refuses *Replace* restores
outright, because fake contents overwriting a real file would destroy real work to
demonstrate a feature.

The stand-in is also sealed **read-only** once populated. A real mount is
read-only, so code that wrote into a snapshot would otherwise have passed against
the stand-in and failed against a mount — the one respect in which a populated
directory was still unlike a mount, and the reason the work built on top of it can
be trusted before mounting itself is verified.

**Git hooks, from github-guard:** squash-only merges, a protected default branch,
no merge commits, plus `gofmt` and `go vet` on commit and `go test` on push. Test
runs on push rather than on commit because the suite drives a real FSEvents stream
and costs seconds — paid per push it is invisible, paid per commit it would train
someone into `--no-verify`, which switches off every other guard at once.

A project-local `generated-normalise` guard exists for the same reason:
`wails3 generate bindings` emits trailing whitespace that the whitespace guard
rightly blocks, so it is stripped from `frontend/bindings/` automatically rather
than becoming a standing excuse to bypass everything. The Go guards fail open when
`frontend/dist` is unbuilt, since `main.go` embeds it and a fresh clone would
otherwise be unable to commit at all.

**Compare one snapshot against another:** Compare ran only against the live disk,
and *"what changed between Tuesday and Wednesday"* needed no new machinery — the
diff engine takes two directories and has no idea whether either is a mount. Which
of the two is older is decided by their own timestamps rather than by the order
they arrived in: a change between two snapshots has no inherent direction, and
getting it backwards does not fail, it inverts every row, so a file you recovered
is reported as one you lost. Between two snapshots the rows read *removed* and
*added* rather than *deleted since* and *new since*, because "since" has no
referent when both ends are in the past.

**Tiered retention:** keep everything recent, then thin with age. A flat 6h/14d
window is 57 snapshots covering a fortnight; the same count reaches thirteen weeks,
or a year in 41. Each bucket keeps its **oldest** member, on absolute boundaries,
which is what makes the kept set stable — keeping the newest would delete
yesterday's keeper today merely because a newer snapshot joined its bucket, and
local-midnight boundaries would re-choose a day's keeper at every DST change.
Pruning incrementally across 400 simulated days lands on a subset of planning the
whole history at once. The newest snapshot is kept whatever a policy says, an empty
policy keeps everything rather than reading as "delete the lot", and the default
stays flat — a retention default that changes silently deletes something somebody
expected to find.

**The interface can be driven by code:** verifying a screen used to mean launching
the application and looking at it, because accessibility presses do not reach into
a WKWebView. Server mode (`wails3 task server`) serves the real frontend with the
real bindings and no native window, so a browser or a `curl` can press things.

Scenarios replace this Mac with a described one. `main.go` hardcoded
`apfs.SystemRunner()` and everything below it already went through that interface,
so one fake Runner now covers the whole machine — tmutil, diskutil and launchctl
alike — and any screen can be put into any state, including states impossible or
destructive to produce for real. Its output is shaped from the real commands,
quirks included, because the point is that it parses through the *real* parsers; an
unmodelled command fails rather than returning nothing, since empty output would
parse as "no snapshots" and read as a finding instead of a gap. Plists are written
through the real install paths into a per-process sandbox, never into the real
`~/Library/LaunchAgents`, and a scenario announces itself in the window, the menu
bar and the interface — one that looked real would be indistinguishable from one
that lies.

That last requirement produced `LevelInfo`: something worth saying that is not a
degradation. Reporting the simulation as a warning made the clean "nothing to say"
verdict unreachable under any scenario, which is one of the states a scenario is
most useful for reaching.

**The menu bar carries the health level**, drawn to a different extent per level so
severity survives greyscale rather than resting on colour. These are deliberately
not template images: Wails latches the template flag the first time it is set and
never clears it, so a single `SetTemplateIcon` call anywhere would flatten every
coloured icon to a black silhouette.

### Known limits

**Mounting is unverified on the author's machine.** `mount_apfs` is refused by TCC
with "Operation not permitted", which reads like an ownership problem and is not
one: root is necessary and not sufficient, and the process responsible for the
call also needs Full Disk Access. Granting it to the application bundle did not
help, most likely because the privileged command runs by way of `osascript` and
the permission is checked against that. An `SMAppService` helper — which would
also give one authorization prompt at install instead of one per action — is the
real fix. See `docs/DECISIONS.md`.

**Snapshots are whole-volume.** APFS has no per-path snapshot and no exclusion
mechanism. A separate APFS volume in the same container shares free space and can
be mounted anywhere, but `tmutil localsnapshot` takes no volume argument and
`diskutil apfs` has no create verb — so there is no user-space way to snapshot an
arbitrary volume. Moving a folder onto its own volume *excludes* it from coverage;
it cannot give it a schedule of its own.

**Mounts are left attached on quit.** Detaching needs root, and a password prompt
on the way out is a poor trade for tidying something read-only. A mounted snapshot
cannot be deleted, so leftover mounts block pruning — *Close all* is in the
sidebar.
