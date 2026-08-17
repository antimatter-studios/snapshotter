# Duplicate code paths — 2026-08-17

**Scope:** whole codebase, Go and the React frontend, looking specifically for
duplicated code paths and duplicated functionality.

**Found 6, fixed 4, one partly fixed, one rejected as a false positive.**

The scan was prompted by two bugs found the same day, both of the same shape: the
launchd agents never called `i18n.SetLanguage` because they are a separate entry
path from the window, and `internal/cli` had its own duration formatter separate
from `services/status.go`, so only one of the two was translated. Both were found
by a person looking at output, not by a test.

---

## Changes made

### H1 — Process setup was duplicated across four entry paths

**Files:** [internal/boot/boot.go](../internal/boot/boot.go) (new),
[main.go](../main.go), [settings.go](../settings.go),
[internal/cli/cli.go](../internal/cli/cli.go)

Before, each entry path remembered part of what a process must do:

```go
// main.go, for the agents
if cfg, cerr := config.Load(); cerr == nil { i18n.SetLanguage(cfg.Language()) }
// main.go again, for the window
i18n.SetLanguage(cfg.Language())
// resolvePaths, for whoever happened to call it
trace.SetEnabled(cfg.Logging.Verbose)
// settings.go, for the watcher
trace.SetEnabled(cfg.Logging.Verbose)
i18n.SetLanguage(cfg.Language())
```

After, one function, three callers:

```go
// internal/boot
func Apply(cfg config.Config) {
	i18n.SetLanguage(cfg.Language())
	trace.SetEnabled(cfg.Logging.Verbose)
}
```

**Why it's better:** there is now an answer to "what must every process do before
it prints anything". Adding a fifth process-wide setting means editing one
function rather than finding four places, which is exactly what failed when a
language was added and two of the four were missed.

**It also fixed a live bug.** The command line branch returns before
`resolvePaths()`, which was the only caller of `trace.SetEnabled` outside the
watcher — so `logging.verbose: true` was silently ignored for every CLI command.
Nobody had noticed because the only symptom is the absence of output.

### H2 — `coverage()` existed three times

**Files:** [internal/i18n/span.go](../internal/i18n/span.go) (new),
[services/status.go](../services/status.go),
[internal/cli/cli.go](../internal/cli/cli.go),
[frontend/src/Health.tsx](../frontend/src/Health.tsx)

Three implementations of the same rule — 48 hours becomes days, one hour becomes
hours, below that "under an hour" — in two languages. Go's two are now one
`i18n.Span(hours float64)`. The TypeScript copy cannot call Go, so it keeps its
own arithmetic, but its two thresholds are named constants matching the Go ones
instead of bare numbers.

**Why it's better:** the reason this mattered is not tidiness. When the strings
were translated, two of the three implementations were found and the third kept
printing "4 days" inside a German sentence.

### H3 — The same sentence had three message keys

**Files:** [internal/i18n/locales/](../internal/i18n/locales/),
[frontend/src/locales/](../frontend/src/locales/)

`count.underAnHour`, `cli.underAnHour` and `health.underAnHour` were all "under an
hour"; `count.days`/`health.days` and `count.hours`/`health.hours` likewise. The Go
duplicate is deleted and the frontend's are renamed to `count.*`.

Two catalogues remain, because the frontend needs its text before the first paint
and the menu bar is drawn in Go — but the keys now correspond, so a correction to
a translation can be applied to both by name.

**The frontend's day and hour counts were also not pluralised.** They used one
form with an `{{n}}`, so i18next's plural machinery never saw them. They use
`{{count}}` with `_one`/`_other` forms now.

### M1 — An English-only pluraliser in a translated headline

**Files:** [services/status.go](../services/status.go)

```go
// before
return level, fmt.Sprintf("%s, %s of cover — %s to look at",
	snapshotCount(h.SnapshotCount), coverage(h.CoverageHours),
	text.Plural(actionable, "thing"))
// after
return level, i18n.N("status.headline.coveredWithFindings", actionable,
	"Snapshots", snapshotCount(h.SnapshotCount), "Cover", i18n.Span(h.CoverageHours))
```

**Why it's better:** the sentence is one message rather than three fragments, so a
language that orders it differently can. The fragment that remained was still
coming from a hand-written pluraliser, so a German headline ended "— 3 things to
look at".

### M2 — `age()` in the frontend was never translated at all

**Files:** [frontend/src/format.ts](../frontend/src/format.ts),
[frontend/src/App.tsx](../frontend/src/App.tsx)

Looking for the Go/TypeScript duplicate turned up something worse than
duplication: the frontend's version returned bare English — "just now",
"5 min ago", "yesterday" — and was shown against every snapshot in the sidebar. It
is translated now, with i18next plural forms.

`yesterday` is a named case rather than a count, because "1 day ago" is not
something anyone says.

---

## Items skipped

| item | reason |
|---|---|
| **M3** — 18 `config.Load()` call sites | *False positive.* Nine of the twelve remaining sites read the file in order to write it back, so they must see it as it is rather than as something remembered — using a cached copy to save from would overwrite a concurrent change. Of the three read-only sites, two have a reason to read fresh: `ConfigService.Get` reports parse errors to the window, and `config get` is usually run just after someone edited the file. That left one caller, in `installTray`, which runs once at startup. A cache with one startup-time user is indirection without a benefit, and it would add a stale-settings failure mode that does not exist today. Implemented, inspected, reverted. |

---

## Test results

| | before | after |
|---|---|---|
| Go packages passing | 21 | 21 |
| Frontend tests | 50 | 50 |
| `go vet` | clean | clean |
| Tests repointed | — | 2 (`coverage` → `i18n.Span`, both kept with their cases) |
| Tests added | — | 2 (`Span` wording, `Span` follows the language) |

Two existing tests asserted the same rule in two packages, which is the
duplication in test form; both now exercise the single implementation rather than
being deleted. One test needed a real translator passed in rather than calling
`age()` with one argument.

Two documented exceptions were added to the punctuation checks, in both
catalogues: German abbreviations — `Min.`, `Std.`, `T.` — carry a full stop that
English does not, and matching English would be wrong German rather than
consistent punctuation.
