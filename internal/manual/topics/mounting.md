title: Mounting
summary: why opening a snapshot asks for a password, and why browsing does not
aliases: mount, password, authorization, full-disk-access, fda

# Mounting

Creating, listing and deleting snapshots need **no privileges**. `tmutil` is a
client of `backupd`, which does the privileged work. The schedule and the
bulk-deletion watcher need nothing either.

Mounting is the one step that does, and it is the only prompt this application
raises. `mount(2)` attaches a filesystem to the global namespace, which is
privileged regardless of who owns the files inside — an unprivileged mount could
use `-o noowners` to read every user's home directory. Snapshots are mounted
read-only, so browsing cannot alter what was recorded.

One prompt covers a whole batch. Opening everything on a machine with two disks
is two prompts, because they are two mounts.

## Full Disk Access is separate, and also required

Authorization gets the mount attempted; TCC decides whether it is allowed. macOS
attributes that decision to **the process making the call**, which is why the
application elevates *itself* rather than `/sbin/mount_apfs`: a stock Apple
binary carries none of this application's identity, so no amount of granting
Full Disk Access could reach it.

    System Settings → Privacy & Security → Full Disk Access → add Snapshotter

Without the grant, every mount is refused with "Operation not permitted".

## Why it only works from the installed application

TCC keys an ad-hoc signed bundle on a hash of the build, so every rebuild looks
like a different application and silently voids the grant. Under `go run`, or
from a bare binary, mounting is refused — correctly. The command line is
symlinked to the bundle's own executable rather than copied for the same reason:
a second copy would need its own grant.

## Mounts are left attached on quit

Detaching needs root, and a password prompt on the way out is a poor trade for
tidying something read-only. A mounted snapshot cannot be deleted, so leftover
mounts block pruning — *Close all* is in the sidebar.
