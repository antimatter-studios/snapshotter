#!/usr/bin/env bash
# guard: go-test
# Runs `go test ./...` and blocks the push on a failure. Go modules only.
#
# On push rather than on commit: the suite touches the real filesystem (the
# bulk-deletion watcher's test drives a real FSEvents stream), so it costs
# seconds. Paid once per push it is invisible; paid on every commit it would
# train people into --no-verify, which disables every other guard too.
#
# Fails OPEN when the module cannot be built for an environmental reason — see
# go-vet for why.
set -u
root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
[ -f "$root/go.mod" ] || exit 0
command -v go >/dev/null 2>&1 || exit 0

if [ -d "$root/frontend" ] && [ ! -f "$root/frontend/dist/index.html" ]; then
  echo "github-guard: go-test skipped — frontend/dist is not built yet." >&2
  exit 0
fi

out=$(cd "$root" && go test ./... 2>&1)
status=$?
if [ $status -ne 0 ]; then
  echo "github-guard: go test failed — push blocked." >&2
  printf '%s\n' "$out" | grep -vE "^ld: warning|was built for newer" >&2
  exit 1
fi
exit 0
