# How a folder is judged changed

Open [`change-detection.html`](change-detection.html) beside this page — it is the
same story as a diagram, and it is generated from
[`change-detection.archify.json`](change-detection.archify.json) rather than drawn
by hand, so it can be corrected when the code moves.

Browsing a snapshot asks one question over and over: **does this folder differ
from the snapshot?** Answering it honestly costs a walk of everything beneath it.
Answering it constantly, for every row of every listing, is what this whole
arrangement exists to avoid.

## The tiers, cheapest first

`directoryStatus` in [`services/browse.go`](../services/browse.go) tries each in
turn and stops at the first that can answer.

| | What it costs | Where |
| --- | --- | --- |
| 1. Ignore list | a string comparison | `changeIgnore().Match` |
| 2. Verdict cache | a map lookup | `Verdicts.Get` |
| 3. Changed-path index | a map lookup | `Verdicts.ChangedPathUnder` |
| 4. `change_detection` | one indexed SQL row | `changedb.Store.Under` |
| 5. Re-check one file | one `stat` | `b.confirms` |
| 6. Walk the tree | reads every directory beneath | `diffs.Explain` |

The ignore list goes first deliberately. It reads nothing, so looking it up in a
cache could not be cheaper than answering it outright.

## Why one file can settle it

A walk stops at the first difference it finds. So a "changed" verdict always
rests on a **single file**, and that path is kept with the verdict.

That turns the expensive direction into the cheap one. If the remembered file
still differs, the verdict still holds — not only for that folder but for every
folder between it and the file, because one difference anywhere means the whole
tree above it differs. One `stat` replaces a tree.

It only ever answers *changed*. A file that matches again says nothing about the
rest of the tree, so the record is dropped and the walk happens as it would have.

## Two tiers for the same question

Tiers 3 and 4 answer "is a difference known anywhere under this folder", once
from memory and once from disk.

Both are indexed by parent directory. `change_detection` has always been: a
generated `parent` column with an index on `(snapshot, parent)`. The in-memory
half scanned every recorded difference instead, until it was given the same
shape — see the benchmarks in
[`internal/verdict`](../internal/verdict/touched_bench_test.go).

The in-memory index is a **hint**; the changed set is the truth. A hint is
checked against that set when it is used, and a stale one is dropped and
answered as a miss. A miss costs a walk, which is what would have happened
without any index at all — it is never wrong.

## What makes caching safe at all

A snapshot is read-only. Nothing inside it can change, so **only the live disk
can invalidate an answer**, and the filesystem will say when it does.

That is the entire justification, and it has two consequences worth stating:

- **Recorded differences persist between runs; recorded sameness never does.**
  A difference is re-checked every time it is used, so keeping it costs a `stat`
  at worst. A remembered *sameness* would be a claim about everything that
  happened while the application was not running, which nothing can keep.
- **A gap in watching forgets every verdict.** Same reasoning, shorter timescale:
  a verdict is trusted without being re-checked, which is only safe if the
  filesystem was watched continuously between the answer and its use.

## The watch runs only while somebody is browsing

The watch is recursive over the home directory and every volume holding
snapshots, and asks for every kind of event. It is the most expensive thing this
application does in the background.

It used to start when the window opened and run with a context nothing ever
cancelled. An application sitting in the menu bar therefore watched every write
on the machine for as long as it ran. Measured on the machine that found it:
**22.5 hours of CPU across two days**, while disk images were being built and
nobody was browsing anything.

So `verdict.Watching` starts it the first time a verdict is wanted and stops it
once nothing has asked for two minutes. It says so both ways, because whether it
is running was otherwise knowable only by counting threads from outside:

```
watching the filesystem for changes: somebody is browsing
no longer watching the filesystem: nothing has asked for a verdict in 2m0s
```

Measured against the same 20 seconds of filesystem churn, before and after:

| | CPU used |
| --- | --- |
| before, idle | 30.8s |
| before, browsing | 31.3s |
| after, browsing | 0.72s |
| after, idle | **0.00s** |

## The three passes of a listing

[`frontend/src/Browser.tsx`](../frontend/src/Browser.tsx) asks in three rounds,
and the order matters:

1. **`KnownDirectoryStatus`** per row — tiers 1 to 5, and never the walk. A
   listing whose folders are all already known can say so immediately.
2. **`ScanEventLog`** — replays the volume's own FSEvents history to seed known
   differences.
3. **`DirectoryStatus`** per row — the walk, for whatever is still unanswered.

The replay used to run first. A listing whose folders were all already known
still waited on it before it could say anything.

## Regenerating the diagram

```sh
node ~/.claude/skills/archify/bin/archify.mjs deliver workflow \
  docs/change-detection.archify.json docs/change-detection.html --quality showcase
```
