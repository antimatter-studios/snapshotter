# Changelog

All notable changes, per released version. The most recent releases are also
summarized in the README; the full history lives here.

## Unreleased

Nothing yet.

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
