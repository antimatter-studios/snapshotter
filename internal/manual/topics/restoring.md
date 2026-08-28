title: Restoring
summary: where a restored file lands, and what is never deleted
aliases: restore, recover, recovery

# Restoring

A snapshot must be open before anything can be read out of it — see
`snapshotter help mounting`. Once it is, restoring copies a file or a folder
from the snapshot back to the live filesystem.

## Nothing is ever deleted

By default the restored copy lands **beside** the original rather than on it, so
the file you have now and the file you had then are both in front of you and you
choose.

*Replace* puts the restored copy at the original path — and moves whatever is
there to a `.bak-` copy first. There is no path through this application that
destroys a live file.

A folder restore writes into a temporary directory beside the target and swaps
it in, so an interrupted restore leaves the original intact rather than a
half-written tree.

## Where a file goes back to

To the volume it came from. A snapshot's contents are rooted wherever its volume
is: a file at `/Volumes/sdcard256gb/projects/notes.md` inside an SD card's
snapshot restores to that same path on the SD card, not to a lookalike path on
the startup disk.

## Browsing another volume

Snapshots on any volume can be opened, listed and deleted. What the window
browses is the startup disk, because the browser is rooted at a home directory —
another volume's snapshot opens and tells you where it is mounted, and you can
read it from there with any tool.

## The one thing that cannot be undone

Deleting a snapshot. It records a state of the disk that has passed and nothing
recreates it, which is why the window asks twice and refuses while the snapshot
is open.
