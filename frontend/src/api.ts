// A single place where the generated bindings are named, so the views import
// from here rather than reaching into the binding tree.

import * as Snapshots from "../bindings/snapshotter/services/snapshotservice.js";
import * as Browse from "../bindings/snapshotter/services/browseservice.js";
import * as Diff from "../bindings/snapshotter/services/diffservice.js";
import * as Restore from "../bindings/snapshotter/services/restoreservice.js";
import * as Schedule from "../bindings/snapshotter/services/scheduleservice.js";
import * as Status from "../bindings/snapshotter/services/statusservice.js";
import * as Search from "../bindings/snapshotter/services/searchservice.js";
import * as Config from "../bindings/snapshotter/services/configservice.js";

export { Snapshots, Browse, Diff, Restore, Schedule, Status, Search, Config };

export type { SnapshotView, Overview, MergedListing, ScheduleView, Presence, CompareRequest, RestoreRequest, Health, Finding, SearchResult, ConfigView, Warning } from "../bindings/snapshotter/services/models.js";
export type { Change, Result as DiffResult } from "../bindings/snapshotter/internal/diffs/models.js";
export type { Result as RestoreResult } from "../bindings/snapshotter/internal/restore/models.js";

/**
 * Asks a log call for as much as the service chooses to give.
 *
 * The size lives in Go, next to the code that reads the file, so the screens
 * cannot end up showing different amounts of the same log — which is what
 * happened when each named a size of its own.
 */
export const serviceChosenTail = 0;

/**
 * Turns whatever a rejected binding call produced into a sentence.
 *
 * Errors arriving from Go are the user's main feedback channel here — an
 * authorization prompt that was dismissed, a snapshot that is not mounted, a
 * folder no snapshot covers — so they are shown rather than logged.
 */
export function message(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return String(err);
}
