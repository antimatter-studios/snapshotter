# Roadmap

Everything proposed so far, in the order it can actually be built.

The ordering was by what could be **verified** on this machine rather than by what
was worth most, because mounting did not work and a feature built on an unverified
foundation is a guess wearing a test suite.

Both halves of that have since loosened. Mounting is fixed — the cause was that
`/sbin/mount_apfs` was being elevated rather than this binary, so no Full Disk
Access grant could reach it (see `DECISIONS.md`) — and what stands in the way now is
a grant, not a bug. Separately, the stand-in for a mount is sealed read-only, which
was the only respect in which a populated directory differed from a real one, so
directory-walking logic is genuinely covered either way.

Three gates decide when something can be worked on:

| Gate | Meaning |
| --- | --- |
| **free** | needs no root, no mounting, no permission. Buildable and verifiable now. |
| **mock** | reads snapshot contents. `mountmgr.Fake` is sealed read-only exactly like a real mount, so the logic is genuinely covered; what is unproven is a real mount end to end, which needs the grant. |
| **blocked** | needs a privilege or a decision that only Chris can give. Design only. |

---

## Done

- **Health panel** — the "am I protected right now" verdict, findings, and the
  numbers behind them.
- **Menu bar item** — the same verdict where it is visible without the window.
- **Snapshot space** — purgeable count and which snapshot pins the container.
  There is no per-snapshot byte figure and there cannot be one.
- **Mount refusal diagnosis** — a TCC refusal is named as such and offered the
  settings pane, instead of leaking a raw `mount_apfs` string.
- **Fake mounts** — `mountmgr.Fake` clones a seed, injects all three differences,
  and is sealed read-only like a real mount, so browse, compare and search are
  testable without root.
- **Search** — by name, across every open snapshot, plus a deleted-since view.
- **Bulk-deletion tripwire** — `internal/watch`, with its own LaunchAgent.
- **Notifications** — when a scheduled run fails or the tripwire trips.
- **Command line** — `list`, `status`, `take`, `run`.
- **Application icon and menu bar glyph**, the latter now carrying the health level.
- **Tiered retention** — `Plan` is pure, and the scheduled run prunes by the policy
  its plist carries.
- **Snapshot-to-snapshot compare**, with direction taken from the snapshots' own
  timestamps.
- **Server mode and scenarios** — the interface is drivable headlessly, and any
  screen can be put into any state.
- **github-guard** — squash-only merges, protected default branch, `gofmt`/`go vet`
  on commit, `go test` on push.

---

## Free — no permission needed, verifiable now

### 1. Findings that can be acted on — ✅ built
Every finding now carries its fix: install the schedule, take a snapshot, show the
failing task's log inline. A finding you cannot act on from where you read it is
just anxiety. Smallest change on this list, largest visible effect.

### 2. Notifications when protection lapses — ✅ built
A schedule that dies silently is worse than no schedule, because you believe you
are covered — which is precisely the state in which a deletion becomes
unrecoverable.
`internal/notify`. Fires when a scheduled run fails and when the tripwire trips.
Still to do: notify when snapshots are purged under space pressure — the overdue
condition is computed in `findings` but nothing pushes it.

### 3. Risk-triggered snapshots — ✅ built, with its claim corrected
`internal/watch`. Two hundred removals inside five seconds trips a snapshot, on
a ten-minute cooldown. Verified against real FSEvents.

**It watches the directories you name, and nothing else.** It used to watch the
whole home directory with an ignore list to quiet the rest, which is the wrong way
round: `~/Library` deletes in bulk as a matter of routine, so most of what it
caught was housekeeping and every catch pinned another whole-volume snapshot on
the disk. The ignore list needed to stop that is a list of everything the machine
does, written after each surprise, never finished.

The count is per watched directory and the cooldown is shared. Per directory
because a single running total made the threshold easier to reach the more
directories were watched; shared because an APFS snapshot is of the whole volume,
so the one taken for `~/projects` already covers `~/Documents`.

**It cannot snapshot "before the damage", and the original pitch saying so was
wrong.** FSEvents reports what has already happened; by the time a removal is
observed that file is gone. It is a tripwire, not an interlock: it stops a
deletion running to completion unwitnessed. Trip at the two-hundredth file of
ten thousand and the rest are still recoverable. An `rm -rf` of one small folder
is over before anything can react.

Preventing a deletion would need Endpoint Security AUTH events — an
Apple-granted entitlement and root. Out of scope.

It runs from its own LaunchAgent (`schedule.Tripwire`), with `KeepAlive` rather
than `StartInterval` since it runs continuously, so it keeps watching with the
window closed. Installing it is one button in Health — refused while `tripwire.watch`
is empty, because an installed watcher watching nothing reports itself as running
and protects nothing.

### 4. Tiered retention — ✅ built
`internal/schedule/retention.go`. Each bucket keeps its **oldest** member on
absolute boundaries, which is what makes the kept set stable — keeping the newest
would delete yesterday's keeper today merely because a newer snapshot joined its
bucket. The newest snapshot is kept whatever a policy says, and an empty policy
keeps everything rather than reading as "delete the lot". The plist carries the
policy and the scheduled run prunes by it. Default stays flat, because a retention
default that changes silently deletes something somebody expected to find.

Flat 6h/14d is 57 snapshots covering a fortnight; the same count reaches thirteen
weeks, or a year in 41.

### 5. A real CLI — ✅ built
`list`, `status`, `take`, `run`. `restore` is deliberately absent: it needs a
mounted snapshot, so it waits on item 15. A verb selects the command line and a
bare invocation opens the window; the launchd agents keep `--take-snapshot` and
`--watch`, because installed plists name those. Opens the door to a Homebrew
**formula** rather than only a cask.

### 6. Facts grid leaves dead cells — ✅ fixed
Fixed twice. Pinning the column count worked until a seventh fact was added and
re-created the gap, so separators are now drawn on the cells instead of letting a
gap reveal the container colour — which is count-independent, and facts will keep
being added.

---

## Mock — build against the fake, verify when mounting works

### 7. Search across snapshots, and a deleted-since view — ✅ built
`internal/find` plus `SearchService`, and a Search tab. Case-insensitive on the
name, shallowest first, bounded and honest about the bound. It names the snapshots
it could not search, so an absence never reads as proof. `DeletedSince` filters a
comparison to what the snapshot held and the disk no longer does.

Verified by a table of phrases someone would really type, run through the same
service call the panel makes against a known tree — including that `known_hosts`
does *not* match "rsa" despite containing it, so a name search never silently
becomes a grep.

The panel itself is visually unverified: accessibility presses do not reach into
the WKWebView, so the tab cannot be switched programmatically.

### 8. Snapshot-to-snapshot compare — ✅ built
Which of the two is older is decided by their own timestamps rather than by the
order they were picked in: a change between two snapshots has no inherent
direction, and getting it backwards does not fail, it inverts every row. The
direction assertions were mutation-tested by swapping the arguments and confirming
the tests caught it.

### 9. `snapshotter run -- <cmd>` with rollback
Snapshot, run, and on a non-zero exit put the touched paths back.
Commit-or-rollback for the filesystem. The direct descendant of the incident
that created this project: instead of hoping a restore point existed, the risky
thing makes its own.

### 10. Churn map
APFS will not give bytes per snapshot. It *will* let two snapshots be diffed and
the changed bytes totalled per directory — which is what people mean when they
ask what is eating the disk. Render as a treemap over a time range.

### 11. Filesystem bisect
`git bisect` for the disk. Give it a test command; it binary-searches the
snapshots — mount, clone the tree out with `cp -c`, run, narrow — and lands on
the snapshot where it broke, then shows the diff across that boundary. Nothing
else does this.

### 12. Scratch branches
Clone any folder out of any snapshot into a throwaway workspace, instantly, at
no disk cost. Filesystem branching on COW clones.

### 13. Preview before restore
Quick Look for binaries, a real side-by-side diff for text. Whole-folder restore
and restore-to-arbitrary-location belong here too.

### 14. Activity feed
A chronological stream from consecutive snapshot diffs: *"14:00–20:00, 340 files
changed in ~/Projects, 12 deleted in ~/Downloads"*. `git log` for the whole
machine, retroactively, for free. The idea most likely to make someone want the
app rather than merely approve of it.

---

## Blocked — needs Chris

### 15. `mount_apfs` refused by TCC — ✅ diagnosed and fixed, not yet proven
The cause was neither of the two explanations recorded overnight. TCC judges the
identity of the binary being **elevated**, and we were elevating `/sbin/mount_apfs`
— Apple's binary, carrying none of ours — so no grant on this application could
ever reach it. `Manager` now elevates this binary as its own helper and calls
`mount_apfs` from there.

Settled by Chris's `diskcutter`, which does privileged raw-device I/O through the
same `osascript` route and works here. The `SMAppService` recommendation is
withdrawn; it was a much larger change than the problem needed. Full write-up in
`DECISIONS.md`.

**What remains is not engineering.** A real mount needs Full Disk Access granted to
the packaged bundle — and re-granted after every rebuild while the signature is
ad-hoc, because TCC keys the grant to a cdhash that changes each time. Until that
happens, everything in the Mock section stays exercised against `mountmgr.Fake`
only.

### 16. Stable code signing
Ad-hoc signatures change cdhash every build, so any Full Disk Access grant dies
on the next `wails3 task package`. This stopped being a distribution nicety and
became a prerequisite for the app working at all.

### 17. Exclusion volumes — *corrected scope*
An APFS volume in the same container shares free space, takes `-reserve` and
`-quota`, and mounts at any path. Confirmed.

**But `tmutil localsnapshot` takes no volume argument and `diskutil apfs` has no
create verb.** There is no user-space way to snapshot an arbitrary volume, so
this cannot give a folder "its own schedule" — only remove it from coverage.

That is still worth having: moving a folder of large rewritten files off the
data volume stops each snapshot pinning another generation of them. It must be
labelled as *exclusion*, never as protection. Open question 4 in `DECISIONS.md`
made the wrong claim and has been corrected.

Needs root for `addVolume`, a boot-time mount, and a real cross-volume copy —
COW clones do not cross volumes.

### 18. The name
`Snapshotter` collides with the containerd/Kubernetes term of art. Homebrew is
clear. Undecided, and cheap to change until the schedule is installed anywhere.

---

## Order of work

Free first, in the order above, because it is the only work that can be
finished rather than merely written. Then the mock items, biggest first
(search, then snapshot-to-snapshot compare, then `run`), since each of those
gives the fake more to exercise and so makes the eventual real-mount
verification cover more ground.
