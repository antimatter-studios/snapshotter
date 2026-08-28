title: Snapshots
summary: what a local snapshot is, and why one can vanish before its time
aliases: snapshot, purgeable, local-snapshots

# Snapshots

An APFS local snapshot is a record of the whole volume at one instant, kept on
the same disk as the data. It costs almost nothing when it is taken: nothing is
copied, and the snapshot only starts occupying space as the live disk moves away
from it.

macOS only schedules these when Time Machine has a backup destination
configured. Without a backup disk nothing takes them, and nothing browses them
either — which is the gap this application exists to fill.

## They are not a backup

They live on the same disk as your data. They protect against deleting a file,
not against the disk failing. A snapshot cannot be recreated once removed,
because it records a state of the disk that has passed.

## Why one can disappear

Every local snapshot is **purgeable**: macOS may reclaim it, without asking, when
the disk runs short. `snapshotter list` marks them.

That makes a retention window an upper bound rather than a promise. Setting
"keep 14 days" does not reserve anything; it says which snapshots this
application will delete, and macOS remains free to delete more. Freeing disk
space is what actually keeps them.

Time Machine having a destination has the same effect for a different reason:
its backup cycle thins local snapshots to roughly 24 hours, so any longer
retention shown elsewhere is a promise the system breaks.

## Why there is no size per snapshot

There cannot be one. A snapshot shares blocks with the live volume and with its
neighbours, so "how big is this snapshot" has no single answer — delete it and
you may reclaim nothing, because the next one still references the same blocks.

What is reported instead is honest: which snapshots are purgeable, and which one
is **pinning the container** — holding it at its current size. That last one is
the snapshot whose deletion actually returns space, and it is usually the
oldest.
