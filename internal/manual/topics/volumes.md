title: Volumes
summary: why every mounted APFS disk gets snapshotted, and what that costs
aliases: volume, disks, sdcard, external

# Volumes

`tmutil localsnapshot` takes no arguments at all — not a volume, not a flag — so
it snapshots **every eligible mounted APFS volume at once**. Every neighbouring
command takes a mount point:

```
tmutil listlocalsnapshots <mount_point>
tmutil thinlocalsnapshots <mount_point>
tmutil deletelocalsnapshots [<mount_point> | <snapshot_date>]
tmutil localsnapshot                        # no argument, no choice
```

Creation is the one verb Apple did not make selectable. So an external disk is
snapshotted whenever the startup disk is, whether or not anyone asked for that.

## What this application does about it

It lists and prunes **every** volume that holds local snapshots, not just the
startup disk. Doing otherwise is how a disk fills with snapshots nothing will
ever delete: they are purgeable, so macOS reclaims them per volume under space
pressure, and a date it drops from a full startup disk survives on every other
volume — where nothing was looking.

The Health screen reports each volume's own free space and its own pinning
snapshot, because a container is per volume and neither figure is knowable from
another disk's numbers.

## Deleting one copy

The same date exists on every volume that was mounted when it was taken, and
each copy is a separate thing. Deleting by date removes all of them, which is
right for a retention policy — the verdict on a date is the same everywhere —
and wrong for a button beside one row.

So the window deletes by volume and identifier, which is the only call that can
tell two copies apart. Retention still works by date.

## Keeping a disk out of it

`tmutil addexclusion -v <volume>` is the only lever, and it is the system's
rather than this application's.
