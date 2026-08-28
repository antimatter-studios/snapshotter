import type { TFunction } from "i18next";
// Formatting helpers shared by the views.

/** Renders a byte count in the units a person reads sizes in. */
export function bytes(n: number): string {
  if (!n) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = n;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value < 10 && unit > 0 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}

/**
 * Renders a snapshot's age. Snapshots are chosen by "how far back do I need to
 * go", so the elapsed time matters more than the calendar date.
 */
export function age(when: string | Date, t: TFunction): string {
  const then = new Date(when).getTime();
  const minutes = Math.max(0, Math.round((Date.now() - then) / 60000));
  if (minutes < 1) return t("age.justNow");
  if (minutes < minutesPerHour) return t("age.minutes", { count: minutes });
  const hours = Math.round(minutes / minutesPerHour);
  if (hours < hoursPerDay) return t("age.hours", { count: hours });
  const days = Math.round(hours / hoursPerDay);
  // Named rather than counted, because "1 day ago" is a thing nobody says.
  return days === 1 ? t("age.yesterday") : t("age.days", { count: days });
}

const minutesPerHour = 60;
const hoursPerDay = 24;

/** Renders a timestamp as a short local date and time. */
export function stamp(when: string | Date): string {
  const date = new Date(when);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString(undefined, {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Splits a path into its clickable segments. */
export function breadcrumbs(path: string, root = "/"): { label: string; path: string }[] {
  // Never above the volume's own root.
  //
  // This used to start at "/" whatever was being browsed, so a snapshot of an SD
  // card read "/ › Volumes › sdcard256gb › projects" with the first two
  // clickable — and clicking either led to an error saying that volume's
  // snapshots do not cover them. A control that cannot work should not be drawn.
  const top = root === "" ? "/" : root.replace(/\/+$/, "") || "/";
  const label = top === "/" ? "/" : top.split("/").filter(Boolean).pop()!;
  const crumbs = [{ label, path: top }];

  // The part of the path below the root, which is all anyone can navigate.
  const rest = top === "/" ? path : path.startsWith(top + "/") ? path.slice(top.length) : "";
  let current = top === "/" ? "" : top;
  for (const part of rest.split("/").filter(Boolean)) {
    current += `/${part}`;
    crumbs.push({ label: part, path: current });
  }
  return crumbs;
}

/** The label shown on a status badge. */
export const statusLabel: Record<string, string> = {
  // "Identical" rather than "unchanged": what was actually compared is two
  // versions of a file, and the word says they match. "Unchanged" describes a
  // history nobody observed — the file may well have been edited twice since the
  // snapshot and put back.
  same: "identical",
  modified: "changed",
  onlyInSnapshot: "deleted since",
  onlyOnDisk: "new since",
  typeChanged: "type changed",
  // A folder whose verdict has not arrived yet.
  detecting: "detecting…",
  // The walk finished without an answer — either something inside could not be
  // read, or the folder was vast enough to hit the backstop. The label does not
  // guess which: it said "too large to check" for a while, which was confidently
  // wrong whenever the real reason was a folder it could not open. It must also
  // not read as "detecting…", which would leave a row looking like it is still
  // working when it has stopped for good.
  notExamined: "could not check",
  // Deliberately not looked inside, because the ignore list says so. "Not
  // checked" rather than "ignored": the second reads as a judgement about the
  // folder, and this is a statement about what this program did.
  ignored: "not checked",
};
