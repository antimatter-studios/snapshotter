#!/usr/bin/env bash
# guard: go-vet
# Runs `go vet ./...` and blocks the commit on a finding. Go modules only.
#
# Fails OPEN when the module cannot be built for a reason that is not the code:
# frontend/embed.go embeds frontend/dist, so a fresh clone has nothing to embed until the
# frontend is built once. Blocking there would make a new checkout unable to
# commit anything, which is a worse failure than a missed vet — and CI, which
# builds the frontend first, is the backstop.
set -u
root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
[ -f "$root/go.mod" ] || exit 0
command -v go >/dev/null 2>&1 || exit 0

# Nothing staged that go vet could have an opinion about.
git diff --cached --name-only --diff-filter=ACM -- '*.go' 'go.mod' 'go.sum' | grep -q . || exit 0

if [ -d "$root/frontend" ] && [ ! -f "$root/frontend/dist/index.html" ]; then
  echo "github-guard: go-vet skipped — frontend/dist is not built yet, so the embed" >&2
  echo "             directive in frontend/embed.go cannot resolve. Run 'wails3 task build' once." >&2
  exit 0
fi

# Linker version warnings are noise on a machine newer than the build target and
# say nothing about the code, so they are filtered out of the report.
out=$(cd "$root" && go vet ./... 2>&1)
status=$?
out=$(printf '%s\n' "$out" | grep -vE "^ld: warning|was built for newer")

if [ $status -ne 0 ]; then
  echo "github-guard: go vet failed — commit blocked." >&2
  printf '%s\n' "$out" >&2
  exit 1
fi
exit 0
