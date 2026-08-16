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
export function age(when: string | Date): string {
  const then = new Date(when).getTime();
  const minutes = Math.max(0, Math.round((Date.now() - then) / 60000));
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} hr ago`;
  const days = Math.round(hours / 24);
  return days === 1 ? "yesterday" : `${days} days ago`;
}

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
export function breadcrumbs(path: string): { label: string; path: string }[] {
  const parts = path.split("/").filter(Boolean);
  const crumbs = [{ label: "/", path: "/" }];
  let current = "";
  for (const part of parts) {
    current += `/${part}`;
    crumbs.push({ label: part, path: current });
  }
  return crumbs;
}

/** The label shown on a status badge. */
export const statusLabel: Record<string, string> = {
  same: "unchanged",
  modified: "changed",
  onlyInSnapshot: "deleted since",
  onlyOnDisk: "new since",
  typeChanged: "type changed",
  // A folder whose verdict has not arrived yet, and one the walk gave up on.
  // Both read the same because the distinction is ours, not the reader's: either
  // way the answer is not here.
  detecting: "detecting…",
  notExamined: "detecting…",
};
