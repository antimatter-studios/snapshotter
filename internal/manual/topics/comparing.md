title: Comparing a snapshot with the disk
summary: why proving a folder unchanged is slow, and what change_detection.ignore skips
aliases: change-detection, ignore, node_modules, slow

# Comparing a snapshot with the disk

Every folder in the browser carries a verdict: changed, identical, or not
checked. Reaching it means comparing the folder inside the snapshot with the
folder on the live disk — and the two possible answers cost wildly different
amounts.

A folder that HAS changed is usually answered in milliseconds. The walk stops at
the first difference it finds, because the row shows one word and that word is
"changed" whether one file differs or ten thousand.

A folder that has NOT changed has to be read to the end. There is no early exit
from proving a negative: the only way to know nothing differs is to have looked
at everything. Measured on a tree of 192,635 files, the first difference was
found in 11ms and the full proof took 456ms.

On an SD card or an external disk, where each read is far slower than on the
internal one, that asymmetry is what makes a folder full of dependencies feel
like the application has locked up.

## The ignore list

    snapshotter config get change_detection.ignore
    snapshotter config set change_detection.ignore "node_modules,.git,*.tmp"

Anything matching this list is not read when a folder is compared. A folder that
is itself on the list is answered instantly, without a single directory being
opened.

The saving is not marginal. In one real project directory, 17,239 of 19,788
entries were node modules — about nine seconds of an SD card's reading, per
project, to confirm something nobody would ever restore.

The window has the same list under Options, with the usual names offered as one
click each.

## What the patterns mean

A bare name matches that name as a whole path component, at any depth:

    node_modules      every node_modules directory anywhere
    .git              every .git directory anywhere

It matches the directory and everything under it. It does NOT match a longer
name that merely contains it, so `node_modules` leaves `node_modules_backup`
alone.

Wildcards are the shell's:

    *.tmp             anything ending .tmp
    build-*           build-2024, build-old

A pattern containing a slash is matched against the whole path instead, so it
can name one place rather than every directory of that name:

    /Users/me/work/*/dist

## What you give up

A folder that was skipped is a folder this application will not tell you about.
If something inside `node_modules` is deleted, nothing on screen will say so,
and the folder above it will still read as identical.

That is usually the point. It is worth being deliberate about anyway, which is
why the list ships empty rather than helpfully pre-filled: a build directory is
cheap to rebuild right up until the toolchain that made it is gone, and `.git`
is the whole history of a project rather than a cache.

Where something was skipped, the folder's row says so — hover it, and the
tooltip reads "unchanged, apart from N paths not looked inside". "Identical" on
its own always means everything was read.

## Not the same list as the tripwire's

`tripwire.ignore` is a different setting answering a different question:
deletions in those paths do not count towards a burst. `change_detection.ignore`
is about what gets read when a folder is compared.

They will usually name the same directories, and that is exactly why they are
kept apart — otherwise changing what you are warned about would silently change
what gets walked, and the other way round.
