#!/usr/bin/env bash
# guard: github-merge-squash-only
# Ensure the GitHub repo allows only squash merges (no merge commits, no rebase),
# so PRs always land linear. Owner-only, fail-open — NEVER blocks the push.
#
# A push hook rather than a commit hook, because this touches GitHub and a commit
# does not. Reconciling on every commit spent an API round-trip to change nothing,
# warned on every offline commit, and still could not help: the setting only
# matters once a pull request exists, which is after a push. Push is both the
# last moment it can be checked and the first moment it means anything.
set -u
dir=$(cd "$(dirname "$0")/.." && pwd)   # .githooks/
# shellcheck source=../lib/common.sh
. "$dir/lib/common.sh"

slug=$(gg_repo_slug); [ -n "$slug" ] || exit 0
gg_have_gh || { echo "github-guard: gh not installed/authed — skipping merge-settings check for $slug" >&2; exit 0; }
owner=${slug%%/*}
gg_user_owns "$owner" || exit 0

# Reconcile the FULL desired state — SQUASH ONLY — whenever any of the three
# differ. Reading only allow_merge_commit missed the common case of a repo that
# already has merge commits off but still disallows squash (e.g. rebase-only): the
# old check saw merge_commit=false and skipped, leaving squash disabled so
# `gh pr merge --squash` failed.
#
# Rebase is off, not merely unused. This guard used to ENFORCE rebase=true, so a
# repo set to squash-only was silently switched back on the next push. One push
# per pull request is the point: a branch's work-in-progress history is noise once
# it lands, and the squash message is written deliberately rather than composed by
# GitHub from whatever the repository happened to say.
set -- $(gh api "repos/$slug" \
  --jq '"\(.allow_merge_commit) \(.allow_squash_merge) \(.allow_rebase_merge)"' 2>/dev/null)
mc=${1:-} sq=${2:-} rb=${3:-}
# Only act on a well-formed response: each field must be a literal boolean. A
# missing field makes jq emit "null" (non-empty), and a failed/edge read yields
# empty — neither must be mistaken for "needs reconciling" and trigger a PATCH.
for v in "$mc" "$sq" "$rb"; do
  case "$v" in
    true|false) ;;
    *) echo "github-guard: unexpected merge-settings response for $slug — skipping" >&2; exit 0 ;;
  esac
done

if [ "$mc" != "false" ] || [ "$sq" != "true" ] || [ "$rb" != "false" ]; then
  echo "github-guard: $slug merge settings (merge=$mc squash=$sq rebase=$rb) — reconciling to squash only…" >&2
  if gh api -X PATCH "repos/$slug" \
       -F allow_merge_commit=false -F allow_squash_merge=true -F allow_rebase_merge=false \
       >/dev/null 2>&1; then
    echo "github-guard: $slug merge settings fixed ✓" >&2
  else
    echo "github-guard: PATCH failed for $slug (need repo admin?) — not blocking" >&2
  fi
fi
exit 0
