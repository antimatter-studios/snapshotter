# Changelog

All notable changes, per released version. The most recent releases are also
summarized in the README; the full history lives here.

## Unreleased

Nothing yet.

## v0.63.2 — 2026-08-30

**`snapshotter open` could not find the bundle it was launched from.**

It answered "this copy is not in an application bundle, so there is no window to
open" — from inside the installed application. os.Executable on macOS hands back
the path the process was started with rather than the file it ended up at, so run
through Homebrew's link it is /opt/homebrew/bin/snapshotter, which is not a
bundle.

Resolved there, and deliberately not in the check that decides whether a bare
invocation is a question or a launch: that one wants to know how the program was
addressed — through a bundle by the Dock, by name at a prompt — and resolving
would erase the difference it exists to see.

The command line and the window are one binary; the only thing that separates
them is how it was asked for, which is why this distinction has to be made
carefully.

## v0.63.1 — 2026-08-30

**A way in, and a way home.**

`snapshotter` with no arguments printed nothing and opened a window. That is how
somebody finds out what the commands are — and when a window was already open it
refused with an explanation of two menu bar icons, which answered a question
nobody had asked. It prints the help now.

Told apart by how the program was addressed rather than by what it is. Homebrew
links /opt/homebrew/bin/snapshotter straight into the bundle, so the file on disk
is the same either way and os.Executable resolves the link and loses the
difference. os.Args[0] keeps it: the Dock, Finder, `open` and `wails3 dev` all
address the binary through the bundle it lives in, and a person types a name. A
terminal check was tried first and was wrong — `wails3 dev` runs the binary with
the developer's terminal still attached, so the window never opened at all.

`snapshotter open` is the other half: asking for the window now needs a word, so
there is one. It goes through LaunchServices, which is what gives an application
its Dock icon and its menu bar, and what brings a running copy forward instead of
starting a second one the instance guard would refuse.

And the Home button was a line of plain text the width of the sidebar, which read
as a heading rather than as somewhere to go. An outline and a word were no better:
everything else in that panel is an outline and a word. It is built as a
destination now — a filled mark, a name, and a line saying what is on the other
side — and it is the one place in the sidebar entitled to the accent, because the
snapshot list below it is deliberately quiet.

## v0.63.0 — 2026-08-29

**`list` answers for every disk, not just the startup one.**

It read the data volume alone. That stopped being the whole answer when `tmutil
localsnapshot` turned out to take no arguments and write to every mounted APFS
volume at once, so somebody wanting to know what was on an external disk had to
leave this application and read diskutil — the one thing it exists to save them
from.

Reading diskutil by hand is also easy to get wrong. The flags column comes from
the NOTE line rather than the Purgeable line, and on the machine this was written
for that is the difference between "nothing is pinning the SD card's container"
and the truth, which is that a snapshot from 22:10 is. The two disks pin at
different dates, so there is no single oldest snapshot whose deletion frees space
on both.

Grouped by disk, with the name and the mount point: two disks can share a name,
and only the mount point says which one this is. A machine with one disk gets no
heading, because labelling it would make every Mac look like it had something to
disambiguate.

The help text said "of the data volume" in four languages — the same rot as the
notice removed in v0.62.2, true when written and false since.

## v0.62.2 — 2026-08-29

**A notice that was no longer true.**

Every volume that was not the startup disk carried a note above its snapshots:
"Snapshotted and pruned here, and can be opened. Browsing what is inside is the
startup disk's alone — open one and it says where it is mounted."

That was true when it was written and stopped being true when snapshots on any
volume became browsable. It sat there afterwards contradicting what the
application had just done, which is worse than clutter: somebody reading it would
believe the thing they had done was impossible. Nothing was holding it to the
truth — no test mentioned it, so it went stale silently.

The half of it that is still true is said elsewhere already: the volume heading
carries its mount point and device, and an opened snapshot reports where it is
mounted.

## v0.62.1 — 2026-08-28

**A progress bar that runs for the whole wait, not just the countable part.**

The status bar's two halves now say different things: the left says what is
happening, the right says how far it has got.

They change independently, because reading a folder, asking the event log and
walking the disk are three stages of one wait and only the last of them has a
number. The bar appeared for that one alone, which left the first two looking
like nothing happening — the very thing the bar exists to answer.

Where there is no number it moves rather than fills. A bar sitting at an invented
percentage would be a claim about progress nobody is measuring, which is the same
fault as a folder reporting itself identical without having looked. Somebody who
has asked for less movement gets a still bar rather than none: its presence is
what says work is in progress.

Also in this release, and visible to nobody: the test suite no longer depends on
what is listening on port 80 of the machine running it. Two screens refresh
themselves on a timer and read the interval with a binding call, so any test
rendering one made a real HTTP request unless it happened to stub that call —
which is how the same suite passed on one machine and failed on another twice in
one afternoon, and how one test failed roughly one run in four for no reason
visible anywhere in its own file.

## v0.62.0 — 2026-08-28

**Browsing stopped re-reading whole trees to answer questions it had already
answered.**

Deciding a folder has changed needs one difference. Deciding it has NOT changed
needs everything under it read, because there is no early exit from proving a
negative — and on an SD card that was seconds per folder, paid again every time
somebody navigated back.

Nothing below concludes that anything is unchanged from anything cheaper than a
full read. Every shortcut can only ever find a difference, and each is verified
against the disk before it is believed.

What is asked, in order of what it costs:

1. **What is already recorded.** A walk stops at the first difference, so a
   "changed" verdict always rests on a single path. Recording which one turns the
   next question into one stat — and it answers not just its own folder but every
   folder between it and wherever the question was asked. Kept in a
   `change_detection` table so it survives a restart, which is only safe because
   it is re-checked rather than trusted. The reverse holds too: a walk that
   completes without finding a difference has read every folder below it and
   proved them identical, which used to be thrown away.
2. **The event log.** macOS already records what changed; it is replayed from
   where we last looked and every path it names is verified before being
   recorded. Measured here: 43 paths in 145ms, against 178,570 and a timeout for
   the same call anchored at the start of history.
3. **Reading the tree**, which the two above exist to avoid. A directory is now
   settled by its own entries before anything is descended into: presence and
   type first, which costs no reading at all, then file contents, then recursion.

Also in this release:

- `change_detection.ignore`, a list of paths not to read at all. It is the only
  setting that helps the expensive direction — 17,239 of a project's 19,788
  entries were node modules. It ships empty, with the usual names offered as one
  click each, because a folder skipped is a folder this will not tell you about.
- Leaving a folder gives up on its walks instead of letting them run to the end
  for rows nobody will see again, with the next folder queued behind them.
- How many folders are checked at once comes from the disk rather than being
  three for everything: an SD card has one slow channel, internal storage wants a
  deep queue. What is on screen is answered first.
- Clicking a folder blanks the window in the same moment rather than when the new
  listing arrives, which on a slow disk was eight to ten seconds later and read
  as a window that had stopped responding.
- A snapshot of an external disk no longer offers folders above that disk. The
  trail across the top started at `/` whatever was being browsed, so `/Volumes`
  was clickable and led straight to "that volume's snapshots do not cover it".
- The status bar along the bottom of the right-hand panel is always there now.
  It used to appear only while there was something to count, so it was missing at
  exactly the moment it was most wanted: the window sitting still, with no way to
  tell waiting from finished. It says what is being done — reading a folder,
  asking the event log, walking the disk — and grows a bar and a count only for
  the part that has a number.
- A folder's verdict row took its class name from the status word, and `ignored`
  was already the name of the bulk-deletion watcher's panel — which put a border
  across the row and a box around the folder's name.

## v0.61.0 — 2026-08-28

**A status bar along the bottom of the window, counting the slow work.**

    Checking folders     245/567

The application had one honest way to say it was busy — a disabled button — and
several kinds of work that take long enough to look like a freeze. A folder's
verdict can be a walk of everything beneath it, so a listing of source trees sat
on a column of "detecting…" that never changed, and there was no way to tell that
from stuck.

A bar rather than a spinner, because the count is knowable: a spinner says
something is happening, this says how much is left. It sits in the same place
every time, so a reader learns where to look rather than hunting for it, and the
count is centred over the bar because the number is the thing being read.

The bar is fed by a callback rather than reaching into the browser, so anything
else slow can report into the same place without the bar knowing what the work
was.

Three details decide whether the meter can be trusted. Only folders are counted,
because a file is not walked and counting it would make the total larger than the
work. A folder that could not be checked still advances the count, or the bar
stops short of the end at the exact moment it has finished — the reading it
exists to prevent. And the meter is cleared only by the listing that owns it, so
a slow listing finishing after a newer one started does not wipe the newer one's
progress.

## v0.60.2 — 2026-08-28

**The Health screen said "Checking…" and never finished.** Browsing hung with it.
`Status.Check` did not return at all: five minutes in, it was still inside the
volume enumeration.

Enumerating volumes is one `mount`, a `diskutil apfs listSnapshots` for every
mounted APFS filesystem — which includes every snapshot this application has
opened — and a `diskutil info` for each one. Twenty-odd subprocesses, twenty-odd
seconds on a loaded machine.

That is fine once, and it was not once. Translating a path needs to know which
volume it is on, and translating happens per directory entry — so a listing of
two hundred files asked the machine to enumerate its disks two hundred times.

- The volume's name is looked up after the snapshot filter rather than before it.
  It is one subprocess per volume and it is wanted only for the ones that reach
  the screen; asking for all twelve mount points a Mac has, most of which hold
  nothing, was most of the cost. Alone this took an enumeration from about
  twenty-one seconds to one.
- A short-lived cache, shared by every service. Ten seconds: long enough that one
  listing enumerates once, short enough that a disk plugged in appears without a
  relaunch. A failed refresh keeps the answer it had, because diskutil not
  answering for a moment is not evidence that the disks have gone — and reporting
  none would empty the sidebar and refuse every translation.
- The lookup is hoisted out of the loop over directory entries, which is where
  the multiplication actually came from.

Mounting and unmounting forget the cache rather than waiting it out: they add and
remove APFS filesystems, which is what the list is a list of. The command line
builds no cache and pays the full cost once, which is the right trade for a
process that exits.

Measured on the machine that reported it: Check went from not returning to 273ms,
and a listing went from thousands of subprocesses to none.

## v0.60.1 — 2026-08-28

**Opening a snapshot said nothing until it was over.** It raises an authorization
prompt and then attaches a filesystem, so it is the slowest thing in this window
by a wide margin — and it reported nothing at all until a grey dot quietly turned
green. Silence during the slow part reads as a click that did not register, which
is how somebody comes to press it twice and answer two password prompts for one
intention. A refusal looked exactly like a click that never happened.

Three states are visible now where there was one. Waiting is a spinner in the
row's dot position, so nothing shifts as one replaces the other. The outcome is a
tick or a cross, held for five seconds — long enough to be read, then gone, so
the row goes back to reporting what is true rather than what just happened. And
an overlay over the window says which is happening and names the password prompt,
because the wait is mostly macOS asking and somebody who does not know that is
watching a spinner for no stated reason.

Reduced motion slows the spinner rather than stopping it: the motion is the only
thing saying "still going".

**Selecting a snapshot on another volume reported that the home directory is not
on that volume, and then opened anyway.** Both halves were true and the pairing
was not. The device was set immediately while the path still pointed at the
previous volume's home folder, so for one render the browser asked for a home
directory inside an external disk's snapshot — and was told, correctly, that it
is not there. The listing arrived a moment later and the error stayed on screen,
describing a question nobody had asked.

The path and the volume it belongs to are one value now, so they cannot disagree,
and an unresolved path is a state the browser waits through rather than a pairing
it can get wrong.

## v0.60.0 — 2026-08-28

**A built-in manual, so the documentation reaches the machine it is about.** All
of it was already written and none of it reached anybody: the documents are in
the repository, and somebody who installed a disk image has the binary and
`snapshotter help`, which listed six command summaries and nothing else. The page
answering "why did my snapshots disappear" was on GitHub.

Five pages are compiled in — `snapshots`, `volumes`, `mounting`, `tripwire`,
`restoring` — and they are the questions this application actually produces:
purgeable snapshots vanishing early, one command writing to every disk, reading
your own files needing a password, a watcher that cannot prevent a deletion, and
where a restored file lands.

    snapshotter help              the contents page: commands and topics
    snapshotter help volumes      one page, as markdown

They are documents rather than blocks extracted from the source. Lifting pages
out of comments keeps a rule and its paragraph in one diff, which is right when
the manual documents behaviour sitting next to code; this application's hardest
documentation is narrative, and the best of it is attached to declarations where
gofmt would reformat it. The problem here was distribution rather than drift, so
the mechanism is an embed and a lookup rather than a generator and a CI guard.

- Hyphens, underscores and case are interchangeable, because the name is a phrase
  a reader half-remembers rather than an identifier they copied: `help
  bulk_deletion` and `help bulk-deletion` are the same question.
- Aliases reach for the word somebody actually types. `purgeable` is what they
  just read in a listing, not `snapshots`; `fda` reaches mounting, `sdcard`
  reaches volumes.
- A near miss is answered with the page rather than a menu — `help restor`
  suggests `help restoring` — and only something resembling nothing falls back to
  the full help. Refusals go to stderr, so piping the command yields pages.
- The markdown prints as written. It reads well in a terminal, survives being
  piped into something that renders it, and a renderer of our own would be a
  second markdown implementation to keep correct for no gain a reader would
  notice.

**A volume that cannot be identified is an error rather than a home folder.**
Where browsing starts has two answers and the volume decides which: the startup
disk starts at the home directory, and every other volume at its own root. There
was a third answer hiding behind those two — a device that could not be resolved
returned the home directory and logged it, which is the startup disk's answer
wearing a guess's clothes. On an external disk that guess is silently wrong: a
home path does not exist inside that snapshot, so the listing comes back empty
and reads as an empty snapshot rather than as a path that was never right.

The usage block also names the topic form now, which writing the help out as one
page rather than two made obvious.

## v0.59.1 — 2026-08-27

**"/Volumes/sdcard256gb is not on the data volume, so snapshots do not cover
it"** — said about the disk on screen, whose snapshots were mounted and readable
at the time.

`internal/vfs` translated every path as though there were one volume. It turns a
live path into its position inside a snapshot and back, and it did that against
`/System/Volumes/Data` and a fixed list of top-level directories. Asked about a
file on an external disk it answered, correctly, that the data volume does not
contain it — which says nothing about the volume that does.

A snapshot's contents are rooted wherever its volume is, so translation has to
know which volume it is about. The zero value is still the data volume, which is
the one root that is not a plain prefix: it is presented at "/" with a fixed set
of directories and the system volume's symlinks pointing into it.

`ToLive` was wrong in the same way and worse. It returned the snapshot-relative
path, so a file at `/Volumes/sdcard256gb/projects` came back as `/projects` — a
path on the startup disk, and a restore aimed at the wrong volume entirely.

The audit that found it went through every data-volume assumption in the tree
rather than stopping at the first, and five more followed:

- `Locate` lists and resolves through the data volume. Left as it was and now
  says so: it has no route in from the window, and half-widening it would be
  worse than leaving it whole.
- Search looks only through the startup disk's snapshots. The volume is now
  stated where it is chosen rather than defaulted, so the limit is visible at the
  point it is decided.
- The scheduled task prunes every volume and logged the count from one, a line
  describing something the run had not done. It reports per volume.
- The folder-verdict cache was invalidated by watching the home directory alone.
  A verdict about a folder on another volume stayed cached after that folder
  changed, and a stale verdict does not look stale — it looks like a comparison
  that is wrong. Every browsable volume is watched.
- `TakeNow` returns the startup disk's mountpoint though one call snapshots every
  volume. Correct for what the window does with it, and now said.

The package-level `Canonical`, `ToSnapshot`, `ToLive` and `Covered` are gone
rather than kept as data-volume wrappers. A default that silently means one
volume is exactly how this shipped.

## v0.59.0 — 2026-08-27

**A snapshot on another volume can be looked inside, not only opened.** Mounting
one already worked; the row was not selectable, so it could be attached and then
not read, which is most of the way to not working.

The reason was real rather than an oversight. Browsing resolves a snapshot to a
mountpoint and did that through the data volume's mounts alone, so it would have
read the wrong mountpoint for another volume's snapshot, or none. It also started
at a home directory, which another volume does not have.

Browsing, comparing, searching for what is gone, and restoring all name the
volume now. The name alone does not identify a copy: the same date exists on
every volume that was mounted when it was taken, and both copies can be attached
at once — so resolving by name would show someone another disk's files under the
row they opened.

Where browsing starts moves with the selection: a home directory on the startup
disk, and the volume's own root anywhere else. Another disk has no home
directory, and starting at one opens an empty listing that reads as an empty
snapshot rather than as a wrong path.

Search still looks only through the startup disk's snapshots. Crossing volumes
needs a volume on the hit type, so a result can say where it came from and be
restored from there; without one, a hit naming another disk could be restored
from the wrong copy. That is a separate change, and the code says so at the call
site rather than leaving the next reader to find out.

## v0.58.0 — 2026-08-27

**A snapshot on any volume can be opened.** The list has shown every volume's
snapshots since the last release and only the startup disk's had an Open button,
which makes a row that reads as broken.

The privileged helper accepted the data volume and nothing else. That was right
while it was the only volume whose snapshots could be listed, and it no longer
is: `tmutil localsnapshot` takes no arguments and writes to every mounted APFS
volume at once.

What has not changed is the reason the check exists — "the volume to read from"
is exactly the argument you would want to control if you could reach an elevated
process. So it is still an allowlist. It is now discovered rather than written
down, and discovered as root, from the machine itself: a caller may name a
volume, it may not add one. A volume holding no local snapshots is refused like
any other path, including the sealed system volume, whose only snapshot is
macOS's own. A machine that cannot be interrogated mounts nothing rather than
falling back, since a fallback would let an unreadable mount table silently
narrow what can be opened.

Each volume mounts into a directory named for it. Two volumes' snapshots of the
same moment share a date and would otherwise share a mountpoint — the second
landing on top of the first, showing someone another disk's files under the row
they opened. The data volume keeps the directory it has always used, so upgrading
does not orphan mounts already attached.

Closing the window unmounts every volume rather than the data volume. A snapshot
left attached cannot be deleted, and its mountpoint would outlive the window that
made it with nothing left offering to close it.

Browsing what is inside is still the startup disk's alone, because the browser is
rooted at a home directory. Another volume's snapshot opens and says where it is
mounted.

**The home screen has one spacing rule.** Two findings in a row touched while the
sections around them sat 18px apart: each element brought its own margin and one
brought none, so the space between two things depended on which two things they
were. The body owns it now — one gap, the same the screen already used between
the verdict, the body and the figures.

The volumes table was also in the wrong place, and worse than untidy. It sat
after the figures, which are pinned to the bottom of the screen, so it rendered
outside the part that scrolls and a machine with two volumes had a table it could
never reach. It is in the scrolling body now, where everything whose length
varies belongs.

## v0.57.1 — 2026-08-27

**The sidebar stopped scrolling and took the footer off screen.** Grouping the
snapshot list by volume put a wrapper between the sidebar column and the list,
and the list was the scrolling region — so the element that had to shrink was no
longer the flex child of the column. Each group sized to its own content, the
column grew to fit all of them, and the footer, held at the bottom by
`margin-top: auto`, was pushed past the sidebar's own `overflow: hidden` where it
could not be reached at all.

The comment on the rule said this would happen. It explained that `min-height: 0`
is what lets the region shrink, since a flex item otherwise "refuses to go below
its content and would push the footer out of the column instead of scrolling".
The wrapper moved without the rule.

The scrolling region is now a container holding every group, so there is one
element that shrinks however many volumes there are. The lists inside it are
plain lists again, and the spacing between groups comes from that container's gap
rather than from a margin that would have added to it.

Three tests assert the structure layout depends on: one scrolling region, every
group inside it, and the footer and whole-machine buttons outside it and after
it. jsdom has no layout engine, so pixels cannot be asserted — but the structure
is what broke, and all three fail against the markup that shipped.

## v0.57.0 — 2026-08-27

**The snapshot list is grouped by the volume the snapshots are on**, headed by
the disk's name. Headed only when there is more than one: a single disk needs no
label saying which disk, and adding one would make every Mac look as though it
had something to disambiguate.

There was no way to see the snapshots on any volume but the startup disk. `tmutil
localsnapshot` takes no arguments and writes to every mounted APFS volume at
once, so a flat list showed one volume's copies and gave no sign the others
existed — including, on the machine that found this, the six on the SD card
holding this application's own source.

**Deleting is now per copy, which is a change in behaviour.** It deleted by date
through tmutil, and tmutil removes a date from every volume holding it — so
pressing delete on what looked like one row took an external disk's snapshot of
the same moment with it, silently, because that snapshot was not on screen to
begin with.

`diskutil apfs deleteSnapshot <device> -uuid` is the only call that can tell two
copies apart, and the UUID is the only identifier that differs between them: one
date on two volumes has two. Verified on real hardware rather than reasoned
about — a snapshot created, its external copy deleted by UUID, the startup disk's
copy of the same date confirmed still present, then both cleaned up. No
authorization prompt: diskutil's "Ownership of the affected disks is required" is
satisfied by the console user, on an internal volume and an external one alike.

Retention still deletes by date, and should. A policy's verdict on a date is the
same on every volume, so removing it everywhere is the whole intent there; it is
a button beside one row that has to mean that row.

- The row identity is the copy, not the date. Confirming a deletion arms one row
  rather than every volume's row for that date, which a second click would
  otherwise delete unseen.
- Deletion fails closed: without a volume and an identifier the only call
  available is the one that deletes every copy, so it refuses instead.

Opening stays the startup disk's alone. Mounting runs through a privileged helper
that accepts one volume by design — "the volume to read from" is exactly the
argument worth controlling in a process that mounts as root — and widening it is
its own piece of work. The other groups say so in a line under the heading rather
than offering a button that cannot work.

A test flake is fixed along the way, and it had nothing to do with any test. The
Wails runtime polls for its environment on import with a setInterval whose
callback dereferences `window`; the timer outlives the jsdom environment, and the
tick after teardown threw an unhandled error that failed the run with all 252
tests passing. It was a race against the length of the run, so it stayed
invisible until the suite grew enough to still be going a hundred ticks later.

## v0.56.0 — 2026-08-27

**Snapshots were taken on every volume and pruned on one.**

`tmutil localsnapshot` takes no arguments at all — not a volume, not a flag — so
it snapshots every eligible mounted APFS volume at once. Every neighbouring verb
takes a mount point (`listlocalsnapshots`, `thinlocalsnapshots`,
`deletelocalsnapshots`); creation is the one Apple did not make selectable. This
application then listed, pruned and reported on `/System/Volumes/Data` alone, so
every other volume accumulated snapshots nothing would ever delete.

Found on a machine with an SD card at 98% full, holding fourteen local snapshots
against the startup disk's seven. Eight of them existed nowhere else, and one was
pinning its container's minimum size — while the Health screen reported a boot
volume that was entirely fine.

The mechanism is that snapshots are purgeable. macOS reclaims them per volume
under space pressure, so a date it drops from a full data volume before the
retention window expires survives on every other volume, where nothing was
looking. Pruning planned over the data volume's list, never asked for that date's
deletion, and the survivor was permanent.

- Snapshots are now listed and pruned across every mounted APFS volume that holds
  any, deduplicated by device. Two mount points can name one volume — tmutil
  answers for the volume group, so `/` and the data volume return an identical
  list — and the sealed system volume's own `com.apple.os.update-<hash>` snapshot
  is macOS's, not ours to count or delete.
- Pruning plans over the union of every volume's snapshots rather than one
  volume's list. The union is the right unit rather than a loop per volume:
  `tmutil deletelocalsnapshots <date>` removes that date wherever it lives, so a
  date is kept or dropped everywhere at once however the decision is reached.
  What was missing was only ever the seeing.
- The Health screen carries a row per volume — snapshot count, purgeable count,
  free space and the snapshot holding its container open — because a container is
  per volume and none of those is knowable from another disk's numbers. Shown only
  when there is more than one volume; one is what the figures grid already says.
- The low-space warning runs per volume and names the one that is short, along
  with the snapshot whose deletion would actually return space. The disk that
  filled produced no warning at all, because the check ran on the startup disk.

Mounting stays data-volume-only. `mountmgr` refuses any other source
deliberately — "the volume to read from" is exactly the argument worth
controlling in a process that mounts as root — so snapshots of another volume are
pruned and reported here but recovered with `mount_apfs` by hand.

The README claimed this source tree was not protected by the snapshots it
manages. It lives on an APFS SD card, so it always was, and both that and the
whole-volume note are corrected: the latter said `localsnapshot` "takes no volume
argument" where the point is that it writes to all of them.

## v0.55.1 — 2026-08-27

**A mistyped verb opened a window.** `snapshotter health` — which is not a command
— silently launched the application. Nothing said the verb was wrong: the only
thing that spoke up was the one-window guard refusing the second copy, which
describes a different problem and sends the reader looking somewhere else. That is
how it was found, on a machine that then had two icons in its menu bar.

The cause was `main` asking whether the first argument was a verb this build knew,
and falling through to the window when it was not. There is no longer a question
to ask: anything on the command line goes to the command line, which already
refused an unknown verb by name, in every language, with the real commands listed
beside it. That code path existed all along and was simply never reached.

`snapshotter --help` is fixed by the same change, and was the same fault one layer
down. Go's own flag set answers `-h` itself, so it printed the usage for
`-take-snapshot` and `-watch` — the two flags the launchd agents are installed
with — and never mentioned a single command. `snapshotter help` was correct all
along, which is what kept the other spelling hidden. Both now print the same
thing.

| Command | Before | After |
| --- | --- | --- |
| `snapshotter health` | opened a window | `no such command "health"`, exit 2 |
| `snapshotter --nonsense` | Go's flag error | `no such command "--nonsense"`, exit 2 |
| `snapshotter --help` | listed two launchd flags | the real help, exit 0 |
| `snapshotter help` | the real help | unchanged |
| `snapshotter` | opens the window | unchanged |

The flags the launchd agents are installed with reach their branches unchanged,
which matters more than the rest of this: installed plists name them, and
orphaning an agent would take the schedule off a machine silently. Both were run
against the built binary — the watcher against an empty list, and the scheduled
task against a scenario runner, so no real snapshot was taken to prove it.

## v0.55.0 — 2026-08-27

**The bulk-deletion tripwire watches directories you name, and nothing else.** It
watched the entire home directory, with an ignore list to quiet the parts that are
not anybody's work — which is the wrong way round. `~/Library` deletes in bulk as
a matter of routine: caches, container state, mail indexes, every application's
idea of scratch space. So the wire tripped on deletions nobody had asked about,
and each trip pinned another whole-volume snapshot on the disk. The ignore list
needed to stop that is a list of everything the machine does, written after each
surprise, and it is never finished.

Naming what to watch instead is smaller and answerable: `~/projects` is one line
and it is the thing worth protecting. What is not on the list is not watched,
which is a rule someone can hold in their head.

- New setting `tripwire.watch`, a list of directories. A leading `~` is expanded.
  Editable in the window under *Watching for bulk deletions*, or from the command
  line with `snapshotter config` like every other setting.
- **The tripwire is off by default, with nothing on its list.** It was on by
  default before, on the reasoning that it costs nothing until it fires — but it
  fired constantly. An existing settings file keeps whatever it already says, so
  this reaches new installations; the Health screen says when nothing is named.
- Deletions are counted **per watched directory** rather than in one running
  total. A single total made the threshold easier to reach the more directories
  were watched, which is the opposite of what adding one should do. Two hundred
  gone from `~/projects` trips it; a hundred there and a hundred in `~/Documents`
  does not.
- The cooldown after a snapshot stays **shared** across all of them. An APFS
  snapshot is of the whole volume, so the one taken for `~/projects` already
  covers `~/Documents`, and a second a moment later costs disk and captures
  nothing new.
- Installing the watcher is refused while the list is empty, and such a watcher is
  no longer restored at startup. Installed and watching nothing reports itself as
  running and protects nothing.
- The Health screen reports an empty list as information rather than a warning,
  and offers no button: what to watch is the one thing this application cannot
  work out on someone's behalf.
- There is no fallback to the home directory when the settings cannot be read.
  That fallback turned any such failure into watching everything, which is the
  behaviour this list exists to end. The agent logs why and idles rather than
  exiting, so `KeepAlive` cannot turn an unconfigured watcher into a crash loop.

A deletion is attributed to the longest matching watched directory, on the
resolved path. So a directory watched inside another keeps its own count rather
than feeding its parent's, `~/projects-archive` is never counted against
`~/projects`, and a root named through a symlink still matches — FSEvents reports
`/private/tmp` and never `/tmp`.

`ConfigService.WatchFolder`, which removed an entry from the *ignore* list, is now
`StopIgnoringFolder`. Next to a list of watched directories, a method called
`WatchFolder` that does not add to it is a trap.

## v0.54.1 — 2026-08-22

**v0.54.0 shipped with no window in it on Apple Silicon.** It worked on Intel. On
an ARM Mac the window opened white, saying "no `index.html` could be found in your
Assets fs.FS".

The universal build compiled the two architectures in parallel, and each half
pulled in a frontend build — which empties `dist` before writing to it. One
compile read that directory while the other's build had just emptied it, and
embedded a window with nothing in it. `lipo` then joined the good slice to the bad
one, so the released binary contained the assets in the half nobody was running.

That shape defeats every obvious check. `strings` finds the assets, the size looks
right, running the binary prints nothing wrong — Wails returns the error as an
HTTP body, so the webview paints it and stderr stays empty. It took a screenshot
of the window and a `lipo -thin` to see it at all.

The build is sequential now, with the frontend built once. Three things check it
where nothing did before: the embedded assets are asserted against the same asset
handler the window uses, so removing `index.html` fails a test rather than a
release; the release workflow re-runs that after the bundle build, which is the
only moment the files on disk are the ones just embedded; and it thins the binary
to check each slice separately, because a fat binary will pass any check its
better half satisfies.

The same race had already surfaced once as a `go mod tidy` failure mid-release. It
passed on a rerun and was written off as a flake.

`docs/TROUBLESHOOTING.md` is new, and records this with a one-command diagnosis.

## v0.54.0 — 2026-08-22

**How readily the bulk-deletion watcher trips is a setting.**

It was two hundred deletions in five seconds, hardcoded, with no caller able to
say otherwise. There is a dropdown now, on the options screen beside the
watcher's log: Cautious, Balanced, Sensitive, Very sensitive.

A name rather than a number, because the number alone is unanswerable — whether
two hundred files in five seconds is a lot depends entirely on what the machine
does all day, which is the thing being configured. Each option shows the count it
stands for, with the window beside it, since "25 files" would otherwise read as
"25 files ever".

Balanced is the old threshold itself rather than a copy of it, so adding this
cannot quietly change what an existing installation does.

The change applies the next time the watcher starts, and says so: it is a
separate background task that reads the setting at startup. If it is not
installed at all, that is said above the dropdown rather than below it.

Also: `snapshotter config set` refused a bad theme and a non-number but accepted
any string for `appearance.language`, which the window refused — so a value typed
at the command line was accepted and then silently ignored. Both that and the new
setting are validated now, listing what is on offer.

### The numbers belong to the profile that uses them

The interval and the window sat above the choice of retention profile, the second
labelled "Flat window". That read as belonging to the flat profile alone — and it
was reasonable to conclude that choosing a tiered profile was showing the wrong
options.

Both apply to every profile. A tiered profile's first band *is* that rate for that
span, and its later bands are multiples of the span: a fortnight gives reaches of
26 or 104 weeks depending on the shape. The same number, a different promise, under
a name that named only one of them — and the multiplication was nowhere on screen,
which made a large lever look like a small one.

The choice comes first now, and the numbers it uses come after, labelled for the
profile in force: "Keep everything for" when nothing is thinned, "Keep every
snapshot for" when it is, with what that profile does with them underneath.

Nothing about what gets installed changed. Both numbers were already building the
bands — that was v0.53.0's fix. What was missing was the screen saying so.

### Five more messages that were translated and never used

A translated message nobody asks for is invisible to every test that existed: it
does not render wrong, does not throw, and leaves the four catalogues perfectly
consistent with each other, because those tests compare the catalogues to one
another rather than to the code that reads them. So the English stays written into
the markup and the screen looks finished in one language.

That had happened five times. Every key must now be asked for by something, and
the check found all five: "Closed" after closing a snapshot, the disk gauge's
label and its tooltip, the paragraph explaining why a schedule is needed at all,
and the sentence the health screen shows when nothing is wrong — which a reader of
any other language saw in English on a perfectly healthy machine.

Eleven genuinely dead keys went with them, including four translated language
names: the picker deliberately shows endonyms instead, so that "Deutsch" is
findable by a German speaker looking at a Spanish interface.

## v0.53.1 — 2026-08-22

**An upgrade left this Mac unprotected, and the release before this one caused
it.**

Homebrew's cask unloads both launchd agents before staging a new version. That is
expected and handled: the settings file is the intent, launchd is the current
state, and the application reconciles the second to the first at startup. The
comment on that code names this exact scenario.

It did not work, for three reasons that compounded.

The retention presets were renamed in v0.53.0 — correctly, since a name with a
number in it is wrong for four of the five windows once the reach follows the
window — but nothing translated the old names. A settings file saying
`tiered-52-weeks` resolved to no policy at all. There is a migration now, mapping
each old name onto the shape it described.

Then the cascade. Both agents were restored under one early return, which quietly
made them a single protection instead of two: the unresolvable policy failed the
schedule and then skipped the bulk-deletion watcher entirely, though the watcher
has no policy and nothing was wrong with it. One rename took both protections off
the machine. They are attempted independently now, and every failure is carried
back rather than the first one returned.

And it was silent. Putting the agents back posted a notification; failing to put
them back wrote a line to a log file nobody opens. The reader was told when
nothing was wrong and told nothing when something was.

**One window at a time, whatever it was built from.** The old guard searched the
process table for the installed application's path, which caught exactly one of
the four ways two copies happen: a development build beside the installed one.
Two development builds, two installed copies, or a copy launched from anywhere
else were not prevented at all — and two ran at once on the author's machine
today, which is a claim one of the tests explicitly made and was wrong about. It
is a lock now, which catches every combination and releases when the holder dies,
including on a crash. The refusal names the copy holding it, since two identical
menu bar icons cannot answer that.

## v0.53.0 — 2026-08-21

**The retention rules move into a package that knows nothing else, and the two
things they got wrong are fixed.**

`internal/retention` is arithmetic on timestamps. It does not know what a snapshot
is, that they live on APFS, that deleting one needs a privileged command, or that
any of it is described to a person in German. `Plan` takes times and returns
positions, so the caller keeps its own objects and its own tie-break. The rule
that decides what gets destroyed can now be tested exhaustively, in milliseconds,
with no filesystem and no language set — which it could not before, and was not.

**Presets ignored the person entirely.** Their first band was hardcoded to
"everything for two days", so the time period and flat window chosen in the
interface selected nothing: picking a tiered preset silently discarded both.
Presets are built from those two choices now, which makes a preset that ignores
them impossible to construct.

**The line explaining each preset was false.** It claimed tiering costs about half
a flat fortnight. It was 22% of one at an hourly schedule and 180% at a daily one
— false at two of the five intervals a person can choose. It is deleted rather
than corrected: it was a sentence written by hand beside three values derived from
the policy, and a corrected sentence would drift again at the next change to a
band. The derived description says the same thing, truthfully by construction.

Five tests asserted that claim, three of them at a single hardcoded interval,
which is why it survived. They now assert the trade actually on offer, at every
interval.

**Every preset has three bands**, whatever is chosen. They did not: the later
bands were spans fixed in absolute days, so a window reaching past one made it
disappear and a three-band preset quietly became a two-band one. Those bands are
multiples of the window now, and the preset names describe their shape rather than
a reach that no longer holds.

**And the interface says that only one snapshot per period is kept.** It already
worked that way, in the case where it is most surprising: the tripwire takes a
snapshot when files start disappearing, ten minutes after a scheduled one, and
retention removes it. The outcome is right — the earlier snapshot holds more of
what was about to be deleted — but the application sends a notification naming
that snapshot and then quietly deletes it.

### Three capabilities that existed and could not be used

Every exported service method is bound into the window, and eleven of them had no
caller anywhere — not the window, not the menu bar, not the command line. They
were implemented and tested and impossible to invoke. Three now have a way in.

**What has gone since**, a second mode on the search screen. Searching by name
assumes you know what the file was called; this assumes you do not, only that
something is missing from a folder you remember. It lists what the folder held
when the snapshot was taken and holds no longer, with a restore beside each row.
The date shown is when the file was last written, not when it went — nothing
records the moment of a deletion.

**Deleting one snapshot.** Retention deletes on a schedule, so someone reading a
low-space warning had no lever at all. Asked twice, because a snapshot cannot be
recreated, and one question at a time: two identical prompts a row apart is how
the wrong one gets answered.

**The bulk-deletion watcher's log**, beside the scheduled task's. The task's log
answers "why is my history thinner than I asked for". The watcher's answers the
harder one — why a deletion went by without a snapshot — and reaching it needed
the path and a terminal. "Not installed" now says so, rather than showing an empty
log, which reads as "nothing has happened" when the truth is "nothing is
watching".

The audit that found them is a test now: every bound method must be reachable, or
listed as deliberately not, with the reason. The eight remaining have theirs
written down.

### One account of what the schedule does

Three places said what the schedule does, and all three built the sentence
themselves from the interval and the retention window. That ignores the policy,
so all three were wrong for every tiered schedule: they read the horizon as the
retention, which is true of a flat window and of nothing else.

The menu bar announced "Every 3 hours, kept 364 days" for a policy that keeps one
snapshot every four weeks past the twenty-sixth. The window's figure grid said
"Every 3h, kept 14d" — the same mistake, in English, written into the markup. The
settings screen read the policy properly and said something different and correct.
Nothing noticed, because no two of them were ever compared.

There is one place now, and it names the mode, which is the thing a reader most
wants and never had: "Flat window: every 3 hours, kept 14 days", or "Tiered —
daily, then weekly: every 3 hours, thinning out to 26 weeks". It words itself by
kind rather than from one template, because a flat window keeps everything for its
span and a tiered one does not.

Three views, sized to their space: the menu bar's line with the full sentence on
its tooltip, the window's four-column grid with the mode's name and the line on
its tooltip, and the settings screen's full sentence.

Four more blocks of English were written into the settings screen's markup, two of
them with catalogue entries that nothing called — translated once and never wired.

### A refusal that only English speakers could act on

macOS refusing to mount a snapshot for want of Full Disk Access is the one failure
here that needs an explanation rather than a message, so the window replaces it
with instructions and a button to the settings pane. It recognised the refusal by
looking the phrase up in the translation catalogue and searching the error for the
result — but the error is hardcoded English, so in German it searched an English
message for "Voller Festplattenzugriff". For every language but English there were
no instructions, no button, and the raw refusal instead.

### The words the service sends are the reader's words too

The window shows what the service gives it and cannot know what any of it says, so
whatever language a message arrives in is the language it is read in.

`services/diff.go` used the catalogue zero times while its sibling used it
twenty-nine, so all five of its notes were English: a file in neither version, a
picture too large to show, a file that looks binary, a file too large to compare by
lines. `search.go` was half done — two of four notes translated, and one of the
others built "1 snapshot(s) were not searched" by hand, which is the exact shape a
plural rule exists to avoid.

The right-hand side of a comparison was named with the English words "the live
disk", which the window interpolates into a sentence: "nicht mehr in the live
disk". It sends a stamp or nothing now, and the window supplies the word.

Two more were English written into the markup: the search screen built "1 open
snapshot" / "2 open snapshots" with a conditional "s", and a row's tooltip said
"Open" as a bare literal.

### Five corrections to the translations themselves

German said `Speicherdruck`, which reads as memory pressure — the message is about
the disk filling, so it sent the reader to look at the wrong thing. `freigebbar` is
not a German word; Apple's German for purgeable space is `bereinigbar`, and French
uses `purgeable` rather than `libérables`. `Zusicherungen` means assurances, where
"reservations" here means reserved space. And `sous pression disque` is a calque.

Checked across all 858 translations and found correct: the register is consistent,
French keeps its space before `;` and `:`, German and Spanish theirs before `%`,
Spanish opens its questions with `¿`, no string is left in English, and every
macOS name that must not be translated survived.

### The health screen no longer disappears when an action fails

An action that failed took the early-return error branch, which replaced the whole
screen — verdict, findings, and the button that failed — with a single sentence.
The banner sits above the findings now, all of which are still true.

### Words that cross from Go to TypeScript are checked

A status, a finding's action, a finding's level: each is a string invented in Go
and read in TypeScript, and each fails silently in a way that looks like
sloppiness rather than breakage. A status with no catalogue entry shows its own key
where a word belongs; an action nothing branches on gives a finding no button.
Nothing checked any of them.

`Kind` had already drifted from itself: a file found to be binary at the sniff
sample said "binary", one found past it said nothing at all, and a file too large
said nothing either. All five values are named now.

### Tests

The window went from 26 tests to 210, and from an unmeasured 75% of its own
statements to 97%. Go went from 443 to 637 across 24 packages, and the frontend
coverage report stopped counting generated bindings, which had been holding the
headline figure at 40% while the hand-written code sat at 75%.

Two of those tests found the bugs above. `comparePixels` had never executed at
all — jsdom implements neither canvas nor ImageData — and the folder-verdict
watcher, whose failure is a browser that goes on calling a folder identical after
the user has changed it, had nothing pinning it either.

## v0.52.0 — 2026-08-21

**The menu says what the schedule is, under the strip that says whether it was
kept.**

"Every 3 hours, kept 14 days", or "No schedule — snapshots only when you take
one".

The two lines are only readable together. A solid strip means nothing until you
know whether a mark stands for an hour or a day, and an interval means nothing
without seeing whether it was actually kept. Until now the strip was there and
the schedule it measured against was not, so the reader had to infer the interval
from the spacing of the marks.

**Four menu items were still English.** "Next due", "Take a snapshot now", "Open
Snapshotter" and "Quit". Three of them already had messages in the catalogue that
nothing was calling — translated once and then never wired, which no test can
catch: an unused message is not a missing one.

## v0.51.0 — 2026-08-20

**Each mark in the menu bar strip is one scheduled snapshot, not one hour.**

The strip measured the machine against a schedule nobody had chosen. On a
three-hourly schedule two marks in every three were empty however well it was
doing — the most a working machine could reach was 33% filled, and a real machine
sat at 29%. A healthy strip and a failing one looked nearly the same, which makes
the graphic worse than absent: it reported success as failure.

A mark is now one period of the configured schedule. A schedule that never misses
draws a solid strip, and a gap is a snapshot that was due and did not happen —
which is the only thing the strip was ever meant to say.

Changing the interval re-buckets the history rather than invalidating it. Moving
from three hours to six leaves the strip solid: the extra snapshots taken under
the denser schedule share a cell, which is what "at least one snapshot in this
period" means. Moving the other way shows gaps for history taken before the
change, and those gaps are honest — that history did miss, against the schedule
now in force.

The caption names the unit and the span, because a mark standing for three hours
looks exactly like one standing for an hour.

**Two translations were wrong in a way worth naming.** Spanish and French put an
adjective before the interpolated span — "Últimas {{.Span}}" — and the gender of
that value changes with the unit: "2 días" is masculine, "3 horas" is feminine. No
single form agrees, so the construction had to go rather than the word be swapped.
Both now avoid the agreement entirely.

## v0.50.0 — 2026-08-17

**The figures at the foot of the home screen use four columns, or eight.**

v0.49.0 capped them at four and stepped down through three and two as the window
narrowed. Three does not divide eight, so the narrow layouts had the same short
last row the cap was meant to remove — 3+3+2 rather than 7+1, but still ragged.

Four and eight are the only counts that divide eight evenly. It is four, and one
row of eight on a window wide enough to read them.

A ninth figure will break this, and deliberately. The layout no longer quietly
re-wraps around a number that does not fit, which means adding one is a decision
about the ninth figure rather than something the grid absorbs and makes ugly.

## v0.49.0 — 2026-08-17

**The figures at the foot of the home screen stop at four columns.**

They were laid out with `auto-fit`, which takes as many columns as will fit. There
are eight figures, so a wide window fitted seven and left the eighth alone on a row
of its own — a grid that looks broken rather than a grid with a short last row.

Four divides eight evenly. The layout still narrows to three and then two as the
window does, so nothing is cramped on a small screen.

The comment that used to sit here argued against pinning the column count, on the
grounds that the number of figures is not fixed and a fixed count only moves the
problem to the ninth. That is true, and it is still the better trade: 4+4+1 is a
tidy grid with a short last row, where 7+1 is a grid that looks like a fault.

## v0.48.0 — 2026-08-17

**"Free space is low" now says how low, and what it costs.**

It read "Free space is low, so retention is not guaranteed", which is true and
tells nobody anything: not how low, and not what "not guaranteed" means for the
snapshots they think they have. It reads "Only 45 GB left — old snapshots may
start being dropped".

The amount is formatted per language rather than with a format string, because
the decimal separator is a comma in German, Spanish and French: "1.5 GB" is wrong
in three of the four languages this ships with. `x/text/message` knows the rule
and was already a dependency.

**Four finding details were still English.** The low-space one, the stale-schedule
one, the conflicting-agent one and the simulated-readings one. They were details
rather than titles, which is why a sweep looking at what a menu shows kept missing
them — and why the claim that everything was translated was wrong.

Three tests found the finding by matching its title text, so improving the wording
broke tests that had no opinion about the wording. They match on `Kind` now, which
is the stable identifier and the reason `Kind` exists.

One test fixture described a machine that cannot exist: it set the free
*percentage* without the free *bytes*, though Check derives the first from the
second. That was invisible until the finding started naming the amount, at which
point it reported "0 B".

## v0.47.0 — 2026-08-17

**The entry point moves to `cmd/snapshotter`, and the repository root holds no Go
files.**

Two things had been keeping it there, and both were mechanical rather than
deliberate: `go:embed` cannot reach outside the directory of the file declaring
it, and `main.go` embedded two things that live at the root.

`frontend/dist` is now embedded by `frontend/embed.go`, beside what it embeds. The
three menu bar glyphs moved into `internal/menubar/icons/`, which that package
already embeds — they belong there anyway, since drawing menu bar imagery is what
it is for, and `build/icons/findings.sh` had already established that rendered
PNGs live with the package while `assets/` keeps the design sources.

Three things in the build had to follow, and one of them would have broken the
release quietly. `wails3 generate bindings` falls back to the current directory
when given no pattern, so with no Go files at the root it found zero services,
emitted a warning, and — because the task passes `-clean=true` — deleted all
twenty binding files. It is given `./...` now. The two `go build` invocations name
`./cmd/snapshotter`.

One test changed rather than being adapted around: the menu bar glyph test
compared slices by address, which held only because they were package-level
embedded variables. They come from an embedded filesystem now, which returns a
fresh copy per call, so it compares contents.

Verified by running the packaged build rather than by reasoning about it: the
binary carries the frontend and the tray glyphs, and the server build works too.

## v0.46.0 — 2026-08-17

**Removes the duplicate code paths that caused two bugs, and the two they were
still hiding.**

Both of yesterday's translation bugs had the same shape: a second code path doing
the same job, where translating one left the other in English. A scan for the rest
of that class found four more, two of which were live faults.

**Process setup was duplicated across four entry paths** — the window, the two
launchd agents and the command line — each remembering part of what a process must
do before it prints anything. There is one function now, in `internal/boot`.

That fixed a bug nobody had reported, because its only symptom is the absence of
output: the command line returns before the only call to `trace.SetEnabled`, so
`logging.verbose: true` was silently ignored for every CLI command.

**`coverage()` existed three times**, in two languages, all carrying the same two
thresholds. Go's two are one function now. The TypeScript copy cannot call Go, but
its thresholds are named constants matching the Go ones rather than bare numbers.

**The same sentence had three message keys.** "Under an hour" was
`count.underAnHour`, `cli.underAnHour` and `health.underAnHour`. A correction to a
translation had to find every copy. The keys correspond now.

Two more untranslated things fell out of looking:

The headline ended with an English-only pluraliser, so a German reading finished
"— 3 things to look at". The whole sentence is one message now rather than three
fragments, which also lets a language order it differently.

And the window's `age()` — "just now", "5 min ago", "yesterday", shown against
every snapshot in the sidebar — had never been translated at all. It also now uses
i18next's plural forms rather than one form doing duty for both, as do the day and
hour counts, which had been slipping past the plural machinery entirely.

**One item was rejected after implementing it.** Centralising the eighteen
`config.Load()` calls looked worthwhile and was not: nine of them read the file in
order to write it back, so they must see it as it is rather than as something
remembered. That left one caller that runs once at startup, which is indirection
without a benefit and a stale-settings failure mode that does not exist today. The
reasoning is in `docs/human-code-report-2026-08-17.md`.

## v0.45.0 — 2026-08-17

**Changing the language now changes the menu bar.**

It claimed to already. What actually happened is that the menu was rebuilt only
when the *refresh interval* changed, so switching language left the old words in
place until the next tick — up to a minute — which is the relaunch-shaped wait the
whole mechanism exists to avoid.

The cause was a list. The code decided which settings were worth a redraw by
enumerating them, and the enumeration was written before there was a language to
enumerate. It redraws on any settings change now, which costs one health check
each time a person changes a setting.

Also records what was applied, under verbose logging. The effect of this code is
on the menu bar, which no test and no log could show — the only signal that it was
broken was someone looking at a menu that had not changed.

## v0.44.0 — 2026-08-17

**The command line speaks the four languages as well.**

It reads the same settings file the window writes, so it already knew the
language and simply was not using it. `snapshotter config set appearance.language
de` now changes what the terminal prints, from the next invocation onwards. 109
messages per language, up from 70.

Running it in each language found two things a sweep for English literals had
missed, because neither was a literal any more.

The **age column** printed "11m ago" whatever the language: the strings existed
in the catalogue and the call sites had never been wired to them. It reads "vor 11
Min." in German now. The abbreviation keeps its full stop, which German requires
and English does not, so the punctuation test carries a documented exception for
the three of them rather than being loosened.

The **coverage phrase** read "21 Snapshots, decken 4 days ab" — German sentence,
English duration. The command line had a duration formatter of its own, separate
from the window's, and only one of the two had been translated. Both use the same
counting messages now.

Command syntax is deliberately left alone: `run -- <command> [args...]` and
`config get <key>` are what someone types, not what they read, and a translated
flag name would be a flag that does not work.

Also fixes a real defect the translation exposed rather than caused: several
messages had been passed to `fmt.Errorf` as format strings, which `go vet`
rejects. A translated string is data, not a format — a translator could otherwise
have broken a verb and turned an error message into `%!s(MISSING)`. Those are
`errors.New` now.

## v0.43.0 — 2026-08-17

**Everything a person reads is translated.**

What was left after v0.42.0: the browse notes, the schedule's notes and policy
descriptions, the search notes, the Time Machine thinning warning, the macOS
password prompts, and the notifications the two launchd agents post. 70 messages
per language, up from 39.

Three of these needed more than a lookup.

The **retention sentence** — "Everything for 2 days, then one a day out to 14
days." — was assembled from fragments glued with " for " and " out to ". Each
clause is a whole message now, because the order of rate and span is not the same
in every language and gluing fixes English's. In German it reads "Alles für 2
Tage, dann einer pro Tag bis 14 Tage." Its first letter is upper-cased by rune
rather than by byte, so a sentence opening with "Ü" is not cut in half.

The **password prompts** carried two helpers, `possessive()` and `subject()`,
returning "its"/"their" and "It"/"They". They existed only to make English agree,
and no other language agrees along the same axis. Both are gone, replaced by
singular and plural forms of one message.

The **agents** never reached the window's setup, because each runs as its own
launchd process. A scheduled snapshot failing at three in the morning would have
posted its notification in English to someone who chose German. They set the
language before they can post anything now.

Two package-level values had frozen the language at process start — the retention
presets and the thinning warning. Both are functions now, for the same reason the
findings became functions in v0.41.0.

**Deliberately not translated**, because they are not prose: `"No destinations
configured"` and the scenario runner's output, which are matched against what
Apple's own tools print; `"Application Support"`, which is a path; and
`"ProgramArguments"` and `"App Background Activity"`, which are a plist key and a
macOS-defined label. Translating any of them would break something. The command
line tool is also still English, being a separate surface.

## v0.42.0 — 2026-08-16

**Replaces three hand-written implementations with the libraries that already
existed.**

The window now uses **i18next** and **react-i18next**, the Go side uses
**go-i18n**, and image comparison uses **pixelmatch**. Roughly 250 lines of
translation machinery and pixel comparison were deleted.

This should have been the first choice, and the cost of it not being was visible
in this changelog one version ago: v0.41.0 said the counting headlines "need
plural rules per language rather than a lookup, which is a different piece of
work". That work is what i18next and go-i18n do. Having written the lookup, I had
to describe its central limitation as though it were a future feature.

**The plurals are done now.** "12 snapshots, 3 days of cover" is translated,
using CLDR plural categories rather than an appended "s". A test covers the case
that makes this worth a library at all: French treats zero as singular, so it
says "0 jour" where English says "0 days". A hand-rolled pluraliser gets that
wrong and nobody notices for a year.

`internal/text.Plural` is no longer used for anything a person reads.

Two things were kept rather than lost in the move. Keys stay compile-time checked
— i18next allows any string by default, so `CustomTypeOptions` is declared to
restore what the typed catalogue gave. And the tests that check the translations
themselves stay, because no library can see a dropped clause or a per-cent sign
written the English way in a German string; what went are the tests that were
really checking a lookup table, which the library now owns.

Image comparison keeps only the part specific to this application: getting two
data URIs into ImageData, refusing a mismatched pair, and producing something an
image tag can show. The comparison itself is pixelmatch's, which does perceptual
difference in YIQ space and anti-aliasing detection — a naive per-channel
threshold, which is what was there, reports a re-saved JPEG as changed everywhere
and a one-pixel text shift as a changed outline.

## v0.41.0 — 2026-08-16

**The Go side speaks the four languages too.**

Until now the window was translated and everything the Go side produced was not,
which meant the Health screen was half German and the menu bar entirely English.
The health findings, the headline, the menu bar's own text and the notification
posted when a schedule is restored are now translated.

`internal/i18n` is a second catalogue rather than a shared one. The two key sets
barely overlap — almost nothing appears on both surfaces — and they share what
matters, which is the setting. The language is written to the settings file by
the window; the watcher that was already redrawing the menu bar applies it. So
choosing a language changes both surfaces without a relaunch.

The findings became functions rather than package-level values. A value is built
once at startup and keeps whichever language was in force then, which would have
made the setting need a relaunch to take effect — the one thing it was designed
not to need.

Go has no equivalent of the frontend's `Record<Key, string>`, which turns a
missing translation into a compile error, so this carries the same guarantees as
tests instead: every catalogue must hold every key and no others, nothing blank
or padded, placeholders preserved, and nothing obviously truncated. A missing key
returns the key itself, which is deliberately ugly — a missing string should look
like a fault rather than like a terse label, because the second kind gets shipped.

**Still English:** the schedule's policy descriptions and log notes, the browse
and search notes, and the two headlines that count things — "12 snapshots, 3 days
of cover". Those last need plural rules per language rather than a lookup, which
is a different piece of work from this one.

Also validated all four window catalogues: a Spanish per-cent sign missing its
space, and "different sizes" where "different dimensions" was meant, in English
and Spanish both. Three more checks are now tests — per-cent spacing, ellipsis as
one character, and no whitespace at a string's edges.

## v0.40.0 — 2026-08-16

**A file in neither version says so, instead of failing.**

Comparing needs one side to have something on it. With both sides empty there is
nothing to show — but that was returned as an error, which put a red banner over
a question that was perfectly reasonable to ask.

It is reachable by ordinary use rather than by doing anything strange: open a file
that exists only on the live disk, then point the right side at a snapshot taken
before that file was made. Neither version holds it. Nothing has gone wrong, and
the answer is simply that there is nothing to compare.

One test changed with it, from asserting an error to asserting the explanation.

## v0.39.0 — 2026-08-16

**The name and the contents both have to say "picture".**

Only images were ever shown this way — a zip fell through to "no lines to
compare" like any other binary — but the decision was made on the file's
extension alone, and that is wrong in a way worth fixing: a zip renamed
`photo.png` would have been encoded, sent to the window and handed to an image
tag, which draws a broken icon and explains nothing.

The contents are sniffed now. Where the sniff recognises a picture it decides,
because the web view sniffs too and will draw what the file actually is. Where it
positively identifies something else — a zip, a PDF, an executable — that is not
a picture whatever it is called.

The extension is allowed to decide only where the sniff has no opinion at all,
which is not a loophole but the point: HEIC, AVIF and SVG are unknown to the
standard library and drawn perfectly well by the web view, and HEIC is what this
Mac's own screenshots and photographs are. Rejecting them would have been the
worse error.

**And a missing picture says which kind of missing it is.** An empty half of the
pair read "not in this snapshot" on either side, which is only true on the left.
Missing on the left means the picture was added after the snapshot was taken;
missing on the right means it has been deleted — which is the case someone
browsing a snapshot is most often here to find. Each side now says its own, in
the frame the picture would have had, so the two still read as a pair.

## v0.38.0 — 2026-08-16

**Pictures are shown, not described.**

A screenshot used to report "this looks like a binary file, so there are no lines
to compare", which is true and useless. Both versions are now put on screen, in
three modes.

*Side by side* answers "what are these", and is the default because it never
misleads. *Overlay* cross-dissolves between the two in one box, which is how a
shifted button or a changed colour becomes obvious — laid side by side those are
nearly impossible to spot, because the eye has to carry a memory across the gap.
*Difference* walks every pixel and paints what moved in magenta over a dimmed
copy of the original, and says what fraction of the picture changed.

The pixel comparison is written here rather than taken from a package. It is
forty lines against a canvas the browser already provides, and it has a tolerance
below which a pixel counts as unchanged — without one, anti-aliasing and JPEG
ringing report a re-save as a change to everything. Two pictures of different
shapes are refused rather than compared, because overlaying a 1200-wide picture
on an 800-wide one produces a confident, meaningless answer.

The diff viewer already in use cannot do any of this, and no component does both:
its inputs are strings and it virtualises by line, so the split view is two
columns of text rows rather than a general two-pane layout. Feeding it a data URI
would run a word-level comparison across eleven million characters of base64 and
draw line numbers around a photograph. GitHub, GitLab and Kaleidoscope all keep
these as separate views for the same reason.

Dimensions are read where Go has a decoder — PNG, JPEG and GIF — because "was it
resized" is a question the byte size cannot answer: a recompressed picture changes
size without changing shape. Formats without a decoder are still drawn, since the
web view renders more than the standard library reads.

## v0.37.0 — 2026-08-16

**The comparison limit was 1 MiB. It is now 16 MiB.**

One megabyte was the wrong instrument for the same reason the folder-walk budget
was: it declined things people actually wanted to look at. Sixteen takes in large
logs and generated files as well as source.

**And a large image now says it is an image.**

The size check ran before the binary check, so a 1.5 MB screenshot was told it
was "too large to compare line by line" — which implies a smaller screenshot
would diff, and it would not. The message named the first gate it hit rather than
the reason, which is the same fault as a folder reporting "too large to check"
when it was really unreadable.

Binary is decided first now, from the opening 8 KB rather than the whole file, so
asking costs almost nothing against a file that may be sixteen megabytes. A
sample cut mid-character has its trailing partial rune dropped before the UTF-8
check, so a valid file is not called binary for where the read happened to stop.

One existing test was fixed rather than adapted: it filled its oversized file
with zero bytes, which are NULs, so it would have proved the file was binary
rather than that it was large. Its fixture never matched its intent, and
reordering the checks made that visible.

## v0.36.0 — 2026-08-16

**A sentence that lost its ending in every language but English.**

"None yet. Take one now, or set up a schedule so they are taken for you." ended in
German, Spanish and French at "…or set up a schedule". The clause saying what the
schedule is *for* was missing, and so was the full stop. What remained was a
grammatical sentence, which is why it read as finished text rather than as a
fault.

The cause was mine and mechanical: when the English string gained its full stop,
the edit that added it was written to apply to English only.

Two tests now cover the shape of a translation rather than its presence: one
requires the sentence-ending punctuation English has, and one rejects a
translation under half the length of a sentence-long original. Both were checked
against the actual bug — they fail on it and pass once it is fixed.

**Three German wordings corrected as well.** "Container wird belegt" read as the
container being occupied by something else, when the subject is the snapshots and
what they do is hold space; it is now "Belegt den Container". And "nicht prüfbar"
claimed a folder was inherently uncheckable, when what happened is that one
attempt got no answer — "konnte nicht geprüft werden". French carried the same
overreach in "vérification impossible".

## v0.35.0 — 2026-08-16

**The text the sweep missed.**

"Open all", "Nothing to act on", "Newest", "Next due", the cover figure, the
schedule's explanatory paragraphs, the log heading, the search's restore
confirmation, and the browser's remaining tooltips. They survived the first pass
because they sit on lines mixing text with an expression, or span several lines,
and the line-by-line search that found the rest could not see them.

Anything holding a value now uses a named placeholder — `Open all ({count})`,
`Newest {when}` — so a translator can move it to wherever the sentence needs it.

**Two column headers renamed.** "In snapshot" and "On disk" are now "Snapshot
Size" and "Disk Size", which say what the figures are rather than where they came
from, and the header row no longer wraps.

**The bulk-deletion table loses its outcome column.** It read "snapshot taken" on
every healthy row, which is the expected case stated at length. Its absence was
the only part carrying information, so that is what remains: a row whose response
failed says so, and a row that worked says nothing.

## v0.34.0 — 2026-08-16

**The rest of the window is translated.**

v0.33.0 shipped the machinery and three screens' worth of text. This finishes the
window: Home, Health, Schedule, Search, the navigation, the banners, the table
headers, and every tooltip and placeholder. 128 keys in each of the four
languages, up from 57.

Two things needed restructuring rather than replacing. The interval and retention
lists were built at module level, where no translation is in scope; they hold
keys now and are looked up during the render that shows them, which is also what
makes them re-read when the language changes. And "Snapshotter" stays
"Snapshotter" in every language, being a name rather than a word.

**Still English:** everything the Go side produces — the headline and findings on
the Home screen, the menu bar, and notifications. That is the larger half of the
remaining text and it is next.

## v0.33.0 — 2026-08-16

**English, German, Spanish and French**, chosen from a picker beside the theme
toggle.

Two catalogues rather than one. The window's text is compiled into the frontend,
because it is needed for the first paint and a round trip to a service would mean
either a flash of English or an empty window while it arrived. The menu bar's
text stays in Go, where it is drawn. They share one setting: the language is
written to the settings file, so the watcher already there redraws the menu bar
and both surfaces change together, without a relaunch.

English is the source of truth for the key list and the other three catalogues
are typed against it, so a language missing a key fails the build rather than
falling back silently at runtime. A test covers what the types cannot — blank
strings, and placeholders dropped in translation, which would otherwise render a
sentence with its value quietly missing.

The flags are a compromise, and worth naming as one: a flag is a country and a
language is not. Each language is written in its own name beside its flag, and
never translated, so someone who has landed in a language they cannot read can
still find their own.

**Not yet translated:** the Health, Schedule and Search screens, and the Go-side
strings in the menu bar and notifications. The machinery for both is in place;
what is left is the text. The translations are machine-made and have not been
read by a native speaker.

## v0.32.0 — 2026-08-16

**The compare header no longer clips the version it names.**

The two version names were passed to the diff viewer as its own column titles,
which are laid out inside a row this application does not control — and a
snapshot stamp was tall enough to be cut in half by it. They sit in the panel's
own header now, reading left to right in the order the columns appear, with the
target selector on the side it actually changes. A file missing from one side is
said once, above the diff, rather than in a title that could be clipped.

**The menu bar's strip says what a mark means.**

It was not stale, and it was not wrong — it was answering a question nobody
realised it was answering. Each mark is one hour of the last forty-eight in which
at least one snapshot exists, so twenty-two snapshots taken close together fill
three marks. That is the entire point of showing when rather than how many, but
the caption said only "Last two days", which left the strip looking stuck to
anyone who knew their own snapshot count.

It now reads "Last two days (mark represents an hour)", which names the unit the
strip is drawn in. The rule that fills the marks lives in one function, so the
drawing and any future description of it cannot drift apart.

## v0.31.0 — 2026-08-16

**A mark on every verdict, and a tick that reads as a tick.**

Two faults, both mine, from the version that added the first marks.

Only "identical" and "could not check" ever got one. "Changed", "deleted since",
"new since" and "type changed" silently had none — a gap that looks like a
rendering fault rather than a state. All six now have their own: a pencil for
changed, a bin for deleted since, a plus for new since, and a shuffle for type
changed.

And the tick was unreadable. It was `CircleCheck` at 12px, chosen so the settled
marks would echo the spinner's ring — but a tick inside a ring at that size is
about five pixels of tick, and it read as a dot. A motif nobody can resolve is
not a motif. The marks are bare glyphs now, larger and more heavily stroked. The
spinner keeps its ring, being the only one whose shape is carried by movement
rather than detail.

A test now asserts that every status carrying a word also carries a mark, which
is what would have caught the first fault.

## v0.30.0 — 2026-08-16

**Compare, and a choice of what to compare against.**

The button that opens a file's differences never existed. The service behind it,
the panel it opens and the wiring in between all shipped in v0.22.0, but the row
never rendered a control to reach any of it — the callback was threaded through
the browser and dropped. `noUnusedParameters` is off, so nothing said so. Every
file row has a **Compare** button now, which is the first time the feature has
been reachable at all.

**The right side is a choice.** It defaults to the live disk, because "what have
I done to this since" is the usual question. Any other mounted snapshot can be
picked instead, which turns the panel into "what happened to this file between
these two dates" — something the disk alone cannot answer. The left side stays
fixed on the snapshot being browsed.

`FileVersions` gained a target argument and its fields are `Left`/`Right` rather
than `Snapshot`/`Live`, because the right side is no longer always the disk. It
also returns a label for whatever the right side resolved to, so the window names
it rather than restating the rule. An unmounted target is refused rather than
quietly falling back to the disk, which would answer a question nobody asked.

## v0.29.0 — 2026-08-16

Carries the marks work below as well: v0.28.0 was written up but never tagged,
so it reached people here rather than under its own version.

**The file listing is white in the light theme.**

It used to take the window's own background, a cool grey that made small
monospaced text harder to read than it needed to be. The listing now paints
itself, so only it changed: the dark theme is byte-for-byte what it was, because
only light was the problem.

Two things had to move with it. The sticky column header shared the old
background and would otherwise have sat as a grey band on white — it follows the
surface. And row hover was drawn with `--panel`, which is `#ffffff` in the light
theme and therefore invisible against a white listing; it now uses `--hover`,
the token that means this. Hover is consequently a little more legible in the
dark theme too.

**A mark on every folder verdict, and a better word for a match.**

The spinner said a walk was running. Nothing said how it ended, so the two
outcomes were distinguishable only by reading the text. Now a folder that
matches gets a green tick and one that could not be read gets a red cross.

All three are the same circle by design: the ring turns while the walk runs,
then closes around a tick or a cross. One shape settling rather than three
unrelated marks.

The colour is on the mark, not the words. A folder that matches is not news, and
one that could not be read is a fact about this program rather than about the
file.

**"Identical" replaces "unchanged".** What was compared is two versions of a
file, and the word should say they match. "Unchanged" describes a history nobody
observed — a file may well have been edited twice since the snapshot and put
back. The toggle follows the badge it controls: *Show identical*.

## v0.27.0 — 2026-08-16

**A spinner while a folder's verdict is being worked out.**

The word "detecting" on its own read as an answer rather than as work in
progress, which is the one thing it is not. A folder can sit there for a moment
while its walk runs, and nothing on the row said so.

Only "detecting" gets it. "Not examined" shares the same quiet badge colour but
is a finished answer, and a spinner on it would promise a result that is never
coming.

This is the first animation in the application, so it also brings the first
`prefers-reduced-motion` rule: the ring holds still rather than disappearing,
because it still marks a row as working rather than settled.

## v0.26.0 — 2026-08-16

**"Show unchanged" hides unchanged folders again.**

It never stopped hiding unchanged files — those are filtered as the listing is
built. Folders were not, and could not be: a folder arrives unexamined and only
becomes "unchanged" once its own walk answers, which happens well after the rows
have been sent. By then nothing was left to filter them.

They are hidden as they resolve. The visible list is computed once and used both
for the table and for the "nothing has changed in this folder" message, so the
two cannot disagree about whether anything is showing — which they would have, on
a folder whose contents all turned out to be unchanged.

## v0.25.0 — 2026-08-16

**Folders show the verdict they resolved.**

Every folder read "could not check", whatever the answer turned out to be. The
walk ran, reached the right conclusion, and handed it back; the row then
displayed the placeholder the listing had returned before any of that happened.
The resolved verdict was computed and used for one thing — the row's CSS class —
while every visible cell read the value it was supposed to replace.

Three explanations were offered for that behaviour before the cause was found,
and all three were wrong: the walk's budget was too low, unreadable subfolders
were aborting the answer, and macOS privacy protection was denying reads. Each
led to a real change, and none of them was why. The compiler had nothing to say
about it, because the resolved value *was* used, just not where it mattered.

### The application can now say what it knows

`logging.verbose` turns on per-verdict logging: which folder, what was concluded,
how long it took, and what stopped it when nothing could be. It can be turned on
and off while the application is running, because restarting to look at a problem
is how the problem gets lost.

A folder that could not be answered for also carries the reason back to the
window, shown in the row's tooltip — so "could not check" says what stopped it
without anyone having to find a log file. The application knew all along and was
throwing it away, which is what made three wrong guesses possible.

## v0.24.0 — 2026-08-16

**Folder verdicts are remembered, and forgotten when the disk moves.**

Answering "has anything under here changed?" costs a walk. Not a slow one, but
browsing repeats it relentlessly: open a folder, look inside, come back, and
every sibling is walked again to reach the conclusion it reached a moment ago.
Nothing changed in between.

They are cached for as long as the window is open. What makes that safe rather
than merely fast is the shape of the two sides: a snapshot is read-only and can
never invalidate anything, so only the live disk can — and the filesystem says
when it does.

What the filesystem does not say is which folders an event affects. A file edited
five levels down changes the answer for all five folders above it, and none of
their modification times move, because a directory's mtime only changes when
something is added, removed or renamed directly inside it. That is the same fact
that made the original bug possible. Given the path that changed, though, the
ancestors are just that path taken apart — so a change invalidates itself and
every folder containing it.

Nothing is written to disk. A cache that outlived the process would have to be
right about everything that happened while it was gone, which is a promise it
cannot keep and does not need to make: a cold start costs one walk, which is what
every start costs today.

### Also

A folder that cannot be read no longer decides the answer for everything around
it. One unreadable subfolder used to abort the whole walk, so a single protected
directory anywhere beneath a folder made it unanswerable — and that was then
reported as "too large to check", which was not what had happened. Unreadable
subtrees are skipped; a difference found anywhere else answers outright, and only
when nothing differs does the skip matter. The label is "could not check", which
does not guess at a reason it does not know.

## v0.23.0 — 2026-08-16

**Folder verdicts no longer take the machine with them.**

v0.22.0 resolved every folder in a listing at once. On a home directory — which
holds Library and whole source trees — that meant several full walks running
together, and the application took six cores for the best part of a minute
before settling. It finished, and it was correct, and it was unusable while it
did.

They are resolved three at a time now, and the same work finishes just as soon
while leaving the machine alone. Answers for a listing that has been navigated
away from are discarded rather than landing on the folder that replaced it.

The budget each folder was allowed turned out to be the wrong instrument
entirely. At fifty thousand entries, Library and any real source tree passed it
immediately, so every folder worth asking about answered "not examined" — a
refusal dressed up as a result. On a real machine that was every large folder in
the home directory, and none of them ever resolved.

The cost of a walk was never the problem: 192,635 files take 456ms, about 2.4
microseconds an entry, because size and modification time both arrive with the
directory read. What made the machine unusable was running every folder's walk at
once, and that is fixed where it belongs. The budget is now half a million
entries — a backstop against something pathological rather than a limit on
ordinary use — and a folder that does exceed it says "too large to check" rather
than pretending to still be working.

The status pill is held to one line. A wrapping pill stops looking like a pill
and drags its row's height with it, so a listing ends up with rows of differing
heights.

## v0.22.0 — 2026-08-16

**Differences are shown per file, from the row itself. The compare view is gone.**

The compare view walked a tree and produced a list of paths that had changed. That
tells you where to look and nothing about what is there — which is why
`react-diff-viewer-continued` has sat in the dependencies unused since the
beginning. Somebody intended this and never built it.

A file row now offers **Differences**, which opens both versions side by side,
compared word by word within a changed line so a renamed variable does not
present as the whole line differing.

Two cases are declined rather than attempted, and neither is an error: a file too
large to put through a web view, and a binary one, which has no lines to compare.
Both still report their sizes, because 2.1 MB becoming 2.4 MB is a real answer
about a photograph.

### Folders resolve one at a time

A listing is two directory reads and is instant. A folder's verdict may be a walk
of everything beneath it — and only when nothing has changed, since a difference
is returned the moment it appears. Asking for both together made every listing as
slow as its slowest folder, so folders now read as *detecting…* and fill in as
their answers arrive. A large untouched tree delays nothing but its own row.

### What went with the compare view

`Compare.tsx`, its stylesheet, and the tab. This also removes snapshot-to-snapshot
comparison — "what changed between Tuesday and Thursday" — which a per-file button
does not replace. Removed deliberately rather than by accident.

## v0.21.0 — 2026-08-16

**A folder is no longer reported as unchanged without being looked at.**

Browsing a snapshot, every directory present on both sides was reported as
unchanged. Not computed — hard-coded. A home folder with thirteen thousand
modified files under `~/projects` said "no changes", and with unchanged rows
hidden the folder disappeared from the listing entirely.

That is the one thing this application must never get wrong. It exists to answer
"what changed", and for directories it was answering without looking.

A directory is now walked until the first difference is found, and then stopped.
The browser prints one word per row, and that word is "changed" whether one file
differs or ten thousand — so counting them is work with nothing to show for it.

The asymmetry is what makes this affordable, and it is worth stating plainly: a
changed folder is answered almost immediately, because the walk stops at the
first thing it finds. An unchanged one costs the full walk, because proving a
negative means looking everywhere. Measured on a real tree of 192,635 files: the
first difference found in 11ms, the full walk in 456ms. The comparison itself is
free — size and modification time both arrive with the directory read, so no
extra call is made per file.

Where a directory is too large to answer within a fixed budget, the row says so
rather than claiming the tree is unchanged. Not knowing and nothing having
changed are different answers, and only one of them is safe to guess.

## v0.20.0 — 2026-08-16

**The outcome column says whether, not which.**

It showed the snapshot's name, which is a date stamp — the same information as
the first column of the same row, written twice. The column now says whether a
snapshot was taken at all, which is the thing worth knowing and the thing that
distinguishes one row from another. The name is in the tooltip, since the only
use it has is finding that snapshot in the list.

## v0.19.0 — 2026-08-16

**A folder per line, and a button that silences the one you meant.**

The folders in a warning were joined with commas. Once paths stopped being
truncated that became a block of comma-separated text wrapping across several
lines, which is not something anyone can read.

They are one per line now — and each carries its own **Ignore**. The single
button silenced `where[0]`, whichever folder happened to have the most deletions
in it, which is not a choice anyone made: a burst usually spans two or three
folders and often only one of them is the noisy one. The browser burst that
started all this touched two cache directories and one holding actual browser
state, and silencing all three because you clicked once would have been wrong.

Paths are also written the way people write them, with the home directory as
`~`. Twenty characters of `/Users/somebody` carry no information on the machine
they describe. The full path is still what an ignore rule is built from — `~`
means nothing to a comparison against a path the filesystem reported — so both
are sent, and the window shows one while acting on the other.

## v0.18.0 — 2026-08-16

**The buttons look like buttons, and paths are shown whole.**

The ignore action was styled as the quietest thing on its row — a faint link, in
a table whose whole reason for existing is to offer it. Both it and **watch
again** are ordinary bordered buttons now. Undoing has to be as findable as
doing: a list of silenced folders whose removal is a faint link is a list that
only ever grows.

Folder paths are no longer cut off with an ellipsis. That is a reasonable trade
where something can be opened to read the rest, and there is nothing to open
here — so the truncation simply hid the answer, and hid it from the wrong end,
since a cache path carries its distinguishing part last. They wrap now, breaking
inside a path component if they must, because the alternative is a column that
scrolls sideways.

## v0.17.0 — 2026-08-16

**Silencing a folder is a button on the warning, not a setting to go and find.**

v0.16.0 made the tripwire's ignore list configurable, and configurable meant
editing a file or using the command line. But the moment anyone wants to change
it is the moment they are looking at a warning that should not have happened —
a browser clearing its cache, a build directory being emptied — so the control
belongs there.

Each row of the bulk deletion warnings has an **ignore** action that adds that
folder to the list. Underneath, everything currently silenced is shown with a
**watch again** beside it. Being able to see and shorten the list is the half
that matters: one nobody can read grows until the tripwire watches nothing, and
that failure is silent by construction.

A folder is stored with separators around it — `/a/build/` rather than
`/a/build` — so it matches itself and everything under it without also silencing
a sibling called `/a/build-output`. The root cannot be added: a button should not
be able to switch the whole tripwire off by accident.

## v0.16.0 — 2026-08-16

**The tripwire stops crying wolf, and a development build stops hiding.**

### Cache churn is not a bulk deletion

The tripwire fired on this machine because Microsoft Edge cleared its cache:
several hundred files gone in seconds, which is the exact shape of the thing
being watched for and none of its meaning. It took a snapshot nobody needed. Left
alone, that teaches you these warnings are noise — and then the one that mattered
is dismissed with the rest.

`tripwire.ignore` lists path fragments whose deletions do not count. It defaults
to `/Library/Caches/`, `/Caches/`, `/private/var/folders/` and `/.Trash/` —
deliberately short, because every entry is a place this application will stay
quiet about. It is a setting rather than a constant, so a machine with its own
noisy directory can say so, and `config set` now accepts comma-separated lists.

Nothing that anyone would ask to recover is on that list, and a test asserts it:
documents, pictures, source files, and a folder merely *named* cache in someone's
own work all still reach the trigger.

### A development build refuses to join the installed one

Two copies put two identical icons in the menu bar, and only the one in
`/Applications` holds the Full Disk Access grant — so the working build looks the
same and cannot mount anything. Worse, a copy left running is invisible: one sat
at 300% CPU for nineteen hours on the author's machine before anyone noticed,
because nothing about it looked different.

An unstamped build now refuses to open a second window while the installed copy
is running, and says why. `SNAPSHOTTER_ALLOW_SECOND_COPY=1` overrides it. A
released build never refuses: Homebrew replaces the copy in `/Applications`, so
two of them cannot happen.

### Free space reads at a glance

The space indicator in the header is a bar and a number rather than a sentence.
Snapshots are purgeable — macOS reclaims the oldest under space pressure rather
than failing a write — so a disk filling up is how a retention setting quietly
stops being kept, and that is worth seeing without reading.

The bar fills with what is used rather than what is free, because a bar that
empties as things get worse reads backwards: a full bar looks like a full disk.
It is green, then amber below a fifth free, then red below a tenth — the same
threshold the health screen calls low, so the two cannot disagree about whether
this Mac is running out. The text only takes the colour once it matters.

### The release retries hdiutil

v0.15.0 failed with `hdiutil: create failed - Resource busy` — a shared runner
with something still holding a device — after the application had been built,
signed, notarized and stapled. It succeeded on a re-run minutes later. Failing a
release for that means a person re-running it by hand, which is the sort of thing
that gets skipped at midnight. Three attempts, fifteen seconds apart.

## v0.15.0 — 2026-08-16

**The health screen stops giving its best space to its least useful content.**

The eight figures are pinned to the foot of the window and everything else
scrolls behind them. They do not vary in length and they are what someone
glances at, so they should not move — while the area above them is nearly empty
on a healthy Mac and a list that outruns the window on a sick one. The pinned
strip is translucent, so content passing under it stays legible: that is what
says there is more above rather than the list simply ending.

The "Nothing to act on" message was borrowing a stylesheet class built for
whole-view empty states — `margin: auto`, `max-width: 420px`, forty pixels of
padding — so on a healthy machine a block of reassurance sat centred in the
middle of the window, pushing everything apart, restating three things the
header had already said one line above it.

Bulk deletion warnings are a compact table, one row per event: when, where, and
what was taken. A log is read by scanning one column, so the folder gets the
room and every row stays one line, with the full path in the tooltip. A row where
no snapshot was taken is red — that is the one worth seeing from across the room.

The copyright line is gone from this screen. It belongs in the About panel, which
has it.

## v0.14.0 — 2026-08-16

**The home screen lists recent bulk deletion warnings.**

The tripwire is a launchd agent — a separate process from the window, and almost
never running at the same time. So nothing it learns can be held in memory for
the window to show later: by the time anyone opens the window, the process that
saw the deletion has exited.

The two now share a file: one JSON object per line, in
`~/Library/Application Support/Snapshotter/events.jsonl`. The tripwire appends
when it fires — including when it fires and the snapshot *fails*, which is the
case most worth being able to look back at — and the window reads the last five.

It is trimmed to the newest hundred rather than emptied at the limit. Emptying
would throw away exactly the entries the screen is displaying, so the list would
go blank at the moment it had most to say. Writers hold a file lock across the
append and the trim, because three processes can write it and a trim that
rewrites the file would otherwise race an append. A line that cannot be parsed —
a writer that died mid-write, a field from a later version — is skipped rather
than costing the rest of the file.

The section is absent entirely when nothing has happened, rather than being an
empty heading that invites someone to wonder what is missing.

### Also

The attribution now genuinely appears in the window. v0.12.0 claimed it did; the
edit to the component silently failed to match, so the line existed only in the
macOS About panel and the stylesheet was styling an element that was never
rendered. The bundle metadata part of that release was real.

Both launchd agents now declare which application they belong to. Without it,
System Settings had nothing to attribute the background items to and fell back to
the name on the signing certificate — so "App Background Activity" listed
**Chris Thomas**, with two items under it and no way to tell what they were.

## v0.13.0 — 2026-08-16

**The tripwire says where, and errors stop vanishing before they can be read.**

### Where the deletion is happening

"Something is deleting a lot of files" tells you to worry. It does not tell you
whether it is the build directory you just cleaned out or the folder with your
invoices in it, which is the only thing you actually want to know at that moment.

The notification now names the place: **Files are being deleted from
~/Documents/Invoices**. The tripwire records the folder each disappearance was
in — the folder, not the file, because the file is gone and its name helps
nobody — and reports the busiest first when the wire trips. Home is written the
way people write it. At most three are named: past a handful the list stops
describing anything and gets dismissed unread.

### Errors that flashed red and vanished

Trying to open a snapshot and failing showed the reason for a fraction of a
second. This was reliable rather than intermittent, and the cause is worth
writing down: mounting raises an authorization dialog, the window refreshes
whenever it is looked at again, and that refresh cleared the error on success.
Dismissing the password prompt handed focus back, the refresh succeeded, and the
explanation was wiped before it could be read.

A background read succeeding says nothing about whether the thing you asked for
worked. The error is held state now: neither the overview nor the health refresh
clears it, and it survives every refresh until something replaces it. The clears
that happen at the *start* of an action are kept, because a new attempt genuinely
does start with a clean slate.

Because it persists, it can also be dismissed — clicking it clears it, which is
what the status banner beside it has always done. An error that cannot be got rid
of except by doing something else is a worse thing to leave on screen than the
one that vanished too fast.

## v0.12.0 — 2026-08-16

**Snapshotter is written by Chris Thomas and published by Antimatter Studios.
Both are now named.**

The bundle credited one and not the other, so the About panel macOS builds from
that metadata said only half of it. The copyright line now reads
`© 2026 Chris Thomas. Published by Antimatter Studios.`, and the company recorded
in the bundle is the publisher.

The same line appears under the health screen. It was previously only in the
About panel, which means it was only in the menu bar — someone reading the window
to work out what this thing is had nowhere to find it.

The bundle identifier is deliberately unchanged. `com.christhomas.snapshotter` is
what the Full Disk Access grant is attached to, what both launchd agents are
labelled with, and what the Homebrew cask quits and unloads by name. Renaming it
to match the publisher would revoke the grant, orphan the agents and break the
cask, to change a string nobody sees.

## v0.11.0 — 2026-08-16

**Readability, and one thing users read.**

No behaviour changes except the last item.

`main.go` was 808 lines doing eight unrelated jobs. It is now four files: the
process and its window, the menu bar, the two things launchd runs, and applying
settings to a running application. Nothing moved between packages and no
signature changed.

`findings()` was 145 lines of prose wrapped around twelve conditions. The seven
findings that say the same thing every time are values now; the five that name
something specific are built by small functions. What is left is the twelve
conditions, read in order.

The warning about what a configured Time Machine destination does to local
snapshots was worded twice — once in the services and once in the command line,
which cannot import them. It lives beside `DestinationInfo` now, which every
caller invokes immediately before deciding whether to say it at all. It had
already drifted: one copy said `backupd`, the other said Time Machine.

The window's five copies of "set busy, clear the error, await, report, catch"
became one hook. They had drifted too — some cleared the previous error on entry
and some did not, so a stale "authorization was cancelled" could sit underneath a
snapshot that had just been taken.

**The one user-visible change:** the header written into the settings file said
the application "reads this file on the next refresh". Since watching arrived
that is both truer and less precise, so it now names the two exceptions — a
snapshot already mounted stays where it is, and an installed scheduled task keeps
writing to the log named in the copy launchd holds.

## v0.10.0 — 2026-08-16

**Change a setting and it takes effect. No relaunch.**

The settings file was read at startup and then held. Editing the theme worked
immediately, because the window asks for it on every read; editing the window
size, either refresh interval, or any of the paths did nothing until the
application was launched again — which is a poor answer when nothing about the
change requires it.

The file is now watched, and a change is applied to the running application:

- the window resizes;
- the menu bar picks up its new interval and redraws at once, rather than after
  one more wait at the old one;
- the window's own refresh interval is re-read as it ticks;
- the mount root and log paths take effect for work that has not started yet.

Two limits, which are real rather than laziness and are written into the code
next to the parts they apply to. A snapshot already mounted stays where it is,
because a mounted filesystem cannot be moved by editing a file. An installed
launchd agent keeps writing to the log its plist names until the agent is
installed again, because the plist carries its own copy of that path.

Nothing in the watcher installs or removes an agent. Restoring what was asked for
happens at startup; saving a file is not the same as asking for snapshots to
start being taken.

The directory is watched rather than the file, because saving replaces the file
by renaming a new one over it and a watch on the old path would be left pointing
at an inode nothing writes to again. Writes are debounced, so one edit is one
reload rather than one per event, and a file that will not parse is not
delivered at all — what is already in force stays in force rather than the
application reacting to half a line of YAML.

## v0.9.0 — 2026-08-16

**The finding icons come from a designed set instead of being drawn by hand.**

v0.7.0 and v0.8.0 drew nine icons out of circles, arcs and line primitives, with
an antialiaser underneath and tests asserting no two came out identical. It was
a lot of machinery to produce worse icons than are freely available, and the
evidence was there early: the first attempt at the tripwire icon read as a
sunset and had to be replaced with a cross.

They are now [Lucide](https://lucide.dev) (ISC), which has around two thousand of
them, drawn by people who do this. The window uses `lucide-react`; the menu bar
uses the same icons rendered to PNG by `build/icons/findings.sh`, because macOS
wants image bytes for a menu item rather than SVG.

That deleted the drawing code, its antialiaser, and the tests that existed only
to check hand-drawn shapes were distinguishable — 340 lines out, 125 in, most of
the remainder being tests that now check the icons load, are the right size, and
are not blank.

The generated PNGs are committed rather than built, so the release pipeline needs
neither node nor librsvg. Two tests keep the pieces honest: one fails if the
window and the menu bar know different sets of kinds, the other if a kind exists
with no line in the generator.

The coverage strip is still drawn here, and stays that way: it is a picture of
this machine's snapshot history, which no icon set can contain.

## v0.8.0 — 2026-08-15

**The window draws findings the way the menu bar does.**

v0.7.0 gave every finding a kind — what it is about, as opposed to how bad it is
— and drew nine shapes for it in the menu bar. The window kept styling findings
by level alone, so the Health panel still had the problem the menu bar no longer
has: three warnings that look identical and tell you only that there are three.

Each finding now carries its shape. A cross for something absent, a clock for a
timer, a clock crossed through for a schedule that exists and cannot run, bars
thinning to the right, two rings overlapping, a gauge near full, a dashed outline
for invented state.

They are the same nine shapes, drawn twice: the menu bar needs PNG bytes and the
window needs SVG, so no code is shared. What is shared is the list of kinds, and
each side has a test that reads the other's list and fails when they drift — so
a kind added to the service cannot quietly render as a blank in one of the two.

The cross stays red in both, whatever the level's colour is. It marks something
absent, which is the one state worth breaking a palette for.

## v0.7.0 — 2026-08-15

**A schedule that cannot run now says so, and the tripwire is on by default.**

### The quietest failure

A launchd job whose program has been deleted stays loaded. launchd reports it as
running, fails to start it once an interval, and says nothing — so everything
reading the plist went on describing a schedule that was installed and working
while no snapshot was being taken. Restoring what was configured fixed the case
where the plist is *missing*; this is the case where the plist is *there* and
points at nothing.

The schedule now reads back the program its plist names and checks it exists. If
it does not, that is a finding at the worst level, naming the path, with the
button that repairs it by pointing the schedule at this copy.

### The tripwire is on for new installations

It is the half of the protection that catches what people actually lose files to
— something deleting in bulk right now — and it costs nothing until it fires. A
settings file that already exists keeps whatever it says, so this reaches new
installations only; everyone else is told by a finding, with a button.

The schedule is deliberately not treated the same way. It takes snapshots on a
timer, which is a thing to opt into.

### Smaller things

- A build not stamped by the release pipeline marks itself `DEV` in the menu bar.
  Two copies put two identical icons there, and the one in `/Applications` is the
  one holding the Full Disk Access grant — so knowing which is which matters more
  than it sounds.
- `config set` checks meaning as well as type. `appearance.theme purple` is
  refused, and the error names the three that work.
- The finding glyphs are drawn smaller. A menu row is mostly text, and an icon
  that fills its box competes with the words instead of labelling them.

## v0.6.0 — 2026-08-15

**Whatever was configured is put back on launch.**

A launchd job is not as durable as it looks. Upgrading through Homebrew unloads
both agents before staging the new version, so an upgrade silently removed the
schedule — while the settings file still recorded the interval that had been
chosen, and the window still offered to show it. Configured-looking and not
working is the exact failure this application exists to prevent, and it was
doing it to itself.

The settings file is now treated as the intent and launchd as the current state,
and the two are reconciled at startup: anything the settings say was asked for
and launchd no longer has is reinstalled, with the interval, retention and policy
it was installed with. A notification says so, because a repair nobody is told
about is indistinguishable from nothing having been wrong.

It only ever adds. A schedule that was deliberately uninstalled records that, and
is not put back — an application that argues with its user once per launch is
worse than one that forgets.

`schedule.enabled` is new, and is what separates "6 hours was asked for" from "6
hours is the default". **A settings file written before this release does not have
it**, so the first launch after upgrading restores nothing; installing the
schedule once records the intent, and every launch after that is covered.

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
