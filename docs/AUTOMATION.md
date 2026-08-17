# Driving the application by code

Two things stood between this application and being checkable without a person
looking at it.

The interface could not be **driven**: accessibility presses do not reach inside a
WKWebView, so nothing can press a button in the real window — which is why the
Search panel shipped visually unverified.

And its state could not be **chosen**: everything on screen came from whatever
this Mac happened to be doing. Several of the states worth looking at cannot be
produced on demand at all. "No snapshots" means deleting every restore point you
have. "Time Machine is thinning these" means attaching a backup disk. "The
schedule is three days overdue" means waiting three days.

Server mode fixes the first. Scenarios fix the second. They are independent and
they compose.

---

## Server mode

Wails builds headless with a build tag: the same services, the same frontend, the
same bindings, served over HTTP with no native window.

```sh
task server                              # localhost:8080
task server SERVER_ADDR=localhost:9100
```

The task builds the frontend, builds with `-tags server`, and runs the result.
Then `http://localhost:8080/` is the real interface in a browser, drivable by
anything that can drive a browser.

`SNAPSHOTTER_SERVER_ADDR` is the knob, `host:port`, default `localhost:8080`. An
address with no host (`:9100`) is read as localhost rather than as every
interface — the bindings are the whole application, so anything that can reach
the port can take and delete this machine's snapshots without a password, and
that is not a capability to acquire by accident. Bind `0.0.0.0` only on purpose.

Wails reads `WAILS_SERVER_HOST` and `WAILS_SERVER_PORT` *after* the values above
and they win, which is worth remembering if the port is not the one you asked
for.

### Calling a binding directly

`/health` answers `{"status":"ok"}` once the server is up, which is the thing to
poll before doing anything else.

Bindings are called by numeric ID over `POST /wails/runtime`. The IDs live in
`frontend/bindings/`, one file per service — `$Call.ByID(877487856)` in
`frontend/bindings/snapshotter/services/statusservice.ts` is `StatusService.Check`:

```sh
curl -s -X POST http://localhost:8080/wails/runtime \
  -H 'Content-Type: application/json' \
  -H 'x-wails-client-id: probe' \
  -d '{"object":0,"method":0,"args":{"call-id":"1","methodID":877487856,"args":[]}}'
```

The response is the service's return value as JSON — the whole `Health` struct in
that case. Method arguments go in the inner `args` array, in order:
`ScheduleService.Install(6, 14)` is `"args":[6,14]`.

That is enough to assert on a screen's *content* without a browser at all, which
is the cheaper half of the problem and worth doing first.

### What server mode does not give you

The menu bar, the window title and any native dialog have no equivalent. Mounting
still calls the real `mount_apfs` and still raises a real authorization prompt, so
combine it with the fake mounts below rather than expecting Browse to work.

---

## Scenarios

Everything in this application that shells out goes through `apfs.Runner`, and
`cmd/snapshotter/main.go` is the only place that chooses which one. Replacing it replaces the whole
machine at once — the snapshot listing, what diskutil knows about each snapshot,
whether Time Machine has a destination, what launchd has loaded — because those
are all commands and they all go through the same seam.

```sh
SNAPSHOTTER_SCENARIO=overdue task server
SNAPSHOTTER_SCENARIO=empty ./bin/snapshotter          # the window
SNAPSHOTTER_SCENARIO=healthy ./bin/snapshotter list   # the command line
task server SCENARIO=conflict
```

### The built-ins

| Name | What it puts on screen |
| --- | --- |
| `empty` | No snapshots, no schedule, no tripwire. An entirely unprotected machine. |
| `healthy` | Ten snapshots six hours apart, schedule and tripwire installed and loaded. No findings at all. |
| `overdue` | The same schedule, running, and the newest snapshot three days old. Configured and not working. |
| `time-machine` | Everything well, plus a backup destination — so the retention the settings screen shows will not hold. |
| `conflict` | Everything well, plus `com.christhomas.apfs-snapshot` installed beside us. |

`healthy` deliberately produces an empty findings list, which makes it the one to
check a "nothing to say" panel against.

### What a scenario controls

- **Snapshots**, each at a stated age, and per snapshot whether macOS may purge it
  and whether diskutil blames it for the container's minimum size.
- **Time Machine**, whether a destination is configured.
- **The interval schedule**, whether it is installed, whether launchd has loaded
  it, and with what interval and retention.
- **The tripwire**, installed and loaded independently of the schedule, because
  they fail independently.
- **A competing agent**, by launchd label.

Everything is stated as an age or an interval rather than as a date, so a scenario
written today still means the same thing next year.

### What a scenario does not control

**Disk figures.** `services/status.go` reads the volume's size and free space with
`syscall.Statfs`, which is not behind a Runner, so those numbers are always this
machine's. The consequence is that the "free space is low" finding cannot be
reached from a scenario — see [What is still missing](#what-is-still-missing).

**Mounting.** A scenario's snapshots do not exist, so `mount_apfs` cannot attach
them. Browse, Compare and Search need the fake mounts below.

### How a scenario announces itself

It has to be impossible to mistake a scenario for the machine, because this
application exists in the first place because somebody believed they were
protected and was not.

- A banner goes to the log before anything else runs, stating everything the
  scenario asserts and how to switch it off. Diagnosing a surprising screen starts
  by reading it back.
- The window title becomes `Snapshotter — SCENARIO <name> (invented state, not
  this Mac)`.
- The menu bar label is prefixed `SIM`, and the first item in its menu says which
  scenario is running.
- The fake executes nothing. `tmutil`, `diskutil` and `launchctl` are never run,
  and a command the fake does not model fails loudly rather than answering
  emptily — an empty answer would read as "no snapshots" and look like a finding
  about the machine instead of a gap in the fake.

### The sandbox

Two parts of the state are files rather than command output: an installed agent is
a plist in `~/Library/LaunchAgents`, and a competing agent is another plist beside
it. So a scenario also gets a sandbox directory, and the agent, log and mount
directories all move into it for the run.

```
$TMPDIR/snapshotter-scenario/<name>-<pid>/
```

The path is logged at startup. It is named after the process because a scenario is
per-process state — `snapshotter list` in one terminal must not reset the schedule
a window in another has just installed — and it is emptied on every start, because
a scenario that inherited the last run's state would stop being the state that was
written down.

The plists in it are written by the real installer and read back by the real
parser, which is the point: a scenario cannot claim a schedule the application
would not see. Pressing *Install schedule* in a scenario writes a real plist to a
harmless place and the interface updates exactly as it would for real. Nothing is
ever written to the real `~/Library/LaunchAgents`, which would outlive the run and
start taking real snapshots on a real timer.

Sandboxes are left behind for inspection. Removing them all is one command:

```sh
rm -rf "$TMPDIR/snapshotter-scenario"
```

### Scenarios are drivable, not fixtures

The fake holds state. Taking a snapshot from the menu bar adds one to the listing;
installing the schedule makes `launchctl print` report it loaded; the scheduled
task's prune really deletes. So `empty` can be driven all the way to a healthy
machine through the same buttons a user would press, and each step's effect
checked.

### Writing your own

`SNAPSHOTTER_SCENARIO_FILE` takes a JSON file for the states no built-in covers.
Setting both variables is refused rather than resolved by precedence, because a
stale variable in a shell would otherwise look like a broken file.

This is the state the built-ins deliberately leave out — a plist launchd has on
disk and has not loaded, which takes no snapshots while looking configured:

```json
{
  "name": "stalled",
  "summary": "the schedule is installed and launchd has not loaded it",
  "snapshots": [
    {"age": "90m"},
    {"age": "8h"},
    {"age": "3d", "purgeable": false, "limitsContainer": true}
  ],
  "timeMachine": false,
  "schedule": {
    "installed": true,
    "loaded": false,
    "interval": "6h",
    "retention": "14d"
  },
  "tripwire": {"installed": true, "loaded": true},
  "competingAgent": "com.example.rival"
}
```

```sh
SNAPSHOTTER_SCENARIO_FILE=scenarios/stalled.json task server
```

Notes on the format:

- Times are written as `30s`, `90m`, `6h`, `3d`, `2w`. Days and weeks are the units
  a retention window is discussed in and `time.ParseDuration` has neither, so they
  are added. A unitless number is refused.
- `purgeable` defaults to true, because every Time Machine local snapshot on a
  real machine is one. Say `false` only where that is the point.
- `limitsContainer` may be set on at most one snapshot, because diskutil names at
  most one and the interface reports it as the snapshot worth deleting first.
- An unknown key is an error, not a shrug. A misspelt field that silently did
  nothing would be the worst failure this format could have: the scenario would
  run, look plausible, and not be the one that was written.
- `name` is lower-case words joined by hyphens. It becomes a path component of the
  sandbox, which is removed by that path.
- `competingAgent` may not be one of this application's own labels. The conflict
  scan skips those on purpose — the tripwire genuinely does take snapshots, and
  reporting it as a rival would send the user to disable the thing protecting
  them — so naming one would claim a conflict nothing can ever see.

---

## Fake mounts

This mode predates the other two and is documented in the README; it is repeated
here because it is the third of three and the combination matters.

```sh
SNAPSHOTTER_FAKE_MOUNTS=1 SNAPSHOTTER_FAKE_SEED=~/Documents task server
```

Mounting needs root and Full Disk Access and is refused by TCC on this machine.
Nothing in `vfs`, `diffs` or `restore` knows how the tree under a mountpoint got
there, so `mountmgr.Fake` populates a directory instead: every snapshot is
"opened" by cloning the seed directory (`SNAPSHOTTER_FAKE_SEED`, default `$HOME`)
and injecting one of each difference the interface renders, so Browse, Compare and
Search have every state to show.

The stand-in is sealed read-only once populated, because a real mount is read-only.
It announces itself in the interface, refuses to remove any directory lacking its
marker, and refuses *Replace* restores outright — fake contents overwriting a real
file would destroy real work to demonstrate a feature.

---

## Combining them

All three are independent, and the useful combination for working on a screen that
reads inside a snapshot is all three at once:

```sh
SNAPSHOTTER_SCENARIO=healthy \
SNAPSHOTTER_FAKE_MOUNTS=1 \
SNAPSHOTTER_FAKE_SEED=~/Documents \
task server
```

The scenario decides which snapshots exist and what the Health panel says about
them; the fake decides what is inside them. Without the fake, a scenario's
snapshots cannot be opened at all, because they do not exist for `mount_apfs` to
attach.

One practical warning: every fake mount clones the seed directory, and every
snapshot in the scenario is opened at startup. `healthy` has ten snapshots, so
seeding from `$HOME` clones it ten times. The clones are copy-on-write and cost
almost no space until something writes to them, but the walk is not free — point
`SNAPSHOTTER_FAKE_SEED` at something small.

---

## What is still missing

Three seams would finish this, and all of them are in code a scenario cannot reach
from `cmd/snapshotter/main.go`.

**1. `services.Deps` should carry the scenario.** `Deps` already has `Faking` and
`FakeSeed` for the fake mounts, and the scenario needs the same treatment:

```go
// Scenario names the written-down machine state the Runner is answering from,
// empty when the Runner is the real one. Everything the interface reports about
// snapshots, the schedule and Time Machine was invented while it is set.
Scenario string
```

`cmd/snapshotter/main.go` sets it from `sim.Spec.Name`. `StatusService.Check` copies it into
`Health` beside `Faking`, and `findings` adds one at `LevelWarn` saying so — the
same shape as the existing "Mounts are simulated" finding, which is the precedent
to follow. Then the banner appears in the interface as well as in the log, and the
menu bar gets it from `Health` rather than from a parameter threaded through
`installTray`.

**2. The frontend needs a banner for it.** The fake-mount banner is the model.
Wording that matches what the log says: *"Scenario `<name>` — nothing on this
screen describes this Mac. Snapshots, schedule and Time Machine state are all
invented."* It should sit above the tab bar rather than inside a panel, because it
is true of every tab.

**3. The disk figures need a seam.** `services/status.go` reads them directly:

```go
var stat syscall.Statfs_t
if err := syscall.Statfs(s.Volume, &stat); err == nil {
```

Adding one field to `Deps` would make them injectable and let a scenario reach the
low-free-space finding, which is currently unreachable:

```go
// Space reports a volume's total and available bytes. It is a field so a
// scenario can state them; the default reads syscall.Statfs.
Space func(volume string) (total, free uint64, err error)
```

`Check` calls it when non-nil and falls back to `Statfs` otherwise, so nothing
existing changes. The scenario spec then grows `"volumeBytes"` and `"freeBytes"`,
and `<10%` free becomes a state that can be put on screen rather than one that has
to be imagined. This one is worth doing: "retention is not guaranteed because the
disk is nearly full" is precisely the sort of claim that should be looked at before
a user sees it.

### Driving it from a headless browser

The page holds a WebSocket open for the whole time it is loaded, so a headless
browser never reaches "load complete" and anything waiting on that — notably
`--screenshot` — hangs rather than failing. It reads exactly like the server not
responding, and it is not:

```sh
chromium --headless --virtual-time-budget=4000 \
  --screenshot=/tmp/shot.png --window-size=1180,820 http://localhost:8080
```

`--virtual-time-budget` is what makes it terminate. Without it this route looks
like a dead end.

### If a packaged app launches with no window

Rebuild the frontend from clean and package again:

```sh
rm -rf frontend/dist && wails3 task package
```

`build:frontend` used to have `dist/**` in both its `sources` and its
`generates`, so its up-to-date decision depended on its own previous output. That
is fixed, but a `.task/checksum` entry written before the fix can still be stale.
The failure is silent — the process is alive, the tray installs, and nothing is
logged — so it is worth trying this before believing anything deeper.
