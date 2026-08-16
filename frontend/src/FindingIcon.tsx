import {
  CalendarX,
  Camera,
  ChartColumnDecreasing,
  Clock,
  ClockAlert,
  Copy,
  FlaskConical,
  Gauge,
  X,
  type LucideIcon,
} from "lucide-react";

/**
 * The icon beside a finding, chosen by what the finding is about.
 *
 * These are Lucide (lucide.dev, ISC), the same pack the menu bar uses — it takes
 * the same icons rendered to PNG, because macOS wants image bytes for a menu
 * item and a web view wants components. Nothing can be shared across that line
 * except the list of kinds, and each side has a test that reads the other's.
 *
 * An earlier version of this file drew nine shapes by hand. They were worse than
 * the pack's and cost more to keep.
 */

/** Kind -> icon. Mirrors the map in build/icons/findings.sh. */
const icons: Record<string, LucideIcon> = {
  snapshots: Camera,
  schedule: Clock,
  overdue: ClockAlert,
  // A cross rather than a picture of a tripwire: the finding says something is
  // absent, and an absence is what a cross means. Drawing the thing itself
  // produced something that read as a sunset.
  tripwire: X,
  stale: CalendarX,
  thinning: ChartColumnDecreasing,
  conflict: Copy,
  space: Gauge,
  simulated: FlaskConical,
};

/** The kinds the window can draw. Keep in step with services.Kind*. */
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

export function FindingIcon({ kind }: { kind: string }) {
  // A kind this build has not heard of still gets an icon: findings are added in
  // the service, and a blank where the icon should be reads as a rendering fault
  // rather than as a new kind of finding.
  const Icon = icons[kind] ?? Camera;

  // The cross is red at every level, which is the one place the level's colour
  // is overridden. It marks something absent, and that is worth breaking a
  // palette for. Everything else inherits the colour the card already sets.
  const style = kind === "tripwire" ? { color: "var(--deleted)" } : undefined;

  return <Icon className="finding-icon" size={18} aria-hidden="true" style={style} />;
}
