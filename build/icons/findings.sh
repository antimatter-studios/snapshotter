#!/usr/bin/env bash
# Renders the menu bar's finding icons from Lucide.
#
# The menu bar cannot use SVG: macOS wants image bytes for a menu item, and the
# window wants components. So the same icons arrive twice — lucide-react in the
# window, these PNGs in the menu bar — from one source, which is the pack.
#
# They are written straight into the package that embeds them rather than into a
# shared assets directory: go:embed cannot reach outside its own package, and a
# second copy elsewhere is a second copy to drift.
#
# The output is committed rather than generated at build time. It changes only
# when this script does, the release pipeline then needs neither node nor
# librsvg, and the committed files are reviewable in a way a build step is not.
#
#   ./build/icons/findings.sh          regenerate internal/menubar/icons/
#
# Requires: npm i (for lucide-static) and `brew install librsvg`.
set -euo pipefail

cd "$(dirname "$0")/../.."

src="frontend/node_modules/lucide-static/icons"
out="internal/menubar/icons"

if [ ! -d "$src" ]; then
  echo "lucide-static is not installed; run: npm --prefix frontend install" >&2
  exit 1
fi
if ! command -v rsvg-convert >/dev/null; then
  echo "rsvg-convert is not installed; run: brew install librsvg" >&2
  exit 1
fi

# Kind -> icon. The left column is what services/status.go assigns; the right is
# the icon that says it. Anything added there needs a line here, and the Go test
# fails until it gets one.
#
# A cross for the tripwire rather than a picture of one: what the finding says is
# that something is absent, and an absence is what a cross means. Drawing the
# thing itself produced something that read as a sunset.
kinds="
snapshots:camera
schedule:clock
overdue:clock-alert
tripwire:x
stale:calendar-x
thinning:chart-column-decreasing
conflict:copy
space:gauge
simulated:flask-conical
"

# The three level colours, matching the system colours macOS uses for the same
# meanings. The cross is rendered in the bad colour at every level, because it
# marks something absent regardless of how bad that is.
declare -A colour=( [ok]="#34c759" [warn]="#ff9f0a" [bad]="#ff3b30" )

# 32px is 16 points at 2x, which is what a menu item's image gets on a Retina
# display. Lucide draws on a 24x24 grid with a 2px stroke; at this size that
# lands at roughly the weight of the surrounding text.
size=32

rm -rf "$out"
mkdir -p "$out"

count=0
for pair in $kinds; do
  kind="${pair%%:*}"
  icon="${pair##*:}"
  svg="$src/$icon.svg"
  [ -f "$svg" ] || { echo "no such icon: $icon" >&2; exit 1; }

  for level in ok warn bad; do
    fill="${colour[$level]}"
    # The cross is always red: see above.
    [ "$kind" = "tripwire" ] && fill="${colour[bad]}"

    # Lucide strokes with currentColor, which means nothing to a rasteriser, so
    # it is substituted for the level's colour before rendering.
    sed "s|currentColor|$fill|g" "$svg" |
      rsvg-convert --width "$size" --height "$size" --format png \
        --output "$out/$kind-$level.png"
    count=$((count + 1))
  done
done

echo "rendered $count icons into $out"
