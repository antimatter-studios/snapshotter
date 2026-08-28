import { Check, EyeOff, Pencil, Plus, Shuffle, Trash2, X, type LucideIcon } from "lucide-react";

/**
 * The mark beside a row's verdict.
 *
 * These are Lucide (lucide.dev, ISC), the same pack the findings and the menu bar
 * use.
 *
 * Bare glyphs rather than the circled variants. The first version of this used
 * CircleCheck and CircleX at 12px, chosen so the settled marks would echo the
 * spinner's ring — but a tick inside a ring at that size is about five pixels of
 * tick, and it read as a dot. A motif nobody can resolve is not a motif. The
 * spinner keeps its ring because it is the only one whose shape is carried by
 * movement rather than detail.
 *
 * Colour comes from the badge through currentColor, so each mark matches the word
 * beside it without a second palette. The two exceptions are set in the
 * stylesheet: "identical" and "could not check" have deliberately muted text, and
 * their marks carry the colour instead.
 */

/** Status -> icon. Every status the browser can show must appear here. */
const icons: Record<string, LucideIcon> = {
  same: Check,
  modified: Pencil,
  // "Deleted since": present in the snapshot and gone from the target.
  onlyInSnapshot: Trash2,
  // "New since": on the target and absent from the snapshot.
  onlyOnDisk: Plus,
  typeChanged: Shuffle,
  // The walk finished without an answer. A cross rather than a warning triangle:
  // this is a failure to read, not a finding about the file.
  notExamined: X,
  // Not looked inside, on purpose. An eye with a line through it rather than a
  // cross: nothing failed here, somebody said they did not want to be told.
  ignored: EyeOff,
};

/**
 * The statuses a row can carry, mirroring diffs.Status in Go plus the two states
 * that exist only while a folder is being resolved.
 *
 * A status missing from `icons` renders nothing, which reads as a rendering fault
 * rather than as a state — so the test beside this file asserts the two lists
 * agree.
 */
export const rowStatuses = [
  "same",
  "modified",
  "onlyInSnapshot",
  "onlyOnDisk",
  "typeChanged",
  "notExamined",
  "ignored",
  // Drawn as the spinner instead, which is why it has no entry above.
  "detecting",
] as const;

export const iconStatuses = Object.keys(icons);

export function StatusIcon({ status }: { status: string }) {
  const Icon = icons[status];
  if (!Icon) return null;

  // 13px with a heavier stroke: these sit beside 11px text and have to survive
  // being small far more than they have to be delicate.
  return <Icon className="badge-mark" size={13} strokeWidth={2.5} aria-hidden="true" />;
}
