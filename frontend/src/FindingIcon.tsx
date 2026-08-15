/**
 * The picture beside a finding, chosen by what the finding is about.
 *
 * The menu bar draws the same eight shapes in Go, because macOS wants PNG bytes
 * for a menu item. These are the web half of that vocabulary: same shapes, so a
 * cross means the same thing in the window as it does in the menu, and someone
 * who learns one has learned the other.
 *
 * They are not the same code and cannot be — one produces pixels, the other
 * produces SVG — so the thing holding them together is the list of kinds, which
 * comes from the Go service and is checked by a test in both places.
 *
 * Colour comes from `currentColor`, so the level's colour is applied by the CSS
 * that already tints the card rather than being decided here.
 */

/** The kinds the status service assigns. Keep in step with services.Kind*. */
export const findingKinds = [
  "snapshots",
  "schedule",
  "overdue",
  "tripwire",
  "thinning",
  "conflict",
  "space",
  "simulated",
  "stale",
] as const;

export type FindingKind = (typeof findingKinds)[number];

/**
 * A cross always reads red, whatever the card's level colour is. It marks
 * something absent, which is the one state worth breaking the palette for — the
 * same exception the menu bar makes.
 */
const crossRed = "var(--deleted)";

export function FindingIcon({ kind }: { kind: string }) {
  return (
    <svg
      className="finding-icon"
      viewBox="0 0 32 32"
      width="18"
      height="18"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="round"
      aria-hidden="true"
      focusable="false"
    >
      {shape(kind)}
    </svg>
  );
}

function shape(kind: string) {
  switch (kind) {
    case "snapshots":
      // A restore point: the thing there are none of.
      return <circle cx="16" cy="16" r="6" fill="currentColor" stroke="none" />;

    case "schedule":
      return (
        <>
          <circle cx="16" cy="16" r="9" />
          <path d="M16 16 V9 M16 16 L20 18" />
        </>
      );

    case "overdue":
      // The same clock with its hands past the hour, so "late" is a shape.
      return (
        <>
          <circle cx="16" cy="16" r="9" />
          <path d="M16 16 V23 M16 16 L22 14" />
        </>
      );

    case "tripwire":
      // Absent. Always red, and smaller than the rest: it is the strongest
      // shape here and at full size it shouts over rows that are not
      // emergencies.
      return <path d="M11 11 L21 21 M21 11 L11 21" stroke={crossRed} strokeWidth="2.6" />;

    case "stale":
      // A clock with a cross through it: present and broken, which is a
      // different thing from missing.
      return (
        <>
          <circle cx="16" cy="16" r="9" />
          <path d="M10 10 L22 22" stroke={crossRed} strokeWidth="2.6" />
        </>
      );

    case "thinning":
      // Bars getting shorter: history being thinned out.
      return (
        <path
          d="M9.5 10 V24 M15.5 14 V24 M21.5 18 V24"
          strokeWidth="3"
          strokeLinecap="butt"
        />
      );

    case "conflict":
      // Two things overlapping, which is exactly the complaint.
      return (
        <>
          <circle cx="13" cy="16" r="6" />
          <circle cx="19" cy="16" r="6" />
        </>
      );

    case "space":
      // A gauge close to full.
      return (
        <>
          <circle cx="16" cy="16" r="9" />
          <path d="M16 10 A6 6 0 0 1 16 22 Z" fill="currentColor" stroke="none" />
        </>
      );

    case "simulated":
      // Present, but not real.
      return <circle cx="16" cy="16" r="9" strokeDasharray="3.5 3.5" />;

    default:
      // A kind this build has not heard of still gets something: findings are
      // added in the service, and an empty space reads as a rendering fault.
      return <circle cx="16" cy="16" r="5" fill="currentColor" stroke="none" />;
  }
}
