#!/usr/bin/env bash
# guard: generated-normalise
# Strips trailing whitespace from staged *generated* files and re-stages them.
# NEVER blocks — it fixes machine output, so there is nothing to argue about.
#
# Named to sort before git-no-trailing-whitespace, which would otherwise block
# the commit for whitespace nobody typed: `wails3 generate bindings` emits
# trailing spaces in its doc comments, so every regeneration would need a
# --no-verify -- and that flag disables every other guard too, which is a far
# worse outcome than a stray space.
#
# Only paths that are unambiguously generated are touched. Hand-written code is
# left to the blocking guard, because there the whitespace IS worth a complaint.
set -u
root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0

generated='frontend/bindings/'

staged=$(git diff --cached --name-only --diff-filter=ACM -- "$generated")
[ -n "$staged" ] || exit 0
unstaged=$(git diff --name-only --diff-filter=ACMD -- "$generated")

printf '%s\n' "$staged" | while IFS= read -r f; do
  [ -n "$f" ] || continue
  [ -f "$root/$f" ] || continue

  # Same hazard as any format-then-restage guard: rewriting the file would also
  # stage unstaged edits sitting in it. Generated files should never be in that
  # state, so it is reported rather than silently swept.
  if printf '%s\n' "$unstaged" | grep -qxF -- "$f"; then
    echo "github-guard: generated-normalise skipped '$f' — it has unstaged changes" >&2
    continue
  fi

  # In place, portable across BSD and GNU sed via a temp file.
  tmp="$root/$f.gg-tmp"
  if sed -e 's/[[:space:]]*$//' -- "$root/$f" > "$tmp" 2>/dev/null; then
    if ! cmp -s -- "$root/$f" "$tmp"; then
      mv -- "$tmp" "$root/$f" && git add -- "$root/$f"
    else
      rm -f -- "$tmp"
    fi
  else
    rm -f -- "$tmp"
  fi
done
exit 0
