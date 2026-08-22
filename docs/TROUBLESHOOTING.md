# Troubleshooting

Faults that have actually happened, what causes them, and how to fix them
quickly. Each entry is here because diagnosing it once took longer than it should
have.

## A white window saying "no `index.html` could be found in your Assets fs.FS"

**What you see.** The application launches. The menu bar icon appears and works,
the command line answers, snapshots keep being taken. Open the window and it is
blank white, with this text on it:

> no `index.html` could be found in your Assets fs.FS, please make sure the
> embedded directory 'dist' is correct and contains your assets

**What it means.** The binary was built without the window in it. `frontend/dist`
was missing `index.html` at the moment `go build` ran, so `//go:embed all:dist`
embedded the rest of the folder and not the page.

**Fix it now:**

```sh
npm --prefix frontend run build   # writes frontend/dist
go test ./frontend                # proves the window is in there
task darwin:package:universal     # or `go build ./cmd/snapshotter`
```

For an installed copy, reinstall the version that has the fix rather than
rebuilding in place — the bundle's signature seals its contents:

```sh
brew reinstall --cask snapshotter
```

### Why it was hard to find

Three things conspired, and each is worth knowing on its own.

**The error is a page, not a log line.** Wails' asset server returns it as the
body of an HTTP 500, so the webview paints it. Nothing is written to stderr, the
process does not exit non-zero, and no log file mentions it. Running the binary
and watching its output shows a clean start — which is what happened here, and it
produced a confident and wrong "the released app is fine".

**Every other gate passes.** A binary with no frontend compiles, vets, passes the
entire Go and TypeScript suite, signs, notarizes, staples, satisfies `spctl`,
reports the right version, and answers every command line question. There was no
test that opened a window, so nothing in the repository could tell the difference.

**Grepping the binary does not settle it.** `strings binary | grep dist/index.html`
matches in a broken build as well as a working one — the path appears in more than
one place. Two separate wrong conclusions came out of probing it that way. Do not
diagnose this by inspecting the binary; run the test.

### What now catches it

- `go test ./frontend` — asserts `index.html` is in the embedded filesystem, is
  not empty, has a script beside it, and that **the same asset handler the window
  uses serves it**. That last one is the test that reproduces the fault exactly:
  remove `frontend/dist/index.html` and it fails with the message above.
- The release workflow re-runs that test **after** the bundle build, which is the
  only point where the assets on disk are the ones just embedded — `vite build`
  empties `dist` before writing it, so checking earlier proves nothing about what
  went in.

### How a build ends up like this

`npm run build` is `tsc && vite build`, and `vite build` empties `dist` before
writing to it. So between those two moments the directory is incomplete, and
anything that compiles Go in that window embeds an incomplete window. Ways in:

- Building Go without building the frontend first, in a checkout where `dist`
  exists from an earlier build but has been emptied since. (A checkout with no
  `dist` at all fails to compile, which is the safe failure.)
- `tsc` failing, so `vite build` never runs, and a stale or empty `dist` is what
  gets embedded.
- Two builds running at once, one emptying `dist` while the other compiles.

`frontend/dist` is gitignored, being build output, so its state is never carried
by a commit and cannot be inferred from one.
