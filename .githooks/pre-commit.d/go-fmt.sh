#!/usr/bin/env bash
# guard: go-fmt
# Auto-format staged Go code with gofmt and re-stage it, so the commit goes in
# formatted. Only runs in a Go module (skips silently otherwise). NEVER blocks —
# it just fixes layout, so there is nothing to argue about.
#
# Safety (the important bit): gofmt -w rewrites a file's FULL on-disk content,
# so blindly `git add`-ing a formatted file would also stage any UNSTAGED edits
# sitting in that same file — silently sweeping work-in-progress into the
# commit. So only files that were *fully* staged are re-staged. A partially
# staged file is left alone (its staged snapshot commits unformatted) with a
# notice, never a silent sweep.
set -u
root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
[ -f "$root/go.mod" ] || exit 0
command -v gofmt >/dev/null 2>&1 || exit 0

staged=$(git diff --cached --name-only --diff-filter=ACM -- '*.go')
[ -n "$staged" ] || exit 0
unstaged=$(git diff --name-only --diff-filter=ACMD -- '*.go')

printf '%s\n' "$staged" | while IFS= read -r f; do
  [ -n "$f" ] || continue
  [ -f "$root/$f" ] || continue

  if printf '%s\n' "$unstaged" | grep -qxF -- "$f"; then
    # Only complain when formatting would actually change something.
    if [ -n "$(gofmt -l -- "$root/$f" 2>/dev/null)" ]; then
      echo "github-guard: go-fmt left '$f' unformatted in this commit — it has unstaged" >&2
      echo "             changes, and re-staging after format would mix them in. Stage it" >&2
      echo "             fully (git add '$f'), or run 'gofmt -w' + 'git add' yourself." >&2
    fi
    continue
  fi

  before=$(gofmt -l -- "$root/$f" 2>/dev/null)
  [ -n "$before" ] || continue
  gofmt -w -- "$root/$f" 2>/dev/null && git add -- "$root/$f" 2>/dev/null || true
done
exit 0
