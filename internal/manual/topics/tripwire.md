title: Bulk-deletion watcher
summary: what the tripwire can and cannot do, and what it watches
aliases: bulk-deletion, watcher, watch, fsevents

# The bulk-deletion watcher

It watches the directories you name and takes a snapshot as soon as something
starts deleting in bulk in one of them. It runs from its own LaunchAgent, so it
keeps watching while the window is closed — which is most of the time, and all
of the time that matters. A deletion at 3am is exactly the one nobody is
watching.

## What it cannot do

**It cannot prevent a deletion.** FSEvents reports what has already happened, so
by the time a removal is seen that file is gone.

What it prevents is a deletion running to completion unwitnessed. A cleanup
script working through ten thousand files takes seconds to minutes; tripping at
the two-hundredth saves the rest. An `rm -rf` of one small directory is over
before anything can react, and this will not help.

Preventing a deletion would mean interposing on the filesystem — Endpoint
Security AUTH events — which needs an Apple-granted entitlement and root. Out of
scope, and worth saying plainly rather than implying otherwise.

## It watches what you name, and nothing else

It used to watch the entire home directory, with an ignore list to quiet the
parts that are not anybody's work. That is the wrong way round. `~/Library`
deletes in bulk as a matter of routine — caches, container state, mail indexes —
so the wire tripped on deletions nobody had asked about, and every trip pinned
another whole-volume snapshot on the disk. The ignore list needed to stop that is
a list of everything the machine does, written after each surprise, never
finished.

    snapshotter config set tripwire.watch "~/projects,~/Documents"

What is not on the list is not watched. Off by default, with nothing on the list,
because only the person using the machine knows which directories hold work that
could not be reproduced.

## How readily it trips

Deletions are counted **per watched directory**: two hundred gone from
`~/projects` trips it, a hundred there and a hundred in `~/Documents` does not.
A single running total made the threshold easier to reach the more directories
were watched, which is the opposite of what adding one should do.

The cooldown after a snapshot is shared across all of them, because an APFS
snapshot is of the whole volume — the one taken for `~/projects` already covers
`~/Documents`.

    cautious       500 deletions in 5 seconds
    balanced       200        the default
    sensitive       75        a folder of documents going
    very-sensitive  25        work that could not be reproduced
